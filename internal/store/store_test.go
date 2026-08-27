package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return s
}

func TestOpen(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	tables := []string{"accounts", "routing_decisions", "quota_snapshots"}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			var name string
			err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
			if err != nil {
				t.Fatalf("table %s not found: %v", table, err)
			}
		})
	}
}

func TestStore_Accounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		account Account
	}{
		{
			name: "oauth account",
			account: Account{
				Provider:       "anthropic",
				Label:          "personal",
				CredentialType: "oauth",
				CredentialData: "refresh-token-abc",
			},
		},
		{
			name: "api key account",
			account: Account{
				Provider:       "openai",
				Label:          "work",
				CredentialType: "api_key",
				CredentialData: "sk-test-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)

			created, err := s.CreateAccount(tt.account)
			if err != nil {
				t.Fatalf("CreateAccount() error = %v", err)
			}
			if created.ID == 0 {
				t.Fatalf("CreateAccount() ID = 0, want nonzero")
			}

			got, err := s.GetAccount(created.ID)
			if err != nil {
				t.Fatalf("GetAccount() error = %v", err)
			}
			assertAccountsEqual(t, got, created)

			list, err := s.ListAccounts()
			if err != nil {
				t.Fatalf("ListAccounts() error = %v", err)
			}
			if len(list) != 1 {
				t.Fatalf("ListAccounts() = %+v, want 1 account", list)
			}
			assertAccountsEqual(t, list[0], created)
		})
	}
}

func TestStore_GetMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	tests := []struct {
		name string
		get  func() error
	}{
		{
			name: "account",
			get: func() error {
				_, err := s.GetAccount(1)
				return err
			},
		},
		{
			name: "routing decision",
			get: func() error {
				_, err := s.GetRoutingDecision(1)
				return err
			},
		},
		{
			name: "quota snapshot",
			get: func() error {
				_, err := s.GetQuotaSnapshot(1)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.get()
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("missing %s error = %v, want sql.ErrNoRows", tt.name, err)
			}
		})
	}
}

func TestStore_ListEmpty(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	accounts, err := s.ListAccounts()
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("ListAccounts() = %+v, want empty", accounts)
	}

	decisions, err := s.ListRoutingDecisions()
	if err != nil {
		t.Fatalf("ListRoutingDecisions() error = %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("ListRoutingDecisions() = %+v, want empty", decisions)
	}

	snapshots, err := s.ListQuotaSnapshots()
	if err != nil {
		t.Fatalf("ListQuotaSnapshots() error = %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("ListQuotaSnapshots() = %+v, want empty", snapshots)
	}
}

func TestStore_RejectsMissingAccountReferences(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	_, err := s.CreateRoutingDecision(RoutingDecision{
		SessionKey: "conversation-1",
		AccountID:  999,
		Policy:     "hard",
		DecidedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	})
	if err == nil {
		t.Fatal("CreateRoutingDecision() error = nil, want foreign key error")
	}

	_, err = s.CreateQuotaSnapshot(QuotaSnapshot{
		AccountID: 999,
		UsageData: `{"tokens_used":1000}`,
		PolledAt:  now,
		ExpiresAt: now.Add(time.Minute),
	})
	if err == nil {
		t.Fatal("CreateQuotaSnapshot() error = nil, want foreign key error")
	}
}

func TestStore_ReopenExistingDatabase(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "smind.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	created, err := s.CreateAccount(Account{
		Provider:       "anthropic",
		Label:          "personal",
		CredentialType: "oauth",
		CredentialData: "refresh-token-abc",
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	got, err := reopened.GetAccount(created.ID)
	if err != nil {
		t.Fatalf("GetAccount() after reopen error = %v", err)
	}
	assertAccountsEqual(t, got, created)
}

func TestStore_ConcurrentAccountCreates(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	const workers = 20

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.CreateAccount(Account{
				Provider:       "anthropic",
				Label:          "personal-" + string(rune('a'+i)),
				CredentialType: "oauth",
				CredentialData: "refresh-token",
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("CreateAccount() concurrent error = %v", err)
		}
	}

	accounts, err := s.ListAccounts()
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(accounts) != workers {
		t.Fatalf("ListAccounts() len = %d, want %d", len(accounts), workers)
	}
}

func assertAccountsEqual(t *testing.T, got, want Account) {
	t.Helper()
	if got.ID != want.ID || got.Provider != want.Provider || got.Label != want.Label ||
		got.CredentialType != want.CredentialType || got.CredentialData != want.CredentialData {
		t.Errorf("account fields = %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("account timestamps = %+v, want %+v", got, want)
	}
}

func TestStore_RoutingDecisions(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	account, err := s.CreateAccount(Account{Provider: "anthropic", Label: "personal", CredentialType: "oauth", CredentialData: "x"})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	decision := RoutingDecision{
		SessionKey: "conversation-1",
		AccountID:  account.ID,
		Policy:     "hard",
		DecidedAt:  now,
		ExpiresAt:  now.Add(24 * time.Hour),
	}

	created, err := s.CreateRoutingDecision(decision)
	if err != nil {
		t.Fatalf("CreateRoutingDecision() error = %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("CreateRoutingDecision() ID = 0, want nonzero")
	}

	got, err := s.GetRoutingDecision(created.ID)
	if err != nil {
		t.Fatalf("GetRoutingDecision() error = %v", err)
	}
	if !got.DecidedAt.Equal(created.DecidedAt) || !got.ExpiresAt.Equal(created.ExpiresAt) {
		t.Errorf("GetRoutingDecision() timestamps = %+v, want %+v", got, created)
	}
	if got.ID != created.ID || got.SessionKey != created.SessionKey || got.AccountID != created.AccountID || got.Policy != created.Policy {
		t.Errorf("GetRoutingDecision() = %+v, want %+v", got, created)
	}

	list, err := s.ListRoutingDecisions()
	if err != nil {
		t.Fatalf("ListRoutingDecisions() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("ListRoutingDecisions() = %+v, want [%+v]", list, created)
	}
}

func TestStore_QuotaSnapshots(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	account, err := s.CreateAccount(Account{Provider: "anthropic", Label: "personal", CredentialType: "oauth", CredentialData: "x"})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	snapshot := QuotaSnapshot{
		AccountID: account.ID,
		UsageData: `{"tokens_used":1000}`,
		PolledAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	created, err := s.CreateQuotaSnapshot(snapshot)
	if err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("CreateQuotaSnapshot() ID = 0, want nonzero")
	}

	got, err := s.GetQuotaSnapshot(created.ID)
	if err != nil {
		t.Fatalf("GetQuotaSnapshot() error = %v", err)
	}
	if !got.PolledAt.Equal(created.PolledAt) || !got.ExpiresAt.Equal(created.ExpiresAt) {
		t.Errorf("GetQuotaSnapshot() timestamps = %+v, want %+v", got, created)
	}
	if got.ID != created.ID || got.AccountID != created.AccountID || got.UsageData != created.UsageData {
		t.Errorf("GetQuotaSnapshot() = %+v, want %+v", got, created)
	}

	list, err := s.ListQuotaSnapshots()
	if err != nil {
		t.Fatalf("ListQuotaSnapshots() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("ListQuotaSnapshots() = %+v, want [%+v]", list, created)
	}
}

func TestStore_UpdateAccountCredential(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	created, err := s.CreateAccount(Account{
		Provider:       "anthropic",
		Label:          "personal",
		CredentialType: "oauth",
		CredentialData: "old-data",
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	updated, err := s.UpdateAccountCredential(created.ID, "new-data")
	if err != nil {
		t.Fatalf("UpdateAccountCredential() error = %v", err)
	}
	if updated.CredentialData != "new-data" {
		t.Errorf("UpdateAccountCredential() CredentialData = %q, want %q", updated.CredentialData, "new-data")
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("UpdateAccountCredential() UpdatedAt = %v, want after %v", updated.UpdatedAt, created.UpdatedAt)
	}

	got, err := s.GetAccount(created.ID)
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if got.CredentialData != "new-data" {
		t.Errorf("persisted CredentialData = %q, want %q", got.CredentialData, "new-data")
	}
}

func TestStore_UpdateAccountCredentialMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	_, err := s.UpdateAccountCredential(999, "new-data")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateAccountCredential() error = %v, want sql.ErrNoRows", err)
	}
}
