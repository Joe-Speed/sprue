package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

const (
	feedbackMinGap    = 60 * time.Second
	maxFeedbackLength = 2000
)

// feedbackEndpoint is the Discord webhook to post to. Tests point it at a
// local server; production takes it from the configuration.
var feedbackEndpoint = ""

func (s *Server) handleFeedbackForm(w http.ResponseWriter, r *http.Request) {
	if s.config.DiscordWebhook == "" {
		s.renderError(w, r, http.StatusNotFound, "Page not found.")
		return
	}
	if _, ok := s.requireUser(w, r); !ok {
		return
	}
	s.render(w, r, "feedback", "Feedback", nil)
}

// handleFeedback posts a member's message to the Discord channel behind the
// webhook. Nothing is stored here; the channel is the record.
func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	if s.config.DiscordWebhook == "" {
		s.renderError(w, r, http.StatusNotFound, "Page not found.")
		return
	}
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	message := strings.TrimSpace(r.FormValue("message"))
	if message == "" || len(message) > maxFeedbackLength {
		flashRedirect(w, r, "/feedback", "", fmt.Sprintf("Write something, up to %d characters.", maxFeedbackLength))
		return
	}
	if !s.limiter.allow(fmt.Sprintf("feedback:%d", user.ID), feedbackMinGap) {
		flashRedirect(w, r, "/feedback", "", "One message a minute, please.")
		return
	}
	if err := s.postToDiscord(user, message); err != nil {
		log.Printf("web: feedback: %v", err)
		flashRedirect(w, r, "/feedback", "", "Could not send that. Try again in a moment.")
		return
	}
	flashRedirect(w, r, "/", "Thanks, your feedback has been sent.", "")
}

func (s *Server) postToDiscord(user store.User, message string) error {
	endpoint := feedbackEndpoint
	if endpoint == "" {
		endpoint = s.config.DiscordWebhook
	}
	body, err := json.Marshal(map[string]any{
		"username":         "sprue feedback",
		"content":          fmt.Sprintf("**%s** (%s)\n%s", user.DisplayName, s.absolute("/u/"+user.Slug), message),
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("discord answered %d", res.StatusCode)
	}
	return nil
}
