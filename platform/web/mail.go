package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"
)

// brevoEndpoint is Brevo's transactional send call. Tests point it at a
// local server.
var brevoEndpoint = "https://api.brevo.com/v3/smtp/email"

const mailTimeout = 15 * time.Second

// maxMailParagraphs bounds the body of any one email. The nudge is the
// longest message and uses well under this.
const maxMailParagraphs = 12

// mailMessage is one email described once and rendered twice: plain text
// for the fallback part and a small HTML page in the site's look. Intro
// comes before the button, Outro after it, Links close the panel.
type mailMessage struct {
	Subject string
	Intro   []string
	Action  string // button label, empty for no button
	Link    string // button target
	Outro   []string
	Links   []mailLink
	Site    string // site address shown in the footer
}

type mailLink struct {
	Label string
	URL   string
}

// emailTemplate is standalone: emails do not share the page shell because
// mail clients need inline styles and table layout.
var emailTemplate = template.Must(template.ParseFS(templateFiles, "templates/email.html"))

// mailConfigured reports whether the server can send email at all. Without
// it sign-in links are logged, which is local development only.
func (s *Server) mailConfigured() bool {
	return s.config.BrevoKey != "" || s.config.SMTPHost != ""
}

// text renders the message as plain paragraphs separated by blank lines.
func (m mailMessage) text() string {
	var b strings.Builder
	for _, line := range m.Intro {
		b.WriteString(line + "\n\n")
	}
	if m.Link != "" {
		b.WriteString(m.Link + "\n\n")
	}
	for _, line := range m.Outro {
		b.WriteString(line + "\n\n")
	}
	for _, link := range m.Links {
		b.WriteString(link.Label + ": " + link.URL + "\n")
	}
	return b.String()
}

// html renders the message through the email template. Every field passes
// through html/template so a display name cannot inject markup.
func (m mailMessage) html() (string, error) {
	if len(m.Intro)+len(m.Outro) > maxMailParagraphs {
		return "", fmt.Errorf("mail: %d paragraphs is over the cap of %d", len(m.Intro)+len(m.Outro), maxMailParagraphs)
	}
	var b bytes.Buffer
	if err := emailTemplate.Execute(&b, m); err != nil {
		return "", fmt.Errorf("mail: render: %w", err)
	}
	return b.String(), nil
}

// sendMail delivers one message. Brevo's HTTPS API is preferred because
// hosts often block the SMTP ports; SMTP is the fallback.
func (s *Server) sendMail(to string, m mailMessage) error {
	if m.Site == "" {
		m.Site = s.absolute("/")
	}
	html, err := m.html()
	if err != nil {
		return err
	}
	if s.config.BrevoKey != "" {
		return s.sendViaBrevo(to, m.Subject, m.text(), html)
	}
	if s.config.SMTPHost != "" {
		return s.sendViaSMTP(to, m.Subject, m.text(), html)
	}
	return errors.New("mail: no provider configured")
}

func (s *Server) sendViaBrevo(to, subject, text, html string) error {
	message := map[string]any{
		"sender":      map[string]string{"name": "sprue", "email": s.config.SMTPFrom},
		"to":          []map[string]string{{"email": to}},
		"subject":     subject,
		"textContent": text,
		"htmlContent": html,
	}
	if s.config.SupportEmail != "" {
		message["replyTo"] = map[string]string{"email": s.config.SupportEmail}
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, brevoEndpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("api-key", s.config.BrevoKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: mailTimeout}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("brevo: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		reason, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("brevo answered %d: %s", res.StatusCode, reason)
	}
	return nil
}
