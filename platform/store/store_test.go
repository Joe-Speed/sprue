package store

import (
	"errors"
	"path/filepath"
	"strings"
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
	if alice.Slug != "builder" || alice.DisplayName != "Builder" {
		t.Fatalf("new members should start neutral, got %s %s", alice.Slug, alice.DisplayName)
	}
	again := testUser(t, s, "alice@example.com")
	if again.ID != alice.ID {
		t.Fatal("same email created a second user")
	}
	other := testUser(t, s, "alice@other.com")
	if other.Slug != "builder-2" {
		t.Fatalf("expected deduplicated slug, got %s", other.Slug)
	}
	if err := s.SetSlug(alice.ID, "alice-builds"); err != nil {
		t.Fatalf("first choice of address: %v", err)
	}
	if err := s.SetSlug(alice.ID, "alice-again"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a chosen address must be permanent, got %v", err)
	}
	if picked, err := s.userBy("id = ?", alice.ID); err != nil || !picked.SlugChosen {
		t.Errorf("choosing should mark the address chosen: %v", err)
	}
	if err := s.SetSlug(other.ID, "alice-builds"); err == nil {
		t.Error("a taken address was accepted")
	}
	for _, bad := range []string{"Alice", "ab", "builder-9", "has space", strings.Repeat("a", 41)} {
		if err := s.SetSlug(other.ID, bad); err == nil {
			t.Errorf("bad slug %q was accepted", bad)
		}
	}
	renamed, err := s.userBy("id = ?", alice.ID)
	if err != nil || renamed.Slug != "alice-builds" || other.SlugChosen {
		t.Errorf("slug state: %v %s", err, renamed.Slug)
	}
}

// Members who joined before addresses were pickable have a slug taken from
// their email. They start unchosen like everyone else, so they get one
// chance to replace it.
func TestEarlyMemberCanStillPickAnAddress(t *testing.T) {
	s := testStore(t)
	early := testUser(t, s, "joe.speed@example.com")
	if _, err := s.db.Exec(`update users set slug = 'joe.speed', slug_chosen = 0 where id = ?`, early.ID); err != nil {
		t.Fatal(err)
	}
	early, err := s.userBy("id = ?", early.ID)
	if err != nil || early.SlugChosen {
		t.Fatalf("an early member should start unchosen: %v", err)
	}
	if err := s.SetSlug(early.ID, "joe-builds"); err != nil {
		t.Fatalf("early member should get one choice: %v", err)
	}
	moved, err := s.userBy("id = ?", early.ID)
	if err != nil || moved.Slug != "joe-builds" || !moved.SlugChosen {
		t.Errorf("after choosing: %v %s %v", err, moved.Slug, moved.SlugChosen)
	}
	if err := s.SetSlug(early.ID, "joe-again"); !errors.Is(err, ErrNotFound) {
		t.Errorf("only one choice, got %v", err)
	}
}

func TestAdminFollowsConfig(t *testing.T) {
	s := testStore(t)
	user, err := s.FindOrCreateUser("boss@example.com", true)
	if err != nil || !user.IsAdmin {
		t.Fatalf("admin on first sign-in: %v %v", err, user.IsAdmin)
	}
	user, err = s.FindOrCreateUser("boss@example.com", false)
	if err != nil || user.IsAdmin {
		t.Fatalf("admin should be withdrawn when the configuration changes: %v %v", err, user.IsAdmin)
	}
}

func TestMagicTokenSingleUse(t *testing.T) {
	s := testStore(t)
	if err := s.CreateMagicToken("hash1", "a@b.com", false); err != nil {
		t.Fatal(err)
	}
	email, remember, err := s.ConsumeMagicToken("hash1")
	if err != nil || email != "a@b.com" || remember {
		t.Fatalf("consume: %v %s %v", err, email, remember)
	}
	if _, _, err := s.ConsumeMagicToken("hash1"); !errors.Is(err, ErrNotFound) {
		t.Fatal("token worked twice")
	}
	if _, _, err := s.ConsumeMagicToken("never-issued"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown token worked")
	}
	if err := s.CreateMagicToken("hash2", "a@b.com", true); err != nil {
		t.Fatal(err)
	}
	if _, remember, err := s.ConsumeMagicToken("hash2"); err != nil || !remember {
		t.Fatalf("remember should carry through the token: %v %v", err, remember)
	}
}

func TestSessionLifetimes(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	if err := s.CreateSession("short", alice.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("long", alice.ID, true); err != nil {
		t.Fatal(err)
	}
	var shortEnd, longEnd string
	s.db.QueryRow(`select expires_at from sessions where token_hash = 'short'`).Scan(&shortEnd)
	s.db.QueryRow(`select expires_at from sessions where token_hash = 'long'`).Scan(&longEnd)
	if !(shortEnd < longEnd) {
		t.Errorf("short session %s should end before long %s", shortEnd, longEnd)
	}
	// A remembered session near its end is renewed on use; a short one is not.
	soon := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if _, err := s.db.Exec(`update sessions set expires_at = ?`, soon); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser("long"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser("short"); err != nil {
		t.Fatal(err)
	}
	s.db.QueryRow(`select expires_at from sessions where token_hash = 'short'`).Scan(&shortEnd)
	s.db.QueryRow(`select expires_at from sessions where token_hash = 'long'`).Scan(&longEnd)
	if shortEnd != soon {
		t.Errorf("short session was renewed to %s", shortEnd)
	}
	if longEnd == soon {
		t.Error("remembered session near its end was not renewed")
	}
}

func TestMigrateTwice(t *testing.T) {
	s := testStore(t)
	if err := migrate(s.db); err != nil {
		t.Fatalf("second migrate should be a no-op: %v", err)
	}
}

func TestSessions(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	if err := s.CreateSession("sess1", alice.ID, true); err != nil {
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
	if err := s.CreateSession("live", user.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`insert into sessions (token_hash, user_id, expires_at) values ('dead', ?, '2000-01-01T00:00:00Z')`, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMagicToken("fresh", "sweep@example.com", true); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMagicToken("spent", "sweep@example.com", true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ConsumeMagicToken("spent"); err != nil {
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

// A build made by finishing a stash kit keeps a link from the kit. Deleting
// the build must clear that link rather than fail on the foreign key.
func TestDeleteBuildMadeFromStash(t *testing.T) {
	st := testStore(t)
	user, _ := st.FindOrCreateUser("kit@example.com", false)
	kitID, err := st.CreateStashItem(StashItem{UserID: user.ID, Title: "Lancaster"})
	if err != nil {
		t.Fatal(err)
	}
	buildID, err := st.FinishStashItem(kitID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeleteBuild(buildID, user.ID); err != nil {
		t.Fatalf("delete build from stash: %v", err)
	}
}

func TestTakeMonthly(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 3; i++ {
		ok, err := s.TakeMonthly("vision", 3)
		if err != nil || !ok {
			t.Fatalf("use %d: %v %v", i+1, ok, err)
		}
	}
	ok, err := s.TakeMonthly("vision", 3)
	if err != nil || ok {
		t.Fatalf("fourth use should be refused: %v %v", ok, err)
	}
	if ok, err := s.TakeMonthly("other", 3); err != nil || !ok {
		t.Fatalf("a different counter is independent: %v %v", ok, err)
	}
	if _, err := s.db.Exec(`insert into counters (name, count) values ('vision:2001-01', 50)`); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(); err != nil {
		t.Fatal(err)
	}
	var rows int
	s.db.QueryRow(`select count(*) from counters`).Scan(&rows)
	if rows != 2 {
		t.Errorf("sweep should keep only this month's counters, left %d", rows)
	}
}

func TestLikes(t *testing.T) {
	s := testStore(t)
	sam := testUser(t, s, "sam@example.com")
	ravi := testUser(t, s, "ravi@example.com")
	first, err := s.CreateBuild(Build{UserID: sam.ID, Title: "Spitfire", Kit: "Airfix"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateBuild(Build{UserID: sam.ID, Title: "Lancaster", Kit: "Airfix"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LikeBuild(first, sam.ID); err == nil {
		t.Error("liking your own build should be refused")
	}
	if err := s.LikeBuild(first, ravi.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.LikeBuild(first, ravi.ID); err != nil {
		t.Fatal(err)
	}
	build, err := s.BuildByID(first)
	if err != nil || build.Likes != 1 {
		t.Fatalf("one member liking twice is one like: %v %d", err, build.Likes)
	}
	if liked, err := s.HasLikedBuild(first, ravi.ID); err != nil || !liked {
		t.Errorf("has liked: %v %v", err, liked)
	}
	if err := s.LikeBuild(second, ravi.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.LikedBuilds(ravi.ID, MaxLikesShown)
	if err != nil || len(list) != 2 {
		t.Fatalf("liked builds: %v %d", err, len(list))
	}
	// A build that goes private drops out of someone else's likes.
	if err := s.UpdateBuild(Build{ID: second, UserID: sam.ID, Title: "Lancaster", Kit: "Airfix", Private: true}); err != nil {
		t.Fatal(err)
	}
	list, err = s.LikedBuilds(ravi.ID, MaxLikesShown)
	if err != nil || len(list) != 1 || list[0].ID != first {
		t.Errorf("a private build should drop out: %v %d", err, len(list))
	}
	if err := s.UnlikeBuild(first, ravi.ID); err != nil {
		t.Fatal(err)
	}
	build, err = s.BuildByID(first)
	if err != nil || build.Likes != 0 {
		t.Errorf("after taking the like back: %v %d", err, build.Likes)
	}
	if list, err := s.LikedBuilds(ravi.ID, MaxLikesShown); err != nil || len(list) != 0 {
		t.Errorf("likes list should be empty: %v %d", err, len(list))
	}
	// Deleting a build takes its likes with it.
	if err := s.LikeBuild(first, ravi.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteBuild(first, sam.ID); err != nil {
		t.Fatalf("a liked build should still delete: %v", err)
	}
	var left int
	s.db.QueryRow(`select count(*) from build_likes where build_id = ?`, first).Scan(&left)
	if left != 0 {
		t.Errorf("likes left behind after delete: %d", left)
	}
}
