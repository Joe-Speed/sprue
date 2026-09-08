package web

import (
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
)

// siteDescription is the default page description and the one search engines
// see for the front page.
const siteDescription = "A community for scale modellers. Post your builds, vote for the ones you like, run competitions, and win printable trophies."

// meta is what a page hands to the head: a description and, when it has one,
// an absolute image URL for link previews.
type meta struct {
	Description string
	Image       string
}

// noIndexPages are pages search engines should not list: forms, account
// pages, and error pages.
var noIndexPages = map[string]bool{
	"login": true, "check_email": true, "settings": true, "builds": true, "build_form": true, "feedback": true,
	"competition_form": true, "admin": true, "error": true,
}

var analyticsIDPattern = regexp.MustCompile(`^G-[A-Z0-9]{4,16}$`)

// contentSecurityPolicy builds the header for this deployment. Only the
// site's own script may run, plus Google's tag when an analytics ID is set.
// blob: images are the photo previews before upload.
func contentSecurityPolicy(analyticsID string) string {
	if analyticsID == "" {
		return "default-src 'none'; style-src 'self'; font-src 'self'; img-src 'self' data: blob:; script-src 'self'; " +
			"form-action 'self'; base-uri 'self'; frame-ancestors 'none'"
	}
	return "default-src 'none'; style-src 'self'; font-src 'self'; " +
		"img-src 'self' data: blob: https://*.google-analytics.com https://*.googletagmanager.com; " +
		"script-src 'self' https://www.googletagmanager.com; " +
		"connect-src https://*.google-analytics.com https://*.analytics.google.com https://*.googletagmanager.com; " +
		"form-action 'self'; base-uri 'self'; frame-ancestors 'none'"
}

func (s *Server) absolute(path string) string {
	return strings.TrimSuffix(s.config.BaseURL, "/") + path
}

func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	fmt.Fprintf(w, `User-agent: *
Disallow: /admin
Disallow: /settings
Disallow: /login
Disallow: /auth/
Disallow: /mark/
Disallow: /builds$
Disallow: /builds/new
Disallow: /builds/*/edit
Disallow: /competitions/new
Allow: /

Sitemap: %s
`, s.absolute("/sitemap.xml"))
}

// sitemapLimit bounds each section of the sitemap. Google reads up to fifty
// thousand URLs per file; this stays far below with room for years of posts.
const sitemapLimit = 5000

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemap struct {
	XMLName xml.Name     `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []sitemapURL `xml:"url"`
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	builds, err := s.store.BuildsForSitemap(sitemapLimit)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	slugs, err := s.store.UserSlugs(sitemapLimit)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	comps, err := s.store.Competitions("")
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	urls := make([]sitemapURL, 0, 2+len(builds)+len(slugs)+len(comps))
	for _, path := range []string{"/", "/competitions", "/competitions/past", "/members", "/terms", "/privacy"} {
		urls = append(urls, sitemapURL{Loc: s.absolute(path)})
	}
	if s.config.KofiURL != "" {
		urls = append(urls, sitemapURL{Loc: s.absolute("/support")})
	}
	for _, comp := range comps {
		urls = append(urls, sitemapURL{Loc: s.absolute("/competitions/" + comp.Slug)})
	}
	for _, slug := range slugs {
		urls = append(urls, sitemapURL{Loc: s.absolute("/u/" + slug)})
	}
	for _, build := range builds {
		urls = append(urls, sitemapURL{Loc: s.absolute(fmt.Sprintf("/builds/%d", build.ID)), LastMod: build.CreatedAt[:10]})
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	fmt.Fprint(w, xml.Header)
	if err := xml.NewEncoder(w).Encode(sitemap{URLs: urls}); err != nil {
		log.Printf("web: sitemap: %v", err)
	}
}
