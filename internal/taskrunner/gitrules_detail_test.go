package taskrunner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGitRuleInfo(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.txt")

	push := gitRuleInfo(dir, "git push origin smind/task-x")
	if push == nil || push.Class != "always-ask" || push.Remote != "origin" || push.Branch != "smind/task-x" || !push.PerCall || push.ID != "git-push" {
		t.Fatalf("push = %+v", push)
	}
	if len(push.StagedFiles) != 0 {
		t.Fatalf("push staged = %v", push.StagedFiles)
	}

	commit := gitRuleInfo(dir, "git commit -m x")
	if commit == nil || commit.Class != "commit" || !commit.PerCall || len(commit.StagedFiles) != 1 || commit.StagedFiles[0] != "a.txt" {
		t.Fatalf("commit = %+v", commit)
	}

	un := gitRuleInfo(dir, `eval "git push"`)
	if un == nil || un.Class != "unparsed-git" || un.ID != "unparsed-git" || un.Note != unparsedGitNote {
		t.Fatalf("unparsed = %+v", un)
	}

	for _, c := range []string{"git status", "git log --oneline", "ls -la", "echo git push"} {
		if got := gitRuleInfo(dir, c); got != nil {
			t.Errorf("%q = %+v, want nil", c, got)
		}
	}

	// A non-repo cwd yields no staged files rather than an error.
	if c := gitRuleInfo(t.TempDir(), "git commit -m x"); c == nil || len(c.StagedFiles) != 0 {
		t.Fatalf("non-repo commit = %+v", c)
	}
}

func TestGitRuleInfoStagedCap(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	for i := 0; i < gitStagedMaxFiles+5; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "add", ".")
	c := gitRuleInfo(dir, "git commit -m x")
	if c == nil || len(c.StagedFiles) != gitStagedMaxFiles || !c.StagedTruncated {
		t.Fatalf("commit = %+v", c)
	}
}
