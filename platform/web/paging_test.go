package web

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Joe-Speed/sprue/platform/store"
)

func TestPageOfCutsAndLinks(t *testing.T) {
	items := make([]int, 60)
	for i := range items {
		items[i] = i
	}
	first, view := pageOf(items, httptest.NewRequest("GET", "/likes", nil), 24)
	if len(first) != 24 || first[0] != 0 || view.Page != 1 || view.Pages != 3 || view.Total != 60 {
		t.Fatalf("first page: %d items, %+v", len(first), view)
	}
	if view.Prev != "" || view.Next != "/likes?page=2" {
		t.Fatalf("links on the first page: %+v", view)
	}
	last, view := pageOf(items, httptest.NewRequest("GET", "/likes?page=3", nil), 24)
	if len(last) != 12 || last[0] != 48 || view.Next != "" || view.Prev != "/likes?page=2" {
		t.Fatalf("last page: %d items, %+v", len(last), view)
	}
	// A page past the end settles on the last real page, and page one drops
	// the number so the address stays clean.
	past, view := pageOf(items, httptest.NewRequest("GET", "/likes?page=99", nil), 24)
	if len(past) != 12 || view.Page != 3 {
		t.Fatalf("past the end: %d items, %+v", len(past), view)
	}
	second, view := pageOf(items, httptest.NewRequest("GET", "/likes?page=2", nil), 24)
	if len(second) != 24 || view.Prev != "/likes" {
		t.Fatalf("back to the first page: %d items, %+v", len(second), view)
	}
}

// Paging a filtered list keeps the filter.
func TestPageLinksKeepTheFilter(t *testing.T) {
	items := make([]int, 30)
	_, view := pageOf(items, httptest.NewRequest("GET", "/builds?show=private", nil), 24)
	if view.Next != "/builds?page=2&show=private" {
		t.Fatalf("filter lost: %+v", view)
	}
}

func TestEmptyListHasOnePage(t *testing.T) {
	var none []int
	items, view := pageOf(none, httptest.NewRequest("GET", "/stash", nil), 50)
	if len(items) != 0 || view.Page != 1 || view.Pages != 1 || view.Prev != "" || view.Next != "" {
		t.Fatalf("empty list: %+v", view)
	}
}

func TestMatchingBuildsNarrowsToOneState(t *testing.T) {
	builds := []store.Build{
		{ID: 1, Title: "Open"},
		{ID: 2, Title: "Kept back", Private: true},
		{ID: 3, Title: "Reported", Hidden: true},
	}
	if got := matchingBuilds(builds, ""); len(got) != 3 {
		t.Fatalf("no filter should keep everything, got %d", len(got))
	}
	if got := matchingBuilds(builds, "public"); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("public: %+v", got)
	}
	if got := matchingBuilds(builds, "private"); len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("private: %+v", got)
	}
	if got := matchingBuilds(builds, "hidden"); len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("hidden: %+v", got)
	}
	if buildFilter("everything") != "" {
		t.Fatal("an unknown filter should show everything")
	}
}

func TestMatchingKitsNarrowsToOneState(t *testing.T) {
	kits := []store.StashItem{
		{ID: 1, Status: "unbuilt"},
		{ID: 2, Status: "building"},
		{ID: 3, Status: "built"},
	}
	if got := matchingKits(kits, "building"); len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("building: %+v", got)
	}
	if got := matchingKits(kits, ""); len(got) != 3 {
		t.Fatalf("no filter should keep everything, got %d", len(got))
	}
	if stashFilter("sprayed") != "" {
		t.Fatal("an unknown filter should show everything")
	}
}

// The front page walks down through older builds and back up again, and the
// links only appear when there is really a page on that side.
func TestHomePagesBothWays(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	user, err := st.FindOrCreateUser("pager@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	total := homePageSize + 7
	for i := 0; i < total; i++ {
		if _, err := st.CreateBuild(store.Build{UserID: user.ID, Title: "Build", Kit: "Airfix 1/72"}); err != nil {
			t.Fatal(err)
		}
	}
	server, err := New(st, Config{DataDir: t.TempDir(), BaseURL: "https://sprue.test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	oldest := int64(total - homePageSize + 1) // last build on the first page
	first := fetch(t, ts.URL+"/")
	if !strings.Contains(first, fmt.Sprintf(`href="/?before=%d"`, oldest)) {
		t.Fatal("the first page offers no older builds")
	}
	if strings.Contains(first, "Newer builds") {
		t.Fatal("the first page offers newer builds")
	}
	second := fetch(t, fmt.Sprintf("%s/?before=%d", ts.URL, oldest))
	if !strings.Contains(second, fmt.Sprintf(`href="/?after=%d"`, oldest-1)) {
		t.Fatal("the second page has no way back up")
	}
	// Climbing back from the second page lands on the first page again:
	// nothing is newer than that, so it offers no way further up.
	back := fetch(t, fmt.Sprintf("%s/?after=%d", ts.URL, oldest-1))
	if !strings.Contains(back, fmt.Sprintf(`href="/?before=%d"`, oldest)) {
		t.Fatal("climbing back offers no older builds")
	}
	if strings.Contains(back, "Newer builds") {
		t.Fatal("the first page offers newer builds")
	}
	if !strings.Contains(back, `href="/">Latest`) {
		t.Fatal("a paged view offers no way home")
	}
	// Climbing from deeper down stops one page short of the top, so that
	// page does offer another above it.
	middle := fetch(t, ts.URL+"/?after=3")
	if !strings.Contains(middle, fmt.Sprintf(`href="/?after=%d"`, 3+homePageSize)) {
		t.Fatal("a middle page offers no way further up")
	}
	top := fetch(t, fmt.Sprintf("%s/?after=%d", ts.URL, total-1))
	if strings.Contains(top, "Newer builds") {
		t.Fatal("the newest build still offers newer builds")
	}
}

func fetch(t *testing.T, url string) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%s: %d", url, res.StatusCode)
	}
	return string(body)
}
