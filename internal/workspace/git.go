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

// checkpointCommitMessage marks the machine-generated commits ArchiveTask
// creates to preserve unreviewed work before a task's worktree is removed.
const checkpointCommitMessage = "smind: checkpoint before archive"

// gitWorktreeCheckpoint commits everything currently sitting uncommitted in
// worktreePath -- staged, unstaged, and untracked alike -- onto the branch
// the worktree has checked out, so the branch alone retains the full
// working-tree state and the worktree directory can then be removed without
// losing anything reviewable.
//
// Cleanliness is decided by `git status --porcelain`: an empty output is
// exactly the condition under which a following `git commit` would have
// nothing to record, so a worktree whose changes are all already committed
// (or that never had any) returns without creating an empty checkpoint
// commit. The status check is deliberately the same shape of check as
// taskDiff's full snapshot diff, but scoped to "is there anything to
// commit" rather than "what changed since base" -- only the former decides
// whether a commit is possible at all.
func gitWorktreeCheckpoint(worktreePath string) error {
	out, err := runGitOutput(worktreePath, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("read worktree status: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	if err := runGit(worktreePath, "add", "-A"); err != nil {
		return fmt.Errorf("stage worktree changes: %w", err)
	}
	if err := runGit(worktreePath, "commit", "-m", checkpointCommitMessage); err != nil {
		return fmt.Errorf("commit checkpoint: %w", err)
	}
	return nil
}

// gitWorktreeRemove runs `git worktree remove <worktreePath> --force` inside
// repoPath. Callers must have checkpointed any reviewable work first --
// ArchiveTask does so via gitWorktreeCheckpoint -- after which --force only
// guards against refuse-to-remove edge cases (e.g. untracked-but-ignored
// leftovers, submodules) rather than silently discarding reviewable changes.
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

// snapshotIndex copies the worktree's real index into a throwaway temp
// file, runs `git add -A` against that copy (via GIT_INDEX_FILE), and
// returns the env to give any further git invocation that should see the
// fully-snapshotted working-tree state. The caller must invoke the returned
// cleanup once done -- the temp index is this function's only side effect,
// and the worktree's real index (and its actual staged/unstaged state) is
// never touched. See taskDiff's doc comment for why a copy of the real
// index (rather than a fresh empty one) is the required starting point.
func snapshotIndex(worktreePath string) (env []string, cleanup func(), err error) {
	realIndexPath, err := runGitOutput(worktreePath, "rev-parse", "--git-path", "index")
	if err != nil {
		return nil, nil, fmt.Errorf("resolve index path: %w", err)
	}
	realIndexPath = strings.TrimSpace(realIndexPath)
	if !filepath.IsAbs(realIndexPath) {
		realIndexPath = filepath.Join(worktreePath, realIndexPath)
	}
	realIndex, err := os.ReadFile(realIndexPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read worktree index %q: %w", realIndexPath, err)
	}

	tmp, err := os.CreateTemp("", "smind-task-diff-index-")
	if err != nil {
		return nil, nil, fmt.Errorf("create throwaway index: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(realIndex); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return nil, nil, fmt.Errorf("write throwaway index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return nil, nil, fmt.Errorf("write throwaway index: %w", err)
	}

	snapEnv := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)
	if _, err := runGitOutputEnv(worktreePath, snapEnv, "add", "-A"); err != nil {
		os.Remove(tmpPath)
		return nil, nil, fmt.Errorf("snapshot worktree into throwaway index: %w", err)
	}
	return snapEnv, func() { os.Remove(tmpPath) }, nil
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
	env, cleanup, err := snapshotIndex(worktreePath)
	if err != nil {
		return "", fmt.Errorf("snapshot worktree index: %w", err)
	}
	defer cleanup()

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

// taskChangedFiles returns one entry per path in the same base→worktree
// snapshot diff taskDiff computes (`git diff --name-status` against the
// snapshot index -- identical computation, names-and-status instead of
// hunks), cross-referenced with `git status --porcelain` on the *real*
// index so each entry reports whether the file currently has anything
// staged. Status maps git's letter codes onto "added" (A), "modified"
// (M), "deleted" (D); any other code (e.g. R for renames) is carried
// through lowercased rather than collapsed, so callers never see a lie.
func taskChangedFiles(worktreePath, branch string) ([]TaskFile, error) {
	base, err := taskDiffBase(worktreePath, branch)
	if err != nil {
		return nil, fmt.Errorf("resolve base commit: %w", err)
	}
	env, cleanup, err := snapshotIndex(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("snapshot worktree index: %w", err)
	}
	defer cleanup()

	out, err := runGitOutputEnv(worktreePath, env, "diff", "--name-status", "--cached", base)
	if err != nil {
		return nil, fmt.Errorf("diff names against base %s: %w", base, err)
	}

	staged, err := stagedPaths(worktreePath)
	if err != nil {
		return nil, err
	}

	files := make([]TaskFile, 0)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 2 {
			return nil, fmt.Errorf("parse name-status line %q", line)
		}
		code, path := fields[0], fields[len(fields)-1]
		var status string
		switch code[0] {
		case 'A':
			status = "added"
		case 'M':
			status = "modified"
		case 'D':
			status = "deleted"
		default:
			status = strings.ToLower(code)
		}
		files = append(files, TaskFile{Path: path, Status: status, Staged: staged[path]})
	}
	return files, nil
}

// taskFileDiff returns the unified diff for exactly one path of the same
// base→worktree snapshot diff taskDiff computes: the identical invocation
// with a pathspec appended, so a per-file view is always a consistent
// slice of the whole-task diff. A path with no changes yields "".
func taskFileDiff(worktreePath, branch, path string) (string, error) {
	base, err := taskDiffBase(worktreePath, branch)
	if err != nil {
		return "", fmt.Errorf("resolve base commit: %w", err)
	}
	env, cleanup, err := snapshotIndex(worktreePath)
	if err != nil {
		return "", fmt.Errorf("snapshot worktree index: %w", err)
	}
	defer cleanup()

	diff, err := runGitOutputEnv(worktreePath, env, "diff", "--no-color", "--cached", base, "--", path)
	if err != nil {
		return "", fmt.Errorf("diff %q against base %s: %w", path, base, err)
	}
	return diff, nil
}

// taskStageFile stages (staged=true) or unstages (staged=false) a single
// path in the worktree's real index. The `--` separator keeps paths that
// look like options (or contain odd characters) from being interpreted as
// anything but a pathspec.
func taskStageFile(worktreePath, path string, staged bool) error {
	var err error
	if staged {
		err = runGit(worktreePath, "add", "--", path)
	} else {
		_, err = runGitOutput(worktreePath, "restore", "--staged", "--", path)
	}
	return err
}

// stagedPaths returns the set of paths git status --porcelain reports as
// having a staged change: entries whose index column (the first of the two
// XY status characters) is anything other than ' ' (unmodified) or '?'
// (untracked -- an untracked file is by definition not staged, even though
// porcelain prints "??").
func stagedPaths(worktreePath string) (map[string]bool, error) {
	out, err := runGitOutput(worktreePath, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("read worktree status: %w", err)
	}
	staged := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if len(line) < 4 {
			continue
		}
		x := line[0]
		path := strings.TrimPrefix(line[3:], "\"")
		path = strings.TrimSuffix(path, "\"")
		if x != ' ' && x != '?' {
			staged[path] = true
		}
	}
	return staged, nil
}

// gitTaskCommit commits whatever is currently staged in the worktree's
// real index (plain `git commit -m` -- no -a, no implicit add) and returns
// the new commit's full SHA, subject (first line of message), and file
// count. An index with nothing staged returns ErrNothingStaged rather
// than letting git's "nothing to commit" stderr leak: that case is an
// expected, caller-presentable condition, not a plumbing failure.
func gitTaskCommit(worktreePath, message string) (CommitResult, error) {
	names, err := runGitOutput(worktreePath, "diff", "--cached", "--name-only")
	if err != nil {
		return CommitResult{}, fmt.Errorf("read staged files: %w", err)
	}
	var fileCount int
	for _, line := range strings.Split(strings.TrimSpace(names), "\n") {
		if line != "" {
			fileCount++
		}
	}
	if fileCount == 0 {
		return CommitResult{}, ErrNothingStaged
	}

	if err := runGit(worktreePath, "commit", "-m", message); err != nil {
		return CommitResult{}, fmt.Errorf("commit: %w", err)
	}
	sha, err := runGitOutput(worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return CommitResult{}, fmt.Errorf("resolve new commit: %w", err)
	}
	subject := strings.SplitN(message, "\n", 2)[0]
	return CommitResult{
		Commit:  strings.TrimSpace(sha),
		Subject: subject,
		Files:   fileCount,
	}, nil
}
