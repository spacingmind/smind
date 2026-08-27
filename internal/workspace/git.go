package workspace

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, stderr.String())
	}
	return nil
}

// dirExists reports whether path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
