package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

// testServer runs the real handler over a temporary store with one member,
// one build with a cover photo record, and one competition.
func testServer(t *testing.T, analyticsID string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	user, err := st.FindOrCreateUser("alice@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	build, err := st.CreateBuild(store.Build{UserID: user.ID, Title: "Spitfire", Kit: "Airfix 1/72"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddPhoto(build, "0123456789abcdef01234567.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCompetition(store.Competition{
		Title: "Summer sprint", CreatorID: user.ID, EntriesClose: "2999-01-10", VotingCloses: "2999-01-20", Category: "fighter",
	}, time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	server, err := New(st, Config{DataDir: dir, BaseURL: "https://sprue.test", AnalyticsID: analyticsID})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, ts *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	res, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

func TestRobotsAndSitemap(t *testing.T) {
	ts := testServer(t, "")
	res, robots := get(t, ts, "/robots.txt")
	if res.StatusCode != http.StatusOK || !strings.Contains(robots, "Sitemap: https://sprue.test/sitemap.xml") {
		t.Errorf("robots.txt: %d %q", res.StatusCode, robots)
	}
	if !strings.Contains(robots, "Disallow: /admin") {
		t.Error("robots.txt does not keep crawlers out of admin")
	}
	res, sitemap := get(t, ts, "/sitemap.xml")
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/xml") {
		t.Fatalf("sitemap: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	for _, want := range []string{
		"<loc>https://sprue.test/</loc>",
		"<loc>https://sprue.test/competitions/summer-sprint</loc>",
		"<loc>https://sprue.test/u/builder</loc>",
		"<loc>https://sprue.test/builds/1</loc>",
		"<lastmod>",
	} {
		if !strings.Contains(sitemap, want) {
			t.Errorf("sitemap missing %s", want)
		}
	}
}

func TestPageMeta(t *testing.T) {
	ts := testServer(t, "")
	_, home := get(t, ts, "/")
	for _, want := range []string{
		"<title>sprue · The community</title>",
		`<link rel="canonical" href="https://sprue.test/">`,
		`<meta property="og:image" content="https://sprue.test/static/apple-touch-icon.png?v=`,
	} {
		if !strings.Contains(home, want) {
			t.Errorf("home missing %s", want)
		}
	}
	if strings.Contains(home, "noindex") || strings.Contains(home, "googletagmanager") {
		t.Error("home should be indexable and carry no analytics tag by default")
	}
	_, build := get(t, ts, "/builds/1")
	for _, want := range []string{
		"<title>Spitfire · sprue</title>",
		`<meta name="description" content="Airfix 1/72, built by Builder.">`,
		`<meta property="og:image" content="https://sprue.test/photos/1/0123456789abcdef01234567.jpg">`,
	} {
		if !strings.Contains(build, want) {
			t.Errorf("build page missing %s", want)
		}
	}
	res, login := get(t, ts, "/login")
	if !strings.Contains(login, `<meta name="robots" content="noindex">`) {
		t.Error("login page should be noindex")
	}
	csp := res.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self'") {
		t.Errorf("the page must be allowed to post photos back to the site: %s", csp)
	}
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "script-src 'self';") || strings.Contains(csp, "googletagmanager") {
		t.Errorf("csp without analytics: %s", csp)
	}
}

func TestAnalyticsOptIn(t *testing.T) {
	ts := testServer(t, "G-TEST1234")
	if csp := contentSecurityPolicy("G-TEST1234"); !strings.Contains(csp, "connect-src 'self' ") {
		t.Errorf("with analytics the page must still be allowed to post photos back: %s", csp)
	}
	res, home := get(t, ts, "/")
	if !strings.Contains(home, `src="https://www.googletagmanager.com/gtag/js?id=G-TEST1234"`) {
		t.Error("tag script missing")
	}
	if !strings.Contains(home, `data-id="G-TEST1234"`) {
		t.Error("analytics id not handed to the local script")
	}
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "script-src 'self' https://www.googletagmanager.com") {
		t.Errorf("csp does not allow the tag: %s", res.Header.Get("Content-Security-Policy"))
	}
	if _, err := New(nil, Config{}); err == nil {
		t.Error("New should refuse a nil store")
	}
}

func TestBadAnalyticsID(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := New(st, Config{DataDir: "x", BaseURL: "https://sprue.test", AnalyticsID: "UA-12345"}); err == nil {
		t.Error("old style or malformed analytics ids should be refused")
	}
}

func TestWWWRedirectAndHSTS(t *testing.T) {
	server := testServer(t, "")
	defer server.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/competitions?page=2", nil)
	req.Host = "www.sprue.test"
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMovedPermanently || res.Header.Get("Location") != "https://sprue.test/competitions?page=2" {
		t.Errorf("www should redirect to the bare domain, got %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	res, err = http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Header.Get("Strict-Transport-Security") != hstsMaxAge {
		t.Errorf("https site should send HSTS, got %q", res.Header.Get("Strict-Transport-Security"))
	}
}
