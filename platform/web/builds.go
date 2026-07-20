package web

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Joe-Speed/benchtime/platform/images"
	"github.com/Joe-Speed/benchtime/platform/store"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	builds, err := s.store.RecentBuilds(24)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load the bench.")
		return
	}
	s.render(w, r, "home", "benchtime", builds)
}

type profileData struct {
	Owner    store.User
	Builds   []store.Build
	Featured []store.Build
	Trophies []store.Trophy
	IsSelf   bool
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	owner, err := s.store.UserBySlug(r.PathValue("slug"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No builder by that name.")
		return
	}
	all, err := s.store.BuildsForUser(owner.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load builds.")
		return
	}
	trophies, err := s.store.TrophiesForUser(owner.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load trophies.")
		return
	}
	data := profileData{Owner: owner, Trophies: trophies}
	for _, build := range all {
		if build.Featured {
			data.Featured = append(data.Featured, build)
		} else {
			data.Builds = append(data.Builds, build)
		}
	}
	if user, _, err := s.sessionUser(r); err == nil && user.ID == owner.ID {
		data.IsSelf = true
	}
	s.render(w, r, "profile", owner.DisplayName, data)
}

type buildFormData struct {
	Build  store.Build
	Photos []string
	IsNew  bool
}

func (s *Server) handleBuildForm(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	data := buildFormData{IsNew: true}
	if idText := r.PathValue("id"); idText != "" {
		build, err := s.store.BuildByID(parseID(idText))
		if err != nil || build.UserID != user.ID {
			s.renderError(w, r, http.StatusNotFound, "Not your build.")
			return
		}
		photos, err := s.store.Photos(build.ID)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not load photos.")
			return
		}
		data = buildFormData{Build: build, Photos: photos, IsNew: false}
	}
	s.render(w, r, "build_form", "Your build", data)
}

func parseID(text string) int64 {
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

func buildFromForm(r *http.Request, userID int64) store.Build {
	return store.Build{
		UserID:      userID,
		Title:       r.FormValue("title"),
		Kit:         r.FormValue("kit"),
		Brand:       r.FormValue("brand"),
		Scale:       r.FormValue("scale"),
		Description: r.FormValue("description"),
		BuiltOn:     r.FormValue("built_on"),
		Featured:    r.FormValue("featured") == "on",
	}
}

func (s *Server) handleBuildCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	build := buildFromForm(r, user.ID)
	id, err := s.store.CreateBuild(build)
	if errors.Is(err, store.ErrLimit) {
		s.renderError(w, r, http.StatusBadRequest, "You have reached the build limit.")
		return
	}
	if err != nil {
		http.Redirect(w, r, "/builds/new?error=A+build+needs+at+least+a+title+and+a+kit", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/builds/%d/edit?note=Saved.+Add+photos+below", id), http.StatusSeeOther)
}

func (s *Server) handleBuildUpdate(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	build := buildFromForm(r, user.ID)
	build.ID = parseID(r.PathValue("id"))
	existing, err := s.store.BuildByID(build.ID)
	if err != nil || existing.UserID != user.ID {
		s.renderError(w, r, http.StatusNotFound, "Not your build.")
		return
	}
	if err := s.store.UpdateBuild(build); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save the build.")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/builds/%d", build.ID), http.StatusSeeOther)
}

type buildPageData struct {
	Build   store.Build
	Photos  []string
	IsOwner bool
}

func (s *Server) handleBuildPage(w http.ResponseWriter, r *http.Request) {
	build, err := s.store.BuildByID(parseID(r.PathValue("id")))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such build.")
		return
	}
	photos, err := s.store.Photos(build.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load photos.")
		return
	}
	data := buildPageData{Build: build, Photos: photos}
	if user, _, err := s.sessionUser(r); err == nil && user.ID == build.UserID {
		data.IsOwner = true
	}
	s.render(w, r, "build", build.Title, data)
}

func (s *Server) handlePhotoUpload(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	build, err := s.store.BuildByID(parseID(r.PathValue("id")))
	if err != nil || build.UserID != user.ID {
		s.renderError(w, r, http.StatusNotFound, "Not your build.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, images.MaxUploadBytes+64*1024)
	file, _, err := r.FormFile("photo")
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/builds/%d/edit?error=Choose+a+photo+first", build.ID), http.StatusSeeOther)
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, images.MaxUploadBytes+1))
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Upload failed.")
		return
	}
	processed, err := images.Process(raw)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/builds/%d/edit?error=That+file+is+not+a+usable+photo+(8MB+max,+jpeg+or+png)", build.ID), http.StatusSeeOther)
		return
	}
	name, err := randomToken()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Upload failed.")
		return
	}
	fileName := name[:24] + ".jpg"
	dir := filepath.Join(s.config.DataDir, "photos", strconv.FormatInt(build.ID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Upload failed.")
		return
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), processed, 0o644); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Upload failed.")
		return
	}
	if err := s.store.AddPhoto(build.ID, fileName); err != nil {
		if removeErr := os.Remove(filepath.Join(dir, fileName)); removeErr != nil {
			log.Printf("web: orphan photo cleanup: %v", removeErr)
		}
		message := "Could+not+save+the+photo"
		if errors.Is(err, store.ErrLimit) {
			message = "Photo+limit+reached+for+this+build"
		}
		http.Redirect(w, r, fmt.Sprintf("/builds/%d/edit?error=%s", build.ID, message), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/builds/%d/edit?note=Photo+added", build.ID), http.StatusSeeOther)
}

var photoNamePattern = regexp.MustCompile(`^[0-9a-f]{24}\.jpg$`)

func (s *Server) handlePhoto(w http.ResponseWriter, r *http.Request) {
	buildID := parseID(r.PathValue("build"))
	name := r.PathValue("name")
	if buildID == 0 || !photoNamePattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.config.DataDir, "photos", strconv.FormatInt(buildID, 10), name)
	if strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}
