package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"encoding/json"
	"github.com/Joe-Speed/sprue/platform/store"
	"io"
	"strings"
)

func TestScreenPhoto(t *testing.T) {
	verdict := "VERY_UNLIKELY"
	asked, calls := 0, 0
	vision := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "secret" {
			http.Error(w, "no key", http.StatusForbidden)
			return
		}
		calls++
		var sent struct {
			Requests []any `json:"requests"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &sent)
		asked = len(sent.Requests)
		answers := make([]string, 0, asked)
		for i := 0; i < asked; i++ {
			answers = append(answers, `{"safeSearchAnnotation":{"adult":"`+verdict+`","violence":"VERY_UNLIKELY","racy":"POSSIBLE"}}`)
		}
		w.Write([]byte(`{"responses":[` + strings.Join(answers, ",") + `]}`))
	}))
	defer vision.Close()
	visionEndpoint = vision.URL
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server, err := New(st, Config{DataDir: "x", BaseURL: "https://sprue.test", VisionKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.screenPhoto([]byte("jpeg")); err != nil {
		t.Errorf("clean photo refused: %v", err)
	}
	verdict = "LIKELY"
	if err := server.screenPhoto([]byte("jpeg")); err != errUnsafePhoto {
		t.Errorf("adult photo accepted: %v", err)
	}
	server.config.VisionKey = "wrong"
	if err := server.screenPhoto([]byte("jpeg")); err != errScreenUnavailable {
		t.Errorf("failed check should refuse, got %v", err)
	}
	// A whole upload goes in one call, and one bad photo refuses the lot.
	server.config.VisionKey = "secret"
	verdict = "VERY_UNLIKELY"
	before := calls
	if err := server.screenPhotos([][]byte{[]byte("a"), []byte("b"), []byte("c")}); err != nil {
		t.Errorf("a clean batch should pass: %v", err)
	}
	if asked != 3 {
		t.Errorf("three photos should be one call carrying three, got %d", asked)
	}
	if calls != before+1 {
		t.Errorf("the batch should be a single call, it made %d", calls-before)
	}
	verdict = "LIKELY"
	if err := server.screenPhotos([][]byte{[]byte("a"), []byte("b")}); err != errUnsafePhoto {
		t.Errorf("one refused photo should refuse the upload, got %v", err)
	}
	if err := server.screenPhotos(make([][]byte, maxScreenBatch+1)); err == nil {
		t.Error("more photos than the checker takes at once should be an error")
	}
	server.config.VisionKey = "secret"
	verdict = "VERY_UNLIKELY"
	used := 3
	for i := used; i < maxScreensPerMonth; i++ {
		if _, err := st.TakeMonthly("vision", maxScreensPerMonth, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.screenPhoto([]byte("jpeg")); err != errScreenBudget {
		t.Errorf("spent budget should pause checks, got %v", err)
	}
	server.config.VisionKey = ""
	if err := server.screenPhoto([]byte("jpeg")); err != nil {
		t.Errorf("no key should skip the check: %v", err)
	}
}

func TestHiddenBuildVisibility(t *testing.T) {
	owner := &store.User{ID: 1}
	admin := &store.User{ID: 9, IsAdmin: true}
	other := &store.User{ID: 2}
	shown := store.Build{UserID: 1}
	hidden := store.Build{UserID: 1, Hidden: true}
	if !visibleTo(shown, nil) || !visibleTo(shown, other) {
		t.Error("normal builds are public")
	}
	if visibleTo(hidden, nil) || visibleTo(hidden, other) {
		t.Error("hidden builds leak")
	}
	if !visibleTo(hidden, owner) || !visibleTo(hidden, admin) {
		t.Error("owner and admin must still see a hidden build")
	}
	private := store.Build{UserID: 1, Private: true}
	if visibleTo(private, nil) || visibleTo(private, other) || visibleTo(private, admin) {
		t.Error("private builds are for the owner only")
	}
	if !visibleTo(private, owner) {
		t.Error("owner must see their private build")
	}
}

func TestPhotosFollowBuildVisibility(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	user, err := st.FindOrCreateUser("owner@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	build, err := st.CreateBuild(store.Build{UserID: user.ID, Title: "Hurricane", Kit: "Airfix 1/48"})
	if err != nil {
		t.Fatal(err)
	}
	name := "0123456789abcdef01234567.jpg"
	if err := st.AddPhoto(build, name); err != nil {
		t.Fatal(err)
	}
	photoDir := filepath.Join(dir, "photos", "1")
	if err := os.MkdirAll(photoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photoDir, name), []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := New(st, Config{DataDir: dir, BaseURL: "https://sprue.test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	photo := ts.URL + "/photos/1/" + name
	res, err := http.Get(photo)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("public build photo: %d", res.StatusCode)
	}
	if err := st.SetBuildHidden(build, true); err != nil {
		t.Fatal(err)
	}
	res, err = http.Get(photo)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("hidden build photo should stop serving to strangers, got %d", res.StatusCode)
	}
}
