package workspace

import (
	"context"
	"strings"
)

// GitHead returns dir's current HEAD as a full commit SHA (O1 run
// provenance). Any failure -- not a git repo, git missing, no commits yet,
// a cancelled context -- is an error, never a panic; the caller records
// NULL for what it could not read.
func GitHead(ctx context.Context, dir string) (string, error) {
	out, err := runGitOutputEnvCtx(ctx, dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// GitDirty reports whether dir's worktree has any local changes: a
// non-empty `git status --porcelain` output, so untracked files count and
// ignored files do not. --no-optional-locks keeps this read-only: a plain
// status may refresh and rewrite the index, which would take index.lock
// under an agent that is running its own git commands in the same worktree.
// Like GitHead, every failure is an error.
func GitDirty(ctx context.Context, dir string) (bool, error) {
	out, err := runGitOutputEnvCtx(ctx, dir, nil, "--no-optional-locks", "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}
