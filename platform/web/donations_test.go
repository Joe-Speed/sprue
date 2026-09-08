package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Joe-Speed/sprue/platform/store"
)

func kofiServer(t *testing.T, token string) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	server, err := New(st, Config{DataDir: "x", BaseURL: "https://sprue.test", KofiURL: "https://ko-fi.com/sprue", KofiToken: token})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func postKofi(t *testing.T, ts *httptest.Server, payload string) int {
	t.Helper()
	form := url.Values{"data": {payload}}
	res, err := ts.Client().Post(ts.URL+"/webhooks/kofi", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

// sample is the shape Ko-fi posts, with the fields the site reads filled in.
func sample(token, kind string, public bool, name, amount, id string) string {
	return fmt.Sprintf(`{"verification_token":%q,"message_id":"m1","timestamp":"2026-09-08T10:00:00Z","type":%q,"is_public":%t,`+
		`"from_name":%q,"message":"Keep going","amount":%q,"url":"https://ko-fi.com/x","email":"jo@example.com","currency":"GBP",`+
		`"is_subscription_payment":false,"is_first_subscription_payment":false,"kofi_transaction_id":%q,"shop_items":null,"tier_name":null,"shipping":null}`,
		token, kind, public, name, amount, id)
}

func TestKofiWebhookRecordsAndRanks(t *testing.T) {
	ts, st := kofiServer(t, "secret")
	cases := []struct {
		payload string
		want    int
	}{
		{sample("wrong", "Donation", true, "Mallory", "3.00", "tx-0"), http.StatusForbidden},
		{sample("secret", "Donation", true, "Sam", "3.00", "tx-1"), http.StatusOK},
		{sample("secret", "Donation", true, "Sam", "3.00", "tx-1"), http.StatusOK}, // retry, ignored
		{sample("secret", "Donation", true, "Sam", "2.50", "tx-2"), http.StatusOK},
		{sample("secret", "Subscription", true, "Alex", "10.00", "tx-3"), http.StatusOK},
		{sample("secret", "Donation", false, "Quiet", "50.00", "tx-4"), http.StatusOK},
		{sample("secret", "Shop Order", true, "Shop", "99.00", "tx-5"), http.StatusOK}, // not counted
		{sample("secret", "Donation", true, "Bad", "lots", "tx-6"), http.StatusBadRequest},
	}
	for i, c := range cases {
		if got := postKofi(t, ts, c.payload); got != c.want {
			t.Errorf("case %d: status %d, want %d", i, got, c.want)
		}
	}
	donors, err := st.TopDonors()
	if err != nil {
		t.Fatal(err)
	}
	if len(donors) != 2 || donors[0].Name != "Alex" || donors[0].AmountMinor != 1000 || donors[1].Name != "Sam" || donors[1].AmountMinor != 550 || donors[1].Count != 2 {
		t.Errorf("top donors: %+v", donors)
	}
	total, gifts, err := st.DonationTotal("GBP")
	if err != nil || total != 6550 || gifts != 4 {
		t.Errorf("total %d over %d gifts, %v", total, gifts, err)
	}
	res, body := get(t, ts, "/support")
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Alex") || strings.Contains(body, "Quiet") || !strings.Contains(body, "£65.50") {
		t.Errorf("support page: %d %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `href="https://ko-fi.com/sprue"`) {
		t.Error("support page should link to Ko-fi")
	}
	_, sitemap := get(t, ts, "/sitemap.xml")
	if !strings.Contains(sitemap, "https://sprue.test/support") {
		t.Error("sitemap should list the support page when Ko-fi is set")
	}
}

func TestKofiWebhookOffWithoutToken(t *testing.T) {
	ts, _ := kofiServer(t, "")
	if got := postKofi(t, ts, sample("", "Donation", true, "Sam", "3.00", "tx-1")); got != http.StatusNotFound {
		t.Errorf("webhook without a token configured: %d", got)
	}
}

func TestSupportPageOffWithoutKofi(t *testing.T) {
	ts := testServer(t, "")
	res, _ := get(t, ts, "/support")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("support without Ko-fi: %d", res.StatusCode)
	}
	_, sitemap := get(t, ts, "/sitemap.xml")
	if strings.Contains(sitemap, "/support") {
		t.Error("sitemap should not list the support page without Ko-fi")
	}
}

func TestFormatGift(t *testing.T) {
	if got := formatGift(1250, "GBP"); got != "£12.50" {
		t.Errorf("GBP: %q", got)
	}
	if got := formatGift(300, "SEK"); got != "3.00 SEK" {
		t.Errorf("SEK: %q", got)
	}
}
