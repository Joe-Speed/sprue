package web

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/Joe-Speed/sprue/platform/store"
)

// maxWebhookBytes bounds the Ko-fi payload. A real one is under a kilobyte.
const maxWebhookBytes = 16 * 1024

// kofiPayload is the JSON Ko-fi posts, form encoded in a field named data,
// for every payment. Only the fields the site uses are read.
type kofiPayload struct {
	VerificationToken string `json:"verification_token"`
	Type              string `json:"type"`
	IsPublic          bool   `json:"is_public"`
	FromName          string `json:"from_name"`
	Message           string `json:"message"`
	Amount            string `json:"amount"`
	Currency          string `json:"currency"`
	TransactionID     string `json:"kofi_transaction_id"`
}

// handleKofiWebhook records a payment Ko-fi reports. The verification token
// is the only gate, so an unknown token is refused and logged. Ko-fi retries
// on anything but 200, and repeats are ignored by transaction id.
func (s *Server) handleKofiWebhook(w http.ResponseWriter, r *http.Request) {
	if s.config.KofiToken == "" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBytes)
	raw := r.FormValue("data")
	if raw == "" {
		http.Error(w, "no data field", http.StatusBadRequest)
		return
	}
	var payload kofiPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(payload.VerificationToken), []byte(s.config.KofiToken)) != 1 {
		log.Printf("web: ko-fi webhook with a wrong token from %s", r.RemoteAddr)
		http.Error(w, "bad token", http.StatusForbidden)
		return
	}
	if payload.Type != "Donation" && payload.Type != "Subscription" {
		w.WriteHeader(http.StatusOK)
		return
	}
	amount, ok := parsePence(payload.Amount)
	if !ok || amount <= 0 {
		http.Error(w, "bad amount", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(payload.FromName)
	if name == "" {
		name = "Anonymous"
	}
	err := s.store.AddDonation(store.Donation{
		ExternalID: payload.TransactionID, Name: name, Message: payload.Message,
		AmountMinor: amount, Currency: payload.Currency, Public: payload.IsPublic,
	})
	if err != nil {
		log.Printf("web: ko-fi webhook: %v", err)
		http.Error(w, "not recorded", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// donorView is one line of the supporters list with the amount formatted.
type donorView struct {
	Name   string
	Amount string
	Count  int
}

type supportData struct {
	KofiURL string
	Donors  []donorView
	Total   string // everything given so far, or empty before the first gift
	Gifts   int
}

func (s *Server) handleSupport(w http.ResponseWriter, r *http.Request) {
	if s.config.KofiURL == "" {
		s.renderError(w, r, http.StatusNotFound, "Page not found.")
		return
	}
	donors, err := s.store.TopDonors()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not load supporters.")
		return
	}
	data := supportData{KofiURL: s.config.KofiURL}
	currency := ""
	for _, donor := range donors {
		if currency == "" {
			currency = donor.Currency
		}
		data.Donors = append(data.Donors, donorView{Name: donor.Name, Amount: formatGift(donor.AmountMinor, donor.Currency), Count: donor.Count})
	}
	if currency != "" {
		total, gifts, err := s.store.DonationTotal(currency)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not load supporters.")
			return
		}
		data.Total = formatGift(total, currency)
		data.Gifts = gifts
	}
	s.renderMeta(w, r, http.StatusOK, "support", "Support sprue", data,
		meta{Description: "sprue runs on donations. Buy the site a coffee and keep the bench open for everyone."})
}

func (s *Server) handleAdminRemoveDonation(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if err := s.store.RemoveDonation(parseID(r.PathValue("id"))); err != nil {
		flashRedirect(w, r, "/admin", "", "Could not remove that.")
		return
	}
	flashRedirect(w, r, "/admin", "Removed.", "")
}

// currencySymbols covers the currencies Ko-fi pays out in that have a
// familiar sign. Anything else is shown with its code.
var currencySymbols = map[string]string{"GBP": "£", "USD": "$", "EUR": "€", "CAD": "CA$", "AUD": "A$", "NZD": "NZ$", "JPY": "¥"}

func formatGift(minor int64, currency string) string {
	if symbol, ok := currencySymbols[currency]; ok {
		return symbol + money(minor)
	}
	return fmt.Sprintf("%s %s", money(minor), currency)
}
