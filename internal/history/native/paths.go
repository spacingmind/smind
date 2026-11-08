package native

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// errUnsafe is returned for every path refusal; the reason shown is always
// ReasonSessionMissing, so a probe learns nothing about what exists.
var errUnsafe = errors.New("native: session path refused")

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// validID accepts a session or thread id that cannot be a path: letters,
// digits, dot, dash, underscore; no separator, no "..", no glob character.
func validID(id string) bool { return idPattern.MatchString(id) && !strings.Contains(id, "..") }

// maxProjectDirName is the CLI's cap on a sanitised working-directory name.
const maxProjectDirName = 200

// claudeProjectDir is the directory name Claude Code gives a working
// directory under <config dir>/projects: every UTF-16 code unit that is not an
// ASCII letter or digit becomes "-" (so "/.smind/" is "--smind-", and a
// character outside the BMP is two dashes). A name longer than 200 is cut to
// 200 and gets "-" plus a base36 hash of the whole, unsanitised cwd: the
// CLI's 32-bit "h = h*31 + unit" over the UTF-16 units, absolute value.
func claudeProjectDir(cwd string) string {
	units := utf16.Encode([]rune(cwd))
	name := make([]byte, len(units))
	var h int32
	for i, u := range units {
		switch {
		case u >= 'a' && u <= 'z', u >= 'A' && u <= 'Z', u >= '0' && u <= '9':
			name[i] = byte(u)
		default:
			name[i] = '-'
		}
		h = h<<5 - h + int32(u)
	}
	if len(name) <= maxProjectDirName {
		return string(name)
	}
	abs := int64(h)
	if abs < 0 {
		abs = -abs
	}
	return string(name[:maxProjectDirName]) + "-" + strconv.FormatInt(abs, 36)
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

// afterOpen, when set (tests only), runs between the open and the identity
// check of openRegular, so a test can swap the file there.
var afterOpen func(real string)

// openRegular opens real read-only and re-checks that the handle is the regular
// file that was resolved (a swap between resolve and open is refused).
func openRegular(real string, max int64) (*os.File, error) {
	f, err := os.Open(real)
	if err != nil {
		return nil, errUnsafe
	}
	if afterOpen != nil {
		afterOpen(real)
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

// Probe says what a rebuild would find for ref under roots, without reading the
// session: Found when its file resolves to a regular file inside an allowed root
// and fits the size bound, TooLarge when it is there but over the bound. Both
// false is "gone" (or not a path smind may open).
//
// Size and ModTime (unix nanoseconds) identify the file that was found: a cache
// of a rebuilt history is valid only while both are unchanged.
type Probe struct {
	Found, TooLarge bool
	Size            int64
	ModTime         int64
}

// ProbeSession applies exactly the path rules a rebuild does (validated ids, the
// allowed roots, symlinks resolved, a regular file), and nothing else.
func ProbeSession(ref Ref, roots []string) Probe {
	var err error
	var real string
	switch ref.Harness {
	case HarnessClaudeCode:
		real, err = claudeSessionFile(ref, roots, MaxFileBytes)
	case HarnessCodex:
		real, err = codexSessionFile(ref, roots, MaxFileBytes)
	default:
		return Probe{}
	}
	switch {
	case err == nil:
		st, serr := os.Stat(real)
		if serr != nil {
			return Probe{}
		}
		return Probe{Found: true, Size: st.Size(), ModTime: st.ModTime().UnixNano()}
	case errors.Is(err, errTooLarge):
		return Probe{TooLarge: true}
	}
	return Probe{}
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

// rolloutName matches the rollout file of threadID: rollout-<ts>-<threadId>.jsonl,
// or, for a reverted thread, rollout-<ts>-<threadId>_<rolloutId>.jsonl where
// both ids are UUIDs. The timestamp has a fixed shape and the whole name is
// anchored, so another thread's file (a thread id that merely ends with, or
// extends, this one) never matches, and a "_" suffix can only be a UUID.
func rolloutName(threadID string) *regexp.Regexp {
	const ts = `\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}`
	const uuid = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`
	suffix := ""
	if !strings.Contains(threadID, "_") {
		suffix = `(?:_` + uuid + `)?`
	}
	return regexp.MustCompile(`^rollout-` + ts + `-` + regexp.QuoteMeta(threadID) + suffix + `\.jsonl$`)
}

// codexSessionFile finds the rollout of ref's thread under the allowed roots:
// sessions/YYYY/MM/DD/rollout-<ts>-<threadId>[_<rolloutId>].jsonl, else
// archived_sessions/. A reverted thread has several rollout files; the most
// recently written one is used (ties by name), so the choice is deterministic.
func codexSessionFile(ref Ref, roots []string, max int64) (string, error) {
	if !validID(ref.SessionID) || !allowedRoot(ref.CodexHome, roots) {
		return "", errUnsafe
	}
	// validID leaves no glob character in the id, so the broad pattern only
	// narrows the listing; rolloutName decides what is a match.
	name := "rollout-*-" + ref.SessionID + "*.jsonl"
	re := rolloutName(ref.SessionID)
	home := mustReal(ref.CodexHome)
	for _, pattern := range []string{
		filepath.Join(ref.CodexHome, "sessions", "*", "*", "*", name),
		filepath.Join(ref.CodexHome, "archived_sessions", name),
	} {
		matches, _ := filepath.Glob(pattern)
		var best string
		var bestMod int64
		for _, m := range matches {
			if !re.MatchString(filepath.Base(m)) {
				continue
			}
			// The recorded path is only a hint (the rollout may have been archived
			// since): the file is whatever the glob under the allowed root finds.
			real, err := resolveInside(m, []string{ref.CodexHome}, "", max)
			if err != nil || !(within(filepath.Join(home, "sessions"), real) || within(filepath.Join(home, "archived_sessions"), real)) {
				continue
			}
			st, err := os.Stat(real)
			if err != nil {
				continue
			}
			if mod := st.ModTime().UnixNano(); best == "" || mod > bestMod || (mod == bestMod && real > best) {
				best, bestMod = real, mod
			}
		}
		if best != "" {
			return best, nil
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
