package web

import (
	"errors"
	"net/http"

	"github.com/Joe-Speed/sprue/platform/store"
)

type membersData struct {
	Query   string
	Members []store.Member
}

func (s *Server) handleMembers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	members, err := s.store.SearchMembers(query, store.MaxSearchResults)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not search members.")
		return
	}
	s.render(w, r, "members", "Members", membersData{Query: clipMessage(query), Members: members})
}

type friendsData struct {
	Requests []store.Member // waiting on the viewer
	Sent     []store.Member // waiting on the other member
	Friends  []store.Member
}

func (s *Server) handleFriends(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var data friendsData
	var err error
	if data.Requests, err = s.store.FriendRequests(user.ID); err == nil {
		if data.Sent, err = s.store.SentRequests(user.ID); err == nil {
			data.Friends, err = s.store.Friends(user.ID)
		}
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load friends.")
		return
	}
	s.render(w, r, "friends", "Friends", data)
}

// handleFriendAction changes the viewer's standing with one member:
// request, accept, or remove. Remove also cancels a sent request and
// declines an incoming one.
func (s *Server) handleFriendAction(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	other, err := s.store.UserBySlug(r.PathValue("slug"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No member by that name.")
		return
	}
	back := "/u/" + other.Slug
	if r.FormValue("back") == "/friends" {
		back = "/friends"
	}
	switch r.PathValue("action") {
	case "request":
		err = s.store.RequestFriend(user.ID, other.ID)
	case "accept":
		err = s.store.AcceptFriend(user.ID, other.ID)
	case "remove":
		err = s.store.RemoveFriend(user.ID, other.ID)
	default:
		s.renderError(w, r, http.StatusNotFound, "Nothing at this address.")
		return
	}
	switch {
	case errors.Is(err, store.ErrLimit):
		flashRedirect(w, r, back, "", "You have reached the friends limit.")
	case errors.Is(err, store.ErrInUse):
		flashRedirect(w, r, back, "", "There is already a request between you.")
	case err != nil:
		flashRedirect(w, r, back, "", "That did not save.")
	default:
		flashRedirect(w, r, back, "Saved.", "")
	}
}
