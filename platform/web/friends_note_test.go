package web

import (
	"testing"

	"github.com/Joe-Speed/sprue/platform/store"
)

func TestFriendNoteNamesTheMember(t *testing.T) {
	cases := []struct{ action, before, want string }{
		{"request", store.FriendsNone, "Request sent! Aki will see it on their friends page."},
		{"accept", store.FriendsIncoming, "You and Aki are now friends."},
		{"remove", store.FriendsAccepted, "You and Aki are no longer friends."},
		{"remove", store.FriendsRequested, "Your request to Aki has been withdrawn."},
		{"remove", store.FriendsIncoming, "The request from Aki has been declined."},
		{"remove", store.FriendsNone, "Saved."},
	}
	for _, c := range cases {
		if got := friendNote(c.action, c.before, "Aki"); got != c.want {
			t.Errorf("%s from %q: got %q, want %q", c.action, c.before, got, c.want)
		}
	}
}
