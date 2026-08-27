package workspace

import (
	"fmt"

	"github.com/spacingmind/smind/internal/store"
)

// CreateSpace creates a Space under workspaceID. envData is an opaque
// space-scoped environment blob; Manager does not interpret or validate its
// contents, only stores it.
func (m *Manager) CreateSpace(workspaceID int64, title, envData string) (store.Space, error) {
	sp, err := m.store.CreateSpace(store.Space{
		WorkspaceID: workspaceID,
		Title:       title,
		EnvData:     envData,
	})
	if err != nil {
		return store.Space{}, fmt.Errorf("create space: %w", err)
	}
	return sp, nil
}

// GetSpace returns the space with the given id.
func (m *Manager) GetSpace(id int64) (store.Space, error) {
	return m.store.GetSpace(id)
}

// ListSpaces returns all spaces for workspaceID, ordered by id.
func (m *Manager) ListSpaces(workspaceID int64) ([]store.Space, error) {
	return m.store.ListSpacesByWorkspace(workspaceID)
}
