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
	"unicode/utf8"

	"github.com/Joe-Speed/sprue/platform/store"
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static
var staticFiles embed.FS

const sessionCookie = "sprue_session"

type Config struct {
	DataDir        string
	BaseURL        string
	AdminEmail     string
	BrevoKey       string // Brevo API key; when set, mail goes over HTTPS and SMTP is unused
	SMTPHost       string
	SMTPPort       string
	SMTPUser       string
	SMTPPass       string
	SMTPFrom       string
	AnalyticsID    string // Google Analytics measurement ID, empty for none
	VisionKey      string // Google Cloud Vision API key for photo screening, empty to skip
	DiscordURL     string // invite link shown in the footer, empty to hide
	DiscordWebhook string // webhook the feedback form posts to, empty to hide the form
	SupportEmail   string // address shown in the footer for support
	KofiURL        string // the site's Ko-fi page, empty to hide the donate page
	KofiToken      string // Ko-fi webhook verification token, empty to refuse webhooks
}

type Server struct {
	store     *store.Store
	config    Config
	templates map[string]*template.Template
	limiter   *rateLimiter
	quota     *quota
	csp       string
	host      string // host part of BaseURL; the www form redirects here
	secure    bool   // BaseURL is https, so browsers may be told to insist on it
}

const staticCacheControl = "public, max-age=31536000, immutable"

func New(st *store.Store, config Config) (*Server, error) {
	if st == nil || config.DataDir == "" || config.BaseURL == "" {
		return nil, errors.New("web: store, data dir, and base url are required")
	}
	if config.AnalyticsID != "" && !analyticsIDPattern.MatchString(config.AnalyticsID) {
		return nil, errors.New("web: analytics id must look like G-XXXXXXXX")
	}
	if config.KofiURL != "" && !strings.HasPrefix(config.KofiURL, "https://") {
		return nil, errors.New("web: ko-fi url must start with https://")
	}
	base, err := url.Parse(config.BaseURL)
	if err != nil || base.Host == "" {
		return nil, errors.New("web: base url must be a full address like https://sprue.uk")
	}
	templates, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	return &Server{
		store: st, config: config, templates: templates, limiter: newRateLimiter(), quota: newQuota(),
		csp: contentSecurityPolicy(config.AnalyticsID), host: base.Host, secure: base.Scheme == "https",
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", cacheForever(http.FileServerFS(staticFiles)))
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /auth/start", s.handleAuthStart)
	mux.HandleFunc("GET /auth/verify", s.handleAuthVerifyPage)
	mux.HandleFunc("POST /auth/verify", s.handleAuthVerify)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /settings", s.handleSettingsPage)
	mux.HandleFunc("POST /settings", s.handleSettingsSave)
	mux.HandleFunc("POST /settings/avatar", s.handleAvatarUpload)
	mux.HandleFunc("POST /settings/avatar/remove", s.handleAvatarRemove)
	mux.HandleFunc("GET /avatars/{name}", s.handleAvatar)
	mux.HandleFunc("GET /u/{slug}", s.handleProfile)
	mux.HandleFunc("GET /members", s.handleMembers)
	mux.HandleFunc("GET /friends", s.handleFriends)
	mux.HandleFunc("POST /friends/{slug}/{action}", s.handleFriendAction)
	mux.HandleFunc("GET /builds", s.handleMyBuilds)
	mux.HandleFunc("GET /builds/new", s.handleBuildForm)
	mux.HandleFunc("POST /builds/new", s.handleBuildCreate)
	mux.HandleFunc("GET /builds/{id}", s.handleBuildPage)
	mux.HandleFunc("GET /builds/{id}/edit", s.handleBuildForm)
	mux.HandleFunc("POST /builds/{id}/edit", s.handleBuildUpdate)
	mux.HandleFunc("POST /builds/{id}/photos", s.handlePhotoUpload)
	mux.HandleFunc("POST /builds/{id}/photos/{name}/cover", s.handlePhotoCover)
	mux.HandleFunc("POST /builds/{id}/photos/{name}/move/{way}", s.handlePhotoMove)
	mux.HandleFunc("POST /builds/{id}/photos/{name}/delete", s.handlePhotoDelete)
	mux.HandleFunc("POST /builds/{id}/delete", s.handleBuildDelete)
	mux.HandleFunc("POST /builds/{id}/vote", s.handleBuildVote)
	mux.HandleFunc("POST /builds/{id}/like", s.handleBuildLike)
	mux.HandleFunc("GET /likes", s.handleLikes)
	mux.HandleFunc("POST /builds/{id}/report", s.handleReport)
	mux.HandleFunc("GET /photos/{build}/{name}", s.handlePhoto)
	mux.HandleFunc("GET /stash", s.handleStash)
	mux.HandleFunc("POST /stash", s.handleStashAdd)
	mux.HandleFunc("GET /stash/{id}", s.handleStashItem)
	mux.HandleFunc("POST /stash/{id}/{action}", s.handleStashAction)
	mux.HandleFunc("POST /goal", s.handleGoal)
	mux.HandleFunc("GET /competitions", s.handleCompetitions)
	mux.HandleFunc("GET /competitions/new", s.handleCompetitionForm)
	mux.HandleFunc("GET /competitions/past", s.handlePastCompetitions)
	mux.HandleFunc("POST /competitions/new", s.handleCompetitionCreate)
	mux.HandleFunc("GET /competitions/{slug}", s.handleCompetition)
	mux.HandleFunc("POST /competitions/{slug}/enter", s.handleEnter)
	mux.HandleFunc("POST /competitions/{slug}/vote", s.handleVote)
	mux.HandleFunc("GET /trophies/{id}/download", s.handleTrophyDownload)
	mux.HandleFunc("GET /admin", s.handleAdmin)
	mux.HandleFunc("POST /admin/competitions/{slug}/decide", s.handleAdminDecide)
	mux.HandleFunc("POST /admin/competitions/{slug}/entries/{id}/remove", s.handleAdminRemoveEntry)
	mux.HandleFunc("POST /admin/trophies/{id}/rearm", s.handleAdminRearm)
	mux.HandleFunc("POST /admin/builds/{id}/{action}", s.handleAdminModerate)
	mux.HandleFunc("POST /admin/donations/{id}/remove", s.handleAdminRemoveDonation)
	mux.HandleFunc("GET /support", s.handleSupport)
	mux.HandleFunc("POST /webhooks/kofi", s.handleKofiWebhook)
	mux.HandleFunc("GET /terms", s.handleTerms)
	mux.HandleFunc("GET /privacy", s.handlePrivacy)
	mux.HandleFunc("GET /feedback", s.handleFeedbackForm)
	mux.HandleFunc("POST /feedback", s.handleFeedback)
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
	return path == "/builds/new" || path == "/settings/avatar" || strings.HasSuffix(path, "/photos")
}

func withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !carriesPhotos(r.URL.Path) {
			r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// hstsMaxAge is one year: how long browsers remember to use https only.
const hstsMaxAge = "max-age=31536000; includeSubDomains"

// withSecurityHeaders sets the headers every response carries. Visitors on
// the www form of the site are sent to the bare domain first, so there is
// one address, one set of cookies, and one entry in search results.
func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "www."+s.host {
			http.Redirect(w, r, s.absolute(r.URL.RequestURI()), http.StatusMovedPermanently)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", s.csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		if s.secure {
			h.Set("Strict-Transport-Security", hstsMaxAge)
		}
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

// policyUpdated is the date the terms and privacy text last changed.
const policyUpdated = "2026-09-08"

func (s *Server) handleTerms(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "terms", "Terms", policyUpdated)
}

func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "privacy", "Privacy", policyUpdated)
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

// site is the part of the configuration templates may see. The rest, SMTP
// credentials included, never reaches a template.
type site struct {
	AnalyticsID  string
	DiscordURL   string
	SupportEmail string
	Feedback     bool // the feedback form is available
	Donate       bool // the support page is available
	Screening    bool // photos are checked by the image checker before saving
	Year         int
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
	Site        site
	Requests    int // friend requests waiting on the signed-in member
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
	note, errorText := readFlash(w, r)
	p := page{
		Title: title + " · sprue", Description: m.Description, Image: m.Image,
		Canonical: s.absolute(r.URL.Path), NoIndex: noIndexPages[name],
		Path: r.URL.Path, Mark: readMark(r), Data: data,
		Error: errorText, Note: note,
		Site: site{
			AnalyticsID: s.config.AnalyticsID,
			DiscordURL:  s.config.DiscordURL, SupportEmail: s.config.SupportEmail,
			Feedback: s.config.DiscordWebhook != "", Donate: s.config.KofiURL != "",
			Screening: s.config.VisionKey != "", Year: time.Now().UTC().Year(),
		},
	}
	if name == "home" {
		p.Title = "sprue · " + title
	}
	if user, token, err := s.sessionUser(r); err == nil {
		p.User = &user
		p.CSRF = csrfToken(token)
		if count, err := s.store.PendingRequestCount(user.ID); err == nil {
			p.Requests = count
		}
		if p.Note == "" && p.Error == "" {
			if trophy, err := s.store.UnseenTrophy(user.ID); err == nil {
				p.Note = fmt.Sprintf("You placed %s in %s. Your trophy is on your profile.", strings.ToLower(placeName(trophy.Place)), trophy.CompetitionTitle)
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.Execute(w, p); err != nil {
		log.Printf("web: render %s: %v", name, err)
	}
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "Page not found.")
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	s.renderMeta(w, r, status, "error", "Something went wrong", message, meta{})
}

// maxMessageLength bounds the flash text a page will show from its query
// string, which anyone can put in a link.
const maxMessageLength = 200

func clipMessage(text string) string {
	if len(text) <= maxMessageLength {
		return text
	}
	cut := maxMessageLength
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func buildPage(id int64) string { return fmt.Sprintf("/builds/%d", id) }
func buildEdit(id int64) string { return fmt.Sprintf("/builds/%d/edit", id) }

// flashCookie carries one message across a redirect. A cookie rather than
// the query string, so nobody can craft a link that puts words in the
// site's green tick box.
const flashCookie = "sprue_flash"

// flashRedirect sends the reader to path with one message: an error if
// errorText is set, otherwise a note.
func flashRedirect(w http.ResponseWriter, r *http.Request, path, note, errorText string) {
	kind, text := "note", note
	if errorText != "" {
		kind, text = "error", errorText
	}
	if text != "" {
		http.SetCookie(w, &http.Cookie{
			Name: flashCookie, Value: url.QueryEscape(kind + ":" + text), Path: "/", MaxAge: 60,
			HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: requestIsSecure(r),
		})
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

// readFlash returns the pending message, if any, and clears it.
func readFlash(w http.ResponseWriter, r *http.Request) (note, errorText string) {
	cookie, err := r.Cookie(flashCookie)
	if err != nil {
		return "", ""
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1})
	raw, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		return "", ""
	}
	kind, text, ok := strings.Cut(raw, ":")
	if !ok {
		return "", ""
	}
	if kind == "error" {
		return "", clipMessage(text)
	}
	return clipMessage(text), ""
}

// requestIsSecure is true when the reader arrived over HTTPS, directly or
// through the platform's proxy.
func requestIsSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
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
		// Every form carries the field, so an empty one means the body never
		// arrived in full. On a photo post that is a slow or dropped upload,
		// not a stale page, and saying "expired" would send the member to
		// look in the wrong place.
		if r.FormValue("csrf") == "" && strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			s.renderError(w, r, http.StatusRequestTimeout,
				"The upload did not finish. Try again with fewer photos, or smaller ones.")
			return store.User{}, false
		}
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

// rememberedCookieDays is how long the browser keeps a remembered session
// cookie. The store renews the session itself while the member keeps
// visiting, so the cookie only needs to outlive the longest quiet spell.
// Browsers cap cookies at about 400 days.
const rememberedCookieDays = 365

// setSessionCookie stores the session token. Without remember the cookie
// has no age, so the browser drops it when it closes.
func (s *Server) setSessionCookie(w http.ResponseWriter, token string, remember bool) {
	maxAge := 0
	if remember {
		maxAge = rememberedCookieDays * 24 * 60 * 60
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
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
		oldestKey, oldest := "", now
		for k, when := range l.seen {
			if now.Sub(when) > time.Hour {
				delete(l.seen, k)
			} else if when.Before(oldest) {
				oldestKey, oldest = k, when
			}
		}
		if len(l.seen) >= maxRateSlots && oldestKey != "" {
			delete(l.seen, oldestKey)
		}
	}
	l.seen[key] = now
	return true
}

// clientKey is the visitor's address for rate limiting. Behind a proxy the
// real address is the last entry of X-Forwarded-For, the one the proxy
// appended; earlier entries are whatever the client chose to send.
func clientKey(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		if comma := strings.LastIndexByte(forwarded, ','); comma >= 0 {
			return strings.TrimSpace(forwarded[comma+1:])
		}
		return strings.TrimSpace(forwarded)
	}
	return r.RemoteAddr
}

// quota counts how many times a key acts inside a rolling window, with the
// same bounded table as the rate limiter. It backs the daily mail ceiling
// and the hourly per visitor sign-in cap, so one patient bot cannot spend
// the day's email allowance.
type quota struct {
	mu   sync.Mutex
	seen map[string]quotaSlot
}

type quotaSlot struct {
	started time.Time
	count   int
}

func newQuota() *quota {
	return &quota{seen: make(map[string]quotaSlot, 64)}
}

// allow records one use of key and reports whether it stayed within limit
// uses per window.
func (q *quota) allow(key string, limit int, window time.Duration) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	slot, ok := q.seen[key]
	if !ok || now.Sub(slot.started) >= window {
		if len(q.seen) >= maxRateSlots {
			oldestKey, oldest := "", now
			for k, old := range q.seen {
				if now.Sub(old.started) >= window {
					delete(q.seen, k)
				} else if old.started.Before(oldest) {
					oldestKey, oldest = k, old.started
				}
			}
			if len(q.seen) >= maxRateSlots && oldestKey != "" {
				delete(q.seen, oldestKey)
			}
		}
		slot = quotaSlot{started: now}
	}
	slot.count++
	q.seen[key] = slot
	return slot.count <= limit
}
