package web

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/Joe-Speed/sprue/platform/images"
	"github.com/Joe-Speed/sprue/platform/store"
)

// homePageSize is how many builds the front page shows before offering the
// next older page.
const homePageSize = 24

type homeData struct {
	Featured []store.Build // most voted in the featured window, shown on the first page only
	Builds   []store.Build
	Older    int64 // ID to page from, zero when this is the last page
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	before := parseID(r.URL.Query().Get("before"))
	builds, err := s.store.RecentBuilds(before, homePageSize)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load the community page.")
		return
	}
	data := homeData{Builds: builds}
	if len(builds) == homePageSize {
		data.Older = builds[len(builds)-1].ID
	}
	if before == 0 {
		since := time.Now().Add(-store.FeaturedWindowDays * 24 * time.Hour)
		data.Featured, err = s.store.FeaturedBuilds(since, store.MaxFeatured)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not load the community page.")
			return
		}
	}
	s.render(w, r, "home", "The community", data)
}

type profileData struct {
	Owner      store.User
	Builds     []store.Build
	Pinned     []store.Build
	Trophies   []store.Trophy
	BuildCount int
	IsSelf     bool
	Friendship string // viewer's standing with the owner, empty when none or signed out
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
	var viewer *store.User
	if user, _, err := s.sessionUser(r); err == nil {
		viewer = &user
	}
	data := profileData{Owner: owner, Trophies: trophies, IsSelf: viewer != nil && viewer.ID == owner.ID}
	if viewer != nil && !data.IsSelf {
		if data.Friendship, err = s.store.Friendship(viewer.ID, owner.ID); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not load the profile.")
			return
		}
	}
	for _, build := range all {
		if !visibleTo(build, viewer) {
			continue
		}
		data.BuildCount++
		if build.Pinned {
			data.Pinned = append(data.Pinned, build)
		} else {
			data.Builds = append(data.Builds, build)
		}
	}
	description := fmt.Sprintf("%d build%s by %s on sprue.", data.BuildCount, plural(data.BuildCount), owner.DisplayName)
	s.renderMeta(w, r, http.StatusOK, "profile", owner.DisplayName, data, meta{Description: description})
}

// handleMyBuilds lists everything the member has added, whatever its state,
// with a way into each one's edit page.
func (s *Server) handleMyBuilds(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	builds, err := s.store.BuildsForUser(user.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load your builds.")
		return
	}
	s.render(w, r, "builds", "My builds", builds)
}

type buildFormData struct {
	Build     store.Build
	Photos    []string
	IsNew     bool
	CanDelete bool // false once the build has entered a competition
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
		entered, err := s.store.BuildHasEntries(build.ID)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not load the build.")
			return
		}
		data = buildFormData{Build: build, Photos: photos, IsNew: false, CanDelete: !entered}
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
		Pinned:      r.FormValue("pinned") == "on",
		Private:     r.FormValue("private") == "on",
	}
}

// handleBuildCreate takes the details and, when the form is multipart, any
// photos in the same submission, so a build can go up in one step.
func (s *Server) handleBuildCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotosPerUpload*images.MaxUploadBytes+64*1024)
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
		flashRedirect(w, r, "/builds/new", "", "A build needs at least a title and a kit.")
		return
	}
	var files []*multipart.FileHeader
	if r.MultipartForm != nil {
		files = r.MultipartForm.File["photos"]
	}
	if len(files) == 0 {
		flashRedirect(w, r, buildEdit(id), "Saved. Add photos below.", "")
		return
	}
	if len(files) > maxPhotosPerUpload {
		files = files[:maxPhotosPerUpload]
	}
	added, failure := s.savePhotos(id, files)
	note := "Posted."
	if build.Private {
		note = "Saved to your profile."
	}
	flashRedirect(w, r, buildPage(id), note, uploadError(added, len(files), failure))
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
	if err := s.store.UpdateBuild(build); errors.Is(err, store.ErrInUse) {
		flashRedirect(w, r, buildEdit(build.ID), "", "This build is in a competition, so it stays public.")
		return
	} else if err != nil {
		flashRedirect(w, r, buildEdit(build.ID), "", "A build needs at least a title and a kit.")
		return
	}
	http.Redirect(w, r, buildPage(build.ID), http.StatusSeeOther)
}

type buildPageData struct {
	Build   store.Build
	Photos  []string
	IsOwner bool
	CanVote bool // signed in and not the owner; also allows reporting
	Voted   bool
}

func (s *Server) handleBuildPage(w http.ResponseWriter, r *http.Request) {
	build, err := s.store.BuildByID(parseID(r.PathValue("id")))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such build.")
		return
	}
	var viewer *store.User
	if user, _, err := s.sessionUser(r); err == nil {
		viewer = &user
	}
	if !visibleTo(build, viewer) {
		s.renderError(w, r, http.StatusNotFound, "No such build.")
		return
	}
	photos, err := s.store.Photos(build.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load photos.")
		return
	}
	data := buildPageData{Build: build, Photos: photos}
	if viewer != nil {
		data.IsOwner = viewer.ID == build.UserID
		data.CanVote = !data.IsOwner && !build.Private
		if voted, err := s.store.HasVotedBuild(build.ID, viewer.ID); err == nil {
			data.Voted = voted
		}
	}
	m := meta{Description: fmt.Sprintf("%s, built by %s.", build.Kit, build.OwnerName)}
	if build.CoverPhoto != "" {
		m.Image = s.absolute(fmt.Sprintf("/photos/%d/%s", build.ID, build.CoverPhoto))
	}
	s.renderMeta(w, r, http.StatusOK, "build", build.Title, data, m)
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// handleBuildVote toggles the caller's vote for a build.
func (s *Server) handleBuildVote(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	build, err := s.store.BuildByID(parseID(r.PathValue("id")))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such build.")
		return
	}
	page := buildPage(build.ID)
	if !visibleTo(build, &user) || build.Private {
		s.renderError(w, r, http.StatusNotFound, "No such build.")
		return
	}
	if build.UserID == user.ID {
		flashRedirect(w, r, page, "", "You cannot vote for your own build.")
		return
	}
	voted, err := s.store.HasVotedBuild(build.ID, user.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not record the vote.")
		return
	}
	if voted {
		err = s.store.UnvoteBuild(build.ID, user.ID)
	} else {
		err = s.store.VoteBuild(build.ID, user.ID)
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not record the vote.")
		return
	}
	if voted {
		flashRedirect(w, r, page, "Vote removed.", "")
		return
	}
	flashRedirect(w, r, page, "Vote recorded.", "")
}

// maxPhotosPerUpload bounds one request. A build holds MaxPhotosPerBuild
// in total, enforced by the store.
const maxPhotosPerUpload = 6

var errUnusablePhoto = errors.New("web: unusable photo")

func (s *Server) handlePhotoUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotosPerUpload*images.MaxUploadBytes+64*1024)
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	build, err := s.store.BuildByID(parseID(r.PathValue("id")))
	if err != nil || build.UserID != user.ID {
		s.renderError(w, r, http.StatusNotFound, "Not your build.")
		return
	}
	editPath := buildEdit(build.ID)
	if r.MultipartForm == nil {
		flashRedirect(w, r, editPath, "", "Choose a photo first.")
		return
	}
	files := r.MultipartForm.File["photos"]
	if len(files) == 0 {
		flashRedirect(w, r, editPath, "", "Choose a photo first.")
		return
	}
	if len(files) > maxPhotosPerUpload {
		flashRedirect(w, r, editPath, "", fmt.Sprintf("Up to %d photos at a time.", maxPhotosPerUpload))
		return
	}
	added, failure := s.savePhotos(build.ID, files)
	flashRedirect(w, r, editPath, uploadNote(added), uploadError(added, len(files), failure))
}

// savePhotos stores files in order and stops at the first failure, returning
// how many made it and why it stopped.
func (s *Server) savePhotos(buildID int64, files []*multipart.FileHeader) (int, error) {
	added := 0
	for _, header := range files {
		if err := s.savePhoto(buildID, header); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

func uploadNote(added int) string {
	if added == 1 {
		return "Photo added."
	}
	return fmt.Sprintf("%d photos added.", added)
}

func uploadError(added, wanted int, failure error) string {
	if failure == nil {
		return ""
	}
	reason := "Could not save the photo."
	switch {
	case errors.Is(failure, store.ErrLimit):
		reason = fmt.Sprintf("This build holds %d photos at most.", store.MaxPhotosPerBuild)
	case errors.Is(failure, errUnusablePhoto):
		reason = "That file is not a usable photo. JPEG or PNG up to 15MB; iPhone HEIC photos need converting first."
	case errors.Is(failure, errUnsafePhoto):
		reason = "That photo was refused by the image check."
	case errors.Is(failure, errScreenUnavailable):
		reason = "The image check did not answer. Try again in a moment."
	case errors.Is(failure, errScreenBudget):
		reason = "Photo checks have reached this month's limit. Uploads reopen next month."
	}
	if added == 0 {
		return reason
	}
	return fmt.Sprintf("Added %d of %d. %s", added, wanted, reason)
}

// savePhoto processes one upload, writes it under the build's photo folder,
// and records it. A failed record removes the file again.
func (s *Server) savePhoto(buildID int64, header *multipart.FileHeader) error {
	file, err := header.Open()
	if err != nil {
		return err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, images.MaxUploadBytes+1))
	if err != nil {
		return err
	}
	processed, err := images.Process(raw)
	if err != nil {
		return errUnusablePhoto
	}
	if err := s.screenPhoto(processed); err != nil {
		return err
	}
	name, err := randomToken()
	if err != nil {
		return err
	}
	fileName := name[:24] + ".jpg"
	dir := filepath.Join(s.config.DataDir, "photos", strconv.FormatInt(buildID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, processed, 0o644); err != nil {
		return err
	}
	if err := s.store.AddPhoto(buildID, fileName); err != nil {
		if removeErr := os.Remove(path); removeErr != nil {
			log.Printf("web: orphan photo cleanup: %v", removeErr)
		}
		return err
	}
	return nil
}

// ownedBuildPhoto loads the build in the path, checks the caller owns it and
// the photo name is well formed, and returns both.
func (s *Server) ownedBuildPhoto(w http.ResponseWriter, r *http.Request) (store.Build, string, bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return store.Build{}, "", false
	}
	build, err := s.store.BuildByID(parseID(r.PathValue("id")))
	if err != nil || build.UserID != user.ID {
		s.renderError(w, r, http.StatusNotFound, "Not your build.")
		return store.Build{}, "", false
	}
	name := r.PathValue("name")
	if !photoNamePattern.MatchString(name) {
		s.renderError(w, r, http.StatusNotFound, "No such photo.")
		return store.Build{}, "", false
	}
	return build, name, true
}

func (s *Server) handlePhotoCover(w http.ResponseWriter, r *http.Request) {
	build, name, ok := s.ownedBuildPhoto(w, r)
	if !ok {
		return
	}
	editPath := buildEdit(build.ID)
	if err := s.store.SetCoverPhoto(build.ID, name); err != nil {
		flashRedirect(w, r, editPath, "", "Could not change the cover.")
		return
	}
	flashRedirect(w, r, editPath, "Cover changed.", "")
}

func (s *Server) handlePhotoDelete(w http.ResponseWriter, r *http.Request) {
	build, name, ok := s.ownedBuildPhoto(w, r)
	if !ok {
		return
	}
	editPath := buildEdit(build.ID)
	if err := s.store.RemovePhoto(build.ID, name); err != nil {
		flashRedirect(w, r, editPath, "", "Could not remove that photo.")
		return
	}
	s.removePhotoFiles(build.ID, []string{name})
	flashRedirect(w, r, editPath, "Photo removed.", "")
}

// removePhotoFiles deletes photo files after their records are gone.
// Failures are logged; there is nothing useful to show the user.
func (s *Server) removePhotoFiles(buildID int64, names []string) {
	dir := filepath.Join(s.config.DataDir, "photos", strconv.FormatInt(buildID, 10))
	for _, name := range names {
		if !photoNamePattern.MatchString(name) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("web: remove photo %s/%s: %v", dir, name, err)
		}
	}
	// The folder goes only when it is empty; a folder with photos left in it
	// refuses, which is the normal case and not worth logging.
	_ = os.Remove(dir)
}

func (s *Server) handleBuildDelete(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	id := parseID(r.PathValue("id"))
	editPath := buildEdit(id)
	if r.FormValue("confirm") != "on" {
		flashRedirect(w, r, editPath, "", "Tick the box to confirm.")
		return
	}
	photos, err := s.store.DeleteBuild(id, user.ID)
	if errors.Is(err, store.ErrInUse) {
		flashRedirect(w, r, editPath, "", "This build is entered in a competition and cannot be deleted.")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Not your build.")
		return
	}
	s.removePhotoFiles(id, photos)
	flashRedirect(w, r, "/u/"+user.Slug, "Build deleted.", "")
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
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}
