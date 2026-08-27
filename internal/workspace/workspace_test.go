package workspace

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// newTestRepo creates a real git repository in a temp dir with one commit,
// so worktree creation has a commit to branch from.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGitT(t, dir, "init")
	runGitT(t, dir, "config", "user.email", "test@example.com")
	runGitT(t, dir, "config", "user.name", "Test")

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitT(t, dir, "add", "README.md")
	runGitT(t, dir, "commit", "-m", "initial commit")

	return dir
}

func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, stderr.String())
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})
	return New(s)
}

func TestManager_CreateWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)
		repo := newTestRepo(t)

		acct, err := m.store.CreateAccount(store.Account{
			Provider: "anthropic", Label: "personal",
			CredentialType: "oauth", CredentialData: "token",
		})
		if err != nil {
			t.Fatalf("CreateAccount() error = %v", err)
		}

		w, err := m.CreateWorkspace(repo, "My Workspace", "hard", []int64{acct.ID})
		if err != nil {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		if w.ID == 0 {
			t.Fatalf("CreateWorkspace() returned zero id")
		}
		if w.Path != repo || w.Title != "My Workspace" || w.RoutingPolicy != "hard" {
			t.Fatalf("CreateWorkspace() = %+v, unexpected fields", w)
		}

		ids, err := m.ListWorkspaceAccountIDs(w.ID)
		if err != nil {
			t.Fatalf("ListWorkspaceAccountIDs() error = %v", err)
		}
		if len(ids) != 1 || ids[0] != acct.ID {
			t.Fatalf("ListWorkspaceAccountIDs() = %v, want [%d]", ids, acct.ID)
		}
	})

	t.Run("rejects non-existent path", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)

		_, err := m.CreateWorkspace(filepath.Join(t.TempDir(), "does-not-exist"), "t", "hard", nil)
		if err == nil {
			t.Fatalf("CreateWorkspace() error = nil, want error")
		}
	})

	t.Run("rejects non-git directory", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)

		_, err := m.CreateWorkspace(t.TempDir(), "t", "hard", nil)
		if err == nil {
			t.Fatalf("CreateWorkspace() error = nil, want error")
		}
	})

	t.Run("rejects relative path", func(t *testing.T) {
		t.Parallel()
		m := newTestManager(t)

		_, err := m.CreateWorkspace("relative/path", "t", "hard", nil)
		if err == nil {
			t.Fatalf("CreateWorkspace() error = nil, want error")
		}
	})
}

func TestManager_GetWorkspace_ListWorkspaces(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)

	w, err := m.CreateWorkspace(repo, "W1", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	got, err := m.GetWorkspace(w.ID)
	if err != nil {
		t.Fatalf("GetWorkspace() error = %v", err)
	}
	if got.ID != w.ID {
		t.Fatalf("GetWorkspace() = %+v, want id %d", got, w.ID)
	}

	all, err := m.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error = %v", err)
	}
	if len(all) != 1 || all[0].ID != w.ID {
		t.Fatalf("ListWorkspaces() = %+v, want single workspace %d", all, w.ID)
	}
}
