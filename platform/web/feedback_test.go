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

func TestFeedbackPostsToDiscord(t *testing.T) {
	var got map[string]any
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discord.Close()
	feedbackEndpoint = discord.URL
	defer func() { feedbackEndpoint = "" }()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server, err := New(st, Config{DataDir: "x", BaseURL: "https://sprue.test", DiscordWebhook: "set"})
	if err != nil {
		t.Fatal(err)
	}
	user := store.User{DisplayName: "Sam", Slug: "sam"}
	if err := server.postToDiscord(user, "The stash page @everyone is great"); err != nil {
		t.Fatal(err)
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "**Sam**") || !strings.Contains(content, "https://sprue.test/u/sam") || !strings.Contains(content, "stash page") {
		t.Errorf("content: %q", content)
	}
	mentions, _ := got["allowed_mentions"].(map[string]any)
	if parse, _ := mentions["parse"].([]any); len(parse) != 0 {
		t.Error("mentions must be disabled so feedback cannot ping the server")
	}
}

func TestFeedbackHiddenWithoutWebhook(t *testing.T) {
	ts := testServer(t, "")
	res, _ := get(t, ts, "/feedback")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("feedback without a webhook: %d", res.StatusCode)
	}
	_, home := get(t, ts, "/")
	if strings.Contains(home, `href="/feedback"`) || strings.Contains(home, ">Discord<") || strings.Contains(home, `href="/support"`) {
		t.Error("footer should not offer feedback, Discord, or donations when none is configured")
	}
	if !strings.Contains(home, "&copy;</span> ") || !strings.Contains(home, `href="/privacy">Privacy`) {
		t.Error("footer should always carry the year and the policies")
	}
}
