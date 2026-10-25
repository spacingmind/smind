package workspace

import "strings"

// ParentStackBase is the branch a human-created sub-task of parentID should
// start from: the parent's own task branch, provided it still exists as a
// local branch. ok is false (today's default base) when the parent is
// unknown, has no branch, or its branch is gone.
func (m *Manager) ParentStackBase(parentID int64) (branch string, ok bool) {
	parent, err := m.store.GetTask(parentID)
	if err != nil || parent.Branch == nil || *parent.Branch == "" {
		return "", false
	}
	ws, err := m.store.GetWorkspace(parent.WorkspaceID)
	if err != nil {
		return "", false
	}
	b := strings.TrimSpace(*parent.Branch)
	if b == "" || !gitRefCommitExists(ws.Path, "refs/heads/"+b) {
		return "", false
	}
	return b, true
}
