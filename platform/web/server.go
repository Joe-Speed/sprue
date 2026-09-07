// Package web is the HTTP layer: routing, sessions, templates, handlers.
package web

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static
var staticFiles embed.FS

const sessionCookie = "sprue_session"

type Config struct {
	DataDir          string
	BaseURL          string
	AdminEmail       string
	SMTPHost         string
	SMTPPort         string
	SMTPUser         string
	SMTPPass         string
	SMTPFrom         string
	AnalyticsID      string // Google Analytics measurement ID, empty for none
	SiteVerification string // Google Search Console meta tag value, empty for none
	Currency         string // symbol shown before stash costs
	VisionKey        string // Google Cloud Vision API key for photo screening, empty to skip
}

type Server struct {
	store     *store.Store
	config    Config
	templates map[string]*template.Template
	limiter   *rateLimiter
	csp       string
}

const staticCacheControl = "public, max-age=31536000, immutable"

func New(st *store.Store, config Config) (*Server, error) {
	if st == nil || config.DataDir == "" || config.BaseURL == "" {
		return nil, errors.New("web: store, data dir, and base url are required")
	}
	if config.AnalyticsID != "" && !analyticsIDPattern.MatchString(config.AnalyticsID) {
		return nil, errors.New("web: analytics id must look like G-XXXXXXXX")
	}
	if config.Currency == "" {
		config.Currency = "£"
	}
	templates, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	return &Server{
		store: st, config: config, templates: templates, limiter: newRateLimiter(),
		csp: contentSecurityPolicy(config.AnalyticsID),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", cacheForever(http.FileServerFS(staticFiles)))
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /auth/start", s.handleAuthStart)
	mux.HandleFunc("GET /auth/verify", s.handleAuthVerify)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /settings", s.handleSettingsPage)
	mux.HandleFunc("POST /settings", s.handleSettingsSave)
	mux.HandleFunc("GET /u/{slug}", s.handleProfile)
	mux.HandleFunc("GET /builds/new", s.handleBuildForm)
	mux.HandleFunc("POST /builds/new", s.handleBuildCreate)
	mux.HandleFunc("GET /builds/{id}", s.handleBuildPage)
	mux.HandleFunc("GET /builds/{id}/edit", s.handleBuildForm)
	mux.HandleFunc("POST /builds/{id}/edit", s.handleBuildUpdate)
	mux.HandleFunc("POST /builds/{id}/photos", s.handlePhotoUpload)
	mux.HandleFunc("POST /builds/{id}/photos/{name}/cover", s.handlePhotoCover)
	mux.HandleFunc("POST /builds/{id}/photos/{name}/delete", s.handlePhotoDelete)
	mux.HandleFunc("POST /builds/{id}/delete", s.handleBuildDelete)
	mux.HandleFunc("POST /builds/{id}/vote", s.handleBuildVote)
	mux.HandleFunc("POST /builds/{id}/report", s.handleReport)
	mux.HandleFunc("GET /photos/{build}/{name}", s.handlePhoto)
	mux.HandleFunc("GET /stash", s.handleStash)
	mux.HandleFunc("POST /stash", s.handleStashAdd)
	mux.HandleFunc("GET /stash/{id}", s.handleStashItem)
	mux.HandleFunc("POST /stash/{id}/{action}", s.handleStashAction)
	mux.HandleFunc("POST /goal", s.handleGoal)
	mux.HandleFunc("GET /competitions", s.handleCompetitions)
	mux.HandleFunc("GET /competitions/new", s.handleCompetitionForm)
	mux.HandleFunc("POST /competitions/new", s.handleCompetitionCreate)
	mux.HandleFunc("GET /competitions/{slug}", s.handleCompetition)
	mux.HandleFunc("POST /competitions/{slug}/enter", s.handleEnter)
	mux.HandleFunc("POST /competitions/{slug}/vote", s.handleVote)
	mux.HandleFunc("GET /trophies/{id}/download", s.handleTrophyDownload)
	mux.HandleFunc("GET /admin", s.handleAdmin)
	mux.HandleFunc("POST /admin/competitions/{slug}/decide", s.handleAdminDecide)
	mux.HandleFunc("POST /admin/trophies/{id}/rearm", s.handleAdminRearm)
	mux.HandleFunc("POST /admin/builds/{id}/{action}", s.handleAdminModerate)
	mux.HandleFunc("GET /mark/next", s.handleMarkNext)
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
	mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("/", s.handleNotFound)
	return s.withRequestLog(s.withSecurityHeaders(withBodyLimit(mux)))
}

// maxFormBytes bounds every POST except the two that carry photos, which set
// their own larger limit. Text forms never need more than this.
const maxFormBytes = 64 * 1024

func carriesPhotos(path string) bool {
	return path == "/builds/new" || strings.HasSuffix(path, "/photos")
}

func withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !carriesPhotos(r.URL.Path) {
			r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", s.csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func readMark(r *http.Request) string {
	cookie, err := r.Cookie(markCookie)
	if err != nil {
		return markColours[0]
	}
	return validMark(cookie.Value)
}

// handleMarkNext moves the airplane mark to its next colour and sends the
// reader back to the page they clicked from. Same-origin referers only;
// anything else goes to the front page.
func (s *Server) handleMarkNext(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     markCookie,
		Value:    nextMark(readMark(r)),
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.config.BaseURL, "https://"),
	})
	http.Redirect(w, r, localReferer(r), http.StatusSeeOther)
}

// localReferer returns the path of the referring page when it is a plain
// path on this site, otherwise the front page. A path starting with two
// slashes would be read by browsers as another host, so it is refused too.
func localReferer(r *http.Request) string {
	from, err := url.Parse(r.Referer())
	if err != nil || from.Host != r.Host || from.Path == "/mark/next" {
		return "/"
	}
	if !strings.HasPrefix(from.Path, "/") || strings.HasPrefix(from.Path, "//") {
		return "/"
	}
	back := from.Path
	if from.RawQuery != "" {
		back += "?" + from.RawQuery
	}
	return back
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintln(w, "ok")
}

// cacheForever marks embedded static files immutable. Every static URL
// carries the build's asset version, so a new binary is a new URL.
func cacheForever(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", staticCacheControl)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started).Round(time.Millisecond))
	})
}

// page is the data every template receives.
type page struct {
	Title       string // full document title
	Description string
	Image       string // absolute URL for link previews
	Canonical   string // absolute URL of this page without its query
	NoIndex     bool
	Path        string // request path, so the nav can mark where the reader is
	Mark        string // colour of the airplane mark, from the reader's cookie
	User        *store.User
	CSRF        string
	Data        any
	Error       string
	Note        string
	Config      *Config
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name, title string, data any) {
	s.renderMeta(w, r, http.StatusOK, name, title, data, meta{})
}

func (s *Server) renderMeta(w http.ResponseWriter, r *http.Request, status int, name, title string, data any, m meta) {
	t, ok := s.templates[name]
	if !ok {
		http.Error(w, "template missing", http.StatusInternalServerError)
		return
	}
	if m.Description == "" {
		m.Description = siteDescription
	}
	if m.Image == "" {
		m.Image = s.absolute(staticPath("apple-touch-icon.png"))
	}
	query := r.URL.Query()
	p := page{
		Title: title + " · sprue", Description: m.Description, Image: m.Image,
		Canonical: s.absolute(r.URL.Path), NoIndex: noIndexPages[name],
		Path: r.URL.Path, Mark: readMark(r), Data: data,
		Error: clipMessage(query.Get("error")), Note: clipMessage(query.Get("note")),
		Config: &s.config,
	}
	if name == "home" {
		p.Title = "sprue · " + title
	}
	if user, token, err := s.sessionUser(r); err == nil {
		p.User = &user
		p.CSRF = csrfToken(token)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.Execute(w, p); err != nil {
		log.Printf("web: render %s: %v", name, err)
	}
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "Nothing at this address.")
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	s.renderMeta(w, r, status, "error", "Something went wrong", message, meta{})
}

// maxMessageLength bounds the flash text a page will show from its query
// string, which anyone can put in a link.
const maxMessageLength = 200

func clipMessage(text string) string {
	if len(text) > maxMessageLength {
		return text[:maxMessageLength]
	}
	return text
}

// flashRedirect sends the reader to path with one message: an error if
// errorText is set, otherwise a note.
func flashRedirect(w http.ResponseWriter, r *http.Request, path, note, errorText string) {
	query := url.Values{}
	if errorText != "" {
		query.Set("error", errorText)
	} else if note != "" {
		query.Set("note", note)
	}
	http.Redirect(w, r, path+"?"+query.Encode(), http.StatusSeeOther)
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func csrfToken(sessionToken string) string {
	sum := sha256.Sum256([]byte("csrf:" + sessionToken))
	return hex.EncodeToString(sum[:16])
}

func (s *Server) sessionUser(r *http.Request) (store.User, string, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return store.User{}, "", store.ErrNotFound
	}
	user, err := s.store.SessionUser(hashToken(cookie.Value))
	if err != nil {
		return store.User{}, "", err
	}
	return user, cookie.Value, nil
}

// requireUser loads the session and checks the CSRF field on mutations.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	user, token, err := s.sessionUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return store.User{}, false
	}
	if r.Method == http.MethodPost && r.FormValue("csrf") != csrfToken(token) {
		s.renderError(w, r, http.StatusForbidden, "That form expired. Go back and try again.")
		return store.User{}, false
	}
	return user, true
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return store.User{}, false
	}
	if !user.IsAdmin {
		s.renderError(w, r, http.StatusForbidden, "Admins only.")
		return store.User{}, false
	}
	return user, true
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   90 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.config.BaseURL, "https://"),
	})
}

// rateLimiter caps how often one key may perform an action, with a bounded
// table so memory cannot grow past maxRateSlots.
type rateLimiter struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

const maxRateSlots = 4096

func newRateLimiter() *rateLimiter {
	return &rateLimiter{seen: make(map[string]time.Time, 64)}
}

func (l *rateLimiter) allow(key string, minGap time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if last, ok := l.seen[key]; ok && now.Sub(last) < minGap {
		return false
	}
	if len(l.seen) >= maxRateSlots {
		for k, when := range l.seen {
			if now.Sub(when) > time.Hour {
				delete(l.seen, k)
			}
		}
		if len(l.seen) >= maxRateSlots {
			return false
		}
	}
	l.seen[key] = now
	return true
}

func clientKey(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		if comma := strings.IndexByte(forwarded, ','); comma > 0 {
			return strings.TrimSpace(forwarded[:comma])
		}
		return strings.TrimSpace(forwarded)
	}
	return r.RemoteAddr
}
