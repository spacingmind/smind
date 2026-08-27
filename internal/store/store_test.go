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

	tables := []string{
		"accounts", "routing_decisions", "quota_snapshots",
		"workspaces", "workspace_accounts", "spaces", "tasks",
	}
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
		{
			name: "workspace",
			get: func() error {
				_, err := s.GetWorkspace(1)
				return err
			},
		},
		{
			name: "space",
			get: func() error {
				_, err := s.GetSpace(1)
				return err
			},
		},
		{
			name: "task",
			get: func() error {
				_, err := s.GetTask(1)
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

	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error = %v", err)
	}
	if len(workspaces) != 0 {
		t.Fatalf("ListWorkspaces() = %+v, want empty", workspaces)
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

func TestStore_Workspaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		workspace Workspace
	}{
		{
			name:      "hard policy",
			workspace: Workspace{Path: "/home/user/repo-a", Title: "repo-a", RoutingPolicy: "hard"},
		},
		{
			name:      "pool policy",
			workspace: Workspace{Path: "/home/user/repo-b", Title: "repo-b", RoutingPolicy: "pool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)

			created, err := s.CreateWorkspace(tt.workspace)
			if err != nil {
				t.Fatalf("CreateWorkspace() error = %v", err)
			}
			if created.ID == 0 {
				t.Fatalf("CreateWorkspace() ID = 0, want nonzero")
			}

			got, err := s.GetWorkspace(created.ID)
			if err != nil {
				t.Fatalf("GetWorkspace() error = %v", err)
			}
			assertWorkspacesEqual(t, got, created)

			list, err := s.ListWorkspaces()
			if err != nil {
				t.Fatalf("ListWorkspaces() error = %v", err)
			}
			if len(list) != 1 {
				t.Fatalf("ListWorkspaces() = %+v, want 1 workspace", list)
			}
			assertWorkspacesEqual(t, list[0], created)
		})
	}
}

func TestStore_WorkspaceAccounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		count int
	}{
		{name: "zero accounts", count: 0},
		{name: "one account", count: 1},
		{name: "multiple accounts", count: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)

			ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "pool"})
			if err != nil {
				t.Fatalf("CreateWorkspace() error = %v", err)
			}

			var want []int64
			for i := 0; i < tt.count; i++ {
				acct, err := s.CreateAccount(Account{Provider: "anthropic", Label: "acct", CredentialType: "oauth", CredentialData: "x"})
				if err != nil {
					t.Fatalf("CreateAccount() error = %v", err)
				}
				if err := s.AddWorkspaceAccount(ws.ID, acct.ID); err != nil {
					t.Fatalf("AddWorkspaceAccount() error = %v", err)
				}
				want = append(want, acct.ID)
			}

			got, err := s.ListWorkspaceAccountIDs(ws.ID)
			if err != nil {
				t.Fatalf("ListWorkspaceAccountIDs() error = %v", err)
			}
			if len(got) != len(want) {
				t.Fatalf("ListWorkspaceAccountIDs() = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("ListWorkspaceAccountIDs()[%d] = %d, want %d", i, got[i], want[i])
				}
			}
		})
	}
}

func TestStore_RejectsMissingWorkspaceAccountReferences(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "pool"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	acct, err := s.CreateAccount(Account{Provider: "anthropic", Label: "acct", CredentialType: "oauth", CredentialData: "x"})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	if err := s.AddWorkspaceAccount(999, acct.ID); err == nil {
		t.Fatal("AddWorkspaceAccount() with missing workspace error = nil, want foreign key error")
	}
	if err := s.AddWorkspaceAccount(ws.ID, 999); err == nil {
		t.Fatal("AddWorkspaceAccount() with missing account error = nil, want foreign key error")
	}
}

func TestStore_Spaces(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	created, err := s.CreateSpace(Space{WorkspaceID: ws.ID, Title: "feature-x", EnvData: `{"FOO":"bar"}`})
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("CreateSpace() ID = 0, want nonzero")
	}

	got, err := s.GetSpace(created.ID)
	if err != nil {
		t.Fatalf("GetSpace() error = %v", err)
	}
	assertSpacesEqual(t, got, created)
}

func TestStore_SpacesByWorkspace(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	wsA, err := s.CreateWorkspace(Workspace{Path: "/repo-a", Title: "repo-a", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	wsB, err := s.CreateWorkspace(Workspace{Path: "/repo-b", Title: "repo-b", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	spaceA, err := s.CreateSpace(Space{WorkspaceID: wsA.ID, Title: "space-a", EnvData: "{}"})
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	if _, err := s.CreateSpace(Space{WorkspaceID: wsB.ID, Title: "space-b", EnvData: "{}"}); err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}

	list, err := s.ListSpacesByWorkspace(wsA.ID)
	if err != nil {
		t.Fatalf("ListSpacesByWorkspace() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != spaceA.ID {
		t.Fatalf("ListSpacesByWorkspace(%d) = %+v, want [%+v]", wsA.ID, list, spaceA)
	}
}

func strPtr(s string) *string { return &s }

func TestStore_Tasks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		task func(workspaceID int64, spaceID int64) Task
	}{
		{
			name: "all nullable fields unset",
			task: func(workspaceID, _ int64) Task {
				return Task{WorkspaceID: workspaceID, Title: "do the thing", Status: "created"}
			},
		},
		{
			name: "all nullable fields set",
			task: func(workspaceID, spaceID int64) Task {
				sid := spaceID
				return Task{
					WorkspaceID:  workspaceID,
					SpaceID:      &sid,
					Title:        "do the thing",
					Status:       "running",
					WorktreePath: strPtr("/repo/.worktrees/task-1"),
					Branch:       strPtr("task-1"),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)

			ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
			if err != nil {
				t.Fatalf("CreateWorkspace() error = %v", err)
			}
			sp, err := s.CreateSpace(Space{WorkspaceID: ws.ID, Title: "space", EnvData: "{}"})
			if err != nil {
				t.Fatalf("CreateSpace() error = %v", err)
			}

			created, err := s.CreateTask(tt.task(ws.ID, sp.ID))
			if err != nil {
				t.Fatalf("CreateTask() error = %v", err)
			}
			if created.ID == 0 {
				t.Fatalf("CreateTask() ID = 0, want nonzero")
			}

			got, err := s.GetTask(created.ID)
			if err != nil {
				t.Fatalf("GetTask() error = %v", err)
			}
			assertTasksEqual(t, got, created)
		})
	}
}

func TestStore_TasksByWorkspace(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	wsA, err := s.CreateWorkspace(Workspace{Path: "/repo-a", Title: "repo-a", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	wsB, err := s.CreateWorkspace(Workspace{Path: "/repo-b", Title: "repo-b", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	taskA, err := s.CreateTask(Task{WorkspaceID: wsA.ID, Title: "task-a", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := s.CreateTask(Task{WorkspaceID: wsB.ID, Title: "task-b", Status: "created"}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	list, err := s.ListTasksByWorkspace(wsA.ID)
	if err != nil {
		t.Fatalf("ListTasksByWorkspace() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != taskA.ID {
		t.Fatalf("ListTasksByWorkspace(%d) = %+v, want [%+v]", wsA.ID, list, taskA)
	}
}

func TestStore_ArchiveTask(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ws, err := s.CreateWorkspace(Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	created, err := s.CreateTask(Task{WorkspaceID: ws.ID, Title: "task", Status: "running"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	archived, err := s.ArchiveTask(created.ID)
	if err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	if archived.Status != "archived" {
		t.Errorf("ArchiveTask() Status = %q, want %q", archived.Status, "archived")
	}
	if archived.ArchivedAt == nil {
		t.Fatal("ArchiveTask() ArchivedAt = nil, want set")
	}

	again, err := s.ArchiveTask(created.ID)
	if err != nil {
		t.Fatalf("ArchiveTask() second call error = %v", err)
	}
	if again.Status != "archived" {
		t.Errorf("ArchiveTask() second call Status = %q, want %q", again.Status, "archived")
	}
	if again.ArchivedAt == nil || !again.ArchivedAt.Equal(*archived.ArchivedAt) {
		t.Errorf("ArchiveTask() second call ArchivedAt = %v, want unchanged %v", again.ArchivedAt, archived.ArchivedAt)
	}
}

func assertWorkspacesEqual(t *testing.T, got, want Workspace) {
	t.Helper()
	if got.ID != want.ID || got.Path != want.Path || got.Title != want.Title || got.RoutingPolicy != want.RoutingPolicy {
		t.Errorf("workspace fields = %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("workspace timestamps = %+v, want %+v", got, want)
	}
}

func assertSpacesEqual(t *testing.T, got, want Space) {
	t.Helper()
	if got.ID != want.ID || got.WorkspaceID != want.WorkspaceID || got.Title != want.Title || got.EnvData != want.EnvData {
		t.Errorf("space fields = %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("space timestamps = %+v, want %+v", got, want)
	}
}

func assertTasksEqual(t *testing.T, got, want Task) {
	t.Helper()
	if got.ID != want.ID || got.WorkspaceID != want.WorkspaceID || got.Title != want.Title || got.Status != want.Status {
		t.Errorf("task fields = %+v, want %+v", got, want)
	}
	if !ptrEqual(got.SpaceID, want.SpaceID) {
		t.Errorf("task SpaceID = %v, want %v", derefInt64(got.SpaceID), derefInt64(want.SpaceID))
	}
	if !strPtrEqual(got.WorktreePath, want.WorktreePath) {
		t.Errorf("task WorktreePath = %v, want %v", derefString(got.WorktreePath), derefString(want.WorktreePath))
	}
	if !strPtrEqual(got.Branch, want.Branch) {
		t.Errorf("task Branch = %v, want %v", derefString(got.Branch), derefString(want.Branch))
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("task timestamps = %+v, want %+v", got, want)
	}
}

func ptrEqual(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func strPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
