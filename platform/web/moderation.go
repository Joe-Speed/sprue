package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	build, err := s.store.BuildByID(parseID(r.PathValue("id")))
	if err != nil || !visibleTo(build, &user) || build.Private {
		s.renderError(w, r, http.StatusNotFound, "No such build.")
		return
	}
	page := buildPage(build.ID)
	if err := s.store.ReportBuild(build.ID, user.ID, r.FormValue("reason")); err != nil {
		flashRedirect(w, r, page, "", "Could not report that build.")
		return
	}
	flashRedirect(w, r, page, "Reported. The admin will take a look.", "")
}

// handleAdminModerate acts on one reported build: hide, unhide, or dismiss.
func (s *Server) handleAdminModerate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	id := parseID(r.PathValue("id"))
	var err error
	switch r.PathValue("action") {
	case "hide":
		err = s.store.SetBuildHidden(id, true)
	case "unhide":
		err = s.store.SetBuildHidden(id, false)
	case "dismiss":
		err = s.store.DismissReports(id)
	default:
		s.renderError(w, r, http.StatusNotFound, "Page not found.")
		return
	}
	if err != nil {
		flashRedirect(w, r, "/admin", "", "Could not save.")
		return
	}
	flashRedirect(w, r, "/admin", "Saved.", "")
}

// visionEndpoint is Google Cloud Vision's annotate call. Tests point it at a
// local server.
var visionEndpoint = "https://vision.googleapis.com/v1/images:annotate"

var errUnsafePhoto = errors.New("web: photo refused by the image check")
var errScreenUnavailable = errors.New("web: image check unavailable")

// screenPhoto asks SafeSearch about a processed photo and refuses anything
// likely adult or violent. Without a key every photo passes. If the check
// cannot be reached the photo is refused, never quietly let through.
func (s *Server) screenPhoto(jpeg []byte) error {
	if s.config.VisionKey == "" {
		return nil
	}
	request, err := json.Marshal(map[string]any{"requests": []any{map[string]any{
		"image":    map[string]string{"content": base64.StdEncoding.EncodeToString(jpeg)},
		"features": []any{map[string]string{"type": "SAFE_SEARCH_DETECTION"}},
	}}})
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Post(visionEndpoint+"?key="+s.config.VisionKey, "application/json", bytes.NewReader(request))
	if err != nil {
		return errScreenUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errScreenUnavailable
	}
	var reply struct {
		Responses []struct {
			SafeSearch struct {
				Adult    string `json:"adult"`
				Violence string `json:"violence"`
				Racy     string `json:"racy"`
			} `json:"safeSearchAnnotation"`
		} `json:"responses"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64*1024)).Decode(&reply); err != nil || len(reply.Responses) == 0 {
		return errScreenUnavailable
	}
	verdict := reply.Responses[0].SafeSearch
	if likely(verdict.Adult) || likely(verdict.Violence) || verdict.Racy == "VERY_LIKELY" {
		return errUnsafePhoto
	}
	return nil
}

func likely(level string) bool {
	return level == "LIKELY" || level == "VERY_LIKELY"
}

// visibleTo reports whether a build may be shown to the caller: everyone for
// a normal build, the owner and the admin for a hidden one, the owner alone
// for a private one.
func visibleTo(build store.Build, user *store.User) bool {
	owner := user != nil && user.ID == build.UserID
	if build.Hidden {
		return owner || (user != nil && user.IsAdmin)
	}
	if build.Private {
		return owner
	}
	return true
}
