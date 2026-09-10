package web

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

const (
	housekeepingInterval = time.Hour
	maxNudgesPerRun      = 50
)

// Housekeeping runs the hourly jobs until ctx ends: expired sessions and
// tokens go, competitions move along by date, and due stash nudges are sent.
func (s *Server) Housekeeping(ctx context.Context) {
	ticker := time.NewTicker(housekeepingInterval)
	defer ticker.Stop()
	for {
		s.housekeep()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) housekeep() {
	if err := s.store.Sweep(); err != nil {
		log.Printf("web: %v", err)
	}
	if err := s.store.Advance(time.Now()); err != nil {
		log.Printf("web: %v", err)
	}
	s.startScheduledCompetitions(time.Now())
	s.sendNudges()
}

// sendNudges emails members whose reminder is due. A member is marked nudged
// before the attempt so a failing mailbox is not retried every hour.
func (s *Server) sendNudges() {
	if !s.mailConfigured() {
		return
	}
	users, err := s.store.UsersDueNudge(time.Now(), maxNudgesPerRun)
	if err != nil {
		log.Printf("web: nudges: %v", err)
		return
	}
	for _, user := range users {
		if err := s.store.MarkNudged(user.ID); err != nil {
			log.Printf("web: nudge %d: %v", user.ID, err)
			continue
		}
		items, err := s.store.StashForUser(user.ID)
		if err != nil {
			log.Printf("web: nudge %d: %v", user.ID, err)
			continue
		}
		summary, err := s.summaryFor(user, items)
		if err != nil || (summary.Waiting == 0 && user.GoalCount == 0) {
			continue
		}
		if err := s.sendMail(user.Email, nudgeSubject(summary), s.nudgeBody(user, summary)); err != nil {
			log.Printf("web: nudge %d: %v", user.ID, err)
		}
	}
}

func nudgeSubject(summary stashSummary) string {
	if summary.Waiting == 0 {
		return "Your stash is clear"
	}
	return fmt.Sprintf("Your stash: %d kit%s waiting", summary.Waiting, plural(summary.Waiting))
}

func (s *Server) nudgeBody(user store.User, summary stashSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Hello %s,\n\n", user.DisplayName)
	if summary.Waiting > 0 {
		fmt.Fprintf(&b, "%d kit%s in your stash are unbuilt, about %s%s in total.", summary.Waiting, plural(summary.Waiting), currency, money(summary.DebtPence))
		if summary.Oldest != "" {
			fmt.Fprintf(&b, " The oldest, %s, has waited %d days.", summary.Oldest, summary.OldestDays)
		}
		b.WriteString("\n\n")
		if summary.Next != nil {
			fmt.Fprintf(&b, "Next up: %s.\n\n", summary.Next.Title)
		} else {
			b.WriteString("No kit is marked as next. Pick one to start with.\n\n")
		}
	}
	if user.GoalCount > 0 {
		fmt.Fprintf(&b, "Goal: %d of %d finished", summary.GoalDone, user.GoalCount)
		if summary.GoalDaysLeft < 0 {
			fmt.Fprintf(&b, ", %d days past the date.\n\n", -summary.GoalDaysLeft)
		} else {
			fmt.Fprintf(&b, ", %d days left.\n\n", summary.GoalDaysLeft)
		}
	}
	fmt.Fprintf(&b, "Your stash: %s\nChange how often you get this: %s\n", s.absolute("/stash"), s.absolute("/settings"))
	return b.String()
}

// sendViaSMTP delivers one plain text message over SMTP: TLS from the start
// on port 465, STARTTLS on any other port.
func (s *Server) sendViaSMTP(to, subject, body string) error {
	from := s.config.SMTPFrom
	if from == "" {
		from = s.config.SMTPUser
	}
	message := fmt.Sprintf("From: sprue <%s>\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		from, to, subject, strings.ReplaceAll(body, "\n", "\r\n"))
	address := s.config.SMTPHost + ":" + s.config.SMTPPort
	tlsConfig := &tls.Config{ServerName: s.config.SMTPHost}
	dialer := &net.Dialer{Timeout: mailTimeout}
	var conn net.Conn
	var err error
	if s.config.SMTPPort == "465" {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", address)
	}
	if err != nil {
		return fmt.Errorf("smtp connect %s: %w", address, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(mailTimeout)); err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, s.config.SMTPHost)
	if err != nil {
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer client.Close()
	if s.config.SMTPPort != "465" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	auth := smtp.PlainAuth("", s.config.SMTPUser, s.config.SMTPPass, s.config.SMTPHost)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp to: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write([]byte(message)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return client.Quit()
}
