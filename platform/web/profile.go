package web

import (
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Joe-Speed/sprue/platform/images"
)

// avatarSize is the side of the square profile picture in pixels.
const avatarSize = 256

// flairs are the small pictures a member can show beside their name, from
// Kenney's Pixel Shmup pack (CC0). Each has a matching static/flair/<name>.png.
// The first twelve are planes; a new member gets one of those by default.
var flairs = []string{
	"plane-blue-1", "plane-red-1", "plane-green-1", "plane-yellow-1",
	"plane-blue-2", "plane-red-2", "plane-green-2", "plane-yellow-2",
	"plane-blue-3", "plane-red-3", "plane-green-3", "plane-yellow-3",
	"bomb-1", "bomb-2", "bomb-3", "bomb-4", "bomb-5",
}

const defaultFlairs = 12

// trophyFlairs are the placement badges, wearable only by members who have
// won that placing. The index is the place.
var trophyFlairs = [...]string{1: "first-place", 2: "second-place", 3: "third-place"}

func validFlair(name string) bool {
	for _, known := range flairs {
		if name == known {
			return true
		}
	}
	return trophyPlace(name) != 0
}

// trophyPlace returns the placing a trophy flair stands for, or zero.
func trophyPlace(name string) int {
	for place := 1; place < len(trophyFlairs); place++ {
		if trophyFlairs[place] == name {
			return place
		}
	}
	return 0
}

// flairPath returns the picture for a member: their chosen flair, or a plane
// picked by their ID so every name has one from the start.
func flairPath(name string, userID int64) string {
	if place := trophyPlace(name); place != 0 {
		return placeBadge(place)
	}
	if !validFlair(name) {
		name = flairs[int(userID%defaultFlairs)]
	}
	return staticPath("flair/" + name + ".png")
}

// wonPlaces reports which placings a member has trophies for, indexed by place.
func (s *Server) wonPlaces(userID int64) ([4]bool, error) {
	var won [4]bool
	trophies, err := s.store.TrophiesForUser(userID)
	if err != nil {
		return won, err
	}
	for _, trophy := range trophies {
		if trophy.Place >= 1 && trophy.Place < len(won) {
			won[trophy.Place] = true
		}
	}
	return won, nil
}

func (s *Server) avatarDir() string {
	return filepath.Join(s.config.DataDir, "avatars")
}

func (s *Server) handleAvatarUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, images.MaxUploadBytes+64*1024)
	extendReadDeadline(w)
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		flashRedirect(w, r, "/settings", "", "Choose a picture first.")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, images.MaxUploadBytes+1))
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Upload failed.")
		return
	}
	processed, err := images.ProcessSquare(raw, avatarSize)
	if err != nil {
		flashRedirect(w, r, "/settings", "", "That file is not a usable picture. JPEG or PNG up to 15MB; iPhone HEIC photos need converting first.")
		return
	}
	if err := s.screenPhoto(processed); err != nil {
		flashRedirect(w, r, "/settings", "", uploadError(0, 1, err))
		return
	}
	name, err := randomToken()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Upload failed.")
		return
	}
	fileName := name[:24] + ".jpg"
	if err := os.MkdirAll(s.avatarDir(), 0o755); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Upload failed.")
		return
	}
	if err := os.WriteFile(filepath.Join(s.avatarDir(), fileName), processed, 0o644); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Upload failed.")
		return
	}
	if err := s.store.SetAvatar(user.ID, fileName); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Upload failed.")
		return
	}
	s.removeAvatarFile(user.Avatar)
	flashRedirect(w, r, "/settings", "Picture saved.", "")
}

func (s *Server) handleAvatarRemove(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if err := s.store.SetAvatar(user.ID, ""); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not remove the picture.")
		return
	}
	s.removeAvatarFile(user.Avatar)
	flashRedirect(w, r, "/settings", "Picture removed.", "")
}

// removeAvatarFile deletes a replaced or cleared picture. The record has
// already changed, so a failure is only logged.
func (s *Server) removeAvatarFile(name string) {
	if !photoNamePattern.MatchString(name) {
		return
	}
	if err := os.Remove(filepath.Join(s.avatarDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("web: remove avatar %s: %v", name, err)
	}
}

func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !photoNamePattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.avatarDir(), name))
}
