package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	message := mailMessage{Subject: "Hello", Intro: []string{"Body text <b>"}, Action: "Go", Link: "https://sprue.test/go"}
	if err := server.sendMail("sam@example.com", message); err != nil {
		t.Fatal(err)
	}
	sender, _ := got["sender"].(map[string]any)
	to, _ := got["to"].([]any)
	replyTo, _ := got["replyTo"].(map[string]any)
	if sender["email"] != "hello@sprue.test" || len(to) != 1 || got["subject"] != "Hello" || got["textContent"] != "Body text <b>\n\nhttps://sprue.test/go\n\n" || replyTo["email"] != "inbox@example.com" {
		t.Errorf("payload: %v", got)
	}
	html, _ := got["htmlContent"].(string)
	if !strings.Contains(html, "Body text &lt;b&gt;") || !strings.Contains(html, `href="https://sprue.test/go"`) || !strings.Contains(html, ">Go<") || !strings.Contains(html, "https://sprue.test/") {
		t.Errorf("html part: %s", html)
	}
	server.config.BrevoKey = "wrong"
	if err := server.sendMail("sam@example.com", message); err == nil {
		t.Error("a refused key should be an error")
	}
}

func TestMailMessageText(t *testing.T) {
	m := mailMessage{
		Intro: []string{"Hello Sam,", "Two kits wait."},
		Link:  "https://sprue.test/stash",
		Outro: []string{"Keep going."},
		Links: []mailLink{{Label: "Settings", URL: "https://sprue.test/settings"}},
	}
	want := "Hello Sam,\n\nTwo kits wait.\n\nhttps://sprue.test/stash\n\nKeep going.\n\nSettings: https://sprue.test/settings\n"
	if got := m.text(); got != want {
		t.Errorf("text:\n%q\nwant\n%q", got, want)
	}
	long := mailMessage{Intro: make([]string, maxMailParagraphs+1)}
	if _, err := long.html(); err == nil {
		t.Error("over the paragraph cap should be an error")
	}
}
