//go:build !windows

package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
)

func bareHas(bare, ref string) string {
	out, err := exec.Command("git", "--git-dir", bare, "rev-parse", "--verify", "-q", ref).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// newPushPREnv is a workspace whose origin is a bare repo holding PR 7's head
// both as refs/pull/7/head and as the branch feat/x.
func newPushPREnv(t *testing.T) (m *Manager, wsID int64, bare string) {
	t.Helper()
	m, wsID, repo, bare, _, _ := newTaskRefEnv(t)
	sha := strings.TrimSpace(runGitOutputT(t, repo, "rev-parse", "HEAD"))
	runGitPushRef(t, repo, bare, sha, "refs/pull/7/head")
	runGitPushRef(t, repo, bare, sha, "refs/heads/feat/x")
	return m, wsID, bare
}

func commitInTask(t *testing.T, wt string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, wt, "add", "a.txt")
	runGitT(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=T", "commit", "-m", "change")
}

// "Run setup" clears untrusted_source; the fork fact lives in git config, so
// the task stays unpushable.
func TestPushForkPRRefusedAfterSetupRun(t *testing.T) {
	m, wsID, bare := newPushPREnv(t)
	pr := sameRepoPR()
	pr.FromFork = true
	useProvider(m, &fakePRProvider{pr: pr})
	task, err := m.CreateTask(wsID, nil, "Fork PR", WithPullRequest(7))
	if err != nil {
		t.Fatal(err)
	}
	if !task.UntrustedSource {
		t.Fatal("fork PR task is trusted")
	}
	if cleared, err := m.store.ClearTaskUntrusted(task.ID); err != nil || !cleared {
		t.Fatalf("ClearTaskUntrusted = %v, %v", cleared, err)
	}
	if got, _ := m.store.GetTask(task.ID); got.UntrustedSource || got.PRNumber != 7 {
		t.Fatalf("task after setup run = untrusted %v pr %d", got.UntrustedSource, got.PRNumber)
	}
	commitInTask(t, *task.WorktreePath)
	before := bareHas(bare, "refs/heads/feat/x")

	got, _ := m.store.GetTask(task.ID)
	ws, _ := m.store.GetWorkspace(got.WorkspaceID)
	g, err := m.freshGit(got, ws)
	if err != nil {
		t.Fatal(err)
	}
	if a := g.Actions["push"]; a.Enabled || a.Code != forge.CodeForkPRReadonly || a.Reason != forge.MsgForkPushReadonly {
		t.Fatalf("push action = %+v", a)
	}
	_, err = m.Push(context.Background(), task.ID)
	if pe, ok := err.(*forge.PRError); !ok || pe.Code != forge.CodeForkPRReadonly {
		t.Fatalf("Push error = %v, want fork_pr_readonly", err)
	}
	if bareHas(bare, "refs/heads/feat/x") != before || bareHas(bare, "refs/heads/"+*task.Branch) != "" {
		t.Fatal("a refused push reached origin")
	}
}

// A same-repo PR task, even once its untrusted flag is cleared, still pushes
// to the PR head.
func TestPushSameRepoPRUnaffectedByForkRule(t *testing.T) {
	m, wsID, bare := newPushPREnv(t)
	useProvider(m, &fakePRProvider{pr: sameRepoPR()})
	task, err := m.CreateTask(wsID, nil, "Same repo", WithPullRequest(7))
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := runGitOutput(*task.WorktreePath, "config", "--get", "branch."+*task.Branch+".smindForkPr"); strings.TrimSpace(out) != "" {
		t.Fatalf("same-repo task has the fork key set: %q", out)
	}
	commitInTask(t, *task.WorktreePath)
	head := strings.TrimSpace(runGitOutputT(t, *task.WorktreePath, "rev-parse", "HEAD"))
	res, err := m.Push(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Branch != "feat/x" || bareHas(bare, "refs/heads/feat/x") != head {
		t.Fatalf("result %+v, origin feat/x = %q", res, bareHas(bare, "refs/heads/feat/x"))
	}
}
