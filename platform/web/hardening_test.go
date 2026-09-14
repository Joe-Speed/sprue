package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Joe-Speed/sprue/platform/store"
)

// signedIn starts a server with one member signed in and returns what a
// browser would hold: the session cookie value and the CSRF token.
func signedIn(t *testing.T, email string, admin bool) (*httptest.Server, *store.Store, string, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	user, err := st.FindOrCreateUser(email, admin)
	if err != nil {
		t.Fatal(err)
	}
	session := strings.Repeat("ab", 32)
	if err := st.CreateSession(hashToken(session), user.ID, true); err != nil {
		t.Fatal(err)
	}
	config := Config{DataDir: t.TempDir(), BaseURL: "https://sprue.test"}
	if admin {
		config.AdminEmail = email
	}
	server, err := New(st, config)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return ts, st, session, csrfToken(session)
}

func sendAs(t *testing.T, ts *httptest.Server, method, session, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, ts.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	text, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(text)
}

func TestPanicBecomesLoggedErrorPage(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server, err := New(st, Config{DataDir: t.TempDir(), BaseURL: "https://sprue.test"})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.withRequestLog(server.withRecover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "Something went wrong") {
		t.Errorf("a panic should become a 500 page, got %d", rec.Code)
	}
}

func TestSignedInPagesAreNotCached(t *testing.T) {
	ts, _, session, _ := signedIn(t, "sam@example.com", false)
	res, _ := sendAs(t, ts, http.MethodGet, session, "/stash", nil)
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("a signed-in page must not be cached: %q", res.Header.Get("Cache-Control"))
	}
	res, _ = sendAs(t, ts, http.MethodGet, "", "/", nil)
	if res.Header.Get("Cache-Control") == "no-store" {
		t.Errorf("the public front page may be cached")
	}
}

func TestLogoutNeedsTheForm(t *testing.T) {
	ts, st, session, csrf := signedIn(t, "sam@example.com", false)
	res, _ := sendAs(t, ts, http.MethodPost, session, "/logout", url.Values{})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("a logout without the token should be refused, got %d", res.StatusCode)
	}
	if _, err := st.SessionUser(hashToken(session)); err != nil {
		t.Fatalf("the session should still stand: %v", err)
	}
	res, _ = sendAs(t, ts, http.MethodPost, session, "/logout", url.Values{"csrf": {csrf}})
	if res.StatusCode != http.StatusSeeOther {
		t.Errorf("logout with the token should work, got %d", res.StatusCode)
	}
}

func TestAccountRemoval(t *testing.T) {
	ts, st, session, csrf := signedIn(t, "sam@example.com", false)
	res, _ := sendAs(t, ts, http.MethodPost, session, "/settings/delete", url.Values{"csrf": {csrf}})
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(flashOf(res), "Tick the box") {
		t.Fatalf("without the tick nothing should happen: %d %q", res.StatusCode, flashOf(res))
	}
	res, _ = sendAs(t, ts, http.MethodPost, session, "/settings/delete", url.Values{"csrf": {csrf}, "confirm": {"on"}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Fatalf("removal should send the reader home: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	if _, err := st.SessionUser(hashToken(session)); err == nil {
		t.Error("the session should be gone")
	}
	if _, err := st.UserBySlug("builder"); err == nil {
		t.Error("the member page should be gone")
	}
}

func TestAdminCannotRemoveOwnAccount(t *testing.T) {
	ts, _, session, csrf := signedIn(t, "admin@example.com", true)
	res, _ := sendAs(t, ts, http.MethodPost, session, "/settings/delete", url.Values{"csrf": {csrf}, "confirm": {"on"}})
	if !strings.Contains(flashOf(res), "admin account") {
		t.Errorf("the admin should be refused: %q", flashOf(res))
	}
}

func TestRapidLikesAreSlowed(t *testing.T) {
	ts, st, session, csrf := signedIn(t, "sam@example.com", false)
	other, err := st.FindOrCreateUser("other@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	build, err := st.CreateBuild(store.Build{UserID: other.ID, Title: "Spitfire", Kit: "Airfix"})
	if err != nil {
		t.Fatal(err)
	}
	path := buildPage(build) + "/like"
	res, _ := sendAs(t, ts, http.MethodPost, session, path, url.Values{"csrf": {csrf}})
	if flashOf(res) != "note:Liked." {
		t.Fatalf("first like should land: %q", flashOf(res))
	}
	res, _ = sendAs(t, ts, http.MethodPost, session, path, url.Values{"csrf": {csrf}})
	if !strings.Contains(flashOf(res), "Slow down") {
		t.Errorf("a second press inside a second should be slowed: %q", flashOf(res))
	}
}

func TestCompetitionFormKeepsInputOnRefusal(t *testing.T) {
	ts, _, session, csrf := signedIn(t, "sam@example.com", false)
	res, body := sendAs(t, ts, http.MethodPost, session, "/competitions/new", url.Values{
		"csrf": {csrf}, "title": {"Winter Wellingtons"}, "category": {"heavy bomber"},
		"entries_close": {"2020-01-01"}, "voting_closes": {"2020-01-02"},
	})
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a refused form should come straight back, got %d", res.StatusCode)
	}
	if !strings.Contains(body, `value="Winter Wellingtons"`) || !strings.Contains(body, "Entries must close after today") {
		t.Errorf("the form should keep the title and say what is wrong")
	}
}

func TestAdminBackupDownload(t *testing.T) {
	ts, _, session, _ := signedIn(t, "admin@example.com", true)
	res, body := sendAs(t, ts, http.MethodGet, session, "/admin/backup", nil)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(body, "SQLite format 3") {
		t.Fatalf("the admin should get a database file, got %d", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("Content-Disposition"), "sprue-") {
		t.Errorf("the download should carry a file name: %q", res.Header.Get("Content-Disposition"))
	}
	ts2, _, member, _ := signedIn(t, "sam@example.com", false)
	res, _ = sendAs(t, ts2, http.MethodGet, member, "/admin/backup", nil)
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a member must not get the backup, got %d", res.StatusCode)
	}
}

func TestBackupCopyIsRemovedAfterSending(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, err := st.FindOrCreateUser("admin@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	session := strings.Repeat("ab", 32)
	if err := st.CreateSession(hashToken(session), admin.ID, true); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	server, err := New(st, Config{DataDir: dataDir, BaseURL: "https://sprue.test", AdminEmail: "admin@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	sendAs(t, ts, http.MethodGet, session, "/admin/backup", nil)
	left, err := filepath.Glob(filepath.Join(dataDir, "backup-*.db"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("the copy should be removed once sent: %v", left)
	}
}
