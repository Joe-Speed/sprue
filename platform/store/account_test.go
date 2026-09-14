package store

import (
	"errors"
	"testing"
	"time"
)

func TestDeleteAccountRemovesEverythingButKeepsPodium(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	bob := testUser(t, s, "bob@example.com")
	plain := testBuild(t, s, alice.ID, "Hurricane")
	if err := s.AddPhoto(plain, "aaaaaaaaaaaaaaaaaaaaaaaa.jpg"); err != nil {
		t.Fatal(err)
	}
	entered := testBuild(t, s, alice.ID, "Spitfire")
	comp := testCompetition(t, s, bob.ID, "Summer sprint")
	if err := s.EnterCompetition(comp.ID, entered, alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateStashItem(StashItem{UserID: alice.ID, Title: "Lancaster"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestFriend(alice.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession("sess-alice", alice.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAvatar(alice.ID, "bbbbbbbbbbbbbbbbbbbbbbbb.jpg"); err != nil {
		t.Fatal(err)
	}

	removed, err := s.DeleteAccount(alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Photos) != 1 || removed.Photos[0].BuildID != plain || removed.Avatar != "bbbbbbbbbbbbbbbbbbbbbbbb.jpg" {
		t.Errorf("files to unlink: %+v", removed)
	}
	if _, err := s.BuildByID(plain); !errors.Is(err, ErrNotFound) {
		t.Errorf("a build outside any competition should be gone: %v", err)
	}
	kept, err := s.BuildByID(entered)
	if err != nil || !kept.Hidden || kept.Title != "Removed build" || kept.Kit != "" {
		t.Errorf("an entered build should stay as a hidden blank: %+v %v", kept, err)
	}
	if _, err := s.SessionUser("sess-alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("sessions should be gone: %v", err)
	}
	items, err := s.StashForUser(alice.ID)
	if err != nil || len(items) != 0 {
		t.Errorf("stash should be empty: %d %v", len(items), err)
	}
	if count, err := s.PendingRequestCount(bob.ID); err != nil || count != 0 {
		t.Errorf("friend requests should be gone: %d %v", count, err)
	}
	blank, err := s.userBy("id = ?", alice.ID)
	if err != nil || blank.Email == "alice@example.com" || blank.DisplayName != "Removed member" || blank.Avatar != "" {
		t.Errorf("the row should be blanked: %+v %v", blank, err)
	}
	if _, err := s.userBy("email = ?", "alice@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the email must be free again: %v", err)
	}
	fresh, err := s.FindOrCreateUser("alice@example.com", false)
	if err != nil || fresh.ID == alice.ID {
		t.Errorf("signing in again should make a new account: %+v %v", fresh, err)
	}
}

func TestMigrateOldDatabaseWithoutVersionTable(t *testing.T) {
	s := testStore(t)
	// A database from before versions were recorded has every column but
	// no record of them. Forgetting the records must not break the next start.
	if _, err := s.db.Exec(`delete from schema_versions`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatalf("migrating an unrecorded database should tolerate existing columns: %v", err)
	}
	var applied int
	if err := s.db.QueryRow(`select max(version) from schema_versions`).Scan(&applied); err != nil || applied != migrations[len(migrations)-1].version {
		t.Errorf("every version should be recorded, got %d %v", applied, err)
	}
	if err := s.Advance(time.Now()); err != nil {
		t.Fatal(err)
	}
}
