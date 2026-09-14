package workspace

import (
	"fmt"
	"strings"
)

// TaskSearchIndex returns every worktree-relative path eligible for the
// web UI's quick-open (ui-redesign-parity plan, Item 18): everything git
// considers part of this worktree -- tracked files plus anything
// untracked but not gitignored -- via one `git ls-files` invocation.
//
// A client-side walk (repeatedly calling file.list, one directory at a
// time) was the plan's default and was measured against this repo's own
// worktree before reaching for an RPC, per the plan's own rule: ~150
// directories excluding node_modules, ~3,900 including it (this project
// doesn't gitignore it inside web/, since bun vendors there) -- each of
// which would be its own round trip under a client-side walk. One git
// invocation returns the whole list in a single RPC, and `-co
// --exclude-standard` gets gitignore correctness for free (smind doesn't
// parse .gitignore itself; git already does).
func (m *Manager) TaskSearchIndex(id int64) ([]string, error) {
	wt, _, err := m.taskWorktree(id)
	if err != nil {
		return nil, err
	}
	paths, err := taskSearchIndex(wt)
	if err != nil {
		return nil, fmt.Errorf("task search index %d: %w", id, err)
	}
	return paths, nil
}

// taskSearchIndex is TaskSearchIndex's git invocation, factored out so it
// can be tested directly against a bare worktree path the same way
// taskChangedFiles/taskFileDiff are (see git.go).
func taskSearchIndex(worktreePath string) ([]string, error) {
	out, err := runGitOutput(worktreePath, "ls-files", "-co", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	trimmed := strings.TrimSuffix(out, "\x00")
	if trimmed == "" {
		return []string{}, nil
	}
	return strings.Split(trimmed, "\x00"), nil
}
