package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Joe-Speed/sprue/platform/store"
)

func TestFailedUploadSaysSoNotExpired(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	user, err := st.FindOrCreateUser("sam@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	session := strings.Repeat("ef", 32)
	if err := st.CreateSession(hashToken(session), user.ID, true); err != nil {
		t.Fatal(err)
	}
	server, err := New(st, Config{DataDir: t.TempDir(), BaseURL: "https://sprue.test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// A multipart body that carries no fields is what a cut-off upload looks
	// like to the handler.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/builds/new", strings.NewReader("--x--\r\n"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusRequestTimeout || !strings.Contains(string(body), "upload did not finish") {
		t.Errorf("a cut-off upload should say so, got %d %s", res.StatusCode, string(body)[:200])
	}
	// A plain form post with a bad token is still an expired form.
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/settings", strings.NewReader("csrf=wrong&display_name=Sam"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "form expired") {
		t.Errorf("a bad token should still say expired, got %d", res.StatusCode)
	}
}
