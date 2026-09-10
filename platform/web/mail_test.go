package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Joe-Speed/sprue/platform/store"
)

func TestSendViaBrevo(t *testing.T) {
	var got map[string]any
	var key string
	brevo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("api-key")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		if key != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer brevo.Close()
	brevoEndpoint = brevo.URL
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server, err := New(st, Config{DataDir: "x", BaseURL: "https://sprue.test", BrevoKey: "secret", SMTPFrom: "hello@sprue.test", SupportEmail: "inbox@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !server.mailConfigured() {
		t.Fatal("a Brevo key alone should count as mail configured")
	}
	if err := server.sendMail("sam@example.com", "Hello", "Body text"); err != nil {
		t.Fatal(err)
	}
	sender, _ := got["sender"].(map[string]any)
	to, _ := got["to"].([]any)
	replyTo, _ := got["replyTo"].(map[string]any)
	if sender["email"] != "hello@sprue.test" || len(to) != 1 || got["subject"] != "Hello" || got["textContent"] != "Body text" || replyTo["email"] != "inbox@example.com" {
		t.Errorf("payload: %v", got)
	}
	server.config.BrevoKey = "wrong"
	if err := server.sendMail("sam@example.com", "Hello", "Body"); err == nil {
		t.Error("a refused key should be an error")
	}
}
