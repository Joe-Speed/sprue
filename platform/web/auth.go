package web

import (
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

const magicLinkMinGap = 30 * time.Second

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
	if !s.limiter.allow("email:"+email, magicLinkMinGap) || !s.limiter.allow("ip:"+clientKey(r), magicLinkMinGap) {
		flashRedirect(w, r, "/login", "", "Too many attempts. Wait a moment and try again.")
		return
	}
	token, err := randomToken()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not create a sign-in link.")
		return
	}
	if err := s.store.CreateMagicToken(hashToken(token), email); err != nil {
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

// sendMagicLink emails the link when SMTP is configured and logs it otherwise.
// main refuses to start without SMTP unless the base URL is localhost, so the
// logging path is local development only.
func (s *Server) sendMagicLink(email, link string) error {
	if s.config.SMTPHost == "" {
		log.Printf("web: magic link for %s: %s", email, link)
		return nil
	}
	from := s.config.SMTPFrom
	if from == "" {
		from = s.config.SMTPUser
	}
	message := fmt.Sprintf("From: sprue <%s>\r\nTo: %s\r\nSubject: Your sprue sign-in link\r\n\r\nSign in to sprue:\r\n\r\n%s\r\n\r\nThis link works once and expires in 15 minutes.\r\n", from, email, link)
	address := s.config.SMTPHost + ":" + s.config.SMTPPort
	auth := smtp.PlainAuth("", s.config.SMTPUser, s.config.SMTPPass, s.config.SMTPHost)
	return smtp.SendMail(address, auth, from, []string{email}, []byte(message))
}

func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if len(token) != 64 {
		s.renderError(w, r, http.StatusBadRequest, "That sign-in link is not valid.")
		return
	}
	email, err := s.store.ConsumeMagicToken(hashToken(token))
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
	if err := s.store.CreateSession(hashToken(session), user.ID); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not sign you in.")
		return
	}
	s.setSessionCookie(w, session)
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

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	s.render(w, r, "settings", "Settings", user)
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
	if err := s.store.RenameUser(user.ID, name); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save your settings.")
		return
	}
	flashRedirect(w, r, "/settings", "Saved.", "")
}
