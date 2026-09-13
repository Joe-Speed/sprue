package web

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestPhotoOrderFromDrag(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	user, err := st.FindOrCreateUser("sam@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	session := strings.Repeat("ab", 32)
	if err := st.CreateSession(hashToken(session), user.ID, true); err != nil {
		t.Fatal(err)
	}
	one, two, three := "aaaaaaaaaaaaaaaaaaaaaaaa.jpg", "bbbbbbbbbbbbbbbbbbbbbbbb.jpg", "cccccccccccccccccccccccc.jpg"
	build, err := st.CreateBuild(store.Build{UserID: user.ID, Title: "Lancaster", Kit: "Airfix 1/72"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{one, two, three} {
		if err := st.AddPhoto(build, name); err != nil {
			t.Fatal(err)
		}
	}
	server, err := New(st, Config{DataDir: t.TempDir(), BaseURL: "https://sprue.test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	send := func(order []string, quiet bool) int {
		form := url.Values{"csrf": {csrfToken(session)}, "order": order}
		req, _ := http.NewRequest(http.MethodPost, ts.URL+fmt.Sprintf("/builds/%d/photos/order", build), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if quiet {
			req.Header.Set("X-Requested-With", "fetch")
		}
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	order := func() string {
		names, err := st.Photos(build)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(names, ",")
	}
	// The script gets a quiet answer and the order it sent.
	if code := send([]string{three, one, two}, true); code != http.StatusNoContent {
		t.Errorf("a dragged order should answer 204, got %d", code)
	}
	if order() != three+","+one+","+two {
		t.Errorf("order after drag: %s", order())
	}
	// A stale page that names the wrong photos changes nothing.
	if code := send([]string{three, one}, true); code != http.StatusConflict {
		t.Errorf("a stale order should answer 409, got %d", code)
	}
	if order() != three+","+one+","+two {
		t.Errorf("a refused order must change nothing, got %s", order())
	}
	// A plain form post lands back on the edit page.
	if code := send([]string{two, three, one}, false); code != http.StatusSeeOther {
		t.Errorf("a form post should redirect, got %d", code)
	}
	if order() != two+","+three+","+one {
		t.Errorf("order after form post: %s", order())
	}
}
