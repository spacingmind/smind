package workspace

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/store"
)

// MaxCommitMessageStyleRunes bounds a workspace's commit-message style text.
const MaxCommitMessageStyleRunes = 1000

// SetCommitMessageStyle stores style as workspaceID's commit-message style
// guidance (G17). The text is trimmed; "" clears it. Over-long text and
// control characters other than \n and \t are rejected. The updated workspace
// is returned.
func (m *Manager) SetCommitMessageStyle(workspaceID int64, style string) (store.Workspace, error) {
	ws, err := m.store.GetWorkspace(workspaceID)
	if err != nil {
		return store.Workspace{}, fmt.Errorf("set commit message style: %w", err)
	}
	style = strings.TrimSpace(style)
	if utf8.RuneCountInString(style) > MaxCommitMessageStyleRunes {
		return store.Workspace{}, errors.New("commit message style is too long (max 1000 characters)")
	}
	for _, r := range style {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return store.Workspace{}, errors.New("commit message style contains control characters")
		}
	}
	if err := m.store.SetWorkspaceCommitMessageStyle(workspaceID, style); err != nil {
		return store.Workspace{}, fmt.Errorf("set commit message style: %w", err)
	}
	ws.CommitMessageStyle = style
	return ws, nil
}

// CommitMessageStyle returns workspaceID's commit-message style guidance, or
// "" when none is set or the workspace cannot be read.
func (m *Manager) CommitMessageStyle(workspaceID int64) string {
	ws, err := m.store.GetWorkspace(workspaceID)
	if err != nil {
		return ""
	}
	return ws.CommitMessageStyle
}
