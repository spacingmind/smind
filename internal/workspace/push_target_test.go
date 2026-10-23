//go:build !windows

package workspace

import (
	"context"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
)

// PushTarget (what the git card shows) and Push (what actually runs) share one
// resolver; the result's remote and branch must agree in every shape.
func TestPushTargetMatchesPush(t *testing.T) {
	ctx := context.Background()
	check := func(t *testing.T, m *Manager, id int64, wantRemote, wantBranch string) {
		t.Helper()
		task, _ := m.store.GetTask(id)
		ws, _ := m.store.GetWorkspace(task.WorkspaceID)
		pt := m.PushTarget(ctx, task, ws)
		if pt == nil {
			t.Fatal("PushTarget = nil")
		}
		res, err := m.Push(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if res.Remote != pt.Remote || res.Branch != pt.Branch {
			t.Fatalf("Push %s/%s, PushTarget %s/%s", res.Remote, res.Branch, pt.Remote, pt.Branch)
		}
		if pt.Remote != wantRemote || pt.Branch != wantBranch {
			t.Fatalf("PushTarget = %s/%s, want %s/%s", pt.Remote, pt.Branch, wantRemote, wantBranch)
		}
	}

	t.Run("normal task", func(t *testing.T) {
		m, wsID, _, _, _, _ := newTaskRefEnv(t)
		task, err := m.CreateTask(wsID, nil, "Plain")
		if err != nil {
			t.Fatal(err)
		}
		commitInTask(t, *task.WorktreePath)
		check(t, m, task.ID, "origin", *task.Branch)
	})

	t.Run("pushRemote configured", func(t *testing.T) {
		m, wsID, _, _, _, _ := newTaskRefEnv(t)
		task, err := m.CreateTask(wsID, nil, "Other remote")
		if err != nil {
			t.Fatal(err)
		}
		second := t.TempDir()
		runGitT(t, second, "init", "--bare", "-q")
		runGitT(t, *task.WorktreePath, "remote", "add", "mirror", second)
		runGitT(t, *task.WorktreePath, "config", "branch."+*task.Branch+".pushRemote", "mirror")
		commitInTask(t, *task.WorktreePath)
		check(t, m, task.ID, "mirror", *task.Branch)
	})

	t.Run("same-repo PR head", func(t *testing.T) {
		m, wsID, _ := newPushPREnv(t)
		useProvider(m, &fakePRProvider{pr: sameRepoPR()})
		task, err := m.CreateTask(wsID, nil, "Same repo", WithPullRequest(7))
		if err != nil {
			t.Fatal(err)
		}
		commitInTask(t, *task.WorktreePath)
		check(t, m, task.ID, "origin", "feat/x")
	})

	t.Run("fork PR", func(t *testing.T) {
		m, wsID, _ := newPushPREnv(t)
		pr := sameRepoPR()
		pr.FromFork = true
		useProvider(m, &fakePRProvider{pr: pr})
		task, err := m.CreateTask(wsID, nil, "Fork PR", WithPullRequest(7))
		if err != nil {
			t.Fatal(err)
		}
		ws, _ := m.store.GetWorkspace(task.WorkspaceID)
		if pt := m.PushTarget(ctx, task, ws); pt != nil {
			t.Fatalf("PushTarget = %+v, want nil", pt)
		}
		_, err = m.Push(ctx, task.ID)
		if pe, ok := err.(*forge.PRError); !ok || pe.Code != forge.CodeForkPRReadonly {
			t.Fatalf("Push error = %v, want fork_pr_readonly", err)
		}
	})
}
