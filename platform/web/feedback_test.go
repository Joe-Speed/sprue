package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	discordEndpoint = discord.URL
	defer func() { discordEndpoint = "" }()
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

func TestAnnounceDecidedPostsOnce(t *testing.T) {
	var posts []map[string]any
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		posts = append(posts, got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discord.Close()
	discordEndpoint = discord.URL
	defer func() { discordEndpoint = "" }()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cara, err := st.FindOrCreateUser("cara@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	dan, err := st.FindOrCreateUser("dan@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := st.CreateCompetition(store.Competition{
		Title: "Autumn Skies", CreatorID: cara.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-06-20", Category: "fighter",
	}, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	winner, err := st.CreateBuild(store.Build{UserID: cara.ID, Title: "Spitfire", Kit: "Airfix"})
	if err != nil {
		t.Fatal(err)
	}
	also, err := st.CreateBuild(store.Build{UserID: dan.ID, Title: "Hurricane", Kit: "Airfix"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnterCompetition(comp.ID, winner, cara.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.EnterCompetition(comp.ID, also, dan.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := st.EntriesWithVotes(comp.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Build.ID == winner {
			if err := st.Vote(comp.ID, dan.ID, entry.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := st.Advance(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	server, err := New(st, Config{DataDir: "x", BaseURL: "https://sprue.test", DiscordResults: "set"})
	if err != nil {
		t.Fatal(err)
	}
	server.announceDecided()
	server.announceDecided()
	if len(posts) != 1 {
		t.Fatalf("a decided competition is announced exactly once, got %d posts", len(posts))
	}
	content, _ := posts[0]["content"].(string)
	for _, want := range []string{"**Autumn Skies** has been decided!", "\U0001F947 Spitfire by", "Well done to all 2 who entered!", "https://sprue.test/competitions/autumn-skies"} {
		if !strings.Contains(content, want) {
			t.Errorf("announcement should say %q, got:\n%s", want, content)
		}
	}
	if strings.Contains(content, "\U0001F948") {
		t.Errorf("an entry with no votes never places:\n%s", content)
	}
	mentions, _ := posts[0]["allowed_mentions"].(map[string]any)
	if parse, _ := mentions["parse"].([]any); len(parse) != 0 {
		t.Error("mentions must be disabled")
	}
}

func TestAnnounceFailureIsRetried(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	discordEndpoint = failing.URL
	defer func() { discordEndpoint = "" }()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cara, err := st.FindOrCreateUser("cara@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := st.CreateCompetition(store.Competition{
		Title: "Quiet One", CreatorID: cara.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-06-20", Category: "fighter",
	}, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Advance(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	server, err := New(st, Config{DataDir: "x", BaseURL: "https://sprue.test", DiscordResults: "set"})
	if err != nil {
		t.Fatal(err)
	}
	server.announceDecided()
	waiting, err := st.ClaimUnannounced(10)
	if err != nil || len(waiting) != 1 || waiting[0].ID != comp.ID {
		t.Fatalf("a failed post should be released for the next run: %v %d", err, len(waiting))
	}
}
