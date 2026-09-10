package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// brevoEndpoint is Brevo's transactional send call. Tests point it at a
// local server.
var brevoEndpoint = "https://api.brevo.com/v3/smtp/email"

const mailTimeout = 15 * time.Second

// mailConfigured reports whether the server can send email at all. Without
// it sign-in links are logged, which is local development only.
func (s *Server) mailConfigured() bool {
	return s.config.BrevoKey != "" || s.config.SMTPHost != ""
}

// sendMail delivers one plain text message. Brevo's HTTPS API is preferred
// because hosts often block the SMTP ports; SMTP is the fallback.
func (s *Server) sendMail(to, subject, body string) error {
	if s.config.BrevoKey != "" {
		return s.sendViaBrevo(to, subject, body)
	}
	if s.config.SMTPHost != "" {
		return s.sendViaSMTP(to, subject, body)
	}
	return errors.New("mail: no provider configured")
}

func (s *Server) sendViaBrevo(to, subject, body string) error {
	message := map[string]any{
		"sender":      map[string]string{"name": "sprue", "email": s.config.SMTPFrom},
		"to":          []map[string]string{{"email": to}},
		"subject":     subject,
		"textContent": body,
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
