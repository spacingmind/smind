// Package workspace provides Workspace CRUD and Task lifecycle management
// on top of internal/store, materializing each Task as a real git worktree
// on disk rather than just a database row.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/store"
)

// Manager provides Workspace and Task operations backed by a store.Store.
type Manager struct {
	store *store.Store
}

// New returns a Manager backed by s.
func New(s *store.Store) *Manager {
	return &Manager{store: s}
}

// CreateWorkspace validates that path is an absolute path to an existing git
// repository (a directory containing a .git entry, either a directory for a
// normal checkout or a file for a worktree/submodule), creates the Workspace
// row, and adds each of accountIDs to its candidate account pool.
//
// store exposes no transactions (adding that is out of scope here), so if
// an AddWorkspaceAccount call fails partway through accountIDs, there's no
// way to roll back the workspace row and any accounts already added across
// separate SQL statements. Rather than attempt a fragile manual rollback,
// CreateWorkspace leaves the partial state in place and returns an error
// naming which account failed and how many succeeded, so the caller can see
// exactly what happened and retry or clean up explicitly.
func (m *Manager) CreateWorkspace(path, title, routingPolicy string, accountIDs []int64) (store.Workspace, error) {
	if err := validateGitRepoPath(path); err != nil {
		return store.Workspace{}, err
	}

	w, err := m.store.CreateWorkspace(store.Workspace{
		Path:          path,
		Title:         title,
		RoutingPolicy: routingPolicy,
	})
	if err != nil {
		return store.Workspace{}, fmt.Errorf("create workspace: %w", err)
	}

	for i, accountID := range accountIDs {
		if err := m.store.AddWorkspaceAccount(w.ID, accountID); err != nil {
			return store.Workspace{}, fmt.Errorf(
				"workspace %d created, but failed adding account %d (%d/%d accounts added): %w",
				w.ID, accountID, i, len(accountIDs), err,
			)
		}
	}

	return w, nil
}

func validateGitRepoPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("workspace path %q must be absolute", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("workspace path %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace path %q is not a directory", path)
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		return fmt.Errorf("workspace path %q is not a git repository: %w", path, err)
	}
	return nil
}

// GetWorkspace returns the workspace with the given id.
func (m *Manager) GetWorkspace(id int64) (store.Workspace, error) {
	return m.store.GetWorkspace(id)
}

// ListWorkspaces returns all workspaces, ordered by id. It's a thin wrapper
// over store.ListWorkspaces: a workspace's candidate account ids are a
// separate concern (store.ListWorkspaceAccountIDs), and folding them in here
// would mean either N+1 queries on every list call or a richer return type
// that most callers don't need. Callers that want account ids per workspace
// can call ListWorkspaceAccountIDs themselves for the workspaces they care
// about.
func (m *Manager) ListWorkspaces() ([]store.Workspace, error) {
	return m.store.ListWorkspaces()
}

// ListWorkspaceAccountIDs returns the candidate account ids for workspaceID.
func (m *Manager) ListWorkspaceAccountIDs(workspaceID int64) ([]int64, error) {
	return m.store.ListWorkspaceAccountIDs(workspaceID)
}
