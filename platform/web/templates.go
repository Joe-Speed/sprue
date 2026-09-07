package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"strings"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

// pageNames are the templates that render a whole page. Each is parsed with
// base.html and partials.html so every page shares the same shell and cards.
var pageNames = []string{
	"home", "login", "check_email", "settings", "profile", "build", "build_form",
	"competitions", "competition", "competition_form", "stash", "stash_item", "admin", "error",
}

// assetVersion is a short hash of every embedded static file, appended to
// every static URL so browsers can cache assets for a year and any change
// to any file gives a new URL.
var assetVersion = hashStaticFiles()

func hashStaticFiles() string {
	hash := sha256.New()
	err := fs.WalkDir(staticFiles, "static", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		content, err := fs.ReadFile(staticFiles, path)
		if err != nil {
			return err
		}
		hash.Write([]byte(path))
		hash.Write(content)
		return nil
	})
	if err != nil {
		return "0"
	}
	return hex.EncodeToString(hash.Sum(nil)[:4])
}

func parseTemplates() (map[string]*template.Template, error) {
	helpers := template.FuncMap{
		"placeBadge": placeBadge,
		"placeName":  placeName,
		"static":     staticPath,
		"markPath":   markPath,
		"card":       newCardView,
		"under":      under,
		"niceDate":   niceDate,
		"monthYear":  monthYear,
		"daysSince":  daysSince,
		"money":      money,
		"neg":        func(n int) int { return -n },
	}
	templates := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		t, err := template.New("base.html").Funcs(helpers).ParseFS(templateFiles,
			"templates/base.html", "templates/partials.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("web: template %s: %w", name, err)
		}
		templates[name] = t
	}
	return templates, nil
}

// cardView is what the build_card partial renders. ShowOwner is on for mixed
// lists (home, competition entries) and off on a builder's own page.
type cardView struct {
	Build     store.Build
	ShowOwner bool
}

func newCardView(build store.Build, showOwner bool) cardView {
	return cardView{Build: build, ShowOwner: showOwner}
}

// under reports whether path is prefix or sits below it, for nav highlighting.
func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// Stored dates are either a bare day from a date input or an RFC3339 stamp.
var dateLayouts = []string{"2006-01-02", time.RFC3339}

func parseStoredDate(value string) (time.Time, bool) {
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func niceDate(value string) string {
	if t, ok := parseStoredDate(value); ok {
		return t.Format("2 Jan 2006")
	}
	return value
}

func daysSince(value string) int {
	if t, ok := parseStoredDate(value); ok {
		return int(time.Since(t).Hours() / 24)
	}
	return 0
}

func monthYear(value string) string {
	if t, ok := parseStoredDate(value); ok {
		return t.Format("January 2006")
	}
	return value
}

// markColours is the order the airplane mark cycles through when clicked.
// Each colour has a matching static/mark-<colour>.svg.
var markColours = []string{"blue", "green", "yellow", "orange", "red"}

const markCookie = "sprue_mark"

// validMark returns the colour if it is one we draw, otherwise the first.
func validMark(colour string) string {
	for _, known := range markColours {
		if colour == known {
			return colour
		}
	}
	return markColours[0]
}

func nextMark(colour string) string {
	for i, known := range markColours {
		if colour == known {
			return markColours[(i+1)%len(markColours)]
		}
	}
	return markColours[0]
}

func markPath(colour string) string {
	return staticPath("mark-" + validMark(colour) + ".svg")
}

func staticPath(name string) string {
	return "/static/" + name + "?v=" + assetVersion
}

func placeBadge(place int) string {
	switch place {
	case 1:
		return staticPath("trophies/first-place.svg")
	case 2:
		return staticPath("trophies/second-place.svg")
	case 3:
		return staticPath("trophies/third-place.svg")
	}
	return ""
}

func placeName(place int) string {
	switch place {
	case 1:
		return "First"
	case 2:
		return "Second"
	case 3:
		return "Third"
	}
	return ""
}
