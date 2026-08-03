package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

const voteMinGap = 5 * time.Second

func (s *Server) handleCompetitions(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Competitions()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load competitions.")
		return
	}
	s.render(w, r, "competitions", "Competitions", list)
}

type competitionData struct {
	Competition store.Competition
	Entries     []store.Entry
	Trophies    []store.Trophy
	MyBuilds    []store.Build
	CanEnter    bool
	CanVote     bool
	HasVoted    bool
	ShowVotes   bool
}

func (s *Server) handleCompetition(w http.ResponseWriter, r *http.Request) {
	comp, err := s.store.CompetitionBySlug(r.PathValue("slug"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such competition.")
		return
	}
	entries, err := s.store.EntriesWithVotes(comp.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load entries.")
		return
	}
	data := competitionData{Competition: comp, Entries: entries, ShowVotes: comp.Status == "decided"}
	if comp.Status == "decided" {
		trophies, err := s.store.TrophiesForCompetition(comp.ID)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not load results.")
			return
		}
		data.Trophies = trophies
	}
	if user, _, err := s.sessionUser(r); err == nil {
		s.fillViewerState(&data, user)
	}
	s.render(w, r, "competition", comp.Title, data)
}

func (s *Server) fillViewerState(data *competitionData, user store.User) {
	entered := false
	for _, entry := range data.Entries {
		if entry.Build.UserID == user.ID {
			entered = true
			break
		}
	}
	switch data.Competition.Status {
	case "open":
		if !entered {
			builds, err := s.store.BuildsForUser(user.ID)
			if err == nil && len(builds) > 0 {
				data.MyBuilds = builds
				data.CanEnter = true
			}
		}
	case "voting":
		voted, err := s.store.HasVoted(data.Competition.ID, user.ID)
		if err == nil {
			data.HasVoted = voted
			data.CanVote = !voted
		}
	}
}

func (s *Server) handleEnter(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	comp, err := s.store.CompetitionBySlug(r.PathValue("slug"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such competition.")
		return
	}
	if comp.Status != "open" {
		http.Redirect(w, r, "/competitions/"+comp.Slug+"?error=Entries+are+closed", http.StatusSeeOther)
		return
	}
	buildID := parseID(r.FormValue("build"))
	if err := s.store.EnterCompetition(comp.ID, buildID, user.ID); err != nil {
		http.Redirect(w, r, "/competitions/"+comp.Slug+"?error=Could+not+enter+that+build", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/competitions/"+comp.Slug+"?note=You+are+in.+Good+luck", http.StatusSeeOther)
}

func (s *Server) handleVote(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	comp, err := s.store.CompetitionBySlug(r.PathValue("slug"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such competition.")
		return
	}
	if comp.Status != "voting" {
		http.Redirect(w, r, "/competitions/"+comp.Slug+"?error=Voting+is+not+open", http.StatusSeeOther)
		return
	}
	if !s.limiter.allow("vote:"+clientKey(r), voteMinGap) {
		http.Redirect(w, r, "/competitions/"+comp.Slug+"?error=Slow+down+a+moment", http.StatusSeeOther)
		return
	}
	entryID := parseID(r.FormValue("entry"))
	if err := s.store.Vote(comp.ID, user.ID, entryID); err != nil {
		http.Redirect(w, r, "/competitions/"+comp.Slug+"?error=That+vote+did+not+count:+one+vote+each,+and+not+for+yourself", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/competitions/"+comp.Slug+"?note=Vote+recorded", http.StatusSeeOther)
}

func (s *Server) handleTrophyDownload(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	trophy, err := s.store.TrophyByID(parseID(r.PathValue("id")))
	if err != nil || trophy.UserID != user.ID {
		s.renderError(w, r, http.StatusNotFound, "That trophy is not yours to download.")
		return
	}
	stlNames := map[int]string{1: "first-place.stl", 2: "second-place.stl", 3: "third-place.stl"}
	stlName := stlNames[trophy.Place]
	if stlName == "" {
		s.renderError(w, r, http.StatusInternalServerError, "Bad trophy record.")
		return
	}
	path := filepath.Join(s.config.DataDir, "stl", stlName)
	if _, err := os.Stat(path); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "The trophy file is missing. Tell Joe.")
		return
	}
	if err := s.store.UseTrophyDownload(trophy.ID, user.ID); err != nil {
		s.renderError(w, r, http.StatusGone, "This download has been used. Ask Joe to re-arm it if something went wrong.")
		return
	}
	w.Header().Set("Content-Type", "model/stl")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", stlName))
	http.ServeFile(w, r, path)
}

type adminData struct {
	Competitions []store.Competition
	Trophies     []store.Trophy
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	_, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	comps, err := s.store.Competitions()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load competitions.")
		return
	}
	data := adminData{Competitions: comps}
	for _, comp := range comps {
		if comp.Status == "decided" {
			trophies, err := s.store.TrophiesForCompetition(comp.ID)
			if err == nil {
				data.Trophies = append(data.Trophies, trophies...)
			}
		}
	}
	s.render(w, r, "admin", "Admin", data)
}

func (s *Server) handleAdminCreateCompetition(w http.ResponseWriter, r *http.Request) {
	_, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	_, err := s.store.CreateCompetition(r.FormValue("title"), r.FormValue("deadline"))
	if err != nil {
		http.Redirect(w, r, "/admin?error=Could+not+create+the+competition", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin?note=Competition+created", http.StatusSeeOther)
}

func (s *Server) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	_, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	comp, err := s.store.CompetitionBySlug(r.PathValue("slug"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such competition.")
		return
	}
	status := r.FormValue("status")
	if status == "decided" {
		if err := s.store.Decide(comp.ID); err != nil {
			http.Redirect(w, r, "/admin?error=Could+not+decide:+are+there+any+votes?", http.StatusSeeOther)
			return
		}
	} else if err := s.store.SetCompetitionStatus(comp.ID, status); err != nil {
		http.Redirect(w, r, "/admin?error=Bad+status", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin?note=Updated", http.StatusSeeOther)
}

func (s *Server) handleAdminRearm(w http.ResponseWriter, r *http.Request) {
	_, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := s.store.RearmTrophy(parseID(r.PathValue("id"))); err != nil {
		http.Redirect(w, r, "/admin?error=Could+not+re-arm", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin?note=Download+re-armed", http.StatusSeeOther)
}
