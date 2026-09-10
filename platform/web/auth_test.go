package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "Too+many") {
		t.Errorf("repeat within the gap should be refused: %d %s", res.StatusCode, res.Header.Get("Location"))
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
