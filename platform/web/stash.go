package web

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

// stashSummary is the member's model debt in one glance: what is waiting,
// what it cost, how long the oldest kit has sat, and how the goal stands.
type stashSummary struct {
	Waiting      int // unbuilt plus building
	Built        int
	DebtPence    int64
	Oldest       string
	OldestDays   int
	Next         *store.StashItem
	GoalDone     int
	GoalDaysLeft int
}

func summarise(items []store.StashItem, user store.User, goalDone int, now time.Time) stashSummary {
	var summary stashSummary
	var oldest time.Time
	for i := range items {
		item := &items[i]
		if item.Status == "built" {
			summary.Built++
			continue
		}
		summary.Waiting++
		summary.DebtPence += item.CostPence
		if item.Next {
			summary.Next = item
		}
		if added, ok := parseStoredDate(item.AddedAt); ok && (oldest.IsZero() || added.Before(oldest)) {
			oldest = added
			summary.Oldest = item.Title
			summary.OldestDays = int(now.Sub(added).Hours() / 24)
		}
	}
	if user.GoalCount > 0 {
		summary.GoalDone = goalDone
		if by, ok := parseStoredDate(user.GoalBy); ok {
			summary.GoalDaysLeft = int(by.Sub(now.UTC().Truncate(24*time.Hour)).Hours() / 24)
		}
	}
	return summary
}

type stashData struct {
	Items    []store.StashItem
	Summary  stashSummary
	Tomorrow string
}

func (s *Server) handleStash(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	items, err := s.store.StashForUser(user.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load your stash.")
		return
	}
	summary, err := s.summaryFor(user, items)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load your stash.")
		return
	}
	data := stashData{Items: items, Summary: summary, Tomorrow: time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02")}
	s.render(w, r, "stash", "My stash", data)
}

func (s *Server) summaryFor(user store.User, items []store.StashItem) (stashSummary, error) {
	done := 0
	if user.GoalCount > 0 {
		var err error
		if done, err = s.store.FinishedSince(user.ID, user.GoalSetAt); err != nil {
			return stashSummary{}, err
		}
	}
	return summarise(items, user, done, time.Now()), nil
}

var pencePattern = regexp.MustCompile(`^(\d{1,6})(?:\.(\d{1,2}))?$`)

// parsePence reads a price like 12, 12.5, or 12.99, with or without a
// leading currency sign. Empty means free.
func parsePence(text string) (int64, bool) {
	text = strings.TrimSpace(text)
	text = strings.TrimLeft(text, "£$€")
	if text == "" {
		return 0, true
	}
	parts := pencePattern.FindStringSubmatch(text)
	if parts == nil {
		return 0, false
	}
	pounds, _ := strconv.ParseInt(parts[1], 10, 64)
	pence := int64(0)
	if parts[2] != "" {
		pence, _ = strconv.ParseInt((parts[2] + "0")[:2], 10, 64)
	}
	return pounds*100 + pence, true
}

func money(pence int64) string {
	return fmt.Sprintf("%d.%02d", pence/100, pence%100)
}

func (s *Server) handleStashAdd(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	cost, ok := parsePence(r.FormValue("cost"))
	if !ok {
		flashRedirect(w, r, "/stash", "", "Cost should be a price like 12.99, or empty.")
		return
	}
	item := store.StashItem{
		UserID: user.ID, Title: r.FormValue("title"), Brand: r.FormValue("brand"),
		Scale: r.FormValue("scale"), Note: r.FormValue("note"), CostPence: cost,
	}
	_, err := s.store.CreateStashItem(item)
	if errors.Is(err, store.ErrLimit) {
		flashRedirect(w, r, "/stash", "", fmt.Sprintf("A stash holds %d kits at most.", store.MaxStashPerUser))
		return
	}
	if err != nil {
		flashRedirect(w, r, "/stash", "", "A kit needs at least a title.")
		return
	}
	flashRedirect(w, r, "/stash", "Added to your stash.", "")
}

type stashItemData struct {
	Item    store.StashItem
	Journal []store.JournalEntry
}

func (s *Server) handleStashItem(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	item, err := s.store.StashItemFor(parseID(r.PathValue("id")), user.ID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such kit in your stash.")
		return
	}
	journal, err := s.store.JournalFor(item.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load the journal.")
		return
	}
	s.render(w, r, "stash_item", item.Title, stashItemData{Item: item, Journal: journal})
}

// handleStashAction covers the small changes to one kit: next, building,
// unbuilt, finish, delete, and journal. Each is a form button on the kit's page.
func (s *Server) handleStashAction(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	id := parseID(r.PathValue("id"))
	itemPath := fmt.Sprintf("/stash/%d", id)
	var err error
	switch r.PathValue("action") {
	case "next":
		err = s.store.SetStashNext(id, user.ID)
	case "building", "unbuilt":
		err = s.store.SetStashStatus(id, user.ID, r.PathValue("action"))
	case "journal":
		if err = s.store.AddJournalEntry(id, user.ID, r.FormValue("text")); err == nil {
			flashRedirect(w, r, itemPath, "Entry added.", "")
			return
		}
	case "finish":
		var buildID int64
		if buildID, err = s.store.FinishStashItem(id, user.ID); err == nil {
			flashRedirect(w, r, fmt.Sprintf("/builds/%d/edit", buildID), "Finished. Add photos below.", "")
			return
		}
	case "delete":
		if err = s.store.DeleteStashItem(id, user.ID); err == nil {
			flashRedirect(w, r, "/stash", "Removed from your stash.", "")
			return
		}
	default:
		s.renderError(w, r, http.StatusNotFound, "Nothing at this address.")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "No such kit in your stash.")
		return
	}
	if err != nil {
		flashRedirect(w, r, itemPath, "", "That did not save.")
		return
	}
	flashRedirect(w, r, itemPath, "Saved.", "")
}

func (s *Server) handleGoal(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	count, err := strconv.Atoi(r.FormValue("count"))
	if err != nil || count < 0 {
		flashRedirect(w, r, "/stash", "", "A goal is a number of kits and a date.")
		return
	}
	err = s.store.SetGoal(user.ID, count, r.FormValue("by"))
	if errors.Is(err, store.ErrBadDates) {
		flashRedirect(w, r, "/stash", "", "Pick a date for the goal.")
		return
	}
	if err != nil {
		flashRedirect(w, r, "/stash", "", "A goal is a number of kits and a date.")
		return
	}
	if count == 0 {
		flashRedirect(w, r, "/stash", "Goal cleared.", "")
		return
	}
	flashRedirect(w, r, "/stash", "Goal set.", "")
}
