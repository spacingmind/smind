package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// oldAccountsDB creates a raw database holding only the pre-ADR-0028-§7
// accounts table plus one account row, and returns its path.
func oldAccountsDB(t *testing.T, extraDDL ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open raw db error = %v", err)
	}
	defer db.Close()
	now := time.Now().UTC().Truncate(time.Second)
	stmts := append([]string{`CREATE TABLE accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT, provider TEXT NOT NULL, label TEXT NOT NULL,
		credential_type TEXT NOT NULL, credential_data TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL)`}, extraDDL...)
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO accounts (id, provider, label, credential_type, credential_data, created_at, updated_at)
		 VALUES (1, 'anthropic', 'old', 'api_key', '{"key":"k"}', ?, ?)`, now, now,
	); err != nil {
		t.Fatalf("seed old account: %v", err)
	}
	return path
}

// TestStore_QuotaCooldown_TableCreatedOnExistingDB is plan scenario
// quota_cooldown_table_created_on_existing_db.
func TestStore_QuotaCooldown_TableCreatedOnExistingDB(t *testing.T) {
	t.Parallel()

	path := oldAccountsDB(t)
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		var name string
		if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'quota_cooldown'`).Scan(&name); err != nil {
			t.Fatalf("quota_cooldown table not found after Open #%d: %v", i, err)
		}
		now := time.Now().UTC().Truncate(time.Second)
		if err := s.SaveQuotaCooldown(QuotaCooldown{AccountID: 1, Until: now.Add(time.Hour), Reason: "r", ObservedAt: now}); err != nil {
			t.Fatalf("SaveQuotaCooldown() on migrated db error = %v", err)
		}
		s.Close()
	}
}

// TestStore_QuotaSnapshotsKindColumnMigrationOnExistingDB is plan scenario
// quota_snapshots_kind_column_migration_on_existing_db.
func TestStore_QuotaSnapshotsKindColumnMigrationOnExistingDB(t *testing.T) {
	t.Parallel()

	path := oldAccountsDB(t, `CREATE TABLE quota_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT, account_id INTEGER NOT NULL REFERENCES accounts(id),
		usage_data TEXT NOT NULL, polled_at TIMESTAMP NOT NULL, expires_at TIMESTAMP NOT NULL)`)
	raw, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open raw db error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := raw.Exec(`INSERT INTO quota_snapshots (account_id, usage_data, polled_at, expires_at) VALUES (1, '{"old":1}', ?, ?)`, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("seed old snapshot: %v", err)
	}
	raw.Close()

	for i := 0; i < 2; i++ { // second Open proves idempotence
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		got, ok, err := s.GetLatestQuotaSnapshot(1)
		if err != nil || !ok {
			t.Fatalf("GetLatestQuotaSnapshot() #%d = %+v, %v, %v", i, got, ok, err)
		}
		if got.Kind != QuotaKindFetched || got.UsageData != `{"old":1}` {
			t.Fatalf("snapshot #%d = %+v, want kind %q with old data", i, got, QuotaKindFetched)
		}
		s.Close()
	}
}

// TestStore_QuotaCooldown_DeletedWithAccount is plan scenario
// quota_cooldown_deleted_with_account.
func TestStore_QuotaCooldown_DeletedWithAccount(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	keep := newTestAccount(t, s, "anthropic", "keep")
	kill := newTestAccount(t, s, "anthropic", "kill")
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []int64{keep.ID, kill.ID} {
		if err := s.SaveQuotaCooldown(QuotaCooldown{AccountID: id, Until: now.Add(time.Hour), Reason: "r", ObservedAt: now}); err != nil {
			t.Fatalf("SaveQuotaCooldown(%d) error = %v", id, err)
		}
	}
	if err := s.DeleteAccount(kill.ID); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	if got, err := s.ListQuotaCooldowns(kill.ID); err != nil || len(got) != 0 {
		t.Fatalf("ListQuotaCooldowns(kill) = %+v, %v, want empty", got, err)
	}
	if got, err := s.ListQuotaCooldowns(keep.ID); err != nil || len(got) != 1 {
		t.Fatalf("ListQuotaCooldowns(keep) = %+v, %v, want 1 row", got, err)
	}
}

// TestStore_QuotaSnapshots_RetentionLatestPerAccountAndKind is plan scenario
// retention_latest_per_account_and_kind.
func TestStore_QuotaSnapshots_RetentionLatestPerAccountAndKind(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a := newTestAccount(t, s, "anthropic", "a")
	base := time.Now().UTC().Truncate(time.Second)
	mk := func(kind, data string, off time.Duration) QuotaSnapshot {
		return QuotaSnapshot{AccountID: a.ID, UsageData: data, PolledAt: base.Add(off), ExpiresAt: base.Add(off + time.Hour), Kind: kind}
	}
	for _, q := range []QuotaSnapshot{
		mk("", "f1", 0), mk(QuotaKindObserved, "o1", time.Minute),
		mk(QuotaKindFetched, "f2", 2*time.Minute), mk(QuotaKindObserved, "o2", 3*time.Minute),
	} {
		if _, err := s.ReplaceQuotaSnapshot(q); err != nil {
			t.Fatalf("ReplaceQuotaSnapshot(%s) error = %v", q.UsageData, err)
		}
	}
	all, err := s.ListQuotaSnapshots()
	if err != nil {
		t.Fatalf("ListQuotaSnapshots() error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListQuotaSnapshots() = %+v, want one fetched and one observed row", all)
	}
	f, ok, err := s.GetLatestQuotaSnapshot(a.ID)
	if err != nil || !ok || f.UsageData != "f2" || f.Kind != QuotaKindFetched {
		t.Fatalf("GetLatestQuotaSnapshot() = %+v, %v, %v, want f2 fetched", f, ok, err)
	}
	o, ok, err := s.GetLatestQuotaSnapshotKind(a.ID, QuotaKindObserved)
	if err != nil || !ok || o.UsageData != "o2" || o.Kind != QuotaKindObserved {
		t.Fatalf("GetLatestQuotaSnapshotKind(observed) = %+v, %v, %v, want o2 observed", o, ok, err)
	}

	// An account with only an observed row has no fetched snapshot.
	b := newTestAccount(t, s, "anthropic", "b")
	if _, err := s.ReplaceQuotaSnapshot(QuotaSnapshot{AccountID: b.ID, UsageData: "o", PolledAt: base, ExpiresAt: base, Kind: QuotaKindObserved}); err != nil {
		t.Fatalf("ReplaceQuotaSnapshot() error = %v", err)
	}
	if _, ok, err := s.GetLatestQuotaSnapshot(b.ID); err != nil || ok {
		t.Fatalf("GetLatestQuotaSnapshot(b) ok = %v, err = %v, want false, nil", ok, err)
	}
}

// TestStore_QuotaCooldown_SaveListRoundtripAndUpsert is plan scenario
// quota_cooldown_save_list_roundtrip_and_upsert.
func TestStore_QuotaCooldown_SaveListRoundtripAndUpsert(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "smind.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	a := newTestAccount(t, s, "anthropic", "a")
	now := time.Now().UTC().Truncate(time.Second)
	wide := QuotaCooldown{AccountID: a.ID, Until: now.Add(time.Hour), Reason: "five_hour", Level: 1, ObservedAt: now}
	scoped := QuotaCooldown{AccountID: a.ID, ScopeModel: "opus", Until: now.Add(2 * time.Hour), Reason: "model", Level: 2, ObservedAt: now}
	for _, c := range []QuotaCooldown{scoped, wide} {
		if err := s.SaveQuotaCooldown(c); err != nil {
			t.Fatalf("SaveQuotaCooldown() error = %v", err)
		}
	}
	// Upsert overwrites the credential-wide row in place.
	wide.Level, wide.Reason, wide.Until = 3, "weekly", now.Add(3*time.Hour)
	if err := s.SaveQuotaCooldown(wide); err != nil {
		t.Fatalf("SaveQuotaCooldown(upsert) error = %v", err)
	}
	// Expired rows are still listed.
	expired := QuotaCooldown{AccountID: a.ID, ScopeModel: "zzz", Until: now.Add(-time.Hour), Reason: "old", ObservedAt: now.Add(-2 * time.Hour)}
	if err := s.SaveQuotaCooldown(expired); err != nil {
		t.Fatalf("SaveQuotaCooldown(expired) error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer s.Close()
	got, err := s.ListQuotaCooldowns(a.ID)
	if err != nil {
		t.Fatalf("ListQuotaCooldowns() error = %v", err)
	}
	want := []QuotaCooldown{wide, scoped, expired} // ordered by scope_model: "", opus, zzz
	if len(got) != len(want) {
		t.Fatalf("ListQuotaCooldowns() = %+v, want %+v", got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.AccountID != w.AccountID || g.ScopeModel != w.ScopeModel || g.Reason != w.Reason || g.Level != w.Level ||
			!g.Until.Equal(w.Until) || !g.ObservedAt.Equal(w.ObservedAt) {
			t.Fatalf("row %d = %+v, want %+v", i, g, w)
		}
	}

	if err := s.DeleteQuotaCooldown(a.ID, "opus"); err != nil {
		t.Fatalf("DeleteQuotaCooldown() error = %v", err)
	}
	if got, _ := s.ListQuotaCooldowns(a.ID); len(got) != 2 {
		t.Fatalf("after DeleteQuotaCooldown rows = %+v, want 2", got)
	}
	if err := s.DeleteQuotaCooldowns(a.ID); err != nil {
		t.Fatalf("DeleteQuotaCooldowns() error = %v", err)
	}
	if got, _ := s.ListQuotaCooldowns(a.ID); len(got) != 0 {
		t.Fatalf("after DeleteQuotaCooldowns rows = %+v, want none", got)
	}
}

// TestStore_QuotaCooldown_CredentialReplaceClearsButRefreshDoesNot is plan
// scenario credential_replace_clears_cooldown_but_refresh_does_not.
func TestStore_QuotaCooldown_CredentialReplaceClearsButRefreshDoesNot(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a := newTestAccount(t, s, "anthropic", "a")
	other := newTestAccount(t, s, "anthropic", "other")
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []int64{a.ID, other.ID} {
		if err := s.SaveQuotaCooldown(QuotaCooldown{AccountID: id, Until: now.Add(time.Hour), Reason: "r", ObservedAt: now}); err != nil {
			t.Fatalf("SaveQuotaCooldown() error = %v", err)
		}
	}

	if _, err := s.UpdateAccountCredential(a.ID, `{"key":"refreshed"}`); err != nil {
		t.Fatalf("UpdateAccountCredential() error = %v", err)
	}
	if got, err := s.ListQuotaCooldowns(a.ID); err != nil || len(got) != 1 {
		t.Fatalf("after refresh cooldowns = %+v, %v, want 1", got, err)
	}

	if _, err := s.ReplaceAccountCredential(a.ID, "api_key", `{"key":"new"}`); err != nil {
		t.Fatalf("ReplaceAccountCredential() error = %v", err)
	}
	if got, err := s.ListQuotaCooldowns(a.ID); err != nil || len(got) != 0 {
		t.Fatalf("after replace cooldowns = %+v, %v, want none", got, err)
	}
	if got, err := s.ListQuotaCooldowns(other.ID); err != nil || len(got) != 1 {
		t.Fatalf("other account cooldowns = %+v, %v, want 1", got, err)
	}
}

// TestStore_QuotaCooldown_UpsertNeverShortensUntil: a later write with an earlier
// until keeps the longer one but still replaces level, reason and observed_at
// (the level-reset write), and a delete still removes the row.
func TestStore_QuotaCooldown_UpsertNeverShortensUntil(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a := newTestAccount(t, s, "anthropic", "a")
	now := time.Now().UTC().Truncate(time.Second)
	long := now.Add(72 * time.Hour)
	if err := s.SaveQuotaCooldown(QuotaCooldown{AccountID: a.ID, Until: long, Reason: "fallback", Level: 3, ObservedAt: now}); err != nil {
		t.Fatalf("SaveQuotaCooldown() error = %v", err)
	}
	if err := s.SaveQuotaCooldown(QuotaCooldown{AccountID: a.ID, Until: now.Add(15 * time.Second), Reason: "rejected", Level: 0, ObservedAt: now.Add(time.Second)}); err != nil {
		t.Fatalf("SaveQuotaCooldown(shorter) error = %v", err)
	}
	got, err := s.ListQuotaCooldowns(a.ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListQuotaCooldowns() = %+v, %v", got, err)
	}
	if !got[0].Until.Equal(long) || got[0].Level != 0 || got[0].Reason != "rejected" || !got[0].ObservedAt.Equal(now.Add(time.Second)) {
		t.Fatalf("row = %+v, want until %v kept, level 0, reason rejected, observed_at replaced", got[0], long)
	}
	if err := s.DeleteQuotaCooldown(a.ID, ""); err != nil {
		t.Fatalf("DeleteQuotaCooldown() error = %v", err)
	}
	if got, _ := s.ListQuotaCooldowns(a.ID); len(got) != 0 {
		t.Fatalf("after delete rows = %+v, want none", got)
	}
}

// TestStore_QuotaCooldown_CredentialReplaceIsAtomic: when the cooldown delete
// fails, the credential update is rolled back.
func TestStore_QuotaCooldown_CredentialReplaceIsAtomic(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a := newTestAccount(t, s, "anthropic", "a")
	before, err := s.GetAccount(a.ID)
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if _, err := s.db.Exec(`DROP TABLE quota_cooldown`); err != nil {
		t.Fatalf("drop quota_cooldown: %v", err)
	}

	if _, err := s.ReplaceAccountCredential(a.ID, "oauth", `{"access_token":"new"}`); err == nil {
		t.Fatal("ReplaceAccountCredential() error = nil, want the failed cooldown delete reported")
	}
	after, err := s.GetAccount(a.ID)
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if after.CredentialType != before.CredentialType || after.CredentialData != before.CredentialData || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("account after a failed replace = %+v, want unchanged %+v", after, before)
	}
}

// TestStore_QuotaCooldown_RejectedWindowMigrationOnExistingDB is
// quota_cooldown_rejected_window_migration_on_existing_db: a database that
// already has the pre-column quota_cooldown table gets the column on Open (twice:
// idempotent), keeps its rows with an empty window, and saves the new field.
func TestStore_QuotaCooldown_RejectedWindowMigrationOnExistingDB(t *testing.T) {
	t.Parallel()

	path := oldAccountsDB(t, `CREATE TABLE quota_cooldown (
		account_id INTEGER NOT NULL REFERENCES accounts(id),
		scope_model TEXT NOT NULL DEFAULT '',
		until TIMESTAMP NOT NULL,
		reason TEXT NOT NULL,
		level INTEGER NOT NULL DEFAULT 0,
		observed_at TIMESTAMP NOT NULL,
		PRIMARY KEY (account_id, scope_model))`)
	raw, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open raw db error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := raw.Exec(`INSERT INTO quota_cooldown (account_id, scope_model, until, reason, level, observed_at) VALUES (1, '', ?, 'rejected', 0, ?)`, now.Add(time.Hour), now); err != nil {
		t.Fatalf("seed old cooldown: %v", err)
	}
	raw.Close()

	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open() #%d error = %v", i, err)
		}
		got, err := s.ListQuotaCooldowns(1)
		if err != nil || len(got) != 1 {
			t.Fatalf("ListQuotaCooldowns() #%d = %+v, %v", i, got, err)
		}
		if i == 0 {
			if got[0].RejectedWindow != "" || got[0].Reason != "rejected" {
				t.Fatalf("old row = %+v, want empty window", got[0])
			}
			c := got[0]
			c.RejectedWindow = "weekly"
			if err := s.SaveQuotaCooldown(c); err != nil {
				t.Fatalf("SaveQuotaCooldown() on migrated db error = %v", err)
			}
		} else if got[0].RejectedWindow != "weekly" {
			t.Fatalf("window after reopen = %q, want weekly", got[0].RejectedWindow)
		}
		s.Close()
	}
}

// TestStore_QuotaCooldown_RejectedWindowRoundtrip: the window is saved, listed,
// replaced by an upsert (even with an equal until) and defaults to "".
func TestStore_QuotaCooldown_RejectedWindowRoundtrip(t *testing.T) {
	t.Parallel()

	s, err := Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()
	a := newTestAccount(t, s, "anthropic", "a")
	now := time.Now().UTC().Truncate(time.Second)
	wide := QuotaCooldown{AccountID: a.ID, Until: now.Add(time.Hour), Reason: "rejected", RejectedWindow: "five_hour", ObservedAt: now}
	scoped := QuotaCooldown{AccountID: a.ID, ScopeModel: "opus", Until: now.Add(time.Hour), Reason: "fallback", Level: 2, RejectedWindow: "weekly_model", ObservedAt: now}
	bare := QuotaCooldown{AccountID: a.ID, ScopeModel: "zzz", Until: now.Add(time.Hour), Reason: "rejected", ObservedAt: now}
	for _, c := range []QuotaCooldown{wide, scoped, bare} {
		if err := s.SaveQuotaCooldown(c); err != nil {
			t.Fatalf("SaveQuotaCooldown() error = %v", err)
		}
	}
	wide.RejectedWindow = "weekly" // a later rejection extends and renames
	if err := s.SaveQuotaCooldown(wide); err != nil {
		t.Fatalf("SaveQuotaCooldown(upsert) error = %v", err)
	}
	got, err := s.ListQuotaCooldowns(a.ID)
	if err != nil || len(got) != 3 {
		t.Fatalf("ListQuotaCooldowns() = %+v, %v", got, err)
	}
	for i, want := range []string{"weekly", "weekly_model", ""} { // scope order: "", opus, zzz
		if got[i].RejectedWindow != want {
			t.Fatalf("row %d window = %q, want %q", i, got[i].RejectedWindow, want)
		}
	}
}
