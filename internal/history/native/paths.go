package native

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// errUnsafe is returned for every path refusal; the reason shown is always
// ReasonSessionMissing, so a probe learns nothing about what exists.
var errUnsafe = errors.New("native: session path refused")

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// validID accepts a session or thread id that cannot be a path: letters,
// digits, dot, dash, underscore; no separator, no "..", no glob character.
func validID(id string) bool { return idPattern.MatchString(id) && !strings.Contains(id, "..") }

// claudeProjectDir is the directory name Claude Code gives a working
// directory under <config dir>/projects: every character that is not an ASCII
// letter or digit becomes "-" (so "/.smind/" is "--smind-").
func claudeProjectDir(cwd string) string {
	var b strings.Builder
	for _, c := range cwd {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteRune(c)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ClaudeProjectDir is claudeProjectDir for the runs package, which records the
// transcript path at capture time.
func ClaudeProjectDir(cwd string) string { return claudeProjectDir(cwd) }

// resolveInside returns the real path of candidate when, after resolving every
// symlink, it is a regular file inside one of roots' real paths (and, when
// under is not empty, inside root/under). max is the largest size accepted.
func resolveInside(candidate string, roots []string, under string, max int64) (string, error) {
	if candidate == "" || !filepath.IsAbs(candidate) || strings.ContainsRune(candidate, 0) {
		return "", errUnsafe
	}
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", errUnsafe
	}
	st, err := os.Lstat(real)
	if err != nil || !st.Mode().IsRegular() {
		return "", errUnsafe
	}
	if st.Size() > max {
		return "", errTooLarge
	}
	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		rr, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		base := rr
		if under != "" {
			base = filepath.Join(rr, under)
		}
		if within(base, real) {
			return real, nil
		}
	}
	return "", errUnsafe
}

// within reports whether path is strictly inside dir (lexically, both clean and
// already symlink-free).
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// allowedRoot reports whether dir (the root a ref names) is one of the roots the
// caller allows now, comparing real paths: a ref cannot widen the roots.
func allowedRoot(dir string, roots []string) bool {
	if dir == "" || !filepath.IsAbs(dir) {
		return false
	}
	d, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	for _, r := range roots {
		if rr, err := filepath.EvalSymlinks(r); err == nil && rr == d {
			return true
		}
	}
	return false
}

// openRegular opens real read-only and re-checks that the handle is the regular
// file that was resolved (a swap between resolve and open is refused).
func openRegular(real string, max int64) (*os.File, error) {
	f, err := os.Open(real)
	if err != nil {
		return nil, errUnsafe
	}
	fst, err := f.Stat()
	if err != nil || !fst.Mode().IsRegular() {
		f.Close()
		return nil, errUnsafe
	}
	if fst.Size() > max {
		f.Close()
		return nil, errTooLarge
	}
	if lst, err := os.Lstat(real); err != nil || !os.SameFile(fst, lst) {
		f.Close()
		return nil, errUnsafe
	}
	return f, nil
}

// claudeSessionFile finds the transcript of ref under the allowed roots.
func claudeSessionFile(ref Ref, roots []string, max int64) (string, error) {
	if !validID(ref.SessionID) || ref.Cwd == "" || !allowedRoot(ref.ConfigDir, roots) {
		return "", errUnsafe
	}
	want := filepath.Join(ref.ConfigDir, "projects", claudeProjectDir(ref.Cwd), ref.SessionID+".jsonl")
	if ref.TranscriptPath != "" && filepath.Clean(ref.TranscriptPath) != want {
		return "", fmt.Errorf("%w: the recorded transcript path is not the one derived from the ref", errUnsafe)
	}
	return resolveInside(want, []string{ref.ConfigDir}, "projects", max)
}

// codexSessionFile finds the rollout of ref's thread under the allowed roots:
// sessions/YYYY/MM/DD/rollout-<ts>-<threadId>.jsonl, else archived_sessions/.
func codexSessionFile(ref Ref, roots []string, max int64) (string, error) {
	if !validID(ref.SessionID) || !allowedRoot(ref.CodexHome, roots) {
		return "", errUnsafe
	}
	name := "rollout-*-" + ref.SessionID + ".jsonl"
	for _, pattern := range []string{
		filepath.Join(ref.CodexHome, "sessions", "*", "*", "*", name),
		filepath.Join(ref.CodexHome, "archived_sessions", name),
	} {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			// The recorded path is only a hint (the rollout may have been archived
			// since): the file is whatever the glob under the allowed root finds.
			if real, err := resolveInside(m, []string{ref.CodexHome}, "", max); err == nil {
				if within(filepath.Join(mustReal(ref.CodexHome), "sessions"), real) || within(filepath.Join(mustReal(ref.CodexHome), "archived_sessions"), real) {
					return real, nil
				}
			}
		}
	}
	return "", errUnsafe
}

func mustReal(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
