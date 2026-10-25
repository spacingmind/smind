package workspace

import (
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// stackedPRTask is a parent task and a child stacked on the parent's branch.
func stackedPRTask(t *testing.T) (m *Manager, parent, child store.Task, repo string) {
	t.Helper()
	m, parent, repo, _ = prTask(t)
	writeAndCommit(t, *parent.WorktreePath, "p.txt", "p\n", "parent work")
	c, err := m.CreateTask(parent.WorkspaceID, nil, "Child", WithParentTask(parent.ID), WithBaseRef(*parent.Branch))
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	return m, parent, c, repo
}

func TestPRBase_StackedParentPushedUsesParent(t *testing.T) {
	m, parent, child, repo := stackedPRTask(t)
	runGitT(t, repo, "push", "origin", *parent.Branch)
	tg, err := m.prTarget(child.ID, "")
	if err != nil {
		t.Fatalf("prTarget() error = %v", err)
	}
	if tg.base != *parent.Branch {
		t.Fatalf("base = %q, want the pushed parent branch %q", tg.base, *parent.Branch)
	}
}

func TestPRBase_StackedParentNotPushedFallsBack(t *testing.T) {
	m, _, child, _ := stackedPRTask(t)
	tg, err := m.prTarget(child.ID, "")
	if err != nil {
		t.Fatalf("prTarget() error = %v", err)
	}
	if tg.base != "develop" {
		t.Fatalf("base = %q, want workspace default develop", tg.base)
	}
	tg, err = m.prTarget(child.ID, "release")
	if err != nil || tg.base != "release" {
		t.Fatalf("explicit base = %q, %v; want release", tg.base, err)
	}
}

func TestPRBase_ParentBranchDeletedFallsBack(t *testing.T) {
	m, parent, child, repo := stackedPRTask(t)
	runGitT(t, repo, "worktree", "remove", "--force", *parent.WorktreePath)
	runGitT(t, repo, "branch", "-D", *parent.Branch)
	tg, err := m.prTarget(child.ID, "")
	if err != nil {
		t.Fatalf("prTarget() error = %v", err)
	}
	if tg.base != "develop" {
		t.Fatalf("base = %q, want workspace default develop", tg.base)
	}
}

func TestPRBase_NotStackedUnchanged(t *testing.T) {
	m, task, _, _ := prTask(t)
	tg, err := m.prTarget(task.ID, "")
	if err != nil {
		t.Fatalf("prTarget() error = %v", err)
	}
	if tg.base != "develop" {
		t.Fatalf("base = %q, want develop", tg.base)
	}
}
