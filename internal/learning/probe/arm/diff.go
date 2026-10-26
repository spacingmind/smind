package arm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const truncatedMarker = "\n[truncated]\n"

// maxCopyBytes bounds the sanitized copy of an arm made for diffing.
const maxCopyBytes = 128 << 20

// Diff renders what the arm changed relative to baseDir, a pristine export of
// the same commit (see Export) kept outside the arm. It NEVER runs git
// inside armDir: the agent controls that directory, and a planted
// `.git/config` (fsmonitor, textconv, pager, filters) or a symlink would
// execute or read outside the sandbox as the daemon.
//
// Instead the arm is copied into a daemon temp dir by walking it with Lstat
// and ReadDir: the top-level `.git` entry is skipped, every symlink and
// non-regular file is skipped, and nothing is opened through a link. Then
// `git diff --no-index` runs between baseDir and that copy from a temp
// working directory outside any repository, with a scrubbed environment and
// no textconv, external diff, fsmonitor or pager. The a/ and b/ path
// prefixes are rewritten to repo-relative paths. The text is clipped to
// maxBytes with a "[truncated]" marker; n is its size before clipping.
func Diff(ctx context.Context, baseDir, armDir string, maxBytes int) (diff string, n int, err error) {
	for _, d := range []string{baseDir, armDir} {
		if !filepath.IsAbs(d) {
			return "", 0, fmt.Errorf("arm: diff dir %q is not absolute", d)
		}
	}
	if maxBytes <= 0 {
		return "", 0, fmt.Errorf("arm: diff maxBytes %d", maxBytes)
	}
	tmp, err := os.MkdirTemp("", "smind-probe-diff-")
	if err != nil {
		return "", 0, fmt.Errorf("arm: diff temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	copyDir := filepath.Join(tmp, "arm")
	cwd := filepath.Join(tmp, "cwd")
	for _, d := range []string{copyDir, cwd} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return "", 0, fmt.Errorf("arm: diff temp dir: %w", err)
		}
	}
	var total int64
	if err := copySanitized(armDir, copyDir, true, &total); err != nil {
		return "", 0, err
	}

	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	// Slash paths: git prints native Windows paths quoted and backslash-escaped
	// ("a/D:\\tmp\\base/x"), which rewritePrefixes would never strip, leaving
	// daemon temp paths in the stored diff. With slashes it prints a/D:/tmp/base/x.
	cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-c", "core.pager=cat",
		"diff", "--no-index", "--no-textconv", "--no-ext-diff", "--no-color", "--", filepath.ToSlash(baseDir), filepath.ToSlash(copyDir))
	cmd.Dir = cwd
	cmd.Env = append(gitEnv(), "GIT_ATTR_NOSYSTEM=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 { // 1 = differences found
			return "", 0, fmt.Errorf("git diff: %w: %s", err, strings.TrimSpace(clip(errb.String(), 300)))
		}
	}
	text := rewritePrefixes(out.String(), baseDir, copyDir)
	n = len(text)
	if len(text) > maxBytes {
		text = strings.ToValidUTF8(text[:maxBytes], "") + truncatedMarker
	}
	return text, n, nil
}

// rewritePrefixes turns the temp-path a/ and b/ prefixes git printed into
// repo-relative ones.
func rewritePrefixes(s, baseDir, copyDir string) string {
	rel := func(p string) string { return strings.TrimPrefix(filepath.ToSlash(p), "/") }
	return strings.NewReplacer(
		"a/"+rel(baseDir)+"/", "a/",
		"b/"+rel(copyDir)+"/", "b/",
		// For a new or deleted file git prints the existing side's path on
		// both halves of the `diff --git` line.
		"b/"+rel(baseDir)+"/", "b/",
		"a/"+rel(copyDir)+"/", "a/",
	).Replace(s)
}

// copySanitized copies the regular files and directories of src into dst
// (which exists). Symlinks and every other entry type are skipped and never
// followed; the top-level `.git` entry is skipped.
func copySanitized(src, dst string, top bool, total *int64) error {
	ents, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("arm: diff read %q: %w", clip(src, 120), err)
	}
	for _, e := range ents {
		name := e.Name()
		if top && name == ".git" {
			continue
		}
		from, to := filepath.Join(src, name), filepath.Join(dst, name)
		fi, err := os.Lstat(from)
		if err != nil {
			continue // vanished: the arm is over, not running
		}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			continue
		case fi.IsDir():
			if err := os.Mkdir(to, 0o700); err != nil {
				return fmt.Errorf("arm: diff mkdir: %w", err)
			}
			if err := copySanitized(from, to, false, total); err != nil {
				return err
			}
		case fi.Mode().IsRegular():
			*total += fi.Size()
			if *total > maxCopyBytes {
				return ErrTooLarge
			}
			if err := copyRegular(from, to, fi); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyRegular copies one file, refusing it if what was opened is not the
// regular file that was lstat'd (a swapped-in link).
func copyRegular(from, to string, lfi os.FileInfo) error {
	f, err := os.Open(from)
	if err != nil {
		return nil
	}
	defer f.Close()
	ofi, err := f.Stat()
	if err != nil || !ofi.Mode().IsRegular() || !os.SameFile(lfi, ofi) {
		return nil
	}
	mode := os.FileMode(0o644)
	if lfi.Mode()&0o100 != 0 {
		mode = 0o755
	}
	w, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("arm: diff copy: %w", err)
	}
	_, cerr := io.Copy(w, io.LimitReader(f, lfi.Size()))
	if err := w.Close(); cerr == nil {
		cerr = err
	}
	if cerr != nil {
		return fmt.Errorf("arm: diff copy: %w", cerr)
	}
	return os.Chmod(to, mode) // umask must not change what the diff sees
}
