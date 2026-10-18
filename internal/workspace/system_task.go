package workspace

import (
	"fmt"

	"github.com/spacingmind/smind/internal/store"
)

// CreateSystemTask creates a hidden system task (ADR-0026) plus its chat in
// workspaceID, working in cwd. Unlike CreateTask it makes no git worktree,
// branch, port or setup-script run, and fires no Notifier events: system
// tasks are invisible to every client.
func (m *Manager) CreateSystemTask(workspaceID int64, title, cwd string) (store.Task, store.Chat, error) {
	t, c, err := m.store.CreateSystemTask(workspaceID, title, cwd)
	if err != nil {
		return store.Task{}, store.Chat{}, fmt.Errorf("create system task: %w", err)
	}
	return t, c, nil
}
