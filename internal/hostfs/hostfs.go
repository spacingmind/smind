// Package hostfs lists directories on the daemon's whole host filesystem,
// unsandboxed -- unlike internal/files, which is deliberately confined to
// one task's real worktree. It exists for exactly one purpose: letting a
// client browse the host filesystem to pick a path *before* a workspace
// (and therefore a worktree to sandbox into) exists. The daemon already
// runs with the operator's own OS permissions, so this isn't a materially
// larger surface than what internal/files/internal/terminal already expose
// once a workspace/task is in play -- see the workspace-folder-picker plan's
// Decisions section.
package hostfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNotDirectory is List's error when path resolves to something that
// exists but isn't a directory (e.g. a regular file) -- distinct from the
// stdlib's fs.ErrNotExist/fs.ErrPermission, which List's underlying os
// calls already produce (and which List leaves wrapped, not replaced) for
// the "doesn't exist"/"permission denied" cases.
var ErrNotDirectory = errors.New("not a directory")

// Entry is one subdirectory of a listed directory.
type Entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsGitRepo bool   `json:"isGitRepo"`
}

// ListResult is List's return value: the resolved directory that was
// listed, its parent (for an "Up" action), and the subdirectories found in
// it.
type ListResult struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent"`
	Entries []Entry `json:"entries"`
}

// List returns the subdirectories of path, sorted by name
// case-insensitively -- regular files and dot-prefixed entries are
// excluded. An empty path defaults to the current user's home directory
// (os.UserHomeDir()). path is resolved to an absolute, cleaned form
// (filepath.Abs + filepath.Clean) before being listed; the resolved form is
// what's reported back in ListResult.Path and used to compute each entry's
// Path and ListResult.Parent.
//
// Symlinks are followed when deciding whether an entry is a directory: a
// bare fs.DirEntry.Type() check (from os.ReadDir alone) reports a
// symlinked directory as a symlink, not a directory, which would wrongly
// hide it. There's no extra loop protection beyond what the OS's own
// directory listing already provides.
//
// Errors are distinguishable via errors.Is: a nonexistent path wraps
// fs.ErrNotExist, a permission-denied path wraps fs.ErrPermission, and a
// path that exists but isn't a directory wraps ErrNotDirectory.
func List(path string) (ListResult, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ListResult{}, fmt.Errorf("hostfs: resolve home directory: %w", err)
		}
		path = home
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return ListResult{}, fmt.Errorf("hostfs: resolve %q: %w", path, err)
	}
	abs = filepath.Clean(abs)

	info, err := os.Stat(abs)
	if err != nil {
		return ListResult{}, fmt.Errorf("hostfs: list %q: %w", abs, err)
	}
	if !info.IsDir() {
		return ListResult{}, fmt.Errorf("hostfs: list %q: %w", abs, ErrNotDirectory)
	}

	dirEntries, err := os.ReadDir(abs)
	if err != nil {
		return ListResult{}, fmt.Errorf("hostfs: list %q: %w", abs, err)
	}

	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		entryPath := filepath.Join(abs, name)
		// os.Stat (not de.Type()/de.Info()) so a symlinked directory
		// resolves to what it points at, not its own symlink type.
		st, err := os.Stat(entryPath)
		if err != nil {
			// Vanished, inaccessible, or a broken symlink between ReadDir
			// and this Stat -- skip it rather than failing the whole
			// listing, matching internal/files.List's same tolerance.
			continue
		}
		if !st.IsDir() {
			continue
		}

		entries = append(entries, Entry{
			Name:      name,
			Path:      entryPath,
			IsGitRepo: isGitRepo(entryPath),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	parent := filepath.Dir(abs)
	if parent == abs {
		// filepath.Dir("/") == "/", so the filesystem root needs an
		// explicit check rather than relying on Dir's return differing
		// from its input.
		parent = ""
	}

	return ListResult{Path: abs, Parent: parent, Entries: entries}, nil
}

// isGitRepo mirrors workspace.validateGitRepoPath's check: a directory
// containing a .git entry (directory for a normal checkout, or file for a
// worktree/submodule).
func isGitRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}
