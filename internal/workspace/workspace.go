// Package workspace provides Workspace CRUD and Task lifecycle management
// on top of internal/store, materializing each Task as a real git worktree
// on disk rather than just a database row.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spacingmind/smind/internal/store"
)

// Manager provides Workspace and Task operations backed by a store.Store.
type Manager struct {
	store *store.Store

	// notifier, if set via SetNotifier, receives workspace/space/task
	// lifecycle notifications (create/update/archive/delete, plus the
	// pre-existing task.status transitions) so a caller (the wsapi server)
	// can push them as ADR 0005/0009 subscription events from the point
	// state actually changed, rather than a client polling. Mirrors
	// internal/runs.Registry's Notifier/SetNotifier pattern.
	notifierMu sync.Mutex
	notifier   Notifier
}

// Notifier receives workspace/space/task lifecycle notifications. Every
// method is called synchronously from the Manager method that made the
// change, after the underlying store write committed; implementations
// (internal/wsapi's bus adapter) must not block.
type Notifier interface {
	// NotifyTaskStatus fires after every task status transition
	// (created/running/archived) -- wsapi's task.status topic (ADR 0005),
	// unchanged by ADR 0009.
	NotifyTaskStatus(taskID int64, status string)

	// The rest are ADR 0009's lifecycle topics: workspace.created/deleted,
	// space.created/deleted, task.created/updated/archived/deleted.
	NotifyWorkspaceCreated(w store.Workspace)
	NotifyWorkspaceDeleted(id int64)
	NotifySpaceCreated(sp store.Space)
	NotifySpaceDeleted(id, workspaceID int64)
	NotifyTaskCreated(t store.Task)
	NotifyTaskUpdated(t store.Task)
	NotifyTaskArchived(t store.Task)
	NotifyTaskDeleted(id, workspaceID int64, spaceID *int64)
}

// SetNotifier registers n. Nil-safe: notifications with no notifier
// registered are no-ops, and a nil *Manager (wsapi tests construct the
// server without a workspace manager) accepts the call rather than
// panicking.
func (m *Manager) SetNotifier(n Notifier) {
	if m == nil {
		return
	}
	m.notifierMu.Lock()
	m.notifier = n
	m.notifierMu.Unlock()
}

// getNotifier returns the registered Notifier, or nil if none is set (or m
// itself is nil) -- callers check for nil rather than getNotifier itself
// no-oping, matching internal/runs.Registry.getNotifier's shape.
func (m *Manager) getNotifier() Notifier {
	if m == nil {
		return nil
	}
	m.notifierMu.Lock()
	n := m.notifier
	m.notifierMu.Unlock()
	return n
}

// notifyTask fires NotifyTaskStatus for t's current status -- the
// pre-ADR-0009 task.status notification path, called in addition to (and
// after) the lifecycle-specific Notify* call at each of CreateTask/
// RunTask/ArchiveTask's call sites, so a client subscribed to both a
// lifecycle topic and task.status sees the row before its status.
func (m *Manager) notifyTask(t store.Task) {
	if n := m.getNotifier(); n != nil {
		n.NotifyTaskStatus(t.ID, t.Status)
	}
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
	if strings.TrimSpace(title) == "" {
		title = filepath.Base(path)
	}

	w, err := m.store.CreateWorkspace(store.Workspace{
		Path:          path,
		Title:         title,
		RoutingPolicy: routingPolicy,
	})
	if err != nil {
		return store.Workspace{}, fmt.Errorf("create workspace: %w", err)
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyWorkspaceCreated(w)
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
		// Deliberately doesn't wrap the underlying os.Stat error (%w) --
		// that's a raw "stat .../.git: no such file or directory" a UI
		// user has no way to act on. This message is written to be read
		// as-is: FormError (web/packages/ui/src/components/crud-dialogs.tsx)
		// passes the daemon's message straight through, verbatim.
		return fmt.Errorf(
			"workspace path %q is not a git repository -- smind workspaces must point at an existing repo "+
				"(tasks are created as git worktrees branched off it); run \"git init\" there first, or pick a different folder",
			path,
		)
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
