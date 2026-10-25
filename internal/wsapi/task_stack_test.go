package wsapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func stackCreate(t *testing.T, e *prEnv, rc *requestContext, params map[string]any) (id int64, baseRef, branch string) {
	t.Helper()
	params["workspaceId"] = e.task.WorkspaceID
	params["title"] = "child"
	out, err := e.callAs(rc, "task.create", params)
	if err != nil {
		t.Fatalf("task.create error = %v", err)
	}
	raw, _ := json.Marshal(out)
	var got struct {
		ID      int64
		BaseRef string
		Branch  *string
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Branch == nil {
		t.Fatal("child has no branch")
	}
	return got.ID, got.BaseRef, *got.Branch
}

func mergeBase(t *testing.T, dir, a, b string) string {
	t.Helper()
	return strings.TrimSpace(gitOut(t, dir, "merge-base", a, b))
}

func TestCreateTask_HumanSubtaskDefaultsToParentBranch(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("p.txt", "p", "parent work")
	_, base, child := stackCreate(t, e, &requestContext{}, map[string]any{"parentTaskId": e.task.ID})
	if base != e.branch {
		t.Fatalf("base_ref = %q, want parent branch %q", base, e.branch)
	}
	tip := strings.TrimSpace(gitOut(t, e.repo, "rev-parse", e.branch))
	if got := mergeBase(t, e.repo, child, e.branch); got != tip {
		t.Fatalf("merge-base = %s, want parent tip %s", got, tip)
	}
}

func TestCreateTask_ExplicitBaseBeatsParentDefault(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("p.txt", "p", "parent work")
	_, base, _ := stackCreate(t, e, &requestContext{}, map[string]any{"parentTaskId": e.task.ID, "baseRef": "develop"})
	if base != "develop" {
		t.Fatalf("base_ref = %q, want develop", base)
	}
}

func TestCreateTask_ParentWithoutBranchFallsBack(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	bare, err := e.db.CreateTask(store.Task{WorkspaceID: e.task.WorkspaceID, Title: "no branch", Status: e.task.Status})
	if err != nil {
		t.Fatal(err)
	}
	_, base, _ := stackCreate(t, e, &requestContext{}, map[string]any{"parentTaskId": bare.ID})
	if base != "develop" {
		t.Fatalf("base_ref = %q, want workspace default develop", base)
	}
}

func TestCreateTask_ParentBranchGoneFallsBack(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	runGitT(t, e.repo, "worktree", "remove", "--force", e.wt)
	runGitT(t, e.repo, "branch", "-D", e.branch)
	_, base, _ := stackCreate(t, e, &requestContext{}, map[string]any{"parentTaskId": e.task.ID})
	if base != "develop" {
		t.Fatalf("base_ref = %q, want workspace default develop", base)
	}
}

func TestCreateTask_AgentChildKeepsDefaultBase(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("p.txt", "p", "parent work")
	rc := &requestContext{runScoped: true, ident: runIdentity{taskID: e.task.ID}}
	_, base, child := stackCreate(t, e, rc, map[string]any{})
	if base != "develop" {
		t.Fatalf("base_ref = %q, want workspace default develop", base)
	}
	tip := strings.TrimSpace(gitOut(t, e.repo, "rev-parse", e.branch))
	if got := mergeBase(t, e.repo, child, e.branch); got == tip {
		t.Fatalf("agent child forked from the parent tip %s; want the default base", tip)
	}
}

func TestCreateTask_GrandchildStacksOnChild(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("p.txt", "p", "parent work")
	childID, _, childBranch := stackCreate(t, e, &requestContext{}, map[string]any{"parentTaskId": e.task.ID})
	_, base, _ := stackCreate(t, e, &requestContext{}, map[string]any{"parentTaskId": childID})
	if base != childBranch {
		t.Fatalf("grandchild base_ref = %q, want child branch %q", base, childBranch)
	}
}
