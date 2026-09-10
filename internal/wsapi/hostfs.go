package wsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/hostfs"
)

// fsListDirEntry is the wire shape of one entry in an fs.listDir response
// (hostfs.Entry) -- credential-free by construction, unlike account.*'s
// accountResult, so no scrubbing is needed here.
type fsListDirEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsGitRepo bool   `json:"isGitRepo"`
}

// fsListDirResult is the result of fs.listDir (hostfs.ListResult).
type fsListDirResult struct {
	Path    string           `json:"path"`
	Parent  string           `json:"parent"`
	Entries []fsListDirEntry `json:"entries"`
}

func toFsListDirResult(r hostfs.ListResult) fsListDirResult {
	entries := make([]fsListDirEntry, len(r.Entries))
	for i, e := range r.Entries {
		entries[i] = fsListDirEntry{Name: e.Name, Path: e.Path, IsGitRepo: e.IsGitRepo}
	}
	return fsListDirResult{Path: r.Path, Parent: r.Parent, Entries: entries}
}

// handleFsListDir serves fs.listDir: {path?} lists directories on the
// daemon's whole host filesystem, unsandboxed -- see internal/hostfs's
// package doc comment for why this (unlike file.list) doesn't confine
// itself to any one task's worktree. Used by the "New workspace" folder
// picker, which needs to browse the host filesystem before any
// workspace/worktree exists to sandbox into.
func handleFsListDir() handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("fs.listDir: invalid params: %w", err)
		}
		result, err := hostfs.List(p.Path)
		if err != nil {
			return nil, fmt.Errorf("fs.listDir: %w", err)
		}
		return toFsListDirResult(result), nil
	}
}
