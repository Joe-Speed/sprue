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
	"home", "login", "check_email", "verify", "settings", "profile", "builds", "build", "build_form",
	"competitions", "competition", "competition_form", "past", "stash", "stash_item", "members", "friends", "likes",
	"terms", "privacy", "feedback", "support", "admin", "error",
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
		"placeBadge":   placeBadge,
		"placeName":    placeName,
		"static":       staticPath,
		"markPath":     markPath,
		"flairPath":    flairPath,
		"avatarPath":   avatarPath,
		"flairs":       func() []string { return flairs },
		"trophyFlairs": func() []string { return trophyFlairs[1:] },
		"trophyPlace":  trophyPlace,
		"lower":        strings.ToLower,
		"card":         newCardView,
		"under":        under,
		"niceDate":     niceDate,
		"niceTime":     niceTime,
		"ago":          ago,
		"daysUntil":    daysUntil,
		"today":        today,
		"inc":          func(n int) int { return n + 1 },
		"monthYear":    monthYear,
		"daysSince":    daysSince,
		"money":        money,
		"currency":     func() string { return currency },
		"neg":          func(n int) int { return -n },
		"meter":        meterBlocks,
		"notLast":      func(index, count int) bool { return index < count-1 },
		"brands":       func() []string { return kitBrands },
		"scales":       func() []string { return kitScales },
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

// kitBrands and kitScales are offered on the build form. They are
// suggestions, not a closed list: anything may still be typed.
var kitBrands = []string{
	"Academy", "AFV Club", "Airfix", "Arma Hobby", "Bandai", "Border Model",
	"Dragon", "Eduard", "Hasegawa", "Heller", "Hobby Boss", "ICM", "Italeri",
	"Meng", "MiniArt", "Monogram", "Revell", "Rye Field Model",
	"Special Hobby", "Takom", "Tamiya", "Trumpeter", "Zvezda",
}

// Largest first, the way a modeller reads them: cars and bikes, armour and
// figures, aircraft, then airliners and ships.
var kitScales = []string{
	"1/12", "1/16", "1/20", "1/24", "1/32", "1/35", "1/48", "1/72",
	"1/100", "1/144", "1/200", "1/350", "1/700",
}

// maxMeterBlocks is the width of a pixel vote meter, in blocks.
const maxMeterBlocks = 12

// meterBlocks lays out a vote meter as filled and empty blocks. With no
// scale each vote takes one block, up to the width of the meter. With a
// scale, that many votes fill the whole meter, which turns a competition's
// entries into a poll anyone can read at a glance.
func meterBlocks(votes, scale int) []bool {
	blocks := make([]bool, maxMeterBlocks)
	if votes <= 0 {
		return blocks
	}
	filled := votes
	if scale > 0 {
		filled = votes * maxMeterBlocks / scale
		if filled == 0 {
			filled = 1
		}
	}
	if filled > maxMeterBlocks {
		filled = maxMeterBlocks
	}
	for i := 0; i < filled; i++ {
		blocks[i] = true
	}
	return blocks
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

// niceTime shows the day and the hour, for journal entries where several
// can land on one day.
func niceTime(value string) string {
	if t, ok := parseStoredDate(value); ok {
		return t.UTC().Format("2 Jan 2006, 15:04")
	}
	return value
}

// ago says how long since a stamp in the words people use for recent things.
// Anything older than a month is better given as its date.
func ago(value string) string {
	t, ok := parseStoredDate(value)
	if !ok {
		return value
	}
	days := int(time.Since(t).Hours() / 24)
	switch {
	case days < 0:
		return niceDate(value)
	case days == 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 7:
		return fmt.Sprintf("%d days ago", days)
	case days < 31:
		weeks := days / 7
		if weeks == 1 {
			return "a week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	}
	return niceDate(value)
}

// daysUntil counts whole days from today to a date, negative once it is past.
func daysUntil(value string) int {
	t, ok := parseStoredDate(value)
	if !ok {
		return 0
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return int(t.UTC().Truncate(24*time.Hour).Sub(today).Hours() / 24)
}

// today is the date a date field should not look past, such as the day a
// build was finished.
func today() string {
	return time.Now().UTC().Format("2006-01-02")
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
