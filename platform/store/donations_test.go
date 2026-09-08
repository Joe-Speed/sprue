package store

import (
	"path/filepath"
	"testing"
)

func TestDonations(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	good := Donation{ExternalID: "tx-1", Name: "Sam", AmountMinor: 300, Currency: "gbp", Public: true}
	if err := st.AddDonation(good); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDonation(good); err != nil {
		t.Errorf("repeat of the same transaction should be ignored, got %v", err)
	}
	bad := []Donation{
		{ExternalID: "", Name: "Sam", AmountMinor: 300, Currency: "GBP"},
		{ExternalID: "tx-2", Name: "", AmountMinor: 300, Currency: "GBP"},
		{ExternalID: "tx-3", Name: "Sam", AmountMinor: 0, Currency: "GBP"},
		{ExternalID: "tx-4", Name: "Sam", AmountMinor: maxDonationMinor + 1, Currency: "GBP"},
		{ExternalID: "tx-5", Name: "Sam", AmountMinor: 300, Currency: "POUNDS"},
	}
	for i, d := range bad {
		if err := st.AddDonation(d); err == nil {
			t.Errorf("bad donation %d accepted", i)
		}
	}
	recent, err := st.RecentDonations()
	if err != nil || len(recent) != 1 || recent[0].Currency != "GBP" || !recent[0].Public {
		t.Fatalf("recent: %+v %v", recent, err)
	}
	if err := st.RemoveDonation(recent[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveDonation(recent[0].ID); err != ErrNotFound {
		t.Errorf("removing twice: %v", err)
	}
	donors, err := st.TopDonors()
	if err != nil || len(donors) != 0 {
		t.Errorf("after removal: %+v %v", donors, err)
	}
}
