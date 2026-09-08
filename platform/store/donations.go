package store

import (
	"errors"
	"strings"
)

const (
	MaxDonations     = 10000 // total rows kept; beyond this new ones are refused
	MaxTopDonors     = 20
	MaxRecentDonors  = 200
	maxDonorName     = 80
	maxDonorMessage  = 500
	maxDonationMinor = 100000000 // one million in the smallest unit, a sanity cap
)

// Donation is one payment reported by Ko-fi's webhook. Amounts are kept in
// the smallest unit of the currency so totals add without rounding.
type Donation struct {
	ID          int64
	ExternalID  string // Ko-fi's transaction id, unique so retries never double count
	Name        string
	Message     string
	AmountMinor int64
	Currency    string // three letter code as Ko-fi sends it
	Public      bool
	CreatedAt   string
}

// Donor is one supporter's total across every donation they made under a
// name, for the supporters list.
type Donor struct {
	Name        string
	AmountMinor int64
	Currency    string
	Count       int
	FirstAt     string
}

// AddDonation records a payment. A repeat of the same external id is
// ignored, so a webhook retry is harmless. Returns ErrLimit at the cap.
func (s *Store) AddDonation(d Donation) error {
	d.ExternalID = strings.TrimSpace(d.ExternalID)
	d.Name = clip(d.Name, maxDonorName)
	d.Message = clip(d.Message, maxDonorMessage)
	d.Currency = strings.ToUpper(strings.TrimSpace(d.Currency))
	if d.ExternalID == "" || d.Name == "" || len(d.Currency) != 3 {
		return errors.New("store: donation needs an id, a name, and a currency")
	}
	if d.AmountMinor <= 0 || d.AmountMinor > maxDonationMinor {
		return errors.New("store: donation amount out of range")
	}
	var count int
	if err := s.db.QueryRow(`select count(*) from donations`).Scan(&count); err != nil {
		return err
	}
	if count >= MaxDonations {
		return ErrLimit
	}
	_, err := s.db.Exec(`insert or ignore into donations (external_id, name, message, amount_minor, currency, public, created_at)
		values (?, ?, ?, ?, ?, ?, ?)`,
		d.ExternalID, d.Name, d.Message, d.AmountMinor, d.Currency, boolInt(d.Public), now())
	return err
}

// TopDonors lists public supporters by what they have given in total, most
// first, earliest supporter first on a tie. Anonymous gifts are not listed.
func (s *Store) TopDonors() ([]Donor, error) {
	rows, err := s.db.Query(`select name, sum(amount_minor), currency, count(*), min(created_at)
		from donations where public = 1
		group by name, currency order by sum(amount_minor) desc, min(created_at) asc limit ?`, MaxTopDonors)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Donor
	for rows.Next() {
		var donor Donor
		if err := rows.Scan(&donor.Name, &donor.AmountMinor, &donor.Currency, &donor.Count, &donor.FirstAt); err != nil {
			return nil, err
		}
		list = append(list, donor)
	}
	return list, rows.Err()
}

// DonationTotal is everything given in one currency, anonymous gifts
// included, and how many gifts that was.
func (s *Store) DonationTotal(currency string) (int64, int, error) {
	var total int64
	var count int
	err := s.db.QueryRow(`select coalesce(sum(amount_minor), 0), count(*) from donations where currency = ?`,
		strings.ToUpper(currency)).Scan(&total, &count)
	return total, count, err
}

// RecentDonations lists the newest gifts for the admin page, where a test
// payment or an unwelcome name can be removed.
func (s *Store) RecentDonations() ([]Donation, error) {
	rows, err := s.db.Query(`select id, external_id, name, message, amount_minor, currency, public, created_at
		from donations order by id desc limit ?`, MaxRecentDonors)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Donation
	for rows.Next() {
		var d Donation
		var public int
		if err := rows.Scan(&d.ID, &d.ExternalID, &d.Name, &d.Message, &d.AmountMinor, &d.Currency, &public, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Public = public != 0
		list = append(list, d)
	}
	return list, rows.Err()
}

// RemoveDonation deletes one record. The payment itself lives at Ko-fi.
func (s *Store) RemoveDonation(id int64) error {
	return s.updateOwned(`delete from donations where id = ?`, id)
}
