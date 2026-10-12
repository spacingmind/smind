package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStore_QuotaPollState_RoundTrip(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	keep := newTestAccount(t, s, "anthropic", "keep")

	now := time.Now().UTC().Truncate(time.Second)
	q := QuotaPollState{
		AccountID:           keep.ID,
		LastAttemptAt:       now,
		NextAllowedAt:       now.Add(30 * time.Minute),
		ConsecutiveFailures: 3,
		ForcedAt:            []int64{1000, 2000, 3000},
		RejectedFP:          "sha256:abc",
		LastError:           "http_429",
	}
	if err := s.SaveQuotaPollState(q); err != nil {
		t.Fatalf("SaveQuotaPollState() error = %v", err)
	}
	got, ok, err := s.GetQuotaPollState(keep.ID)
	if err != nil || !ok {
		t.Fatalf("GetQuotaPollState() = %v, %v, want true, nil", ok, err)
	}
	if got.AccountID != q.AccountID || !got.LastAttemptAt.Equal(q.LastAttemptAt) || !got.NextAllowedAt.Equal(q.NextAllowedAt) ||
		got.ConsecutiveFailures != q.ConsecutiveFailures || got.RejectedFP != q.RejectedFP || got.LastError != q.LastError {
		t.Fatalf("GetQuotaPollState() = %+v, want %+v", got, q)
	}
	if len(got.ForcedAt) != 3 || got.ForcedAt[0] != 1000 || got.ForcedAt[2] != 3000 {
		t.Fatalf("GetQuotaPollState() ForcedAt = %v, want [1000 2000 3000]", got.ForcedAt)
	}

	// Zero times mean never/no backoff and must persist as NULLs.
	blank := QuotaPollState{AccountID: keep.ID}
	if err := s.SaveQuotaPollState(blank); err != nil {
		t.Fatalf("SaveQuotaPollState(blank) error = %v", err)
	}
	got, _, err = s.GetQuotaPollState(keep.ID)
	if err != nil {
		t.Fatalf("GetQuotaPollState() after blank error = %v", err)
	}
	if !got.LastAttemptAt.IsZero() || !got.NextAllowedAt.IsZero() {
		t.Fatalf("blank round-trip times = %v/%v, want zero (NULL)", got.LastAttemptAt, got.NextAllowedAt)
	}
	var lastNull, nextNull bool
	if err := s.db.QueryRow(`SELECT last_attempt_at IS NULL, next_allowed_at IS NULL FROM quota_poll_state WHERE account_id = ?`, keep.ID).Scan(&lastNull, &nextNull); err != nil {
		t.Fatalf("check NULLs: %v", err)
	}
	if !lastNull || !nextNull {
		t.Fatalf("stored NULLs = %v/%v, want true/true", lastNull, nextNull)
	}
}

func TestStore_QuotaPollState_NoRow(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	got, ok, err := s.GetQuotaPollState(999)
	if err != nil {
		t.Fatalf("GetQuotaPollState(999) error = %v", err)
	}
	if ok || got.AccountID != 0 || got.ForcedAt != nil {
		t.Fatalf("GetQuotaPollState(999) = %+v, %v, want zero value, false", got, ok)
	}
}

func TestStore_QuotaPollState_UpsertOverwrites(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	acct := newTestAccount(t, s, "anthropic", "acct")

	now := time.Now().UTC().Truncate(time.Second)
	if err := s.SaveQuotaPollState(QuotaPollState{
		AccountID: acct.ID, LastAttemptAt: now, ConsecutiveFailures: 1, LastError: "http_500",
	}); err != nil {
		t.Fatalf("SaveQuotaPollState() first error = %v", err)
	}
	later := now.Add(time.Hour)
	if err := s.SaveQuotaPollState(QuotaPollState{
		AccountID: acct.ID, NextAllowedAt: later, ConsecutiveFailures: 0, RejectedFP: "sha256:x", LastError: "",
	}); err != nil {
		t.Fatalf("SaveQuotaPollState() second error = %v", err)
	}

	got, ok, err := s.GetQuotaPollState(acct.ID)
	if err != nil || !ok {
		t.Fatalf("GetQuotaPollState() = %v, %v, want true, nil", ok, err)
	}
	if !got.LastAttemptAt.IsZero() || !got.NextAllowedAt.Equal(later) || got.ConsecutiveFailures != 0 ||
		got.RejectedFP != "sha256:x" || got.LastError != "" {
		t.Fatalf("GetQuotaPollState() after upsert = %+v, want fully overwritten by the second save", got)
	}
	var rows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM quota_poll_state WHERE account_id = ?`, acct.ID).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("row count = %d, %v, want 1", rows, err)
	}
}

func TestStore_QuotaPollState_ForcedAtTruncatedToSix(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	acct := newTestAccount(t, s, "anthropic", "acct")

	forced := []int64{1, 2, 3, 4, 5, 6, 7, 8}
	if err := s.SaveQuotaPollState(QuotaPollState{AccountID: acct.ID, ForcedAt: forced}); err != nil {
		t.Fatalf("SaveQuotaPollState() error = %v", err)
	}
	got, _, err := s.GetQuotaPollState(acct.ID)
	if err != nil {
		t.Fatalf("GetQuotaPollState() error = %v", err)
	}
	want := []int64{3, 4, 5, 6, 7, 8}
	if len(got.ForcedAt) != 6 {
		t.Fatalf("ForcedAt length = %d, want 6", len(got.ForcedAt))
	}
	for i := range want {
		if got.ForcedAt[i] != want[i] {
			t.Fatalf("ForcedAt = %v, want %v (newest 6)", got.ForcedAt, want)
		}
	}
}

func TestStore_QuotaPollState_FKMissingAccount(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if err := s.SaveQuotaPollState(QuotaPollState{AccountID: 999}); err == nil {
		t.Fatal("SaveQuotaPollState(999) error = nil, want foreign key error")
	}
}

// TestStore_QuotaPollState_DeletedWithAccount: DeleteAccount also removes the
// account's budget row, while a sibling account's row survives.
func TestStore_QuotaPollState_DeletedWithAccount(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	keep := newTestAccount(t, s, "anthropic", "keep")
	kill := newTestAccount(t, s, "anthropic", "kill")
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []int64{keep.ID, kill.ID} {
		if err := s.SaveQuotaPollState(QuotaPollState{AccountID: id, LastAttemptAt: now}); err != nil {
			t.Fatalf("SaveQuotaPollState(%d) error = %v", id, err)
		}
	}

	if err := s.DeleteAccount(kill.ID); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	if _, ok, err := s.GetQuotaPollState(kill.ID); err != nil || ok {
		t.Fatalf("GetQuotaPollState(kill) = %v, %v, want false, nil", ok, err)
	}
	if _, ok, err := s.GetQuotaPollState(keep.ID); err != nil || !ok {
		t.Fatalf("GetQuotaPollState(keep) = %v, %v, want true, nil", ok, err)
	}
}

// TestStore_QuotaPollState_ExistingDBGetsTable is plan scenario
// budget_state_table_created_on_existing_db: a database created with the
// previous schema (no quota_poll_state table) gets it on the next Open.
func TestStore_QuotaPollState_ExistingDBGetsTable(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open raw db error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.Exec(`CREATE TABLE accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT, provider TEXT NOT NULL, label TEXT NOT NULL,
		credential_type TEXT NOT NULL, credential_data TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL)`); err != nil {
		t.Fatalf("create old accounts table: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO accounts (id, provider, label, credential_type, credential_data, created_at, updated_at)
		 VALUES (1, 'anthropic', 'old', 'api_key', '{"key":"k"}', ?, ?)`, now, now,
	); err != nil {
		t.Fatalf("seed old account: %v", err)
	}
	db.Close()

	for i := 0; i < 2; i++ { // second Open proves idempotence
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		var name string
		if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'quota_poll_state'`).Scan(&name); err != nil {
			t.Fatalf("quota_poll_state table not found after Open #%d: %v", i, err)
		}
		if i == 0 {
			if err := s.SaveQuotaPollState(QuotaPollState{AccountID: 1, ConsecutiveFailures: 2}); err != nil {
				t.Fatalf("SaveQuotaPollState() on migrated db error = %v", err)
			}
		} else {
			got, ok, err := s.GetQuotaPollState(1)
			if err != nil || !ok || got.ConsecutiveFailures != 2 {
				t.Fatalf("GetQuotaPollState() after reopen = %+v, %v, %v", got, ok, err)
			}
		}
		s.Close()
	}
}
