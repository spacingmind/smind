package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func newTestAccount(t *testing.T, s *Store, provider, label string) Account {
	t.Helper()
	a, err := s.CreateAccount(Account{
		Provider:       provider,
		Label:          label,
		CredentialType: "api_key",
		CredentialData: `{"key":"sk-test"}`,
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	return a
}

func TestStore_RenameAccount(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	created := newTestAccount(t, s, "anthropic", "old")

	renamed, err := s.RenameAccount(created.ID, "work-claude")
	if err != nil {
		t.Fatalf("RenameAccount() error = %v", err)
	}
	if renamed.Label != "work-claude" {
		t.Fatalf("RenameAccount() Label = %q, want %q", renamed.Label, "work-claude")
	}
	if renamed.CreatedAt != created.CreatedAt {
		t.Fatalf("RenameAccount() CreatedAt = %v, want the original %v", renamed.CreatedAt, created.CreatedAt)
	}

	got, err := s.GetAccount(created.ID)
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if got.Label != "work-claude" {
		t.Fatalf("GetAccount() after rename Label = %q, want %q", got.Label, "work-claude")
	}
	if got.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatalf("GetAccount() after rename UpdatedAt = %v, want stamped at/after the original %v", got.UpdatedAt, created.UpdatedAt)
	}
}

func TestStore_RenameAccountMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if _, err := s.RenameAccount(999, "x"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("RenameAccount(999) error = %v, want sql.ErrNoRows", err)
	}
}

func TestStore_ReplaceAccountCredential(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	created := newTestAccount(t, s, "anthropic", "work")

	// Swap api_key -> oauth: both credential_type and credential_data change.
	updated, err := s.ReplaceAccountCredential(created.ID, "oauth", `{"access_token":"a","refresh_token":"r","expires_at":"2030-01-01T00:00:00Z"}`)
	if err != nil {
		t.Fatalf("ReplaceAccountCredential() error = %v", err)
	}
	if updated.CredentialType != "oauth" {
		t.Fatalf("ReplaceAccountCredential() CredentialType = %q, want oauth", updated.CredentialType)
	}

	got, err := s.GetAccount(created.ID)
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if got.CredentialType != "oauth" || got.CredentialData == created.CredentialData {
		t.Fatalf("GetAccount() after swap = (%q, %s), want oauth with new data", got.CredentialType, got.CredentialData)
	}
	if got.Label != "work" {
		t.Fatalf("GetAccount() after swap Label = %q, want unchanged", got.Label)
	}
}

func TestStore_ReplaceAccountCredentialMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if _, err := s.ReplaceAccountCredential(999, "api_key", `{}`); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ReplaceAccountCredential(999) error = %v, want sql.ErrNoRows", err)
	}
}

// TestStore_DeleteAccountCascades is ADR-0015's condition (b): the cascade
// must clear the routing session-affinity rows pointing at the account, so
// no session stays pinned to the deleted account -- plus quota snapshots
// and workspace links, while a sibling account's rows survive untouched.
func TestStore_DeleteAccountCascades(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	keep := newTestAccount(t, s, "anthropic", "keep")
	kill := newTestAccount(t, s, "anthropic", "kill")
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	now := time.Now().UTC()
	later := now.Add(24 * time.Hour)
	pins := []RoutingDecision{
		{SessionKey: "sess-a", AccountID: kill.ID, Policy: "affinity", DecidedAt: now, ExpiresAt: later},
		{SessionKey: "sess-b", AccountID: kill.ID, Policy: "affinity", DecidedAt: now, ExpiresAt: later},
		{SessionKey: "sess-c", AccountID: keep.ID, Policy: "affinity", DecidedAt: now, ExpiresAt: later},
	}
	for _, d := range pins {
		if _, err := s.CreateRoutingDecision(d); err != nil {
			t.Fatalf("CreateRoutingDecision(%s) error = %v", d.SessionKey, err)
		}
	}
	if _, err := s.CreateQuotaSnapshot(QuotaSnapshot{AccountID: kill.ID, UsageData: "{}", PolledAt: now, ExpiresAt: later}); err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}
	if _, err := s.CreateQuotaSnapshot(QuotaSnapshot{AccountID: keep.ID, UsageData: "{}", PolledAt: now, ExpiresAt: later}); err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}
	if err := s.AddWorkspaceAccount(ws.ID, kill.ID); err != nil {
		t.Fatalf("AddWorkspaceAccount(kill) error = %v", err)
	}
	if err := s.AddWorkspaceAccount(ws.ID, keep.ID); err != nil {
		t.Fatalf("AddWorkspaceAccount(keep) error = %v", err)
	}

	if err := s.DeleteAccount(kill.ID); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	if _, err := s.GetAccount(kill.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetAccount() after delete error = %v, want sql.ErrNoRows", err)
	}

	// No affinity row still points at the removed account; the sibling's
	// does survive.
	decisions, err := s.ListRoutingDecisions()
	if err != nil {
		t.Fatalf("ListRoutingDecisions() error = %v", err)
	}
	for _, d := range decisions {
		if d.AccountID == kill.ID {
			t.Fatalf("routing decision %q still points at removed account %d", d.SessionKey, kill.ID)
		}
	}
	if len(decisions) != 1 || decisions[0].SessionKey != "sess-c" {
		t.Fatalf("ListRoutingDecisions() = %+v, want only the sibling's sess-c", decisions)
	}

	snapshots, err := s.ListQuotaSnapshots()
	if err != nil {
		t.Fatalf("ListQuotaSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].AccountID != keep.ID {
		t.Fatalf("ListQuotaSnapshots() = %+v, want only the sibling's snapshot", snapshots)
	}

	links, err := s.ListWorkspaceAccountIDs(ws.ID)
	if err != nil {
		t.Fatalf("ListWorkspaceAccountIDs() error = %v", err)
	}
	if len(links) != 1 || links[0] != keep.ID {
		t.Fatalf("ListWorkspaceAccountIDs() = %+v, want only the sibling's link", links)
	}
}

func TestStore_DeleteAccountMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if err := s.DeleteAccount(999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteAccount(999) error = %v, want sql.ErrNoRows", err)
	}
}
