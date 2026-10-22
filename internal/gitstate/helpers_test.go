package gitstate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func write(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, path, content, msg string) {
	t.Helper()
	write(t, dir, path, content)
	git(t, dir, "add", path)
	git(t, dir, "commit", "-m", msg)
}

// fixture is a repo whose develop is pushed to a local bare origin, plus a
// task branch checked out in its own worktree, forked from develop.
type fixture struct {
	repo, bare, worktree, forkSha string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-b", "develop")
	git(t, repo, "config", "user.email", "t@example.com")
	git(t, repo, "config", "user.name", "T")
	git(t, repo, "config", "commit.gpgsign", "false")
	commit(t, repo, "README.md", "hello\n", "initial")
	bare := t.TempDir()
	git(t, bare, "init", "--bare", "-b", "develop")
	git(t, repo, "remote", "add", "origin", bare)
	git(t, repo, "push", "origin", "develop")
	fork := git(t, repo, "rev-parse", "HEAD")
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, repo, "worktree", "add", "-b", "smind/task-x", wt, "develop")
	return fixture{repo: repo, bare: bare, worktree: wt, forkSha: fork}
}

// advanceBase adds a commit to origin/develop (through the main checkout) and
// fetches it, so the task branch falls behind.
func (f fixture) advanceBase(t *testing.T, path, content string) {
	t.Helper()
	commit(t, f.repo, path, content, "base moves: "+path)
	git(t, f.repo, "push", "origin", "develop")
	git(t, f.repo, "fetch", "origin")
}

func (f fixture) input() Input {
	return Input{
		TaskID: 1, WorktreePath: f.worktree, RepoDir: f.repo,
		Branch: "smind/task-x", BaseRef: "develop", BaseSha: f.forkSha, BranchOwned: true,
	}
}

// countingRunner wraps DefaultRunner and counts calls whose first
// non-option argument matches.
type countingRunner struct {
	mu    sync.Mutex
	calls [][]string
}

func (c *countingRunner) run(ctx context.Context, dir string, args ...string) (string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, append([]string(nil), args...))
	c.mu.Unlock()
	return DefaultRunner(ctx, dir, args...)
}

func (c *countingRunner) count(sub string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, a := range c.calls {
		for _, s := range a {
			if s == sub {
				n++
				break
			}
		}
	}
	return n
}

func ip(n int) *int { return &n }
