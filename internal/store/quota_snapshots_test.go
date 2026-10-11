package store

import (
	"testing"
	"time"
)

func TestStore_GetLatestQuotaSnapshot_None(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a := newTestAccount(t, s, "anthropic", "personal")

	got, ok, err := s.GetLatestQuotaSnapshot(a.ID)
	if err != nil {
		t.Fatalf("GetLatestQuotaSnapshot() error = %v", err)
	}
	if ok || got.ID != 0 {
		t.Fatalf("GetLatestQuotaSnapshot() = (%+v, %v), want zero, false", got, ok)
	}
}

func TestStore_GetLatestQuotaSnapshot_PicksNewest(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a := newTestAccount(t, s, "anthropic", "personal")

	base := time.Now().UTC().Truncate(time.Second)
	older, err := s.CreateQuotaSnapshot(QuotaSnapshot{AccountID: a.ID, UsageData: `{"a":1}`, PolledAt: base, ExpiresAt: base.Add(time.Hour)})
	if err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}
	newest, err := s.CreateQuotaSnapshot(QuotaSnapshot{AccountID: a.ID, UsageData: `{"a":2}`, PolledAt: base.Add(time.Minute), ExpiresAt: base.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}

	got, ok, err := s.GetLatestQuotaSnapshot(a.ID)
	if err != nil {
		t.Fatalf("GetLatestQuotaSnapshot() error = %v", err)
	}
	if !ok || got.ID != newest.ID || got.UsageData != `{"a":2}` {
		t.Fatalf("GetLatestQuotaSnapshot() = (%+v, %v), want newest snapshot %d", got, ok, newest.ID)
	}

	// Same polled_at: id breaks the tie, favoring the later insert.
	sameTime, err := s.CreateQuotaSnapshot(QuotaSnapshot{AccountID: a.ID, UsageData: `{"a":3}`, PolledAt: newest.PolledAt, ExpiresAt: base.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}
	got, ok, err = s.GetLatestQuotaSnapshot(a.ID)
	if err != nil {
		t.Fatalf("GetLatestQuotaSnapshot() error = %v", err)
	}
	if !ok || got.ID != sameTime.ID || got.ID <= older.ID {
		t.Fatalf("GetLatestQuotaSnapshot() tiebreak = (%+v, %v), want id %d", got, ok, sameTime.ID)
	}
}

func TestStore_ReplaceQuotaSnapshot_PrunesOldRows(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a := newTestAccount(t, s, "anthropic", "personal")
	b := newTestAccount(t, s, "openai", "work")

	base := time.Now().UTC().Truncate(time.Second)
	for i := range 3 {
		if _, err := s.CreateQuotaSnapshot(QuotaSnapshot{AccountID: a.ID, UsageData: `{"i":0}`, PolledAt: base.Add(time.Duration(i) * time.Minute), ExpiresAt: base.Add(time.Hour)}); err != nil {
			t.Fatalf("CreateQuotaSnapshot() error = %v", err)
		}
	}
	bSnap, err := s.CreateQuotaSnapshot(QuotaSnapshot{AccountID: b.ID, UsageData: `{"b":1}`, PolledAt: base, ExpiresAt: base.Add(time.Hour)})
	if err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}

	latest, err := s.ReplaceQuotaSnapshot(QuotaSnapshot{AccountID: a.ID, UsageData: `{"a":"new"}`, PolledAt: base.Add(10 * time.Minute), ExpiresAt: base.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("ReplaceQuotaSnapshot() error = %v", err)
	}
	if latest.ID == 0 {
		t.Fatal("ReplaceQuotaSnapshot() returned zero ID, want set")
	}

	all, err := s.ListQuotaSnapshots()
	if err != nil {
		t.Fatalf("ListQuotaSnapshots() error = %v", err)
	}
	var aRows, bRows []QuotaSnapshot
	for _, q := range all {
		switch q.AccountID {
		case a.ID:
			aRows = append(aRows, q)
		case b.ID:
			bRows = append(bRows, q)
		}
	}
	if len(aRows) != 1 || aRows[0].ID != latest.ID || aRows[0].UsageData != `{"a":"new"}` {
		t.Fatalf("account A rows = %+v, want exactly the replaced row", aRows)
	}
	if len(bRows) != 1 || bRows[0].ID != bSnap.ID {
		t.Fatalf("account B rows = %+v, want untouched %d", bRows, bSnap.ID)
	}
}

func TestStore_ReplaceQuotaSnapshot_Atomic(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a := newTestAccount(t, s, "anthropic", "personal")

	base := time.Now().UTC().Truncate(time.Second)
	existing, err := s.CreateQuotaSnapshot(QuotaSnapshot{AccountID: a.ID, UsageData: `{"keep":1}`, PolledAt: base, ExpiresAt: base.Add(time.Hour)})
	if err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}

	// schema.sql declares quota_snapshots.account_id REFERENCES accounts(id)
	// and Open enables foreign_keys, so a snapshot for a missing account
	// fails the insert; the transaction must roll back before the delete.
	if _, err := s.ReplaceQuotaSnapshot(QuotaSnapshot{AccountID: a.ID + 999, UsageData: `{"x":1}`, PolledAt: base, ExpiresAt: base.Add(time.Hour)}); err == nil {
		t.Fatal("ReplaceQuotaSnapshot() error = nil, want FK violation for missing account")
	}

	all, err := s.ListQuotaSnapshots()
	if err != nil {
		t.Fatalf("ListQuotaSnapshots() error = %v", err)
	}
	if len(all) != 1 || all[0].ID != existing.ID {
		t.Fatalf("snapshots after failed replace = %+v, want existing row %d untouched", all, existing.ID)
	}
}

func TestStore_DeleteAccount_RemovesSnapshots(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a := newTestAccount(t, s, "anthropic", "personal")

	base := time.Now().UTC().Truncate(time.Second)
	if _, err := s.CreateQuotaSnapshot(QuotaSnapshot{AccountID: a.ID, UsageData: `{"a":1}`, PolledAt: base, ExpiresAt: base.Add(time.Hour)}); err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}
	if err := s.DeleteAccount(a.ID); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	if _, ok, err := s.GetLatestQuotaSnapshot(a.ID); err != nil || ok {
		t.Fatalf("GetLatestQuotaSnapshot() after delete = (%v, %v), want false, nil", ok, err)
	}
	all, err := s.ListQuotaSnapshots()
	if err != nil {
		t.Fatalf("ListQuotaSnapshots() error = %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("ListQuotaSnapshots() after delete = %+v, want empty", all)
	}
}
