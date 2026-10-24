//go:build !windows

package wsapi

import (
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// Tests for the archive preview's on-demand PR lookup (ADR-0039 O6, narrowed):
// a squash-merged branch of a task with no stored PR identity. The origin is a
// local bare repository and gh is the stub; nothing reaches GitHub.

// squashMerged lands the task's work in origin/develop as one squash commit, so
// the branch tip is not an ancestor of origin/develop.
func (e *prEnv) squashMerged() {
	e.t.Helper()
	e.commit("a.txt", "a\n", "work")
	runGitT(e.t, e.repo, "merge", "--squash", e.branch)
	runGitT(e.t, e.repo, "commit", "-m", "squash")
	runGitT(e.t, e.repo, "push", "origin", "develop")
}

// listPR scripts `gh pr list --head` to return one PR for the task's branch.
func (e *prEnv) listPR(state, headSha string, mod ...func(map[string]any)) {
	e.t.Helper()
	row := map[string]any{
		"number": 7, "url": "https://github.com/acme/repo/pull/7", "title": "t", "state": state,
		"isDraft": false, "headRefName": e.branch, "headRefOid": headSha, "baseRefName": "develop",
		"isCrossRepository": false, "mergeable": "UNKNOWN", "reviewDecision": "",
	}
	for _, m := range mod {
		m(row)
	}
	e.gh.mu.Lock()
	e.gh.listOut = "[" + string(mustJSON(e.t, row)) + "]"
	e.gh.mu.Unlock()
}

func (e *prEnv) preview() map[string]any {
	e.t.Helper()
	res, err := e.call("task.archivePreview", map[string]any{"taskId": e.task.ID})
	if err != nil {
		e.t.Fatal(err)
	}
	return asMap(e.t, res)
}

func wantMerged(t *testing.T, m map[string]any, state, via string) {
	t.Helper()
	if mg := m["merged"].(map[string]any); mg["state"] != state || mg["via"] != via {
		t.Fatalf("merged = %v, want %s via %q", mg, state, via)
	}
}

func TestArchivePreviewLookup_SquashMergedBranchWithNoStoredPR(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.squashMerged()
	e.listPR("MERGED", e.head())

	m := e.preview()
	wantMerged(t, m, "merged", "pr")
	if m["canDeleteBranch"] != true {
		t.Fatalf("preview = %v, want an owned merged branch deletable", m)
	}
	got := e.task_()
	if got.PRNumber != 7 || got.PRRepo != "acme/repo" || got.PRForge != "github" || got.PRHeadRef != e.branch ||
		got.PRURL != "https://github.com/acme/repo/pull/7" {
		t.Fatalf("identity = %+v, want PR 7 adopted", got)
	}

	// Adopted: later views read the stored identity, no second lookup.
	before := e.gh.plainCalls("pr")
	wantMerged(t, e.preview(), "merged", "pr")
	if n := e.gh.plainCalls("pr"); n != before {
		t.Fatalf("second preview made %d more gh pr calls", n-before)
	}
}

func TestArchiveLookup_DeleteBranchSeesTheSquashMergedPR(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.squashMerged()
	e.listPR("MERGED", e.head())

	res, err := e.archive(map[string]any{"deleteBranch": true})
	if err != nil {
		t.Fatal(err)
	}
	if task := res.(store.Task); task.ArchivedAt == nil || task.BranchDeletedAt == nil || e.localBranchExists(e.branch) {
		t.Fatalf("task = %+v, want the squash-merged branch deleted", task)
	}
}

func TestArchivePreviewLookup_BranchAdvancedAfterTheMergeStaysUnmerged(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.squashMerged()
	merged := e.head()
	e.commit("b.txt", "b\n", "reused the branch name")
	e.listPR("MERGED", merged)

	m := e.preview()
	wantMerged(t, m, "unmerged", "")
	if m["canDeleteBranch"] != false {
		t.Fatalf("preview = %v, want not deletable", m)
	}
	if got := e.task_(); got.PRNumber != 0 {
		t.Fatalf("adopted PR %d for a stale head sha", got.PRNumber)
	}
}

func TestArchivePreviewLookup_ForkAndOtherHeadAreIgnored(t *testing.T) {
	t.Parallel()
	for name, mod := range map[string]func(map[string]any){
		"fork":       func(r map[string]any) { r["isCrossRepository"] = true },
		"other head": func(r map[string]any) { r["headRefName"] = "someone-else" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newPREnv(t)
			e.squashMerged()
			e.listPR("MERGED", e.head(), mod)
			wantMerged(t, e.preview(), "unmerged", "")
			if got := e.task_(); got.PRNumber != 0 {
				t.Fatalf("adopted PR %d", got.PRNumber)
			}
		})
	}
}

func TestArchivePreviewLookup_OpenPRIsAdopted(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "work")
	e.listPR("OPEN", e.head())

	wantMerged(t, e.preview(), "unmerged", "")
	if got := e.task_(); got.PRNumber != 7 {
		t.Fatalf("identity = %+v, want the open PR adopted", got)
	}
}

func TestArchivePreviewLookup_ForgeFailuresLeaveTodaysAnswer(t *testing.T) {
	t.Parallel()
	for name, listErr := range map[string]error{
		"rate limited":    errors.New("HTTP 403: API rate limit exceeded"),
		"unauthenticated": errors.New("gh auth login required: not logged in to any GitHub hosts"),
		"other":           errors.New("boom"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newPREnv(t)
			e.squashMerged()
			e.gh.mu.Lock()
			e.gh.listErr = listErr
			e.gh.mu.Unlock()

			wantMerged(t, e.preview(), "unmerged", "")
			if got := e.task_(); got.PRNumber != 0 || got.ArchivedAt != nil {
				t.Fatalf("task = %+v", got)
			}
		})
	}
	t.Run("repo cannot be resolved", func(t *testing.T) {
		t.Parallel()
		e := newPREnv(t)
		e.squashMerged()
		e.gh.mu.Lock()
		e.gh.repoErr = errors.New("no git remotes found")
		e.gh.mu.Unlock()
		wantMerged(t, e.preview(), "unmerged", "")
	})
}

func TestArchivePreviewLookup_RunScopedCallerMakesNoForgeCall(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.squashMerged()
	e.listPR("MERGED", e.head())

	res, err := e.callAs(&requestContext{runScoped: true}, "task.archivePreview", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatal(err)
	}
	wantMerged(t, asMap(t, res), "unmerged", "")
	if n := e.gh.calls(); n != 0 {
		t.Fatalf("a run-scoped preview made %d gh calls", n)
	}
	if got := e.task_(); got.PRNumber != 0 {
		t.Fatalf("a run-scoped preview adopted PR %d", got.PRNumber)
	}
}

func TestArchivePreviewLookup_StoredPRAndAncestorMergeCostNoExtraCall(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.squashMerged()
	e.seedPR(7, e.branch, nil)
	before := e.gh.calls()
	e.preview()
	if n := e.gh.calls(); n != before {
		t.Fatalf("a task with a stored PR made %d gh calls", n-before)
	}

	f := newPREnv(t)
	f.commit("a.txt", "a\n", "work")
	f.merge()
	wantMerged(t, f.preview(), "merged", "ancestor")
	if n := f.gh.calls(); n != 0 {
		t.Fatalf("a branch merged by ancestry made %d gh calls", n)
	}
}

func TestArchivePreviewLookup_SecondPreviewInTheFloorMakesNoSecondCall(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.squashMerged() // the stub lists no PR: nothing to adopt, so the floor is what limits calls
	e.preview()
	first := e.gh.plainCalls("pr")
	if first != 1 {
		t.Fatalf("first preview made %d pr list calls, want 1", first)
	}
	for i := 0; i < 3; i++ {
		e.preview()
	}
	if n := e.gh.plainCalls("pr"); n != first {
		t.Fatalf("previews inside the %v floor made %d pr list calls, want %d", 5*time.Second, n, first)
	}
}
