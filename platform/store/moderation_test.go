package store

import (
	"errors"
	"testing"
)

func TestReportsAndHiding(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	bob := testUser(t, s, "bob@example.com")
	cara := testUser(t, s, "cara@example.com")
	build := testBuild(t, s, alice.ID, "Odd one")
	if _, err := s.ReportBuild(build, alice.ID, "mine"); err == nil {
		t.Fatal("reported own build")
	}
	// Bob reports twice. The second is ignored, and the store says so.
	for _, want := range []struct {
		reporter int64
		added    bool
	}{{bob.ID, true}, {bob.ID, false}, {cara.ID, true}} {
		added, err := s.ReportBuild(build, want.reporter, "Not a model.")
		if err != nil {
			t.Fatal(err)
		}
		if added != want.added {
			t.Fatalf("report from %d: added %v, wanted %v", want.reporter, added, want.added)
		}
	}
	reports, err := s.OpenReports()
	if err != nil || len(reports) != 1 || reports[0].Count != 2 || reports[0].Reason != "Not a model." || reports[0].Hidden {
		t.Fatalf("reports: %+v %v", reports, err)
	}
	if err := s.SetBuildHidden(build, true); err != nil {
		t.Fatal(err)
	}
	if recent, _ := s.RecentBuilds(0, 10); len(recent) != 0 {
		t.Error("hidden build still on the workbench")
	}
	if items, _ := s.BuildsForSitemap(10); len(items) != 0 {
		t.Error("hidden build still in the sitemap")
	}
	if b, _ := s.BuildByID(build); !b.Hidden {
		t.Error("hidden flag not read back")
	}
	if err := s.DismissReports(build); err != nil {
		t.Fatal(err)
	}
	if reports, _ := s.OpenReports(); len(reports) != 0 {
		t.Error("reports not cleared")
	}
	if err := s.SetBuildHidden(build, false); err != nil {
		t.Fatal(err)
	}
	if recent, _ := s.RecentBuilds(0, 10); len(recent) != 1 {
		t.Error("unhidden build missing")
	}
}

func TestPrivateBuilds(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	id, err := s.CreateBuild(Build{UserID: alice.ID, Title: "Shelf queen", Kit: "K", Private: true})
	if err != nil {
		t.Fatal(err)
	}
	if recent, _ := s.RecentBuilds(0, 10); len(recent) != 0 {
		t.Error("private build on the workbench")
	}
	if items, _ := s.BuildsForSitemap(10); len(items) != 0 {
		t.Error("private build in the sitemap")
	}
	if mine, _ := s.BuildsForUser(alice.ID); len(mine) != 1 || !mine[0].Private {
		t.Error("owner should still list it")
	}
	comp := testCompetition(t, s, alice.ID, "Cup")
	if err := s.EnterCompetition(comp.ID, id, alice.ID); err == nil {
		t.Error("private build entered a competition")
	}
	build, _ := s.BuildByID(id)
	build.Private = false
	if err := s.UpdateBuild(build); err != nil {
		t.Fatal(err)
	}
	if err := s.EnterCompetition(comp.ID, id, alice.ID); err != nil {
		t.Fatal(err)
	}
	build.Private = true
	if err := s.UpdateBuild(build); !errors.Is(err, ErrInUse) {
		t.Errorf("entered build went private: %v", err)
	}
}
