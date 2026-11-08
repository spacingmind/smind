package steward

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/fsutil"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DefaultStandardsFiles is the fixed, ordered list of the repository files
// that document how code is written (ADR-0054 §3). steward.standards.files
// adds to it, after it.
var DefaultStandardsFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"CONTRIBUTING.md",
	"docs/CONTRIBUTING.md",
	".github/PULL_REQUEST_TEMPLATE.md",
}

// Caps on the standards the Standards pass reads.
const (
	// StandardsFileMax bounds one standards file; a longer one is truncated and flagged.
	StandardsFileMax = 64 * 1024
	// StandardsFilesMax bounds how many standards files one review reads.
	StandardsFilesMax = 25
)

// ErrStandardsBaseUnresolved means the merge-base could not be resolved. The
// Standards pass must never fall back to judging against the head.
var ErrStandardsBaseUnresolved = errors.New("the merge-base of the change could not be resolved")

// MergeBase resolves the merge-base of diffBase and head to a full commit id.
func MergeBase(ctx context.Context, dir, diffBase, head string) (string, error) {
	if !safeRef(diffBase) || !sha40Re.MatchString(head) {
		return "", ErrStandardsBaseUnresolved
	}
	out, err := gitOut(ctx, dir, "merge-base", diffBase, head)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrStandardsBaseUnresolved, err)
	}
	base := strings.TrimSpace(out)
	if !sha40Re.MatchString(base) {
		return "", ErrStandardsBaseUnresolved
	}
	return base, nil
}

// ReadFileAtRef reads one regular file at the commit ref through git's object
// database, cut to StandardsFileMax; ok is false for a missing path, a
// symlink, a submodule, a directory or an unsafe path. It is the one reader
// for every repository file the two-axis review takes from a commit (the
// standards files, the plan).
func ReadFileAtRef(ctx context.Context, dir, ref, p string) (content []byte, ok bool, err error) {
	content, _, ok, err = readBlobAtRef(ctx, dir, ref, p)
	return content, ok, err
}

// readBlobAtRef reads path at the commit base through git's object database,
// never the working tree: a symlink, a submodule or a directory is refused
// (a symlink blob would only hold its target text), so no file outside the
// repository can ever be read, and nothing depends on the checkout. ok is
// false when the path does not exist at base or is not a regular file.
func readBlobAtRef(ctx context.Context, dir, base, p string) (content []byte, truncated, ok bool, err error) {
	if !sha40Re.MatchString(base) || !config.SafeRepoPath(p) {
		return nil, false, false, nil
	}
	tree, err := gitOut(ctx, dir, "ls-tree", "-z", base, "--", p)
	if err != nil {
		return nil, false, false, err
	}
	tree = strings.TrimRight(tree, "\x00")
	if tree == "" {
		return nil, false, false, nil
	}
	// "<mode> blob <sha>\t<path>"
	meta, name, found := strings.Cut(tree, "\t")
	fields := strings.Fields(meta)
	if !found || name != p || len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
		return nil, false, false, nil
	}
	out, err := gitOut(ctx, dir, "cat-file", "blob", fields[2])
	if err != nil {
		return nil, false, false, err
	}
	b := []byte(out)
	if len(b) > StandardsFileMax {
		b, truncated = b[:StandardsFileMax], true
	}
	return b, truncated, true, nil
}

// StandardsAtBase lists the standards files that exist at the commit base,
// in discovery order: DefaultStandardsFiles, then extra (the human-authored
// steward.standards.files), each once. Absent files are skipped; an unsafe
// extra path is skipped too (the config loader already refuses it).
func StandardsAtBase(ctx context.Context, dir, base string, extra []string) ([]StandardsFileInput, error) {
	if !sha40Re.MatchString(base) {
		return nil, ErrStandardsBaseUnresolved
	}
	var out []StandardsFileInput
	seen := map[string]bool{}
	for _, p := range append(append([]string{}, DefaultStandardsFiles...), extra...) {
		if seen[p] || len(out) >= StandardsFilesMax {
			continue
		}
		seen[p] = true
		b, truncated, ok, err := readBlobAtRef(ctx, dir, base, p)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		sum := sha256.Sum256(b)
		out = append(out, StandardsFileInput{Path: p, Bytes: len(b), SHA256: hex.EncodeToString(sum[:]), Truncated: truncated})
	}
	return out, nil
}

// StandardsHeadDiff is what the change at head does to the given standards
// files relative to base, as `git diff` text (empty when it touches none).
func StandardsHeadDiff(ctx context.Context, dir, base, head string, files []StandardsFileInput) (string, error) {
	if len(files) == 0 || !sha40Re.MatchString(base) || !sha40Re.MatchString(head) {
		return "", nil
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", base, head, "--"}
	for _, f := range files {
		args = append(args, f.Path)
	}
	return gitOut(ctx, dir, args...)
}

// WriteStandardsCopies copies each standards file, as it is at base, to
// scratch/StandardsDir/<path> (0o600, directories 0o700) for the Standards
// seat to read. The scratch directory is untracked-and-outside-the-index by
// construction (a dot directory the seat's task never commits), and every
// destination is checked to stay inside it. A file whose content hash no
// longer matches the digest's is an error: the pass must judge what the
// digest says it judges.
func WriteStandardsCopies(ctx context.Context, dir, base, scratch string, files []StandardsFileInput) error {
	root := filepath.Join(scratch, StandardsDir)
	// The scratch tree is the PR's head checkout, so a change may carry its
	// own .smind-standards (a symlink out of the tree, or extra files meant to
	// pose as standards). Whatever is there is removed (a symlink is removed,
	// never followed) before the copies are written into a fresh directory.
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	for _, f := range files {
		b, truncated, ok, err := readBlobAtRef(ctx, dir, base, f.Path)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("standards file %s is gone at %s", f.Path, first12(base))
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != f.SHA256 || truncated != f.Truncated {
			return fmt.Errorf("standards file %s changed since the digest was built", f.Path)
		}
		dest := filepath.Join(root, filepath.FromSlash(f.Path))
		if rel, err := filepath.Rel(root, dest); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("standards file %s escapes the standards directory", f.Path)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(dest, bytes.Clone(b), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// FileInputs is the section's files as the collector read them, for the
// scratch copy to check against.
func (s *StandardsSection) FileInputs() []StandardsFileInput {
	out := make([]StandardsFileInput, 0, len(s.Files))
	for _, f := range s.Files {
		out = append(out, StandardsFileInput{Path: f.Path, Bytes: f.Bytes, SHA256: f.SHA256, Truncated: f.Truncated})
	}
	return out
}

// PruneSpec is what a seat loses from its scratch tree, the PR's head
// checkout. Paths are exact repository-relative paths; Names are base names
// removed at any depth, matched case-insensitively.
type PruneSpec struct {
	Paths []string
	Names []string
}

// autoloadNames are the base names, at any depth, that a seat's CLI would load
// on its own as instructions, settings or tools, or that pose as the
// repository's standards: Claude's CLAUDE.md, CLAUDE.local.md, .claude and
// .mcp.json, the other providers' instruction files and directories, and
// AGENTS.md. A change must not be able to plant any of them. The Spec seat does
// not need AGENTS.md in its tree (the Standards axis judges it, and the Spec
// seat sees what the change touches in the digest), so both passes lose it.
var autoloadNames = []string{
	"claude.md", "claude.local.md", ".claude", ".mcp.json",
	".codex", ".gemini", "gemini.md", ".cursor", ".cursorrules", ".opencode",
	"agents.md",
}

// PruneSpecFor is what a pass loses: the autoload names at any depth for both
// axes, and for the Standards pass also the head copies of the default and the
// configured standards files, so the only standards it can read are the base
// copies.
func PruneSpecFor(axis Axis, extraStandards []string) PruneSpec {
	spec := PruneSpec{Names: append([]string{}, autoloadNames...)}
	if axis == AxisStandards {
		spec.Paths = append(append([]string{}, DefaultStandardsFiles...), extraStandards...)
	}
	return spec
}

// SingleSeatPruneSpec is what the single review's seat (steward.twoAxis off)
// loses from its scratch tree: the autoload names at any depth, as both passes
// do, and no standards paths (it has no base copies and is never pointed at a
// standards file).
func SingleSeatPruneSpec() PruneSpec { return PruneSpecFor("", nil) }

// PruneScratch removes what spec names from a seat's scratch tree and never
// follows a symlink: a symlink met at any component of a path, parent or last,
// is removed itself and the walk stops there, and the by-name walk does not
// descend into symlinked directories. Nothing whose parent resolves outside
// scratch is ever touched. The seat has not started yet, so nothing else is
// changing the tree.
func PruneScratch(scratch string, spec PruneSpec) error {
	for _, p := range spec.Paths {
		if !config.SafeRepoPath(p) {
			continue
		}
		if err := removeNoFollow(scratch, p); err != nil {
			return err
		}
	}
	if len(spec.Names) == 0 {
		return nil
	}
	names := make(map[string]bool, len(spec.Names))
	for _, n := range spec.Names {
		names[strings.ToLower(n)] = true
	}
	return filepath.WalkDir(scratch, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == scratch {
			return nil
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !names[strings.ToLower(d.Name())] {
			return nil
		}
		// d is an entry of a real directory WalkDir reached without following a
		// symlink, so removing it (a symlink is unlinked, a directory tree is
		// removed entry by entry) cannot leave scratch.
		if err := os.RemoveAll(p); err != nil {
			return err
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

// removeNoFollow removes rel (a safe repository-relative path) under root,
// walking it one component at a time with Lstat: the first symlink on the way
// is removed itself and nothing behind it is touched.
func removeNoFollow(root, rel string) error {
	cur := root
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		next := filepath.Join(cur, part)
		fi, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case fsutil.IsLink(fi.Mode()):
			return os.Remove(next)
		case i == len(parts)-1:
			return os.RemoveAll(next)
		case !fi.IsDir():
			return nil
		}
		cur = next
	}
	return nil
}
