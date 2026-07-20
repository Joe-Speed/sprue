package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func testUser(t *testing.T, s *Store, email string) User {
	t.Helper()
	user, err := s.FindOrCreateUser(email, false)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func testBuild(t *testing.T, s *Store, userID int64, title string) int64 {
	t.Helper()
	id, err := s.CreateBuild(Build{UserID: userID, Title: title, Kit: "Airfix 1/72 Something"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestUserCreationAndSlugs(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "Alice@Example.com")
	if alice.Email != "alice@example.com" {
		t.Fatalf("email not normalised: %s", alice.Email)
	}
	if alice.Slug != "alice" {
		t.Fatalf("slug: %s", alice.Slug)
	}
	again := testUser(t, s, "alice@example.com")
	if again.ID != alice.ID {
		t.Fatal("same email created a second user")
	}
	other := testUser(t, s, "alice@other.com")
	if other.Slug != "alice-2" {
		t.Fatalf("expected deduplicated slug, got %s", other.Slug)
	}
}

func TestMagicTokenSingleUse(t *testing.T) {
	s := testStore(t)
	if err := s.CreateMagicToken("hash1", "a@b.com"); err != nil {
		t.Fatal(err)
	}
	email, err := s.ConsumeMagicToken("hash1")
	if err != nil || email != "a@b.com" {
		t.Fatalf("consume: %v %s", err, email)
	}
	if _, err := s.ConsumeMagicToken("hash1"); !errors.Is(err, ErrNotFound) {
		t.Fatal("token worked twice")
	}
	if _, err := s.ConsumeMagicToken("never-issued"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown token worked")
	}
}

func TestSessions(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	if err := s.CreateSession("sess1", alice.ID); err != nil {
		t.Fatal(err)
	}
	user, err := s.SessionUser("sess1")
	if err != nil || user.ID != alice.ID {
		t.Fatalf("session lookup: %v", err)
	}
	if err := s.DeleteSession("sess1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser("sess1"); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted session still valid")
	}
}

func TestBuildOrderingFeaturedAndDate(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	oldID, err := s.CreateBuild(Build{UserID: alice.ID, Title: "Old", Kit: "K", BuiltOn: "2020-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	newID, err := s.CreateBuild(Build{UserID: alice.ID, Title: "New", Kit: "K", BuiltOn: "2025-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	featuredID, err := s.CreateBuild(Build{UserID: alice.ID, Title: "Star", Kit: "K", BuiltOn: "2019-01-01", Featured: true})
	if err != nil {
		t.Fatal(err)
	}
	builds, err := s.BuildsForUser(alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) != 3 {
		t.Fatalf("got %d builds", len(builds))
	}
	if builds[0].ID != featuredID || builds[1].ID != newID || builds[2].ID != oldID {
		t.Fatalf("wrong order: %v %v %v", builds[0].Title, builds[1].Title, builds[2].Title)
	}
}

func TestCompetitionLifecycle(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	bob := testUser(t, s, "bob@example.com")
	cara := testUser(t, s, "cara@example.com")
	dave := testUser(t, s, "dave@example.com")

	aliceBuild := testBuild(t, s, alice.ID, "Spitfire")
	bobBuild := testBuild(t, s, bob.ID, "Hurricane")

	comp, err := s.CreateCompetition("Spring Classic", "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if comp.Slug != "spring-classic" || comp.Status != "open" {
		t.Fatalf("bad competition: %+v", comp)
	}

	if err := s.EnterCompetition(comp.ID, aliceBuild, alice.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnterCompetition(comp.ID, bobBuild, bob.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnterCompetition(comp.ID, bobBuild, alice.ID); err == nil {
		t.Fatal("entered someone else's build")
	}
	secondAlice := testBuild(t, s, alice.ID, "Lancaster")
	if err := s.EnterCompetition(comp.ID, secondAlice, alice.ID); err == nil {
		t.Fatal("entered twice")
	}

	entries, err := s.EntriesWithVotes(comp.ID)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries: %v %d", err, len(entries))
	}

	if err := s.SetCompetitionStatus(comp.ID, "voting"); err != nil {
		t.Fatal(err)
	}
	aliceEntry, bobEntry := entries[0], entries[1]
	if err := s.Vote(comp.ID, alice.ID, aliceEntry.ID); err == nil {
		t.Fatal("voted for own entry")
	}
	if err := s.Vote(comp.ID, alice.ID, bobEntry.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Vote(comp.ID, alice.ID, bobEntry.ID); err == nil {
		t.Fatal("voted twice")
	}
	if err := s.Vote(comp.ID, cara.ID, bobEntry.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Vote(comp.ID, dave.ID, aliceEntry.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.Decide(comp.ID); err != nil {
		t.Fatal(err)
	}
	comp, err = s.CompetitionBySlug("spring-classic")
	if err != nil || comp.Status != "decided" {
		t.Fatalf("not decided: %v %s", err, comp.Status)
	}
	trophies, err := s.TrophiesForCompetition(comp.ID)
	if err != nil || len(trophies) != 2 {
		t.Fatalf("trophies: %v %d", err, len(trophies))
	}
	if trophies[0].Place != 1 || trophies[0].UserID != bob.ID {
		t.Fatalf("first place wrong: %+v", trophies[0])
	}
	if trophies[1].Place != 2 || trophies[1].UserID != alice.ID {
		t.Fatalf("second place wrong: %+v", trophies[1])
	}
}

func TestTrophyDownloadSingleUse(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	bob := testUser(t, s, "bob@example.com")
	cara := testUser(t, s, "cara@example.com")
	aliceBuild := testBuild(t, s, alice.ID, "Spitfire")
	comp, err := s.CreateCompetition("Cup", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnterCompetition(comp.ID, aliceBuild, alice.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := s.EntriesWithVotes(comp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Vote(comp.ID, cara.ID, entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Decide(comp.ID); err != nil {
		t.Fatal(err)
	}
	trophies, err := s.TrophiesForUser(alice.ID)
	if err != nil || len(trophies) != 1 {
		t.Fatalf("trophies: %v", err)
	}
	trophy := trophies[0]
	if err := s.UseTrophyDownload(trophy.ID, bob.ID); err == nil {
		t.Fatal("someone else downloaded the trophy")
	}
	if err := s.UseTrophyDownload(trophy.ID, alice.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UseTrophyDownload(trophy.ID, alice.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("download worked twice")
	}
	if err := s.RearmTrophy(trophy.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UseTrophyDownload(trophy.ID, alice.ID); err != nil {
		t.Fatal("re-armed download failed")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Spring Classic":     "spring-classic",
		"  Weird -- Name!  ": "weird-name",
		"":                   "item",
		"ALL CAPS 42":        "all-caps-42",
	}
	for input, want := range cases {
		if got := Slugify(input); got != want {
			t.Fatalf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
}
