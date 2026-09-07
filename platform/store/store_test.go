package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

var testToday = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func testCompetition(t *testing.T, s *Store, creator int64, title string) Competition {
	t.Helper()
	comp, err := s.CreateCompetition(Competition{
		Title: title, CreatorID: creator, EntriesClose: "2026-06-10", VotingCloses: "2026-06-20", Category: "fighter",
	}, testToday)
	if err != nil {
		t.Fatal(err)
	}
	return comp
}

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

func TestBuildOrderingPinnedAndDate(t *testing.T) {
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
	pinnedID, err := s.CreateBuild(Build{UserID: alice.ID, Title: "Star", Kit: "K", BuiltOn: "2019-01-01", Pinned: true})
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
	if builds[0].ID != pinnedID || builds[1].ID != newID || builds[2].ID != oldID {
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

	comp := testCompetition(t, s, cara.ID, "Spring Classic")
	if comp.Slug != "spring-classic" || comp.Status != "open" || comp.CreatorName != cara.DisplayName {
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

	if err := s.Advance(testToday); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.CompetitionBySlug("spring-classic"); c.Status != "open" {
		t.Fatalf("advanced too early: %s", c.Status)
	}
	if err := s.Advance(time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.CompetitionBySlug("spring-classic"); c.Status != "voting" {
		t.Fatalf("should be voting: %s", c.Status)
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

	if err := s.Advance(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)); err != nil {
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
	if trophies[0].OwnerName != bob.DisplayName || trophies[0].BuildTitle != "Hurricane" {
		t.Fatalf("trophy owner and build: %+v", trophies[0])
	}
	unseen, err := s.UnseenTrophy(bob.ID)
	if err != nil || unseen.Place != 1 {
		t.Fatalf("bob's unseen trophy: %+v %v", unseen, err)
	}
	if _, err := s.UnseenTrophy(bob.ID); !errors.Is(err, ErrNotFound) {
		t.Error("trophy congratulated twice")
	}
	if _, err := s.UnseenTrophy(cara.ID); !errors.Is(err, ErrNotFound) {
		t.Error("cara has no trophy")
	}
	if past, _ := s.DecidedCompetitions(10); len(past) != 1 || past[0].ID != comp.ID {
		t.Errorf("decided list: %+v", past)
	}
	if err := s.Decide(comp.ID); err != nil {
		t.Fatalf("deciding twice should be harmless: %v", err)
	}
	if again, _ := s.TrophiesForCompetition(comp.ID); len(again) != 2 {
		t.Fatalf("second decide changed trophies: %d", len(again))
	}
}

func TestScheduledThemes(t *testing.T) {
	s := testStore(t)
	if _, err := s.AdminUser(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no admin yet: %v", err)
	}
	testUser(t, s, "member@example.com")
	admin, err := s.FindOrCreateUser("admin@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if found, _ := s.AdminUser(); found.ID != admin.ID {
		t.Fatal("admin lookup")
	}
	month := time.Now().UTC().Format("2006-01")
	if started, _ := s.ThemeStartedInMonth("fighting-friday", month); started {
		t.Fatal("theme reported before it exists")
	}
	if _, err := s.CreateCompetition(Competition{Title: "Fighting Friday", CreatorID: admin.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-06-20", Official: true, Theme: "fighting-friday", Category: "fighter"}, testToday); err != nil {
		t.Fatal(err)
	}
	if started, _ := s.ThemeStartedInMonth("fighting-friday", month); !started {
		t.Fatal("theme not found for this month")
	}
	if started, _ := s.ThemeStartedInMonth("tanktastic", month); started {
		t.Fatal("other theme reported")
	}
}

func TestRemoveEntry(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	bob := testUser(t, s, "bob@example.com")
	comp, err := s.CreateCompetition(Competition{Title: "Scramble", CreatorID: alice.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-06-20", Official: true, Category: "fighter"}, testToday)
	if err != nil {
		t.Fatal(err)
	}
	if !comp.Official {
		t.Fatal("official flag lost")
	}
	build := testBuild(t, s, bob.ID, "Tank")
	if err := s.EnterCompetition(comp.ID, build, bob.ID); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.EntriesWithVotes(comp.ID)
	if err := s.Advance(time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := s.Vote(comp.ID, alice.ID, entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveEntry(comp.ID, entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.EntriesWithVotes(comp.ID); len(left) != 0 {
		t.Error("entry still there")
	}
	if voted, _ := s.HasVoted(comp.ID, alice.ID); voted {
		t.Error("vote for a removed entry survived")
	}
	if err := s.RemoveEntry(comp.ID, entries[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing twice: %v", err)
	}
	if err := s.Advance(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveEntry(comp.ID, 999); !errors.Is(err, ErrInUse) {
		t.Errorf("decided competitions must keep their entries: %v", err)
	}
}

func TestCompetitionRules(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	bad := []Competition{
		{Title: "No dates", CreatorID: alice.ID},
		{Title: "Past", CreatorID: alice.ID, EntriesClose: "2026-05-01", VotingCloses: "2026-05-10"},
		{Title: "Today", CreatorID: alice.ID, EntriesClose: "2026-06-01", VotingCloses: "2026-06-10"},
		{Title: "Voting first", CreatorID: alice.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-06-10"},
		{Title: "Too long", CreatorID: alice.ID, EntriesClose: "2027-06-10", VotingCloses: "2027-06-20"},
		{Title: "Voting too long", CreatorID: alice.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-09-10"},
		{Title: "", CreatorID: alice.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-06-20", Category: "tank"},
		{Title: "No category", CreatorID: alice.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-06-20"},
		{Title: "Odd category", CreatorID: alice.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-06-20", Category: "boats"},
	}
	for _, c := range bad {
		if _, err := s.CreateCompetition(c, testToday); err == nil {
			t.Errorf("accepted %q", c.Title)
		}
	}
	for i := 0; i < MaxOpenPerCreator; i++ {
		testCompetition(t, s, alice.ID, "Running")
	}
	_, err := s.CreateCompetition(Competition{Title: "One more", CreatorID: alice.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-06-20", Category: "tank"}, testToday)
	if !errors.Is(err, ErrLimit) {
		t.Errorf("creator cap: %v", err)
	}
	// A competition nobody votes in still closes, with no trophies.
	if err := s.Advance(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Competitions("")
	for _, c := range list {
		if c.Status != "decided" {
			t.Errorf("%s still %s", c.Slug, c.Status)
		}
		if trophies, _ := s.TrophiesForCompetition(c.ID); len(trophies) != 0 {
			t.Errorf("%s has trophies without votes", c.Slug)
		}
	}
	if _, err := s.CreateCompetition(Competition{Title: "After", CreatorID: alice.ID, EntriesClose: "2026-06-10", VotingCloses: "2026-06-20", Category: "tank"}, testToday); err != nil {
		t.Errorf("decided competitions should not count toward the cap: %v", err)
	}
}

func TestBuildVotesAndFeatured(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	bob := testUser(t, s, "bob@example.com")
	cara := testUser(t, s, "cara@example.com")
	quiet := testBuild(t, s, alice.ID, "Quiet")
	popular := testBuild(t, s, alice.ID, "Popular")
	other := testBuild(t, s, bob.ID, "Other")
	if err := s.VoteBuild(popular, alice.ID); err == nil {
		t.Fatal("voted for own build")
	}
	for _, voter := range []int64{bob.ID, cara.ID} {
		if err := s.VoteBuild(popular, voter); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.VoteBuild(popular, bob.ID); err != nil {
		t.Fatal("second vote should be ignored, not fail")
	}
	if err := s.VoteBuild(other, alice.ID); err != nil {
		t.Fatal(err)
	}
	build, _ := s.BuildByID(popular)
	if build.Votes != 2 {
		t.Fatalf("votes = %d", build.Votes)
	}
	featured, err := s.FeaturedBuilds(time.Now().Add(-time.Hour), MaxFeatured)
	if err != nil || len(featured) != 2 || featured[0].ID != popular || featured[1].ID != other {
		t.Fatalf("featured: %v %v", featured, err)
	}
	for _, b := range featured {
		if b.ID == quiet {
			t.Fatal("unvoted build featured")
		}
	}
	if voted, _ := s.HasVotedBuild(popular, bob.ID); !voted {
		t.Fatal("bob's vote missing")
	}
	if err := s.UnvoteBuild(popular, bob.ID); err != nil {
		t.Fatal(err)
	}
	if voted, _ := s.HasVotedBuild(popular, bob.ID); voted {
		t.Fatal("bob's vote still there")
	}
	stale, _ := s.FeaturedBuilds(time.Now().Add(time.Hour), MaxFeatured)
	if len(stale) != 0 {
		t.Fatal("old votes should not feature")
	}
	if _, err := s.DeleteBuild(popular, alice.ID); err != nil {
		t.Fatal(err)
	}
	if voted, _ := s.HasVotedBuild(popular, cara.ID); voted {
		t.Fatal("votes survived the build")
	}
}

func TestTrophyDownloadSingleUse(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	bob := testUser(t, s, "bob@example.com")
	cara := testUser(t, s, "cara@example.com")
	aliceBuild := testBuild(t, s, alice.ID, "Spitfire")
	comp := testCompetition(t, s, bob.ID, "Cup")
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

func TestPhotoOrderAndRemoval(t *testing.T) {
	s := testStore(t)
	user := testUser(t, s, "photos@example.com")
	build := testBuild(t, s, user.ID, "Photo build")
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		if err := s.AddPhoto(build, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetCoverPhoto(build, "c.jpg"); err != nil {
		t.Fatal(err)
	}
	names, _ := s.Photos(build)
	if len(names) != 3 || names[0] != "c.jpg" || names[1] != "a.jpg" {
		t.Fatalf("cover order wrong: %v", names)
	}
	if err := s.RemovePhoto(build, "a.jpg"); err != nil {
		t.Fatal(err)
	}
	names, _ = s.Photos(build)
	if len(names) != 2 || names[0] != "c.jpg" || names[1] != "b.jpg" {
		t.Fatalf("removal order wrong: %v", names)
	}
	if err := s.RemovePhoto(build, "missing.jpg"); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing a missing photo: %v", err)
	}
	if err := s.SetCoverPhoto(build, "missing.jpg"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cover of a missing photo: %v", err)
	}
	if err := s.AddPhoto(build, "d.jpg"); err != nil {
		t.Fatal(err)
	}
	names, _ = s.Photos(build)
	if len(names) != 3 || names[2] != "d.jpg" {
		t.Fatalf("positions did not close up: %v", names)
	}
}

func TestDeleteBuild(t *testing.T) {
	s := testStore(t)
	owner := testUser(t, s, "owner@example.com")
	other := testUser(t, s, "other@example.com")
	build := testBuild(t, s, owner.ID, "Doomed")
	if err := s.AddPhoto(build, "a.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteBuild(build, other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else deleted the build: %v", err)
	}
	entered := testBuild(t, s, owner.ID, "Entered")
	comp := testCompetition(t, s, owner.ID, "Sprint")
	if err := s.EnterCompetition(comp.ID, entered, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteBuild(entered, owner.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("entered build should be kept: %v", err)
	}
	photos, err := s.DeleteBuild(build, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(photos) != 1 || photos[0] != "a.jpg" {
		t.Fatalf("photo names for cleanup: %v", photos)
	}
	if _, err := s.BuildByID(build); !errors.Is(err, ErrNotFound) {
		t.Error("build still there")
	}
	if names, _ := s.Photos(build); len(names) != 0 {
		t.Error("photo rows still there")
	}
}

func TestRecentBuildsPaging(t *testing.T) {
	s := testStore(t)
	user := testUser(t, s, "pages@example.com")
	for i := 0; i < 5; i++ {
		testBuild(t, s, user.ID, "Build")
	}
	first, err := s.RecentBuilds(0, 2)
	if err != nil || len(first) != 2 || first[0].ID != 5 {
		t.Fatalf("first page: %v %v", first, err)
	}
	second, err := s.RecentBuilds(first[1].ID, 2)
	if err != nil || len(second) != 2 || second[0].ID != 3 {
		t.Fatalf("second page: %v %v", second, err)
	}
	last, err := s.RecentBuilds(second[1].ID, 2)
	if err != nil || len(last) != 1 || last[0].ID != 1 {
		t.Fatalf("last page: %v %v", last, err)
	}
}

func TestSweep(t *testing.T) {
	s := testStore(t)
	user := testUser(t, s, "sweep@example.com")
	if err := s.CreateSession("live", user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`insert into sessions (token_hash, user_id, expires_at) values ('dead', ?, '2000-01-01T00:00:00Z')`, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMagicToken("fresh", "sweep@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMagicToken("spent", "sweep@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeMagicToken("spent"); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(); err != nil {
		t.Fatal(err)
	}
	var sessions, tokens int
	s.db.QueryRow(`select count(*) from sessions`).Scan(&sessions)
	s.db.QueryRow(`select count(*) from magic_tokens`).Scan(&tokens)
	if sessions != 1 || tokens != 1 {
		t.Errorf("after sweep: %d sessions, %d tokens", sessions, tokens)
	}
}

func TestRenameUserAndAvatar(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	if err := s.RenameUser(alice.ID, "Alice B", "bomb-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAvatar(alice.ID, "0123456789abcdef01234567.jpg"); err != nil {
		t.Fatal(err)
	}
	user, _ := s.UserBySlug(alice.Slug)
	if user.DisplayName != "Alice B" || user.Flair != "bomb-2" || user.Avatar == "" {
		t.Fatalf("profile fields: %+v", user)
	}
	if err := s.RenameUser(alice.ID, "  ", "bomb-2"); err == nil {
		t.Fatal("empty name accepted")
	}
	if err := s.SetAvatar(999, "x.jpg"); !errors.Is(err, ErrNotFound) {
		t.Fatal("avatar set on a missing member")
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
