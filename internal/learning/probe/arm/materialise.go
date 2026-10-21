// Package arm builds and tears down the throwaway directory a learning-probe
// arm runs in (ADR-0037 §3, R1/R4/R5): a fresh `git archive` export of the
// source run's base commit with its own `git init`, so nothing of the real
// checkout's .git (objects, refs, hooks, remotes, config) is reachable from
// the arm. It also holds the process-tree kill and the orphan sweep that
// back R4, and the cap meter that decides why an arm ends.
package arm

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrDirty is returned for a source run whose checkout was dirty: the
	// archive of its base commit would not reproduce what the run saw.
	ErrDirty = errors.New("arm: source checkout was dirty at the base commit")
	// ErrBadSHA is returned for a base that is not a full commit hash.
	ErrBadSHA = errors.New("arm: base sha is not a full commit hash")
	// ErrUnknownSHA is returned when the repository has no such commit.
	ErrUnknownSHA = errors.New("arm: base commit not found in the repository")
	// ErrBadDir is returned for an arm directory that is not usable.
	ErrBadDir = errors.New("arm: arm directory is not usable")
	// ErrUnsafeEntry is returned for an archive entry that must never be
	// written: one that escapes the arm, or any path with a .git component
	// (the arm's own repository is created afterwards; an exported .git would
	// carry the source repository's config and hooks into it).
	ErrUnsafeEntry = errors.New("arm: archive entry is unsafe")
	// ErrTooLarge is returned when the export exceeds MaxBytes or MaxFiles.
	ErrTooLarge = errors.New("arm: export is too large")
)

const (
	// MaxBytes and MaxFiles bound one export so a huge repository cannot
	// fill the disk through a probe click.
	MaxBytes = 512 << 20
	MaxFiles = 200000

	gitTimeout = 2 * time.Minute
)

var shaRE = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// Spec describes one arm export.
type Spec struct {
	// Repo is the real checkout (read only: only `git archive` runs there).
	Repo string
	// BaseSHA is the full commit hash the source run started from.
	BaseSHA string
	// Dirty is the source run's recorded dirty flag; true is refused.
	Dirty bool
	// Dir is the arm directory to create: absolute, absent or empty, with an
	// existing real (non-symlink) parent.
	Dir string
}

// Result reports what was exported.
type Result struct {
	// Head is the single commit of the arm's own repository.
	Head string
	// Files is the number of regular files written.
	Files int
	// SkippedLinks counts symlinks and special entries that were not
	// exported (a link could point out of the arm; submodules are empty).
	SkippedLinks int
}

// gitEnv is the environment of every git command run here: no user, system
// or global config, no pager, no prompt, no hooks or filters of the host.
func gitEnv() []string {
	env := []string{
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "LC_ALL=C",
		"GIT_AUTHOR_NAME=smind-probe", "GIT_AUTHOR_EMAIL=probe@smind.invalid",
		"GIT_COMMITTER_NAME=smind-probe", "GIT_COMMITTER_EMAIL=probe@smind.invalid",
	}
	if p, ok := os.LookupEnv("PATH"); ok {
		env = append(env, "PATH="+p)
	}
	return env
}

func runGit(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Stdin = stdin
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(clip(errb.String(), 300)))
	}
	return out.Bytes(), nil
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Materialise exports s.BaseSHA into s.Dir and makes it a one-commit git
// repository with no remote and no credentials. It never writes to s.Repo:
// the real checkout's HEAD, index, refs and worktree are untouched (R1, R5).
// On any error the partly built directory is removed.
func Materialise(ctx context.Context, s Spec) (res Result, err error) {
	if s.Dirty {
		return Result{}, ErrDirty
	}
	if !shaRE.MatchString(s.BaseSHA) {
		return Result{}, fmt.Errorf("%w: %q", ErrBadSHA, clip(s.BaseSHA, 80))
	}
	if !filepath.IsAbs(s.Repo) {
		return Result{}, fmt.Errorf("arm: repo %q is not absolute", s.Repo)
	}
	if err := checkDir(s.Dir); err != nil {
		return Result{}, err
	}
	if _, err := runGit(ctx, s.Repo, nil, "cat-file", "-e", s.BaseSHA+"^{commit}"); err != nil {
		return Result{}, fmt.Errorf("%w: %s", ErrUnknownSHA, s.BaseSHA)
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrBadDir, err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(s.Dir)
		}
	}()
	if err := os.Chmod(s.Dir, 0o700); err != nil {
		return Result{}, fmt.Errorf("arm: chmod arm dir: %w", err)
	}

	if res, err = streamArchive(ctx, s.Repo, s.BaseSHA, s.Dir); err != nil {
		return Result{}, err
	}
	if _, err := runGit(ctx, s.Dir, nil, "init", "-q", "-b", "probe"); err != nil {
		return Result{}, err
	}
	// Hooks off, signing off, no remote: the arm repo is only a diff base.
	for _, args := range [][]string{
		{"-c", "core.hooksPath=/dev/null", "add", "-A", "-f"},
		{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "--no-verify", "-m", "probe base " + s.BaseSHA},
	} {
		if _, err := runGit(ctx, s.Dir, nil, args...); err != nil {
			return Result{}, err
		}
	}
	head, err := runGit(ctx, s.Dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	res.Head = strings.TrimSpace(string(head))
	return res, nil
}

// streamArchive runs `git archive` in repo and extracts its output into dir as
// it is produced, so a huge repository is never held in memory: the raw stream
// is cut off past the size limits, which also kills git.
func streamArchive(ctx context.Context, repo, sha, dir string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "archive", "--format=tar", sha)
	cmd.Dir = repo
	cmd.Env = gitEnv()
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("arm: git archive: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("arm: git archive: %w", err)
	}
	res, xerr := extractLimit(out, dir, MaxBytes, MaxFiles)
	if xerr == nil {
		// Let git finish: read the stream's tail (tar padding) to EOF, bounded.
		_, _ = io.Copy(io.Discard, io.LimitReader(out, 1<<20))
	} else {
		cancel() // stop git before waiting on it
	}
	werr := cmd.Wait()
	if xerr != nil {
		return Result{}, xerr
	}
	if werr != nil {
		return Result{}, fmt.Errorf("git archive: %w: %s", werr, strings.TrimSpace(clip(errb.String(), 300)))
	}
	return res, nil
}

// checkDir requires dir to be absolute, to have a real parent directory and
// to be absent or an empty real directory.
func checkDir(dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return fmt.Errorf("%w: %q is not a clean absolute path", ErrBadDir, dir)
	}
	pfi, err := os.Lstat(filepath.Dir(dir))
	if err != nil || !pfi.IsDir() {
		return fmt.Errorf("%w: parent of %q is not a directory", ErrBadDir, dir)
	}
	fi, err := os.Lstat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("%w: %v", ErrBadDir, err)
	case !fi.IsDir():
		return fmt.Errorf("%w: %q exists and is not a directory", ErrBadDir, dir)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadDir, err)
	}
	if len(ents) > 0 {
		return fmt.Errorf("%w: %q is not empty", ErrBadDir, dir)
	}
	return nil
}

// extract unpacks a git-archive tar into dir. Names are validated against
// traversal, modes are stripped to rwx for the owner/group-world-readable
// (no setuid/setgid/sticky), and symlinks are skipped rather than exported:
// a repository symlink could point out of the arm, and the point of the arm
// is that nothing outside it is reachable.
func extract(r io.Reader, dir string) (Result, error) {
	return extractLimit(r, dir, MaxBytes, MaxFiles)
}

// tarOverheadPerFile bounds the tar header and padding bytes one entry adds
// on top of its content.
const tarOverheadPerFile = 1536

// limitedReader fails with ErrTooLarge once more than n bytes were read, so a
// stream that lies about entry sizes (or has endless entries) is cut off.
type limitedReader struct {
	r io.Reader
	n int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

// unsafeName reports whether a cleaned archive path must be refused: it
// escapes the arm, contains a NUL, or has a component equal to .git in any
// case (macOS and Windows file systems are case-insensitive).
func unsafeName(name string) bool {
	if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsRune(name, 0) {
		return true
	}
	for _, c := range strings.Split(name, "/") {
		if strings.EqualFold(c, ".git") {
			return true
		}
	}
	return false
}

func extractLimit(r io.Reader, dir string, maxBytes int64, maxFiles int) (Result, error) {
	var res Result
	var total int64
	tr := tar.NewReader(&limitedReader{r: r, n: maxBytes + int64(maxFiles)*tarOverheadPerFile})
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return res, nil
		}
		if err != nil {
			return Result{}, fmt.Errorf("arm: read archive: %w", err)
		}
		if h.Typeflag == tar.TypeXGlobalHeader { // git archive's commit-id comment
			continue
		}
		name := path.Clean(h.Name)
		if name == "." {
			continue
		}
		if unsafeName(name) {
			return Result{}, fmt.Errorf("%w: %q", ErrUnsafeEntry, clip(h.Name, 120))
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return Result{}, fmt.Errorf("arm: mkdir: %w", err)
			}
		case tar.TypeReg:
			res.Files++
			total += h.Size
			if res.Files > maxFiles || total > maxBytes {
				return Result{}, ErrTooLarge
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return Result{}, fmt.Errorf("arm: mkdir: %w", err)
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o100 != 0 {
				mode = 0o755
			}
			// O_EXCL|O_NOFOLLOW semantics: a duplicate name or a pre-existing
			// link is an error, never followed.
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return Result{}, fmt.Errorf("arm: create %q: %w", clip(name, 120), err)
			}
			_, cerr := io.Copy(f, io.LimitReader(tr, h.Size))
			if err := f.Close(); cerr == nil {
				cerr = err
			}
			if cerr != nil {
				return Result{}, fmt.Errorf("arm: write %q: %w", clip(name, 120), cerr)
			}
		default: // symlink, hard link, device, fifo
			res.SkippedLinks++
		}
	}
}
