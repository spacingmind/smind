//go:build !windows

package wsapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// Tests for task.archivePreview, task.archive {deleteBranch, force},
// task.restore and task.list {archived} (ADR-0039 section 9). They reuse
// prEnv: an origin served from a local bare repository and a stub gh -- no
// real forge is ever reached.

func (e *prEnv) localBranchExists(branch string) bool {
	e.t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = e.repo
	return cmd.Run() == nil
}

// merge lands the task's committed work in origin/develop with a merge commit.
func (e *prEnv) merge() {
	e.t.Helper()
	runGitT(e.t, e.repo, "merge", "--no-ff", "-m", "merge", e.branch)
	runGitT(e.t, e.repo, "push", "origin", "develop")
}

func (e *prEnv) task_() store.Task {
	e.t.Helper()
	got, err := e.wm.GetTask(e.task.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	return got
}

func (e *prEnv) archive(extra map[string]any) (any, error) {
	e.t.Helper()
	p := map[string]any{"taskId": e.task.ID}
	for k, v := range extra {
		p[k] = v
	}
	return e.call("task.archive", p)
}

func dirExistsT(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func mergeMaps(a, b map[string]any) map[string]any {
	for k, v := range b {
		a[k] = v
	}
	return a
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTaskArchivePreview_RPC(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "work")
	e.merge()
	if err := os.WriteFile(filepath.Join(e.wt, "a.txt"), []byte("a\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	callsBefore := e.gh.calls()

	res, err := e.call("task.archivePreview", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(t, res)
	for _, k := range []string{"branch", "merged", "base", "risks", "ignoredFilesLost", "hasTeardown", "canDeleteBranch", "deleteBranchDefault", "deleteBlockedReason"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("preview has no %q: %v", k, m)
		}
	}
	if b := m["branch"].(map[string]any); b["name"] != e.branch || b["owned"] != true {
		t.Fatalf("branch = %v", b)
	}
	if mg := m["merged"].(map[string]any); mg["state"] != "merged" || mg["via"] != "ancestor" {
		t.Fatalf("merged = %v", mg)
	}
	if m["base"].(map[string]any)["ref"] != "develop" {
		t.Fatalf("base = %v", m["base"])
	}
	risks := m["risks"].(map[string]any)
	if u := risks["uncommitted"].(map[string]any); u["files"] != float64(1) || u["added"] != float64(1) || u["deleted"] != float64(0) {
		t.Fatalf("uncommitted = %v", u)
	}
	if risks["published"] != false || risks["commitsAheadOfBase"] != float64(0) {
		t.Fatalf("risks = %v", risks)
	}
	if m["canDeleteBranch"] != false || m["deleteBranchDefault"] != false || m["ignoredFilesLost"] != true {
		t.Fatalf("preview = %v, want not deletable (dirty)", m)
	}
	if m["deleteBlockedReason"] != forge.NewPRError(forge.CodeBranchDirty, "").Message {
		t.Fatalf("reason = %v", m["deleteBlockedReason"])
	}
	if n := e.gh.calls(); n != callsBefore {
		t.Fatalf("the preview made %d forge calls", n-callsBefore)
	}
	if e.task_().ArchivedAt != nil {
		t.Fatal("the preview archived the task")
	}
}

func TestTaskArchive_TypedErrors(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "work")

	// Unmerged.
	_, err := e.archive(map[string]any{"deleteBranch": true})
	r := wantErrCode(t, err, forge.CodeBranchUnmerged)
	if r.Message != "The branch is not merged anywhere yet." {
		t.Fatalf("message = %q, want the bare sentence", r.Message)
	}
	if got := e.task_(); got.ArchivedAt != nil || !dirExistsT(e.wt) {
		t.Fatalf("task = %+v, want active with its worktree", got)
	}

	// Merged but dirty.
	e.merge()
	if err := os.WriteFile(filepath.Join(e.wt, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = e.archive(map[string]any{"deleteBranch": true})
	wantErrCode(t, err, forge.CodeBranchDirty)
	if _, serr := os.Stat(filepath.Join(e.wt, "wip.txt")); serr != nil {
		t.Fatalf("a refused archive touched the worktree: %v", serr)
	}

	// Checked out in another worktree (merged, then made clean).
	if err := os.Remove(filepath.Join(e.wt, "wip.txt")); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	runGitT(t, e.repo, "worktree", "add", "--force", other, e.branch)
	_, err = e.archive(map[string]any{"deleteBranch": true})
	wantErrCode(t, err, forge.CodeBranchCheckedOut)
	if !e.localBranchExists(e.branch) || e.task_().ArchivedAt != nil {
		t.Fatal("a refused archive changed something")
	}
}

func TestTaskArchive_TypedErrors_UserBranch(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	runGitT(t, e.repo, "branch", "feature/mine")
	own, err := e.wm.CreateTask(e.task.WorkspaceID, nil, "mine", workspace.WithExistingBranch("feature/mine"))
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []map[string]any{{"deleteBranch": true}, {"deleteBranch": true, "force": true}} {
		raw, _ := json.Marshal(mergeMaps(map[string]any{"taskId": own.ID}, extra))
		_, err := e.api.hs["task.archive"](context.Background(), &requestContext{}, raw)
		wantErrCode(t, err, forge.CodeBranchNotOwned)
	}
	if got, _ := e.wm.GetTask(own.ID); got.ArchivedAt != nil {
		t.Fatal("the user-branch task was archived by a refused delete")
	}
	if !e.localBranchExists("feature/mine") {
		t.Fatal("the user's branch is gone")
	}
}

func TestTaskArchive_DeletesAndForces(t *testing.T) {
	t.Parallel()
	// Squash-merged: the PR snapshot is merged at the branch tip.
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "work")
	runGitT(t, e.repo, "merge", "--squash", e.branch)
	runGitT(t, e.repo, "commit", "-m", "squash")
	runGitT(t, e.repo, "push", "origin", "develop")
	e.seedPR(7, "feat", func(p *forge.PR) { p.State = forge.PRStateMerged; p.HeadSha = e.head() })

	res, err := e.archive(map[string]any{"deleteBranch": true})
	if err != nil {
		t.Fatal(err)
	}
	task := res.(store.Task)
	if task.ArchivedAt == nil || task.BranchDeletedAt == nil || e.localBranchExists(e.branch) {
		t.Fatalf("task = %+v, want the squash-merged branch deleted", task)
	}
	if e.remoteSha("develop") == "" {
		t.Fatal("origin lost develop")
	}

	// Force deletes an owned, unmerged branch.
	f := newPREnv(t)
	f.commit("a.txt", "a\n", "unmerged work")
	res, err = f.archive(map[string]any{"deleteBranch": true, "force": true})
	if err != nil {
		t.Fatal(err)
	}
	if task := res.(store.Task); task.BranchDeletedAt == nil || f.localBranchExists(f.branch) {
		t.Fatalf("task = %+v, want the unmerged branch force-deleted", task)
	}
}

func TestTaskArchive_DeleteBranchRefusesRunScoped(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "work")
	e.merge()

	_, err := e.callAs(&requestContext{runScoped: true}, "task.archive", map[string]any{"taskId": e.task.ID, "deleteBranch": true})
	wantErrCode(t, err, codeForbidden)
	if e.task_().ArchivedAt != nil || !e.localBranchExists(e.branch) {
		t.Fatal("a refused run-scoped call changed something")
	}

	// Without deleteBranch an agent archives as before, and keeps the branch.
	if _, err := e.callAs(&requestContext{runScoped: true}, "task.archive", map[string]any{"id": e.task.ID}); err != nil {
		t.Fatal(err)
	}
	if got := e.task_(); got.ArchivedAt == nil || got.BranchDeletedAt != nil || !e.localBranchExists(e.branch) {
		t.Fatalf("task = %+v, want archived with its merged branch kept", got)
	}
}

func TestTaskArchiveFinished_NeverDeletesBranches(t *testing.T) {
	t.Parallel()
	f := newTAFixture(t)
	repo := ""
	if w, err := f.wm.GetWorkspace(f.wsID); err == nil {
		repo = w.Path
	}
	root := f.task("root", 0, "")
	child := f.task("child", root, "reply")
	ct, _ := f.wm.GetTask(child)
	// Make the child's branch merged and its worktree clean: the scenario
	// the dialog would offer to delete.
	f.finishDone(child, root)
	runGitT(t, *ct.WorktreePath, "add", "-A") // the scenario file and whatever the run left
	runGitT(t, *ct.WorktreePath, "commit", "-m", "child work")
	runGitT(t, repo, "merge", "--no-ff", "-m", "merge", *ct.Branch)
	pre, err := f.wm.ArchivePreview(child)
	if err != nil {
		t.Fatal(err)
	}
	if !pre.CanDeleteBranch {
		t.Fatalf("setup: the child's branch is not deletable (%q); the test would prove nothing", pre.DeleteBlockedReason)
	}

	res := f.archiveFinished(root, false)
	if len(res.Archived) != 1 || res.Archived[0].ID != child {
		t.Fatalf("archived = %+v, want the child", res.Archived)
	}
	got, _ := f.wm.GetTask(child)
	if got.BranchDeletedAt != nil {
		t.Fatalf("archiveFinished deleted the branch: %+v", got)
	}
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+*ct.Branch)
	cmd.Dir = repo
	if err := cmd.Run(); err != nil {
		t.Fatalf("the child's branch %q is gone: %v", *ct.Branch, err)
	}
}

func TestTaskRestore_RPC(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "work")

	// Not archived yet.
	_, err := e.call("task.restore", map[string]any{"taskId": e.task.ID})
	wantErrCode(t, err, forge.CodeNotArchived)

	if _, err := e.archive(nil); err != nil {
		t.Fatal(err)
	}
	// Agents may not restore.
	_, err = e.callAs(&requestContext{runScoped: true}, "task.restore", map[string]any{"taskId": e.task.ID})
	wantErrCode(t, err, codeForbidden)

	res, err := e.call("task.restore", map[string]any{"taskId": e.task.ID})
	if err != nil {
		t.Fatal(err)
	}
	got := res.(store.Task)
	if got.ArchivedAt != nil || got.WorktreePath == nil || !dirExistsT(*got.WorktreePath) {
		t.Fatalf("task = %+v, want active with its worktree back", got)
	}
	if b := strings.TrimSpace(runGitTOut(t, *got.WorktreePath, "rev-parse", "--abbrev-ref", "HEAD")); b != e.branch {
		t.Fatalf("restored on %q, want %q", b, e.branch)
	}

	// Branch gone: archive again with the (merged) branch deleted.
	e.merge()
	if _, err := e.archive(map[string]any{"deleteBranch": true}); err != nil {
		t.Fatal(err)
	}
	_, err = e.call("task.restore", map[string]any{"taskId": e.task.ID})
	r := wantErrCode(t, err, forge.CodeBranchGone)
	if !strings.Contains(r.Message, e.branch) || !strings.Contains(r.Message, "no longer exists") {
		t.Fatalf("message = %q", r.Message)
	}
	if e.task_().ArchivedAt == nil {
		t.Fatal("a failed restore un-archived the task")
	}
}

func TestTaskList_Archived(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	other, err := e.wm.CreateTask(e.task.WorkspaceID, nil, "other task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.archive(nil); err != nil {
		t.Fatal(err)
	}

	res, err := e.call("task.list", map[string]any{"workspaceId": e.task.WorkspaceID, "archived": true})
	if err != nil {
		t.Fatal(err)
	}
	got := res.([]taskView)
	if len(got) != 1 || got[0].ID != e.task.ID || got[0].ArchivedAt == nil {
		t.Fatalf("archived list = %+v, want only task %d", got, e.task.ID)
	}
	res, err = e.call("task.list", map[string]any{"workspaceId": e.task.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	live := res.([]taskView)
	if len(live) != 1 || live[0].ID != other.ID {
		t.Fatalf("live list = %+v, want only task %d", live, other.ID)
	}
	res, err = e.call("task.list", map[string]any{"workspaceId": e.task.WorkspaceID + 99, "archived": true})
	if err != nil || len(res.([]taskView)) != 0 {
		t.Fatalf("another workspace's archived list = %v, %v, want empty", res, err)
	}
}

func TestTaskRestore_EventShowsTaskAgain(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	e.commit("a.txt", "a\n", "work")
	srv := httptest.NewServer(e.api.Handler)
	t.Cleanup(srv.Close)
	second := newEventConn(t, dialWS(t, srv, "tok"))
	second.subscribe("sub", TopicTaskCreated, TopicTaskUpdated, TopicTaskArchived)

	if _, err := e.archive(nil); err != nil {
		t.Fatal(err)
	}
	if ev := second.nextEvent(5 * time.Second); ev.Topic != TopicTaskArchived {
		t.Fatalf("event = %q, want %q", ev.Topic, TopicTaskArchived)
	}
	if _, err := e.call("task.restore", map[string]any{"taskId": e.task.ID}); err != nil {
		t.Fatal(err)
	}

	// task.updated with an active row is what the tree upserts from; a
	// restored task is not new, so no task.created fires.
	ev := second.nextEvent(5 * time.Second)
	if ev.Topic != TopicTaskUpdated {
		t.Fatalf("event = %q, want %q", ev.Topic, TopicTaskUpdated)
	}
	var p taskUpdatedPayload
	decodePayload(t, ev, &p)
	if p.Task.ID != e.task.ID || p.Task.ArchivedAt != nil || p.Task.Status != "created" {
		t.Fatalf("payload task = %+v, want the restored, active task", p.Task)
	}
	second.expectNoEvent(300 * time.Millisecond)
}
