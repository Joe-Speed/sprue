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
	DataDir    string
	BaseURL    string
	AdminEmail string
	SMTPHost   string
	SMTPPort   string
	SMTPUser   string
	SMTPPass   string
	SMTPFrom   string
}

type Server struct {
	store     *store.Store
	config    Config
	templates map[string]*template.Template
	limiter   *rateLimiter
}

func New(st *store.Store, config Config) (*Server, error) {
	if st == nil || config.DataDir == "" || config.BaseURL == "" {
		return nil, errors.New("web: store, data dir, and base url are required")
	}
	templates, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	return &Server{store: st, config: config, templates: templates, limiter: newRateLimiter()}, nil
}

var pageNames = []string{
	"home", "login", "check_email", "settings", "profile", "build", "build_form",
	"competitions", "competition", "admin", "error",
}

func parseTemplates() (map[string]*template.Template, error) {
	templates := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		t, err := template.New("base.html").Funcs(template.FuncMap{
			"placeBadge": placeBadge,
			"placeName":  placeName,
		}).ParseFS(templateFiles, "templates/base.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("web: template %s: %w", name, err)
		}
		templates[name] = t
	}
	return templates, nil
}

func placeBadge(place int) string {
	switch place {
	case 1:
		return "/static/trophies/first-place.svg"
	case 2:
		return "/static/trophies/second-place.svg"
	case 3:
		return "/static/trophies/third-place.svg"
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

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(staticFiles))
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
	mux.HandleFunc("GET /photos/{build}/{name}", s.handlePhoto)
	mux.HandleFunc("GET /competitions", s.handleCompetitions)
	mux.HandleFunc("GET /competitions/{slug}", s.handleCompetition)
	mux.HandleFunc("POST /competitions/{slug}/enter", s.handleEnter)
	mux.HandleFunc("POST /competitions/{slug}/vote", s.handleVote)
	mux.HandleFunc("GET /trophies/{id}/download", s.handleTrophyDownload)
	mux.HandleFunc("GET /admin", s.handleAdmin)
	mux.HandleFunc("POST /admin/competitions", s.handleAdminCreateCompetition)
	mux.HandleFunc("POST /admin/competitions/{slug}/status", s.handleAdminStatus)
	mux.HandleFunc("POST /admin/trophies/{id}/rearm", s.handleAdminRearm)
	return s.withRequestLog(mux)
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
	Title string
	User  *store.User
	CSRF  string
	Data  any
	Error string
	Note  string
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name, title string, data any) {
	s.renderStatus(w, r, http.StatusOK, name, title, data)
}

func (s *Server) renderStatus(w http.ResponseWriter, r *http.Request, status int, name, title string, data any) {
	t, ok := s.templates[name]
	if !ok {
		http.Error(w, "template missing", http.StatusInternalServerError)
		return
	}
	p := page{Title: title, Data: data, Error: r.URL.Query().Get("error"), Note: r.URL.Query().Get("note")}
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

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	s.renderStatus(w, r, status, "error", "Something went wrong", message)
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
