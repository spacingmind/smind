package validation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Digest fingerprints a worktree's HEAD and `git status --porcelain` output.
// The seat records it for the lead's worktree before the validator starts and
// compares it after: a difference means the validator changed the lead's
// worktree (a scope violation). It only reads.
func Digest(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	head, err := gitOut(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	status, err := gitOut(ctx, dir, "status", "--porcelain", "--ignore-submodules=all", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(head + "\n" + status))
	return hex.EncodeToString(sum[:]), nil
}

// IsAncestor reports whether commit is an ancestor of (or equal to) of in dir.
// commit must be a full 40-hex id.
func IsAncestor(ctx context.Context, dir, commit, of string) (bool, error) {
	if !IsSHA(commit) {
		return false, fmt.Errorf("%q is not a full commit id", commit)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := gitCmd(ctx, dir, "merge-base", "--is-ancestor", commit, of)
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func gitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_DIR=") || strings.HasPrefix(e, "GIT_WORK_TREE=") || strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	return cmd
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitCmd(ctx, dir, args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// GitHead returns dir's HEAD commit id.
func GitHead(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := gitOut(ctx, dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}
