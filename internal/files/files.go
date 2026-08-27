// Package files provides sandboxed file operations (list/read/write) scoped
// to a single directory root -- in practice, a task's real git worktree
// (store.Task.WorktreePath), as used by internal/wsapi's file.list/
// file.read/file.write handlers.
//
// Every exported function takes root plus a client-supplied path and
// resolves that path *inside* root before touching the filesystem. This is
// the package's whole reason to exist: path is untrusted client input, and
// nothing here may read, write, or even list anything outside root --
// see resolveInRoot for how that's enforced.
package files

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Entry is one entry returned by List: a file or subdirectory of a listed
// directory.
type Entry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// List returns the entries of path (relative to root; "" means root
// itself), sorted by name. path must resolve inside root -- see
// resolveInRoot.
func List(root, path string) ([]Entry, error) {
	target, err := resolveInRoot(root, path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("list %q: %w", path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("list %q: not a directory", path)
	}

	dirEntries, err := os.ReadDir(target)
	if err != nil {
		return nil, fmt.Errorf("list %q: %w", path, err)
	}

	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		info, err := de.Info()
		if err != nil {
			// The entry vanished (or became inaccessible) between ReadDir
			// and Info -- skip it rather than failing the whole listing.
			continue
		}
		entries = append(entries, Entry{Name: de.Name(), IsDir: de.IsDir(), Size: info.Size()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// Read returns path's content as a UTF-8 string. path must resolve inside
// root -- see resolveInRoot. A file that isn't valid UTF-8 (binary) returns
// a clear error rather than silently mangling its bytes; there is no binary
// file support here.
func Read(root, path string) (string, error) {
	target, err := resolveInRoot(root, path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("read %q: is a directory", path)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", path, err)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("read %q: not valid UTF-8 (binary files are not supported)", path)
	}
	return string(data), nil
}

// Write writes content to path, creating the file if it doesn't already
// exist (but not any missing parent directories). path must resolve inside
// root -- see resolveInRoot.
func Write(root, path, content string) error {
	target, err := resolveInRoot(root, path)
	if err != nil {
		return err
	}

	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return fmt.Errorf("write %q: is a directory", path)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("write %q: %w", path, err)
	}

	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

// resolveInRoot resolves reqPath against root (a task's real worktree path)
// and returns the real, symlink-resolved absolute path it names --
// guaranteed to be root itself or a descendant of it -- or an error if it
// would escape root by any means: ".." traversal, an absolute path outside
// root, or a symlink anywhere along the path (including a symlinked leaf
// itself) that points outside root.
//
// The security property this must hold: the returned path, if it exists at
// all, is exactly what the OS will actually operate on -- so checking
// containment on anything less resolved than this (e.g. the textual,
// pre-symlink path) would let a symlink planted inside root smuggle access
// to anywhere else on disk. That's why symlinks are resolved *before* the
// containment check, not after.
//
// reqPath's leaf component is allowed not to exist yet (Write's
// create-a-new-file case): only the leaf's ancestors need to actually exist
// and are symlink-resolved; a missing leaf is appended verbatim onto the
// resolved ancestor, since a path that doesn't exist can't be a symlink.
func resolveInRoot(root, reqPath string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve worktree root %q: %w", root, err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve worktree root %q: %w", root, err)
	}

	if reqPath == "" {
		reqPath = "."
	}

	var candidate string
	if filepath.IsAbs(reqPath) {
		// An absolute client-supplied path is not joined onto root -- it
		// names an absolute location on disk, which the containment check
		// below then holds to exactly the same standard as any relative
		// path: inside root or rejected.
		candidate = filepath.Clean(reqPath)
	} else {
		candidate = filepath.Join(realRoot, reqPath)
	}

	resolved, err := resolveExistingPrefix(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", reqPath, err)
	}

	if !withinRoot(realRoot, resolved) {
		return "", fmt.Errorf("path %q escapes the worktree root", reqPath)
	}
	return resolved, nil
}

// resolveExistingPrefix resolves symlinks along path, tolerating a leaf
// component that doesn't exist yet: it walks up to the nearest existing
// ancestor, resolves *that* via filepath.EvalSymlinks, then rejoins the
// missing suffix component verbatim (recursing again if more than one
// trailing component is missing).
func resolveExistingPrefix(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	parent := filepath.Dir(path)
	if parent == path {
		// Reached the filesystem root and even it doesn't exist/resolve --
		// give up rather than recursing forever.
		return "", err
	}
	resolvedParent, perr := resolveExistingPrefix(parent)
	if perr != nil {
		return "", perr
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}

// withinRoot reports whether candidate (already absolute and
// symlink-resolved, same as root) is root itself or a descendant of it. The
// separator-suffixed prefix check (rather than a bare strings.HasPrefix) is
// what keeps a sibling directory that merely shares root's string prefix --
// e.g. candidate "/foo/bar-evil" against root "/foo/bar" -- from being
// wrongly treated as contained.
func withinRoot(root, candidate string) bool {
	if candidate == root {
		return true
	}
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}
