package web

import (
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
)

const (
	magicLinkMinGap = 30 * time.Second
	maxLinksPerHour = 6   // sign-in emails one visitor address may trigger in an hour
	maxLinksPerDay  = 200 // sign-in emails the whole site sends in a day, under Brevo's 300
	honeypotField   = "website"
)

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if user, _, err := s.sessionUser(r); err == nil {
		http.Redirect(w, r, "/u/"+user.Slug, http.StatusSeeOther)
		return
	}
	s.render(w, r, "login", "Sign in", nil)
}

func (s *Server) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	email, ok := plainEmail(r.FormValue("email"))
	if !ok {
		flashRedirect(w, r, "/login", "", "Enter a real email address.")
		return
	}
	if r.FormValue(honeypotField) != "" {
		// A hidden field only a bot fills in. Pretend it worked and send nothing.
		s.render(w, r, "check_email", "Check your email", email)
		return
	}
	visitor := clientKey(r)
	if !s.limiter.allow("email:"+email, magicLinkMinGap) || !s.limiter.allow("ip:"+visitor, magicLinkMinGap) ||
		!s.quota.allow("links:"+visitor, maxLinksPerHour, time.Hour) {
		flashRedirect(w, r, "/login", "", "Too many attempts. Wait a while and try again.")
		return
	}
	if !s.quota.allow("links:day", maxLinksPerDay, 24*time.Hour) {
		log.Printf("web: daily sign-in email ceiling of %d reached", maxLinksPerDay)
		flashRedirect(w, r, "/login", "", "Sign-in emails are paused for today. Try again tomorrow.")
		return
	}
	token, err := randomToken()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not create a sign-in link.")
		return
	}
	remember := r.FormValue("remember") == "on"
	if err := s.store.CreateMagicToken(hashToken(token), email, remember); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not create a sign-in link.")
		return
	}
	link := s.config.BaseURL + "/auth/verify?token=" + token
	if err := s.sendMagicLink(email, link); err != nil {
		log.Printf("web: send magic link: %v", err)
		s.renderError(w, r, http.StatusInternalServerError, "Could not send the sign-in email.")
		return
	}
	s.render(w, r, "check_email", "Check your email", email)
}

// plainEmail accepts a bare address only: no display name, no whitespace,
// nothing that could reach a mail header unchanged.
func plainEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 200 {
		return "", false
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", false
	}
	return email, true
}

// sendMagicLink emails the link when mail is configured and logs it
// otherwise. main refuses to start without mail unless the base URL is
// localhost, so the logging path is local development only.
func (s *Server) sendMagicLink(email, link string) error {
	if !s.mailConfigured() {
		log.Printf("web: magic link for %s: %s", email, link)
		return nil
	}
	return s.sendMail(email, mailMessage{
		Subject: "Your sprue sign-in link",
		Intro:   []string{"Press the button to sign in to sprue."},
		Action:  "Sign in",
		Link:    link,
		Outro:   []string{"This link works once and expires in 15 minutes. If you did not ask for it, ignore this email and nothing happens."},
	})
}

// handleAuthVerifyPage shows a button instead of signing the reader in on
// the GET. Mail gateways and link scanners open every address in an email
// before the member does, and would otherwise burn the token.
func (s *Server) handleAuthVerifyPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if len(token) != 64 {
		s.renderError(w, r, http.StatusBadRequest, "That sign-in link is not valid.")
		return
	}
	s.render(w, r, "verify", "Sign in", token)
}

func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	if len(token) != 64 {
		s.renderError(w, r, http.StatusBadRequest, "That sign-in link is not valid.")
		return
	}
	email, remember, err := s.store.ConsumeMagicToken(hashToken(token))
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "That sign-in link is expired or already used. Request a new one.")
		return
	}
	isAdmin := s.config.AdminEmail != "" && email == strings.ToLower(s.config.AdminEmail)
	user, err := s.store.FindOrCreateUser(email, isAdmin)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not sign you in.")
		return
	}
	session, err := randomToken()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not sign you in.")
		return
	}
	if err := s.store.CreateSession(hashToken(session), user.ID, remember); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not sign you in.")
		return
	}
	s.setSessionCookie(w, session, remember)
	if !user.SlugChosen {
		flashRedirect(w, r, "/settings", "Welcome to sprue. Pick a display name and your page address.", "")
		return
	}
	http.Redirect(w, r, "/u/"+user.Slug, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if _, token, err := s.sessionUser(r); err == nil {
		if err := s.store.DeleteSession(hashToken(token)); err != nil {
			log.Printf("web: delete session: %v", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// settingsData is the member plus which trophy flairs they have unlocked.
type settingsData struct {
	store.User
	Won [4]bool
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	won, err := s.wonPlaces(user.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load your settings.")
		return
	}
	s.render(w, r, "settings", "Settings", settingsData{User: user, Won: won})
}

func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("display_name"))
	if name == "" || len(name) > 100 {
		flashRedirect(w, r, "/settings", "", "Pick a name under 100 characters.")
		return
	}
	flair := r.FormValue("flair")
	if flair != "" && !validFlair(flair) {
		flashRedirect(w, r, "/settings", "", "Pick one of the pictures shown.")
		return
	}
	if place := trophyPlace(flair); place != 0 {
		won, err := s.wonPlaces(user.ID)
		if err != nil || !won[place] {
			flashRedirect(w, r, "/settings", "", "That flair is for members who have placed there.")
			return
		}
	}
	if err := s.store.RenameUser(user.ID, name, flair); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save your settings.")
		return
	}
	bio := strings.TrimSpace(r.FormValue("bio"))
	if len(bio) > store.MaxBioLength {
		flashRedirect(w, r, "/settings", "", fmt.Sprintf("Keep your line under %d characters.", store.MaxBioLength))
		return
	}
	if bio != user.Bio {
		if err := s.store.SetBio(user.ID, bio); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not save your settings.")
			return
		}
	}
	if wanted := strings.TrimSpace(r.FormValue("slug")); wanted != "" && wanted != user.Slug {
		if user.SlugChosen {
			flashRedirect(w, r, "/settings", "", "Your page address is set and cannot change.")
			return
		}
		if err := s.store.SetSlug(user.ID, wanted); err != nil {
			flashRedirect(w, r, "/settings", "", "That address is taken or not allowed. Use 3 to 40 lowercase letters, numbers, and hyphens.")
			return
		}
	}
	if nudge := r.FormValue("nudge"); nudge != "" && nudge != user.Nudge {
		if err := s.store.SetNudge(user.ID, nudge); err != nil {
			flashRedirect(w, r, "/settings", "", "Pick off, weekly, or monthly for reminders.")
			return
		}
	}
	flashRedirect(w, r, "/settings", "Saved.", "")
}
