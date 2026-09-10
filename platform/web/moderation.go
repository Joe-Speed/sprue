package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
var errScreenBudget = errors.New("web: image check budget spent for the month")

// maxScreensPerMonth caps Vision calls under Google's free thousand a month.
// The count lives in the database so a redeploy cannot reset it. Once spent,
// uploads wait for next month rather than run up a bill.
const maxScreensPerMonth = 900

// screenTimeout allows a base plus a share for every photo in the call.
func screenTimeout(photos int) time.Duration {
	return 10*time.Second + time.Duration(photos)*10*time.Second
}

// maxScreenBatch is how many photos go to the image checker in one call.
// Google allows sixteen images per request; a build post carries at most
// maxPhotosPerUpload.
const maxScreenBatch = 6

// screenPhoto asks SafeSearch about one processed photo.
func (s *Server) screenPhoto(jpeg []byte) error {
	return s.screenPhotos([][]byte{jpeg})
}

// screenPhotos asks SafeSearch about a whole upload in one call and refuses
// anything likely adult or violent. Without a key every photo passes. If the
// check cannot be reached the photos are refused, never quietly let through.
// One call rather than one per photo, because each round trip to Google
// costs the member about two seconds of waiting.
func (s *Server) screenPhotos(photos [][]byte) error {
	if s.config.VisionKey == "" {
		return nil
	}
	if len(photos) == 0 {
		return nil
	}
	if len(photos) > maxScreenBatch {
		return fmt.Errorf("web: %d photos is more than the image checker takes at once", len(photos))
	}
	within, err := s.store.TakeMonthly("vision", maxScreensPerMonth, len(photos))
	if err != nil {
		log.Printf("web: vision counter: %v", err)
		return errScreenUnavailable
	}
	if !within {
		log.Printf("web: vision budget of %d checks spent this month", maxScreensPerMonth)
		return errScreenBudget
	}
	asked := make([]any, 0, len(photos))
	for _, photo := range photos {
		asked = append(asked, map[string]any{
			"image":    map[string]string{"content": base64.StdEncoding.EncodeToString(photo)},
			"features": []any{map[string]string{"type": "SAFE_SEARCH_DETECTION"}},
		})
	}
	request, err := json.Marshal(map[string]any{"requests": asked})
	if err != nil {
		return err
	}
	// Each photo adds its own upload and its own work at Google's end, so
	// the cut-off grows with the number sent. A flat ten seconds refused
	// whole uploads of four photos.
	client := &http.Client{Timeout: screenTimeout(len(photos))}
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
	if err := json.NewDecoder(io.LimitReader(res.Body, 256*1024)).Decode(&reply); err != nil || len(reply.Responses) != len(photos) {
		return errScreenUnavailable
	}
	for _, answer := range reply.Responses {
		verdict := answer.SafeSearch
		if likely(verdict.Adult) || likely(verdict.Violence) || verdict.Racy == "VERY_LIKELY" {
			return errUnsafePhoto
		}
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
