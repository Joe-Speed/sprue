package store

import "testing"

func TestReportsAndHiding(t *testing.T) {
	s := testStore(t)
	alice := testUser(t, s, "alice@example.com")
	bob := testUser(t, s, "bob@example.com")
	cara := testUser(t, s, "cara@example.com")
	build := testBuild(t, s, alice.ID, "Odd one")
	if err := s.ReportBuild(build, alice.ID, "mine"); err == nil {
		t.Fatal("reported own build")
	}
	for _, reporter := range []int64{bob.ID, bob.ID, cara.ID} {
		if err := s.ReportBuild(build, reporter, "Not a model."); err != nil {
			t.Fatal(err)
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
