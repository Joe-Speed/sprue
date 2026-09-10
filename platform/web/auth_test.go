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
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

func postForm(t *testing.T, ts *httptest.Server, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Post(ts.URL+path, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		body.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return res, body.String()
}

func TestSignInHoneypotAndCaps(t *testing.T) {
	ts := testServer(t, "")
	res, body := postForm(t, ts, "/auth/start", url.Values{"email": {"bot@example.com"}, "website": {"http://spam"}})
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Check your email") {
		t.Errorf("honeypot should look like success: %d", res.StatusCode)
	}
	res, _ = postForm(t, ts, "/auth/start", url.Values{"email": {"sam@example.com"}})
	if res.StatusCode != http.StatusOK {
		t.Errorf("first real request: %d", res.StatusCode)
	}
	res, _ = postForm(t, ts, "/auth/start", url.Values{"email": {"sam@example.com"}})
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(flashOf(res), "Too many") {
		t.Errorf("repeat within the gap should be refused: %d %q", res.StatusCode, flashOf(res))
	}
}

// flashOf returns the message a redirect carried in the flash cookie.
func flashOf(res *http.Response) string {
	for _, cookie := range res.Cookies() {
		if cookie.Name == flashCookie {
			text, _ := url.QueryUnescape(cookie.Value)
			return text
		}
	}
	return ""
}

func TestFlashTravelsByCookieNotQuery(t *testing.T) {
	ts := testServer(t, "")
	defer ts.Close()
	res, _ := postForm(t, ts, "/auth/start", url.Values{"email": {"not an address"}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" || !strings.Contains(flashOf(res), "error:Enter a real email") {
		t.Fatalf("bad email should redirect with a flash cookie: %d %s %q", res.StatusCode, res.Header.Get("Location"), flashOf(res))
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/login", nil)
	req.AddCookie(&http.Cookie{Name: flashCookie, Value: url.QueryEscape("error:Enter a real email address.")})
	page, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(body), "Enter a real email address.") {
		t.Error("flash cookie should show on the next page")
	}
	cleared := false
	for _, cookie := range page.Cookies() {
		if cookie.Name == flashCookie && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("flash cookie should be cleared once shown")
	}
	_, forged := get(t, ts, "/login?note=Your+account+was+suspended")
	if strings.Contains(forged, "account was suspended") {
		t.Error("a query string must not put words in the site's toast")
	}
}

func TestVerifyPageDoesNotBurnToken(t *testing.T) {
	var ts *httptest.Server
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server, err := New(st, Config{DataDir: t.TempDir(), BaseURL: "https://sprue.test"})
	if err != nil {
		t.Fatal(err)
	}
	ts = httptest.NewServer(server.Handler())
	defer ts.Close()
	token := strings.Repeat("cd", 32)
	if err := st.CreateMagicToken(hashToken(token), "scan@example.com", true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		res, body := get(t, ts, "/auth/verify?token="+token)
		if res.StatusCode != http.StatusOK || !strings.Contains(body, `name="token"`) {
			t.Fatalf("scanner visit %d should only show the button: %d", i+1, res.StatusCode)
		}
	}
	res, _ := postForm(t, ts, "/auth/verify", url.Values{"token": {token}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/settings" {
		t.Fatalf("pressing the button should sign in and send a new member to settings: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	res, _ = postForm(t, ts, "/auth/verify", url.Values{"token": {token}})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("a used token should be refused, got %d", res.StatusCode)
	}
}

func TestQuota(t *testing.T) {
	q := newQuota()
	for i := 0; i < 3; i++ {
		if !q.allow("k", 3, time.Hour) {
			t.Fatalf("use %d should be allowed", i+1)
		}
	}
	if q.allow("k", 3, time.Hour) {
		t.Error("fourth use should be refused")
	}
	if !q.allow("other", 3, time.Hour) {
		t.Error("another key has its own count")
	}
	q.seen["k"] = quotaSlot{started: time.Now().Add(-2 * time.Hour), count: 3}
	if !q.allow("k", 3, time.Hour) {
		t.Error("an expired window should reset")
	}
}

func TestClientKeyTrustsProxyEntry(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	if got := clientKey(r); got != "10.0.0.1:1234" {
		t.Errorf("no header: %q", got)
	}
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.9")
	if got := clientKey(r); got != "203.0.113.9" {
		t.Errorf("forged first entry must be ignored, got %q", got)
	}
}

func TestLimiterEvictsWhenFull(t *testing.T) {
	l := newRateLimiter()
	for i := 0; i < maxRateSlots; i++ {
		l.allow(fmt.Sprintf("k%d", i), time.Hour)
	}
	if !l.allow("newcomer", time.Hour) {
		t.Error("a full table must make room, not refuse everyone")
	}
}
