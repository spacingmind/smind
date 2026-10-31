package wsapi

import (
	"github.com/spacingmind/smind/internal/store"
)

// fakeFleetWorkspaces serves the store's workspaces with default base main.
type fakeFleetWorkspaces struct{ st *store.Store }

func (f fakeFleetWorkspaces) ListWorkspaces() ([]store.Workspace, error) {
	return f.st.ListWorkspaces()
}

func (fakeFleetWorkspaces) DefaultBase(int64) (string, bool, error) { return "main", false, nil }
