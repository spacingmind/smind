package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// GitHead returns dir's current HEAD as a full commit SHA (O1 run
// provenance). Any failure -- not a git repo, git missing, no commits yet,
// a cancelled context -- is an error, never a panic; the caller records
// NULL for what it could not read.
func GitHead(ctx context.Context, dir string) (string, error) {
	out, err := runProvenanceGit(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// GitDirty reports whether dir's worktree has any local changes: a
// non-empty `git status --porcelain` output, so untracked files count and
// ignored files do not. Like GitHead, every failure is an error.
func GitDirty(ctx context.Context, dir string) (bool, error) {
	out, err := runProvenanceGit(ctx, dir, "status", "--porcelain", "--ignore-submodules=all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// runProvenanceGit runs a read-only git command for run provenance. It runs
// automatically at the start and end of every run, in a worktree the agent
// can edit, so it is hardened beyond the user-triggered git calls:
//   - -c core.fsmonitor=false: git status would otherwise execute whatever
//     command the (agent-editable) repo config names as its fsmonitor hook;
//   - --no-optional-locks / GIT_OPTIONAL_LOCKS=0: status must not rewrite
//     the index or take index.lock under an agent running its own git;
//   - GIT_TERMINAL_PROMPT=0: never block on a credential prompt;
//   - GIT_CEILING_DIRECTORIES=<parent of dir>: a worktree whose own .git is
//     gone must read as "not a repo", not as whichever parent repo (a
//     dotfiles repo at ~) discovery would otherwise walk up into.
func runProvenanceGit(ctx context.Context, dir string, args ...string) (string, error) {
	ceiling := filepath.Dir(dir)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		ceiling = filepath.Dir(real)
	}
	env := append(os.Environ(),
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CEILING_DIRECTORIES="+ceiling,
	)
	full := append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false"}, args...)
	return runGitOutputEnvCtx(ctx, dir, env, full...)
}
