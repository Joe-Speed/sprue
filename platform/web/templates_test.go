package web

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

// Every page renders with realistic data, so template errors fail in tests.
func TestEveryPageRenders(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	user := store.User{ID: 1, DisplayName: "Joe", Slug: "joe", IsAdmin: true, CreatedAt: "2026-01-01"}
	build := store.Build{ID: 7, UserID: 1, Title: "Spitfire Mk.I", Kit: "Airfix 1/72", OwnerName: "Joe", OwnerSlug: "joe", CoverPhoto: "abc.jpg", TrophyPlace: 1, BuiltOn: "2026-05-01", Votes: 2}
	comp := store.Competition{ID: 3, Slug: "summer", Title: "Summer sprint", Description: "Anything with wings.", CreatorID: 1, CreatorName: "Joe", CreatorSlug: "joe", EntriesClose: "2026-07-01", VotingCloses: "2026-08-01", Status: "voting", Category: "fighter"}
	trophy := store.Trophy{ID: 9, CompetitionSlug: "summer", CompetitionTitle: "Summer sprint", Place: 1, UserID: 1, OwnerName: "Joe", OwnerSlug: "joe", BuildID: 7, BuildTitle: "Spitfire Mk.I", CoverPhoto: "abc.jpg", DownloadsLeft: 1}
	entry := store.Entry{ID: 4, Build: build, Votes: 2}
	member := store.Member{ID: 2, DisplayName: "Sam", Slug: "sam", Flair: "bomb-1", Avatar: "0123456789abcdef01234567.jpg", CreatedAt: "2026-03-01T00:00:00Z", BuildCount: 4}
	kit := store.StashItem{ID: 5, UserID: 1, Title: "Lancaster", Brand: "Airfix", Scale: "1/72", CostPence: 1299, Status: "building", Next: true, AddedAt: "2026-04-01T10:00:00Z"}
	user.Flair = "plane-red-2"
	user.Avatar = "0123456789abcdef01234567.jpg"
	user.GoalCount = 3
	user.GoalBy = "2026-06-01"
	user.Nudge = "weekly"

	pages := map[string]any{
		"home":         homeData{Featured: []store.Build{build}, Builds: []store.Build{build}, Older: 7},
		"login":        nil,
		"check_email":  "joe@example.com",
		"verify":       strings.Repeat("ab", 32),
		"settings":     settingsData{User: user, Won: [4]bool{false, true, false, false}},
		"builds":       []store.Build{build},
		"profile":      profileData{Owner: user, Builds: []store.Build{build}, Pinned: []store.Build{build}, Trophies: []store.Trophy{trophy}, Friendship: "incoming"},
		"build":        buildPageData{Build: build, Photos: []string{"abc.jpg"}, CanVote: true, Voted: true},
		"build_form":   buildFormData{Build: build, Photos: []string{"abc.jpg", "def.jpg"}, CanDelete: true},
		"competitions": competitionsData{Competitions: []store.Competition{comp}, Categories: store.Categories, Category: "fighter"},
		"past":         []pastCompetition{{Competition: comp, Trophies: []store.Trophy{trophy}}},
		"terms":        "2026-09-07",
		"privacy":      "2026-09-07",
		"feedback":     nil,
		"support": supportData{KofiURL: "https://ko-fi.com/sprue", Total: "£12.50", Gifts: 3,
			Donors: []donorView{{Name: "Sam", Amount: "£10.00", Count: 2}, {Name: "Alex", Amount: "£2.50", Count: 1}}},
		"competition_form": competitionFormData{Tomorrow: "2026-06-02", Categories: store.Categories},
		"stash": stashData{Items: []store.StashItem{kit}, Tomorrow: "2026-06-02",
			Summary: stashSummary{Waiting: 1, DebtPence: 1299, Oldest: "Lancaster", OldestDays: 40, Next: &kit, GoalDone: 1, GoalDaysLeft: -3}},
		"members":     membersData{Query: "jo", Members: []store.Member{member}},
		"friends":     friendsData{Requests: []store.Member{member}, Sent: []store.Member{member}, Friends: []store.Member{member}},
		"stash_item":  stashItemData{Item: kit, Journal: []store.JournalEntry{{ID: 1, StashID: 5, Text: "Primed.", CreatedAt: "2026-05-02T10:00:00Z"}}},
		"competition": competitionData{Competition: comp, Entries: []store.Entry{entry}, Trophies: []store.Trophy{trophy}, MyBuilds: []store.Build{build}, CanEnter: true, CanVote: true, ShowVotes: true},
		"admin": adminData{Competitions: []store.Competition{comp}, Trophies: []store.Trophy{trophy},
			Reports:   []store.Report{{BuildID: 7, BuildTitle: "Spitfire Mk.I", OwnerName: "Joe", Count: 2, Reason: "Not a model.", LatestAt: "2026-05-01T00:00:00Z"}},
			Donations: []store.Donation{{ID: 1, ExternalID: "tx-1", Name: "Sam", Message: "Lovely site", AmountMinor: 300, Currency: "GBP", Public: true, CreatedAt: "2026-09-01T00:00:00Z"}}},
		"error": "Not your build.",
	}
	if len(pages) != len(pageNames) {
		t.Fatalf("test covers %d pages, server has %d", len(pages), len(pageNames))
	}
	for name, data := range pages {
		var out bytes.Buffer
		p := page{Title: name, Path: "/competitions", Mark: "green", User: &user, CSRF: "token", Data: data, Site: site{Year: 2026, DiscordURL: "https://discord.gg/x", Feedback: true, Donate: true, SupportEmail: "help@example.com"}, Requests: 2, Note: "Saved."}
		if err := templates[name].Execute(&out, p); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		html := out.String()
		if !strings.Contains(html, "?v="+assetVersion) {
			t.Errorf("%s: static assets are not versioned", name)
		}
		if !strings.Contains(html, "</html>") {
			t.Errorf("%s: page did not finish", name)
		}
		if !strings.Contains(html, `href="/competitions" aria-current="page"`) {
			t.Errorf("%s: nav does not mark the current section", name)
		}
		if !strings.Contains(html, "mark-green.svg") {
			t.Errorf("%s: mark colour from the page is not used", name)
		}
		if !strings.Contains(html, `class="toast note"`) || !strings.Contains(html, "tick.svg") {
			t.Errorf("%s: note did not render as a toast", name)
		}
		if !strings.Contains(html, "&copy;</span> 2026 sprue") || !strings.Contains(html, `href="https://discord.gg/x">Discord`) || !strings.Contains(html, `href="/feedback">Feedback`) {
			t.Errorf("%s: footer missing the year, Discord, or feedback link", name)
		}
		if !strings.Contains(html, `href="/support">Donate`) {
			t.Errorf("%s: footer missing the donate link", name)
		}
	}
}

func TestDateHelpers(t *testing.T) {
	if got := niceDate("2026-06-14"); got != "14 Jun 2026" {
		t.Errorf("niceDate day: %q", got)
	}
	if got := monthYear("2026-09-07T10:20:31Z"); got != "September 2026" {
		t.Errorf("monthYear stamp: %q", got)
	}
	if got := niceDate("someday"); got != "someday" {
		t.Errorf("unparseable dates pass through, got %q", got)
	}
	if !under("/competitions/summer", "/competitions") || under("/competitionsx", "/competitions") {
		t.Error("under: prefix must end at a path boundary")
	}
}

func TestFlair(t *testing.T) {
	for _, name := range flairs {
		if _, err := fs.Stat(staticFiles, "static/flair/"+name+".png"); err != nil {
			t.Errorf("no picture for %s", name)
		}
	}
	if !strings.Contains(flairPath("bomb-2", 7), "flair/bomb-2.png") {
		t.Error("chosen flair not used")
	}
	if flairPath("", 5) != flairPath("nonsense", 5) || !strings.Contains(flairPath("", 5), "plane-") {
		t.Error("unknown flair should fall back to a plane chosen by id")
	}
	if flairPath("", 1) == flairPath("", 2) {
		t.Error("different members should get different default planes")
	}
	if validFlair("../secret") {
		t.Error("path pieces are not flairs")
	}
	if !validFlair("first-place") || trophyPlace("second-place") != 2 || trophyPlace("plane-red-1") != 0 {
		t.Error("trophy flairs should be known and map to their place")
	}
	if !strings.Contains(flairPath("third-place", 1), "trophies/third-place.svg") {
		t.Error("trophy flair should show the badge")
	}
}

func TestDueThemes(t *testing.T) {
	// September 2026: the 1st is a Tuesday, the 4th a Friday, the 5th a
	// Saturday, the 7th a Monday, the 8th the second Tuesday.
	cases := map[string]string{
		"2026-09-01": "tanktastic",
		"2026-09-04": "fighting-friday",
		"2026-09-05": "big-bomber",
		"2026-09-07": "moderate-mitchell",
		"2026-09-08": "",
		"2026-09-11": "",
		"2026-09-02": "",
	}
	for day, want := range cases {
		date, _ := time.Parse("2006-01-02", day)
		due := dueThemes(date)
		got := ""
		if len(due) == 1 {
			got = due[0].Key
		}
		if got != want || len(due) > 1 {
			t.Errorf("%s: due %v, want %q", day, due, want)
		}
	}
}

func TestMarkCycle(t *testing.T) {
	seen := map[string]bool{}
	colour := markColours[0]
	for range markColours {
		seen[colour] = true
		colour = nextMark(colour)
	}
	if len(seen) != len(markColours) || colour != markColours[0] {
		t.Errorf("cycle does not visit every colour once: %v", seen)
	}
	if validMark("purple") != markColours[0] || nextMark("purple") != markColours[0] {
		t.Error("unknown colours should fall back to the first")
	}
	for _, colour := range markColours {
		if _, err := fs.Stat(staticFiles, "static/mark-"+colour+".svg"); err != nil {
			t.Errorf("no mark file for %s", colour)
		}
	}
}

func TestLocalReferer(t *testing.T) {
	cases := map[string]string{
		"":                                 "/",
		"http://example.test/competitions": "/competitions",
		"http://example.test/?before=9":    "/?before=9",
		"http://other.test/competitions":   "/",
		"http://example.test//evil.test/x": "/",
		"http://example.test/mark/next":    "/",
		"not a url at all ::":              "/",
	}
	for referer, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "http://example.test/mark/next", nil)
		r.Header.Set("Referer", referer)
		if got := localReferer(r); got != want {
			t.Errorf("referer %q: got %q, want %q", referer, got, want)
		}
	}
}

func TestPlainEmail(t *testing.T) {
	good := []string{"Joe@Example.com", " joe@example.com "}
	for _, raw := range good {
		if email, ok := plainEmail(raw); !ok || email != "joe@example.com" {
			t.Errorf("%q rejected or not normalised: %q", raw, email)
		}
	}
	bad := []string{"", "joe", "joe@example.com\r\nBcc: x@y.z", "Joe <joe@example.com>", "a@b.c d@e.f"}
	for _, raw := range bad {
		if _, ok := plainEmail(raw); ok {
			t.Errorf("%q accepted", raw)
		}
	}
}

func TestParsePence(t *testing.T) {
	good := map[string]int64{"": 0, "12": 1200, "12.5": 1250, "£12.99": 1299, " 0.05 ": 5}
	for text, want := range good {
		if got, ok := parsePence(text); !ok || got != want {
			t.Errorf("%q: got %d %v, want %d", text, got, ok, want)
		}
	}
	for _, text := range []string{"abc", "12.999", "-3", "1,000", "1234567"} {
		if _, ok := parsePence(text); ok {
			t.Errorf("%q accepted", text)
		}
	}
	if money(1205) != "12.05" || money(0) != "0.00" {
		t.Error("money formatting")
	}
}

func TestSummarise(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	items := []store.StashItem{
		{ID: 1, Title: "Old", CostPence: 1000, Status: "unbuilt", AddedAt: "2026-01-01T00:00:00Z"},
		{ID: 2, Title: "New", CostPence: 500, Status: "building", Next: true, AddedAt: "2026-05-01T00:00:00Z"},
		{ID: 3, Title: "Done", CostPence: 9999, Status: "built", AddedAt: "2025-01-01T00:00:00Z"},
	}
	user := store.User{GoalCount: 2, GoalBy: "2026-06-11"}
	got := summarise(items, user, 1, now)
	if got.Waiting != 2 || got.Built != 1 || got.DebtPence != 1500 {
		t.Errorf("counts: %+v", got)
	}
	if got.Oldest != "Old" || got.OldestDays != 151 {
		t.Errorf("oldest: %s %d", got.Oldest, got.OldestDays)
	}
	if got.Next == nil || got.Next.ID != 2 {
		t.Error("next kit not found")
	}
	if got.GoalDone != 1 || got.GoalDaysLeft != 10 {
		t.Errorf("goal: %d done, %d days", got.GoalDone, got.GoalDaysLeft)
	}
}

func TestSignedOutPagesRender(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	comp := store.Competition{ID: 3, Slug: "summer", Title: "Summer sprint", CreatorName: "Joe", CreatorSlug: "joe", EntriesClose: "2026-07-01", VotingCloses: "2026-08-01", Status: "open"}
	var out bytes.Buffer
	data := competitionData{Competition: comp}
	if err := templates["competition"].Execute(&out, page{Title: "x", Data: data, Site: site{Year: 2026, DiscordURL: "https://discord.gg/x", Feedback: true, SupportEmail: "help@example.com"}, Requests: 2, Note: "Saved."}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "/login") {
		t.Error("signed-out competition page should offer sign in")
	}
}

// Every page icon a template asks for must exist, drawn on the pixel grid.
func TestPageIcons(t *testing.T) {
	for _, name := range []string{"icon-heart", "icon-members", "icon-home", "icon-trophy", "icon-envelope", "icon-key", "icon-gear", "icon-padlock",
		"icon-document", "icon-medal", "icon-sprue", "icon-box", "icon-friends", "icon-bubble", "icon-shield", "icon-flag", "icon-pencil", "icon-cross"} {
		content, err := fs.ReadFile(staticFiles, "static/"+name+".svg")
		if err != nil {
			t.Errorf("no icon %s", name)
			continue
		}
		if !strings.Contains(string(content), `shape-rendering="crispEdges"`) || !strings.Contains(string(content), `viewBox="0 0 12 12"`) {
			t.Errorf("%s: icons are 12 by 12 pixel art with crisp edges", name)
		}
	}
}
