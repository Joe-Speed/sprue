package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Joe-Speed/sprue/platform/store"
)

func TestScreenPhoto(t *testing.T) {
	verdict := "VERY_UNLIKELY"
	vision := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "secret" {
			http.Error(w, "no key", http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"responses":[{"safeSearchAnnotation":{"adult":"` + verdict + `","violence":"VERY_UNLIKELY","racy":"POSSIBLE"}}]}`))
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
