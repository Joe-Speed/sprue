package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

const voteMinGap = 5 * time.Second

// advanceCompetitions moves competitions along by date before they are shown
// or acted on. A failure is logged and the stored state is shown as is.
func (s *Server) advanceCompetitions() {
	if err := s.store.Advance(time.Now()); err != nil {
		log.Printf("web: %v", err)
	}
}

type competitionsData struct {
	Competitions []store.Competition
}

func (s *Server) handleCompetitions(w http.ResponseWriter, r *http.Request) {
	s.advanceCompetitions()
	list, err := s.store.Competitions()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load competitions.")
		return
	}
	s.render(w, r, "competitions", "Competitions", competitionsData{Competitions: list})
}

type competitionFormData struct {
	Competition store.Competition
	Tomorrow    string // earliest day entries may close
}

func (s *Server) handleCompetitionForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUser(w, r); !ok {
		return
	}
	tomorrow := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02")
	s.render(w, r, "competition_form", "Start a competition", competitionFormData{Tomorrow: tomorrow})
}

func (s *Server) handleCompetitionCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	comp := store.Competition{
		Title:        r.FormValue("title"),
		Description:  r.FormValue("description"),
		CreatorID:    user.ID,
		EntriesClose: r.FormValue("entries_close"),
		VotingCloses: r.FormValue("voting_closes"),
	}
	created, err := s.store.CreateCompetition(comp, time.Now())
	switch {
	case errors.Is(err, store.ErrBadDates):
		flashRedirect(w, r, "/competitions/new", "", fmt.Sprintf(
			"Entries must close after today and within %d days. Voting must close after entries and within %d days.",
			store.MaxEntryDays, store.MaxVotingDays))
		return
	case errors.Is(err, store.ErrLimit):
		flashRedirect(w, r, "/competitions/new", "", fmt.Sprintf("You can have %d competitions running at once.", store.MaxOpenPerCreator))
		return
	case err != nil:
		flashRedirect(w, r, "/competitions/new", "", "A competition needs a title and both dates.")
		return
	}
	flashRedirect(w, r, "/competitions/"+created.Slug, "Competition created. Entries are open.", "")
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
	s.advanceCompetitions()
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
	description := comp.Description
	if description == "" {
		description = "A scale modelling competition on sprue, started by " + comp.CreatorName + "."
	}
	s.renderMeta(w, r, http.StatusOK, "competition", comp.Title, data, meta{Description: description})
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
	s.advanceCompetitions()
	comp, err := s.store.CompetitionBySlug(r.PathValue("slug"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such competition.")
		return
	}
	page := "/competitions/" + comp.Slug
	if comp.Status != "open" {
		flashRedirect(w, r, page, "", "Entries are closed.")
		return
	}
	buildID := parseID(r.FormValue("build"))
	if err := s.store.EnterCompetition(comp.ID, buildID, user.ID); err != nil {
		flashRedirect(w, r, page, "", "Could not enter that build. One entry per member.")
		return
	}
	flashRedirect(w, r, page, "Entered.", "")
}

func (s *Server) handleVote(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	s.advanceCompetitions()
	comp, err := s.store.CompetitionBySlug(r.PathValue("slug"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such competition.")
		return
	}
	page := "/competitions/" + comp.Slug
	if comp.Status != "voting" {
		flashRedirect(w, r, page, "", "Voting is not open.")
		return
	}
	if !s.limiter.allow("vote:"+clientKey(r), voteMinGap) {
		flashRedirect(w, r, page, "", "Too many attempts. Wait a moment and try again.")
		return
	}
	entryID := parseID(r.FormValue("entry"))
	if err := s.store.Vote(comp.ID, user.ID, entryID); err != nil {
		flashRedirect(w, r, page, "", "Vote not counted. One vote each, and not for your own entry.")
		return
	}
	flashRedirect(w, r, page, "Vote recorded.", "")
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
		s.renderError(w, r, http.StatusInternalServerError, "The trophy file is missing. Contact the admin.")
		return
	}
	if err := s.store.UseTrophyDownload(trophy.ID, user.ID); err != nil {
		s.renderError(w, r, http.StatusGone, "This download has already been used. Ask the admin to re-arm it if something went wrong.")
		return
	}
	w.Header().Set("Content-Type", "model/stl")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", stlName))
	http.ServeFile(w, r, path)
}

// Admin pages. Competitions run themselves by date; the admin can only decide
// one early and re-arm trophy downloads.

type adminData struct {
	Competitions []store.Competition
	Trophies     []store.Trophy
	Reports      []store.Report
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	s.advanceCompetitions()
	comps, err := s.store.Competitions()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load competitions.")
		return
	}
	reports, err := s.store.OpenReports()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load reports.")
		return
	}
	data := adminData{Competitions: comps, Reports: reports}
	for _, comp := range comps {
		if comp.Status != "decided" {
			continue
		}
		trophies, err := s.store.TrophiesForCompetition(comp.ID)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not load trophies.")
			return
		}
		data.Trophies = append(data.Trophies, trophies...)
	}
	s.render(w, r, "admin", "Admin", data)
}

func (s *Server) handleAdminDecide(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	comp, err := s.store.CompetitionBySlug(r.PathValue("slug"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such competition.")
		return
	}
	if comp.Status != "voting" {
		flashRedirect(w, r, "/admin", "", "Only a competition in voting can be decided early.")
		return
	}
	if err := s.store.Decide(comp.ID); err != nil {
		flashRedirect(w, r, "/admin", "", "Could not decide the competition.")
		return
	}
	flashRedirect(w, r, "/admin", "Decided.", "")
}

func (s *Server) handleAdminRearm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if err := s.store.RearmTrophy(parseID(r.PathValue("id"))); err != nil {
		flashRedirect(w, r, "/admin", "", "Could not re-arm that download.")
		return
	}
	flashRedirect(w, r, "/admin", "Download re-armed.", "")
}
