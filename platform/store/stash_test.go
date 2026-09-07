package store

import (
	"errors"
	"testing"
	"time"
)

func TestStashLifecycle(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	bob := testUser(t, s, "bob@example.com")
	if _, err := s.CreateStashItem(StashItem{UserID: alice.ID, Title: "", CostPence: 100}); err == nil {
		t.Fatal("accepted a kit without a title")
	}
	if _, err := s.CreateStashItem(StashItem{UserID: alice.ID, Title: "Free", CostPence: -1}); err == nil {
		t.Fatal("accepted a negative cost")
	}
	old, err := s.CreateStashItem(StashItem{UserID: alice.ID, Title: "Lancaster", Brand: "Airfix", Scale: "1/72", CostPence: 2999})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.CreateStashItem(StashItem{UserID: alice.ID, Title: "Spitfire", CostPence: 1299})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StashItemFor(old, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("bob can see alice's stash")
	}
	if err := s.SetStashNext(newer, alice.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStashNext(old, alice.ID); err != nil {
		t.Fatal(err)
	}
	items, err := s.StashForUser(alice.ID)
	if err != nil || len(items) != 2 || items[0].ID != old || !items[0].Next || items[1].Next {
		t.Fatalf("next should be first and only: %+v %v", items, err)
	}
	if err := s.SetStashStatus(newer, alice.ID, "building"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStashStatus(newer, alice.ID, "built"); err == nil {
		t.Fatal("built must go through FinishStashItem")
	}
	if err := s.AddJournalEntry(newer, alice.ID, "Cockpit done."); err != nil {
		t.Fatal(err)
	}
	if err := s.AddJournalEntry(newer, bob.ID, "Not mine."); !errors.Is(err, ErrNotFound) {
		t.Fatal("bob wrote in alice's journal")
	}
	buildID, err := s.FinishStashItem(newer, alice.ID)
	if err != nil || buildID == 0 {
		t.Fatalf("finish: %v", err)
	}
	build, err := s.BuildByID(buildID)
	if err != nil || build.Title != "Spitfire" || build.Kit != "Spitfire" || build.BuiltOn == "" {
		t.Fatalf("build from kit: %+v %v", build, err)
	}
	again, err := s.FinishStashItem(newer, alice.ID)
	if err != nil || again != buildID {
		t.Fatal("finishing twice should return the same build")
	}
	item, _ := s.StashItemFor(newer, alice.ID)
	if item.Status != "built" || item.BuildID != buildID || item.FinishedAt == "" {
		t.Fatalf("finished item: %+v", item)
	}
	entries, _ := s.JournalFor(newer)
	if len(entries) != 1 {
		t.Fatal("journal lost on finish")
	}
	if err := s.DeleteStashItem(newer, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("bob deleted alice's kit")
	}
	if err := s.DeleteStashItem(newer, alice.ID); err != nil {
		t.Fatal(err)
	}
	if entries, _ := s.JournalFor(newer); len(entries) != 0 {
		t.Fatal("journal survived delete")
	}
	if _, err := s.BuildByID(buildID); err != nil {
		t.Fatal("deleting a finished kit must keep its build")
	}
}

func TestFinishedKitKeepsBrandInKitName(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	id, err := s.CreateStashItem(StashItem{UserID: alice.ID, Title: "Hurricane", Brand: "Airfix", Scale: "1/48"})
	if err != nil {
		t.Fatal(err)
	}
	buildID, err := s.FinishStashItem(id, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	build, _ := s.BuildByID(buildID)
	if build.Kit != "Airfix 1/48 Hurricane" || build.Brand != "Airfix" || build.Scale != "1/48" {
		t.Fatalf("kit fields: %+v", build)
	}
}

func TestGoalAndNudges(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	if err := s.SetGoal(alice.ID, 2, "not a date"); !errors.Is(err, ErrBadDates) {
		t.Fatal("goal without a date")
	}
	if err := s.SetGoal(alice.ID, 2, "2030-01-01"); err != nil {
		t.Fatal(err)
	}
	user, _ := s.UserBySlug(alice.Slug)
	if user.GoalCount != 2 || user.GoalBy != "2030-01-01" || user.GoalSetAt == "" {
		t.Fatalf("goal not stored: %+v", user)
	}
	kit, _ := s.CreateStashItem(StashItem{UserID: alice.ID, Title: "Kit"})
	if _, err := s.FinishStashItem(kit, alice.ID); err != nil {
		t.Fatal(err)
	}
	if done, _ := s.FinishedSince(alice.ID, user.GoalSetAt); done != 1 {
		t.Fatalf("finished since goal: %d", done)
	}
	if err := s.SetGoal(alice.ID, 0, ""); err != nil {
		t.Fatal(err)
	}
	if user, _ := s.UserBySlug(alice.Slug); user.GoalCount != 0 || user.GoalBy != "" {
		t.Fatal("goal not cleared")
	}

	if err := s.SetNudge(alice.ID, "daily"); err == nil {
		t.Fatal("accepted an unknown frequency")
	}
	if err := s.SetNudge(alice.ID, NudgeWeekly); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if due, _ := s.UsersDueNudge(now, 10); len(due) != 0 {
		t.Fatal("nudge due immediately after opting in")
	}
	if due, _ := s.UsersDueNudge(now.Add(8*24*time.Hour), 10); len(due) != 1 || due[0].ID != alice.ID {
		t.Fatalf("weekly nudge not due after eight days: %v", due)
	}
	if err := s.SetNudge(alice.ID, NudgeMonthly); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.UsersDueNudge(now.Add(8*24*time.Hour), 10); len(due) != 0 {
		t.Fatal("monthly nudge due after eight days")
	}
	if due, _ := s.UsersDueNudge(now.Add(31*24*time.Hour), 10); len(due) != 1 {
		t.Fatal("monthly nudge not due after a month")
	}
	if err := s.MarkNudged(alice.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNudge(alice.ID, NudgeOff); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.UsersDueNudge(now.Add(400*24*time.Hour), 10); len(due) != 0 {
		t.Fatal("nudged while off")
	}
}
