package workspace

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitWorktreeAdd runs `git worktree add <worktreePath> -b <branch>` inside
// repoPath, creating a real git worktree checked out on a new branch.
func gitWorktreeAdd(repoPath, worktreePath, branch string) error {
	return runGit(repoPath, "worktree", "add", worktreePath, "-b", branch)
}

// gitWorktreeRemove runs `git worktree remove <worktreePath> --force` inside
// repoPath. --force is used because archiving a task worktree is meant to
// discard it regardless of uncommitted changes.
func gitWorktreeRemove(repoPath, worktreePath string) error {
	return runGit(repoPath, "worktree", "remove", worktreePath, "--force")
}

// runGit shells out to the real git binary rather than using a Go git
// library: this project has no existing git-library dependency, and
// `git worktree` via os/exec is the simplest way to get real worktree
// semantics.
func runGit(repoPath string, args ...string) error {
	_, err := runGitOutputEnv(repoPath, nil, args...)
	return err
}

// runGitOutput behaves like runGit but returns stdout instead of discarding
// it.
func runGitOutput(dir string, args ...string) (string, error) {
	return runGitOutputEnv(dir, nil, args...)
}

// runGitOutputEnv behaves like runGitOutput, but if env is non-nil it
// replaces the subprocess's environment (rather than inheriting the
// current process's, which is what a nil Env means to os/exec) -- used by
// taskDiff to point git at a throwaway index file via GIT_INDEX_FILE
// without touching the worktree's real one.
func runGitOutputEnv(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, err, stderr.String())
	}
	return stdout.String(), nil
}

// dirExists reports whether path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// taskDiff returns the unified diff of everything changed in worktreePath
// (checked out on branch) relative to the commit branch was created from:
// any real commits made on branch since it forked off its base, plus
// whatever is currently sitting uncommitted in the worktree (staged,
// unstaged, and untracked new files) -- combined into a single diff, as if
// everything were already committed. A worktree with no changes at all
// returns an empty string and no error.
//
// This is deliberately not `git diff <base>...HEAD` (committed changes
// only) plus a separate `git diff`/`git diff --cached` for uncommitted
// changes glued together: that would require stitching two independently-
// generated diffs (with their own, possibly inconsistent, hunk contexts)
// into one, and still wouldn't cover brand new untracked files, which
// neither `git diff` nor `git diff --cached` shows without staging them
// first. Instead this uses a single technique, verified against a real
// worktree with staged, unstaged, untracked, and actually-committed
// changes all present at once (see docs/plans/active/web-ui-diff-viewer.md's
// Decisions section):
//
//  1. Find base, the commit branch's ref pointed at immediately after
//     `git worktree add -b branch` created it (see taskDiffBase).
//  2. Copy the worktree's real index into a throwaway temp file, then run
//     `git add -A` against *that* copy (via GIT_INDEX_FILE) so it ends up
//     holding a snapshot of the full current working-tree state --
//     including untracked files and deletions, which a fresh/empty index
//     could not detect since `git add -A` needs pre-existing tracked
//     entries to compare the working tree against. The real index (and
//     the caller's actual staged/unstaged state) is never touched.
//  3. `git diff --cached base` against that snapshot index produces one
//     unified diff: base's tree vs. the fully-snapshotted working tree,
//     which is exactly "everything this task has changed" a reviewer would
//     want to see, committed or not.
func taskDiff(worktreePath, branch string) (string, error) {
	base, err := taskDiffBase(worktreePath, branch)
	if err != nil {
		return "", fmt.Errorf("resolve base commit: %w", err)
	}

	realIndexPath, err := runGitOutput(worktreePath, "rev-parse", "--git-path", "index")
	if err != nil {
		return "", fmt.Errorf("resolve index path: %w", err)
	}
	realIndexPath = strings.TrimSpace(realIndexPath)
	if !filepath.IsAbs(realIndexPath) {
		realIndexPath = filepath.Join(worktreePath, realIndexPath)
	}
	realIndex, err := os.ReadFile(realIndexPath)
	if err != nil {
		return "", fmt.Errorf("read worktree index %q: %w", realIndexPath, err)
	}

	tmp, err := os.CreateTemp("", "smind-task-diff-index-")
	if err != nil {
		return "", fmt.Errorf("create throwaway index: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(realIndex); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write throwaway index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("write throwaway index: %w", err)
	}

	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)
	if _, err := runGitOutputEnv(worktreePath, env, "add", "-A"); err != nil {
		return "", fmt.Errorf("snapshot worktree into throwaway index: %w", err)
	}
	diff, err := runGitOutputEnv(worktreePath, env, "diff", "--no-color", "--cached", base)
	if err != nil {
		return "", fmt.Errorf("diff against base %s: %w", base, err)
	}
	return diff, nil
}

// taskDiffBase returns the full commit hash branch pointed at the moment it
// was created, found via branch's own reflog rather than any named "base
// branch" -- CreateTask never records which branch/commit a task's branch
// was forked from, and `git worktree add -b branch` (no explicit start
// point) always branches from repoPath's HEAD at that instant, so the
// oldest entry in branch's reflog *is* that fork point, regardless of
// whatever the source branch does afterward (further commits, deletion,
// rename, etc.) or what the workspace repo's checkout currently has
// checked out. Confirmed for real: `git worktree add -b <branch>` records
// a "branch: Created from HEAD" reflog entry as branch's very first (i.e.
// last-listed) entry.
func taskDiffBase(worktreePath, branch string) (string, error) {
	out, err := runGitOutput(worktreePath, "log", "-g", "--format=%H", branch)
	if err != nil {
		return "", fmt.Errorf("read reflog for branch %q: %w", branch, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	last := lines[len(lines)-1]
	if last == "" {
		return "", fmt.Errorf("branch %q has no reflog entries; cannot determine its base commit", branch)
	}
	return last, nil
}
