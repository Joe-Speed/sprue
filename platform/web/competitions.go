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

// theme is a fixed brief for a sprue competition. Members run their own
// competitions freely; these are the site's own. Each starts on the first
// day of its kind every month, runs three weeks of entries and one of voting,
// and every entrant confirms their build meets the brief.
type theme struct {
	Key      string
	Title    string
	Brief    string
	Category string
	Weekday  time.Weekday
}

var themes = []theme{
	{"big-bomber", "Big Bomber", "A Second World War heavy bomber: four engines, any air force, any scale. Lancaster, Halifax, B-17, B-24, Stirling, He 177 and their kind.", "heavy bomber", time.Saturday},
	{"moderate-mitchell", "Moderate Mitchell", "A Second World War medium or light bomber: twin engines, any air force, any scale. Mitchell, Mosquito, Ju 88, Blenheim, A-20, Wellington and their kind.", "medium bomber", time.Monday},
	{"fighting-friday", "Fighting Friday", "A Second World War fighter: single seat, any air force, any scale. Spitfire, Hurricane, Bf 109, P-51, Zero, Yak and their kind.", "fighter", time.Friday},
	{"tanktastic", "Tanktastic", "A Second World War tank or self-propelled gun, any army, any scale. Sherman, Tiger, T-34, Churchill, Panther, Cromwell and their kind.", "tank", time.Tuesday},
}

const (
	themeEntryDays  = 21
	themeVotingDays = 7
)

// dueThemes lists the briefs whose competition starts today: the first
// occurrence of their weekday in the month.
func dueThemes(today time.Time) []theme {
	var due []theme
	if today.Day() > 7 {
		return due
	}
	for _, t := range themes {
		if t.Weekday == today.Weekday() {
			due = append(due, t)
		}
	}
	return due
}

// startScheduledCompetitions opens today's sprue competitions if they have
// not been started this month. Without an admin account nothing starts.
func (s *Server) startScheduledCompetitions(today time.Time) {
	today = today.UTC()
	due := dueThemes(today)
	if len(due) == 0 {
		return
	}
	admin, err := s.store.AdminUser()
	if err != nil {
		return
	}
	for _, t := range due {
		started, err := s.store.ThemeStartedInMonth(t.Key, today.Format("2006-01"))
		if err != nil || started {
			continue
		}
		comp := store.Competition{
			Title:        t.Title + ", " + today.Format("January 2006"),
			Description:  t.Brief,
			CreatorID:    admin.ID,
			EntriesClose: today.AddDate(0, 0, themeEntryDays).Format("2006-01-02"),
			VotingCloses: today.AddDate(0, 0, themeEntryDays+themeVotingDays).Format("2006-01-02"),
			Official:     true,
			Theme:        t.Key,
			Category:     t.Category,
		}
		if _, err := s.store.CreateCompetition(comp, today); err != nil {
			log.Printf("web: scheduled %s: %v", t.Key, err)
		}
	}
}

// trophyFiles maps a placing to the STL file in the data directory.
var trophyFiles = [...]string{1: "first-place.stl", 2: "second-place.stl", 3: "third-place.stl"}

// advanceCompetitions moves competitions along by date before they are shown
// or acted on. A failure is logged and the stored state is shown as is.
func (s *Server) advanceCompetitions() {
	if err := s.store.Advance(time.Now()); err != nil {
		log.Printf("web: %v", err)
	}
}

type competitionsData struct {
	Competitions []store.Competition
	Categories   []string
	Category     string // the filter in force, empty for all
}

func (s *Server) handleCompetitions(w http.ResponseWriter, r *http.Request) {
	s.advanceCompetitions()
	category := r.URL.Query().Get("category")
	list, err := s.store.Competitions(category)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load competitions.")
		return
	}
	data := competitionsData{Competitions: list, Categories: store.Categories}
	for _, known := range store.Categories {
		if category == known {
			data.Category = category
		}
	}
	s.render(w, r, "competitions", "Competitions", data)
}

// pastCompetition is a decided competition with its podium.
type pastCompetition struct {
	Competition store.Competition
	Trophies    []store.Trophy
}

const pastPageSize = 24

func (s *Server) handlePastCompetitions(w http.ResponseWriter, r *http.Request) {
	s.advanceCompetitions()
	decided, err := s.store.DecidedCompetitions(pastPageSize)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load past competitions.")
		return
	}
	past := make([]pastCompetition, 0, len(decided))
	for _, comp := range decided {
		trophies, err := s.store.TrophiesForCompetition(comp.ID)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not load past competitions.")
			return
		}
		past = append(past, pastCompetition{Competition: comp, Trophies: trophies})
	}
	s.render(w, r, "past", "Past winners", past)
}

type competitionFormData struct {
	Competition store.Competition
	Tomorrow    string // earliest day entries may close
	Categories  []string
}

func (s *Server) handleCompetitionForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUser(w, r); !ok {
		return
	}
	data := competitionFormData{Tomorrow: time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02"), Categories: store.Categories}
	s.render(w, r, "competition_form", "Start a competition", data)
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
		Category:     r.FormValue("category"),
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
		flashRedirect(w, r, "/competitions/new", "", "A competition needs a title, a category, and both dates.")
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
	TopVotes    int  // votes on the leading entry, the scale for the poll meters
	CanModerate bool // admin, while the competition is still running
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
	for _, entry := range entries {
		if entry.Votes > data.TopVotes {
			data.TopVotes = entry.Votes
		}
	}
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
		data.CanModerate = user.IsAdmin && comp.Status != "decided"
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
			if err != nil {
				return
			}
			for _, build := range builds {
				if !build.Private && !build.Hidden {
					data.MyBuilds = append(data.MyBuilds, build)
				}
			}
			data.CanEnter = len(data.MyBuilds) > 0
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
	if r.FormValue("confirm") != "on" {
		flashRedirect(w, r, page, "", "Tick the box to confirm your build meets the brief.")
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
	stlName := ""
	if trophy.Place >= 1 && trophy.Place < len(trophyFiles) {
		stlName = trophyFiles[trophy.Place]
	}
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
	Donations    []store.Donation
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	s.advanceCompetitions()
	comps, err := s.store.Competitions("")
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
	if s.config.KofiURL != "" {
		data.Donations, err = s.store.RecentDonations()
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not load donations.")
			return
		}
	}
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

// handleAdminRemoveEntry takes an entry out of a running competition when it
// does not meet the brief.
func (s *Server) handleAdminRemoveEntry(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	comp, err := s.store.CompetitionBySlug(r.PathValue("slug"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such competition.")
		return
	}
	page := "/competitions/" + comp.Slug
	switch err := s.store.RemoveEntry(comp.ID, parseID(r.PathValue("id"))); {
	case errors.Is(err, store.ErrInUse):
		flashRedirect(w, r, page, "", "The competition has been decided, so entries cannot be removed.")
	case err != nil:
		flashRedirect(w, r, page, "", "No such entry.")
	default:
		flashRedirect(w, r, page, "Entry removed.", "")
	}
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
