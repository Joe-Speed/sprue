package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/Joe-Speed/sprue/platform/store"
)

type membersData struct {
	Query   string
	Members []store.Member
	Capped  bool // the search filled the page, so there may be more to find
}

func (s *Server) handleMembers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	members, err := s.store.SearchMembers(query, store.MaxSearchResults)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not search members.")
		return
	}
	data := membersData{Query: clipMessage(query), Members: members, Capped: len(members) == store.MaxSearchResults}
	s.render(w, r, "members", "Members", data)
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
		if err == nil {
			s.tellAboutRequest(user, other)
		}
	case "accept":
		err = s.store.AcceptFriend(user.ID, other.ID)
	case "remove":
		err = s.store.RemoveFriend(user.ID, other.ID)
	default:
		s.renderError(w, r, http.StatusNotFound, "Page not found.")
		return
	}
	switch {
	case errors.Is(err, store.ErrLimit):
		flashRedirect(w, r, back, "", "You have reached the friends limit.")
	case errors.Is(err, store.ErrInUse):
		flashRedirect(w, r, back, "", "There is already a request between you.")
	case err != nil:
		flashRedirect(w, r, back, "", "Could not save.")
	default:
		flashRedirect(w, r, back, "Saved.", "")
	}
}

// tellAboutRequest emails the member who has been asked. A friend request
// is invisible until they next visit, so without this it may never be seen.
// A failure is logged and nothing else: the request itself already stands.
func (s *Server) tellAboutRequest(from, to store.User) {
	if !s.mailConfigured() {
		return
	}
	if err := s.sendMail(to.Email, mailMessage{
		Subject: from.DisplayName + " wants to be friends on sprue",
		Intro:   []string{fmt.Sprintf("Hello %s,", to.DisplayName), fmt.Sprintf("%s has asked to be friends. Friend lists are private, so only the two of you see it.", from.DisplayName)},
		Action:  "Open your requests",
		Link:    s.absolute("/friends"),
	}); err != nil {
		log.Printf("web: friend request mail: %v", err)
	}
}
