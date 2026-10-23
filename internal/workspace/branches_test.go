package workspace

import (
	"fmt"
	"strings"
	"testing"
)

// newBranchesEnv builds a workspace on "develop" with origin, where:
//   - feat/a is local only, 2 commits ahead of a develop that has since moved
//     1 commit on origin (ahead 2, behind 1);
//   - shared exists locally (1 commit ahead) and on origin (older);
//   - remote-only exists only as origin/remote-only;
//   - origin/HEAD points at develop.
func newBranchesEnv(t *testing.T) (m *Manager, wsID int64, repo string) {
	t.Helper()
	repo, _ = newRepoWithOrigin(t)
	runGitT(t, repo, "checkout", "-b", "feat/a")
	writeAndCommit(t, repo, "a1.txt", "1\n", "a1")
	writeAndCommit(t, repo, "a2.txt", "2\n", "a2")
	runGitT(t, repo, "checkout", "-b", "shared", "develop")
	runGitT(t, repo, "push", "origin", "shared")
	writeAndCommit(t, repo, "s1.txt", "1\n", "s1")
	runGitT(t, repo, "push", "origin", "develop:refs/heads/remote-only")
	runGitT(t, repo, "checkout", "develop")
	writeAndCommit(t, repo, "d1.txt", "1\n", "d1")
	runGitT(t, repo, "push", "origin", "develop")
	runGitT(t, repo, "fetch", "origin")
	runGitT(t, repo, "remote", "set-head", "origin", "develop")

	m = newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	return m, w.ID, repo
}

func branchByName(t *testing.T, l BranchList, name string) BranchInfo {
	t.Helper()
	for _, b := range l.Branches {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("branch %q not in %+v", name, l.Branches)
	return BranchInfo{}
}

func TestWorkspaceBranches_LocalAndOrigin(t *testing.T) {
	m, wsID, _ := newBranchesEnv(t)

	l, err := m.Branches(wsID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if l.DefaultBase != "develop" || l.Current != "develop" {
		t.Fatalf("defaultBase=%q current=%q, want develop/develop", l.DefaultBase, l.Current)
	}
	counts := map[string]int{}
	for _, b := range l.Branches {
		counts[b.Name]++
		if b.Name == "HEAD" || b.Name == "origin/HEAD" {
			t.Fatalf("origin/HEAD leaked into the list: %+v", b)
		}
	}
	for name, n := range counts {
		if n != 1 {
			t.Fatalf("%q listed %d times, want once (local beats origin): %+v", name, n, l.Branches)
		}
	}
	if got := branchByName(t, l, "shared").Remote; got != BranchLocal {
		t.Fatalf("shared remote = %q, want local (local wins over the origin duplicate)", got)
	}
	if got := branchByName(t, l, "remote-only").Remote; got != BranchOrigin {
		t.Fatalf("remote-only remote = %q, want origin", got)
	}
	if got := branchByName(t, l, "feat/a").Remote; got != BranchLocal {
		t.Fatalf("feat/a remote = %q, want local", got)
	}
	if got := branchByName(t, l, "develop").Remote; got != BranchLocal {
		t.Fatalf("develop remote = %q, want local", got)
	}

	// Query: case-insensitive substring over the name.
	l, err = m.Branches(wsID, "FEAT", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Branches) != 1 || l.Branches[0].Name != "feat/a" {
		t.Fatalf("query FEAT = %+v, want only feat/a", l.Branches)
	}
	if l, _ = m.Branches(wsID, "no-such-branch", 0); len(l.Branches) != 0 || l.Branches == nil {
		t.Fatalf("empty result = %#v, want a non-nil empty slice", l.Branches)
	}

	// Limit clamps to the page size.
	if l, _ = m.Branches(wsID, "", 2); len(l.Branches) != 2 {
		t.Fatalf("limit 2 returned %d branches", len(l.Branches))
	}
	if _, err := m.Branches(999999, "", 0); err == nil {
		t.Fatal("unknown workspace: want an error")
	}
}

func TestWorkspaceBranches_AheadBehindVsDefaultBase(t *testing.T) {
	m, wsID, _ := newBranchesEnv(t)
	l, err := m.Branches(wsID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, ahead, behind int) {
		t.Helper()
		b := branchByName(t, l, name)
		if b.Ahead == nil || b.Behind == nil || *b.Ahead != ahead || *b.Behind != behind {
			t.Fatalf("%s ahead/behind = %v/%v, want %d/%d", name, deref(b.Ahead), deref(b.Behind), ahead, behind)
		}
	}
	check("feat/a", 2, 1)
	check("shared", 1, 1)
	check("remote-only", 0, 1)
	check("develop", 0, 0)
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestWorkspaceBranches_AheadBehindPageOnly(t *testing.T) {
	m, wsID, repo := newBranchesEnv(t)
	for i := 0; i < 8; i++ {
		runGitT(t, repo, "branch", fmt.Sprintf("many/b%d", i), "develop")
	}

	before := branchCountCalls.Load()
	l, err := m.Branches(wsID, "many/", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Branches) != 3 {
		t.Fatalf("page = %d, want 3", len(l.Branches))
	}
	if n := branchCountCalls.Load() - before; n != 3 {
		t.Fatalf("rev-list --count calls = %d for a 3-branch page of 8 matches, want 3", n)
	}

	before = branchCountCalls.Load()
	if l, err = m.Branches(wsID, "", 1000); err != nil {
		t.Fatal(err)
	}
	if n := branchCountCalls.Load() - before; n != int64(len(l.Branches)) || len(l.Branches) > maxBranchLimit {
		t.Fatalf("calls=%d for %d branches (max %d): want one per returned branch, capped at the max", n, len(l.Branches), maxBranchLimit)
	}
}

func TestWorkspaceBranches_NoOriginRemote(t *testing.T) {
	m := newTestManager(t)
	repo := newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "main")
	runGitT(t, repo, "branch", "side")
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := m.Branches(w.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, b := range l.Branches {
		names = append(names, b.Name+"@"+b.Remote)
	}
	if len(l.Branches) != 2 || !strings.Contains(strings.Join(names, ","), "side@local") || l.DefaultBase != "main" {
		t.Fatalf("branches = %v defaultBase=%q, want main and side, both local", names, l.DefaultBase)
	}
}
