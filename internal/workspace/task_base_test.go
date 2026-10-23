package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// commitTreeOn writes a commit (no checkout) with parent on top of the
// parent's tree and returns its sha; parent "" makes an unrelated root.
func commitTreeOn(t *testing.T, repo, parent string) string {
	t.Helper()
	args := []string{"commit-tree"}
	if parent == "" {
		args = append(args, emptyTreeSHA)
	} else {
		args = append(args, parent+"^{tree}", "-p", parent)
	}
	args = append(args, "-m", "synthetic")
	return strings.TrimSpace(runGitOutputT(t, repo, args...))
}

func getTask(t *testing.T, m *Manager, id int64) store.Task {
	t.Helper()
	task, err := m.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask(%d) error = %v", id, err)
	}
	return task
}

func TestCreateTask_StoresBase_DefaultMode(t *testing.T) {
	m, wsID, _, _, baseSHA, headSHA := newTaskRefEnv(t)
	task, err := m.CreateTask(wsID, nil, "Default base")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	got := getTask(t, m, task.ID)
	// HEAD is behind origin/develop, so the fork point is HEAD itself.
	if got.BaseRef != "develop" || got.BaseSha != headSHA {
		t.Fatalf("base = %q/%s, want develop/%s (origin/develop is %s)", got.BaseRef, got.BaseSha, headSHA, baseSHA)
	}
	if !got.BranchOwned {
		t.Fatal("BranchOwned = false, want true for a created branch")
	}
}

// A workspace checkout ahead of origin/<base> with unpushed commits must not
// leak them into the task's diff base: base_sha is the fork commit (HEAD),
// not merge-base(HEAD, origin/<base>).
func TestCreateTask_StoresBase_DefaultMode_CheckoutAheadOfBase(t *testing.T) {
	m, wsID, repo, _, _, _ := newTaskRefEnv(t)
	writeAndCommit(t, repo, "unpushed.txt", "unrelated\n", "unpushed work")
	ahead := worktreeHead(t, repo)
	task, err := m.CreateTask(wsID, nil, "Ahead")
	if err != nil {
		t.Fatal(err)
	}
	if got := getTask(t, m, task.ID); got.BaseRef != "develop" || got.BaseSha != ahead {
		t.Fatalf("base = %q/%s, want develop/%s (the fork commit)", got.BaseRef, got.BaseSha, ahead)
	}
}

func TestCreateTask_StoresBase_BaseRefModeNonBranchFallsBack(t *testing.T) {
	m, wsID, _, _, _, headSHA := newTaskRefEnv(t)
	task, err := m.CreateTask(wsID, nil, "From a sha", WithBaseRef(headSHA))
	if err != nil {
		t.Fatal(err)
	}
	if got := getTask(t, m, task.ID); got.BaseRef != "develop" || got.BaseSha != headSHA {
		t.Fatalf("base = %q/%s, want develop/%s", got.BaseRef, got.BaseSha, headSHA)
	}
}

func TestCreateTask_StoresBase_DefaultMode_NoOrigin(t *testing.T) {
	repo := newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "main")
	m := newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := m.CreateTask(w.ID, nil, "No origin")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	got := getTask(t, m, task.ID)
	if got.BaseRef != "main" || got.BaseSha != worktreeHead(t, repo) {
		t.Fatalf("base = %q/%s, want main/%s", got.BaseRef, got.BaseSha, worktreeHead(t, repo))
	}
}

func TestCreateTask_StoresBase_BaseRefMode(t *testing.T) {
	m, wsID, _, _, baseSHA, _ := newTaskRefEnv(t)
	task, err := m.CreateTask(wsID, nil, "From origin develop", WithBaseRef("origin/develop"))
	if err != nil {
		t.Fatal(err)
	}
	got := getTask(t, m, task.ID)
	if got.BaseRef != "develop" || got.BaseSha != baseSHA {
		t.Fatalf("base = %q/%s, want develop/%s", got.BaseRef, got.BaseSha, baseSHA)
	}
	if !got.BranchOwned {
		t.Fatal("BranchOwned = false, want true")
	}
}

func TestCreateTask_StoresBase_ExistingBranchMode(t *testing.T) {
	m, wsID, repo, _, _, headSHA := newTaskRefEnv(t)
	runGitT(t, repo, "branch", "feat/x", headSHA)
	task, err := m.CreateTask(wsID, nil, "Existing", WithExistingBranch("feat/x"))
	if err != nil {
		t.Fatal(err)
	}
	got := getTask(t, m, task.ID)
	if got.BaseRef != "develop" || got.BaseSha != headSHA {
		t.Fatalf("base = %q/%s, want develop/%s", got.BaseRef, got.BaseSha, headSHA)
	}
	if got.BranchOwned {
		t.Fatal("BranchOwned = true, want false for a user's existing branch")
	}

	// A branch with no history in common with the base keeps an empty sha
	// and still creates the task.
	runGitT(t, repo, "branch", "unrelated", commitTreeOn(t, repo, ""))
	task, err = m.CreateTask(wsID, nil, "Unrelated", WithExistingBranch("unrelated"))
	if err != nil {
		t.Fatalf("CreateTask(unrelated) error = %v", err)
	}
	if got := getTask(t, m, task.ID); got.BaseSha != "" || got.BaseRef != "develop" {
		t.Fatalf("unrelated base = %q/%q, want develop/empty", got.BaseRef, got.BaseSha)
	}
}

func TestCreateTask_StoresBase_PullRequestMode(t *testing.T) {
	m, wsID, repo, bare, _, headSHA := newTaskRefEnv(t)
	prSHA := commitTreeOn(t, repo, headSHA)
	runGitPushRef(t, repo, bare, prSHA, "refs/pull/9/head")

	task, err := m.CreateTask(wsID, nil, "From PR nine", WithPullRequest(9))
	if err != nil {
		t.Fatal(err)
	}
	got := getTask(t, m, task.ID)
	if got.BaseRef != "develop" || got.BaseSha != headSHA {
		t.Fatalf("base = %q/%s, want develop/%s (the PR head %s forked from it)", got.BaseRef, got.BaseSha, headSHA, prSHA)
	}
	if !got.BranchOwned {
		t.Fatal("BranchOwned = false, want true for the smind/pr-<n>-<slug> branch")
	}
}

func TestCreateTask_BranchOwned_PerMode(t *testing.T) {
	m, wsID, repo, bare, _, headSHA := newTaskRefEnv(t)
	runGitT(t, repo, "branch", "feat/own", headSHA)
	runGitPushRef(t, repo, bare, headSHA, "refs/pull/3/head")
	cases := []struct {
		name  string
		opts  []CreateTaskOption
		owned bool
	}{
		{"default", nil, true},
		{"baseRef", []CreateTaskOption{WithBaseRef("origin/develop")}, true},
		{"existing branch", []CreateTaskOption{WithExistingBranch("feat/own")}, false},
		{"pull request", []CreateTaskOption{WithPullRequest(3)}, true},
	}
	for _, c := range cases {
		task, err := m.CreateTask(wsID, nil, c.name, c.opts...)
		if err != nil {
			t.Fatalf("%s: CreateTask() error = %v", c.name, err)
		}
		if got := getTask(t, m, task.ID).BranchOwned; got != c.owned {
			t.Fatalf("%s: BranchOwned = %v, want %v", c.name, got, c.owned)
		}
	}
}

func TestCreateTask_FailureLeavesNoRow(t *testing.T) {
	m, wsID, _, _, _, _ := newTaskRefEnv(t)
	before := snapshotTaskRefState(t, m, wsID)
	for _, opts := range [][]CreateTaskOption{
		{WithBaseRef("origin/nope")},
		{WithExistingBranch("no-such-branch")},
		{WithPullRequest(404)},
	} {
		if _, err := m.CreateTask(wsID, nil, "Fails", opts...); err == nil {
			t.Fatal("CreateTask() error = nil, want failure")
		}
	}
	assertNoTaskLeftBehind(t, m, wsID, before)
}

func TestTaskDiff_UsesStoredBaseSha_ReflogExpired(t *testing.T) {
	m, wsID, repo, _, _, _ := newTaskRefEnv(t)
	task, err := m.CreateTask(wsID, nil, "Reflog gone")
	if err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, *task.WorktreePath, "work.txt", "work\n", "work")
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "untracked.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := m.Diff(task.ID)
	if err != nil || !strings.Contains(before, "work.txt") || !strings.Contains(before, "untracked.txt") {
		t.Fatalf("Diff() = %q, %v; want both files", before, err)
	}

	runGitT(t, repo, "reflog", "expire", "--expire=now", "--all")
	_ = os.RemoveAll(filepath.Join(repo, ".git", "logs"))
	if out := runGitOutputT(t, repo, "reflog", "show", "--all"); strings.TrimSpace(out) != "" {
		t.Fatalf("reflog still has entries: %q", out)
	}

	after, err := m.Diff(task.ID)
	if err != nil {
		t.Fatalf("Diff() after reflog expiry error = %v", err)
	}
	if after != before {
		t.Fatalf("diff changed after the reflog expired:\n%s\n---\n%s", before, after)
	}
	files, err := m.TaskFiles(task.ID)
	if err != nil || len(files) != 2 {
		t.Fatalf("TaskFiles() = %v, %v; want 2 files", files, err)
	}
}

func TestTaskDiff_BaseUnknown_TypedError(t *testing.T) {
	m, wsID, _, _, _, _ := newTaskRefEnv(t)
	task, err := m.CreateTask(wsID, nil, "No base")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetTaskBase(task.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	checks := map[string]func() error{
		"Diff":         func() error { _, err := m.Diff(task.ID); return err },
		"TaskFiles":    func() error { _, err := m.TaskFiles(task.ID); return err },
		"TaskFileDiff": func() error { _, err := m.TaskFileDiff(task.ID, "x"); return err },
	}
	for name, f := range checks {
		err := f()
		if !errors.Is(err, ErrBaseUnknown) {
			t.Fatalf("%s error = %v, want ErrBaseUnknown", name, err)
		}
		if !strings.Contains(err.Error(), "task.setBase") {
			t.Fatalf("%s error = %q, want the task.setBase sentence", name, err)
		}
	}
}

// TestNoReflogReaders: the reflog is read in exactly one place, the one-time
// startup backfill (ADR-0039 section 2).
func TestNoReflogReaders(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		hasLogG := strings.Contains(src, `"log", "-g"`) || strings.Contains(src, `"reflog"`)
		if hasLogG && f != "base_backfill.go" {
			t.Errorf("%s reads the reflog; only base_backfill.go may", f)
		}
		if strings.Contains(src, "legacyReflogBase(") && f != "base_backfill.go" {
			t.Errorf("%s calls legacyReflogBase; only BackfillTaskBases may", f)
		}
		if strings.Contains(src, "taskDiffBase") && !strings.Contains(src, "// taskDiffBase") {
			t.Errorf("%s still references taskDiffBase", f)
		}
	}
}

// newDetectRepo is a repo whose only branch is name, optionally with an
// origin that has it.
func newDetectRepo(t *testing.T, name string, withOrigin bool) string {
	t.Helper()
	repo := newTestRepo(t)
	runGitT(t, repo, "branch", "-M", name)
	if withOrigin {
		runGitT(t, repo, "remote", "add", "origin", newBareRepo(t))
		runGitT(t, repo, "push", "origin", name)
	}
	return repo
}

func TestDefaultBase_DetectionOrder(t *testing.T) {
	t.Run("origin/HEAD wins over develop", func(t *testing.T) {
		repo := newDetectRepo(t, "main", true)
		runGitT(t, repo, "branch", "develop")
		runGitT(t, repo, "remote", "set-head", "origin", "main")
		if got := detectDefaultBase(repo); got != "main" {
			t.Fatalf("detect = %q, want main", got)
		}
	})
	t.Run("develop over main", func(t *testing.T) {
		repo := newDetectRepo(t, "main", false)
		runGitT(t, repo, "branch", "develop")
		if got := detectDefaultBase(repo); got != "develop" {
			t.Fatalf("detect = %q, want develop", got)
		}
	})
	t.Run("develop only on origin", func(t *testing.T) {
		repo := newDetectRepo(t, "develop", true)
		runGitT(t, repo, "checkout", "-b", "work")
		runGitT(t, repo, "branch", "-D", "develop")
		if got := detectDefaultBase(repo); got != "develop" {
			t.Fatalf("detect = %q, want develop (origin/develop exists)", got)
		}
	})
	t.Run("main over master", func(t *testing.T) {
		repo := newDetectRepo(t, "master", false)
		runGitT(t, repo, "branch", "main")
		if got := detectDefaultBase(repo); got != "main" {
			t.Fatalf("detect = %q, want main", got)
		}
	})
	t.Run("master", func(t *testing.T) {
		if got := detectDefaultBase(newDetectRepo(t, "master", false)); got != "master" {
			t.Fatalf("detect = %q, want master", got)
		}
	})
	t.Run("current branch", func(t *testing.T) {
		if got := detectDefaultBase(newDetectRepo(t, "trunk", false)); got != "trunk" {
			t.Fatalf("detect = %q, want trunk", got)
		}
	})
}

func TestDefaultBase_MainOnlyRepo(t *testing.T) {
	repo := newDetectRepo(t, "main", true)
	m := newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	ref, detected, err := m.DefaultBase(w.ID)
	if err != nil || ref != "main" || !detected {
		t.Fatalf("DefaultBase() = %q, %v, %v; want main, true", ref, detected, err)
	}
	task, err := m.CreateTask(w.ID, nil, "On main")
	if err != nil {
		t.Fatal(err)
	}
	got := getTask(t, m, task.ID)
	if got.BaseRef != "main" || got.BaseSha != worktreeHead(t, repo) {
		t.Fatalf("base = %q/%s, want main/%s", got.BaseRef, got.BaseSha, worktreeHead(t, repo))
	}
}

func TestWorkspaceSetDefaultBase_ValidatesRef(t *testing.T) {
	repo := newDetectRepo(t, "main", true)
	runGitT(t, repo, "branch", "release")
	runGitT(t, repo, "push", "origin", "release:refs/heads/remote-only")
	runGitT(t, repo, "fetch", "origin")
	m := newTestManager(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"-x", "a..b", "a b", "no-such-branch"} {
		if _, err := m.SetDefaultBase(w.ID, bad); err == nil {
			t.Fatalf("SetDefaultBase(%q) error = nil, want rejection", bad)
		}
	}
	if ref, detected, _ := m.DefaultBase(w.ID); ref != "main" || !detected {
		t.Fatalf("a rejected override changed the default: %q, %v", ref, detected)
	}

	for _, ok := range []string{"release", "remote-only"} {
		if _, err := m.SetDefaultBase(w.ID, ok); err != nil {
			t.Fatalf("SetDefaultBase(%q) error = %v", ok, err)
		}
		if ref, detected, _ := m.DefaultBase(w.ID); ref != ok || detected {
			t.Fatalf("DefaultBase() = %q, %v; want %s, false (override)", ref, detected, ok)
		}
	}
	if _, err := m.SetDefaultBase(w.ID, ""); err != nil {
		t.Fatal(err)
	}
	if ref, detected, _ := m.DefaultBase(w.ID); ref != "main" || !detected {
		t.Fatalf("after clearing: %q, %v; want main, true", ref, detected)
	}
}

func TestTaskSetBase_RecomputesSha(t *testing.T) {
	m, wsID, repo, _, _, headSHA := newTaskRefEnv(t)
	task, err := m.CreateTask(wsID, nil, "Rebase me")
	if err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, *task.WorktreePath, "w.txt", "w\n", "w")
	tip := worktreeHead(t, *task.WorktreePath)
	runGitT(t, repo, "branch", "child", tip)

	updated, err := m.SetTaskBase(task.ID, "child")
	if err != nil {
		t.Fatalf("SetTaskBase() error = %v", err)
	}
	if updated.BaseRef != "child" || updated.BaseSha != tip {
		t.Fatalf("base = %q/%s, want child/%s", updated.BaseRef, updated.BaseSha, tip)
	}
	if got := getTask(t, m, task.ID); got.BaseSha != tip {
		t.Fatalf("not persisted: %+v", got)
	}

	updated, err = m.SetTaskBase(task.ID, "origin/develop")
	if err != nil || updated.BaseRef != "develop" || updated.BaseSha != headSHA {
		t.Fatalf("SetTaskBase(origin/develop) = %+v, %v; want develop/%s", updated, err, headSHA)
	}

	runGitT(t, repo, "branch", "unrelated", commitTreeOn(t, repo, ""))
	if _, err := m.SetTaskBase(task.ID, "unrelated"); !errors.Is(err, ErrBaseNotFound) {
		t.Fatalf("SetTaskBase(unrelated) error = %v, want ErrBaseNotFound", err)
	}
	if _, err := m.SetTaskBase(task.ID, "missing"); !errors.Is(err, ErrBaseNotFound) {
		t.Fatalf("SetTaskBase(missing) error = %v, want ErrBaseNotFound", err)
	}
	if _, err := m.SetTaskBase(task.ID, "-x"); err == nil {
		t.Fatal("SetTaskBase(-x) error = nil")
	}
	if got := getTask(t, m, task.ID); got.BaseRef != "develop" || got.BaseSha != headSHA {
		t.Fatalf("failed SetTaskBase changed the base: %+v", got)
	}
}

// --- backfill (the former migration tests; git is needed, so they live here) ---

// wipeBase puts a task back in its pre-ADR-0039 state.
func wipeBase(t *testing.T, m *Manager, id int64) {
	t.Helper()
	if err := m.store.SetTaskBase(id, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetTaskBranchOwned(id, false); err != nil {
		t.Fatal(err)
	}
}

func TestMigrate_BackfillsBaseSha_FromReflogOnce(t *testing.T) {
	m, wsID, _, _, _, headSHA := newTaskRefEnv(t)
	task, err := m.CreateTask(wsID, nil, "Legacy")
	if err != nil {
		t.Fatal(err)
	}
	wipeBase(t, m, task.ID)
	writeAndCommit(t, *task.WorktreePath, "w.txt", "w\n", "w") // the branch moves on

	if n := m.BackfillTaskBases(); n != 1 {
		t.Fatalf("BackfillTaskBases() = %d, want 1", n)
	}
	got := getTask(t, m, task.ID)
	if got.BaseSha != headSHA || got.BaseRef != "develop" || !got.BranchOwned {
		t.Fatalf("after backfill: %q/%s owned=%v, want develop/%s owned", got.BaseRef, got.BaseSha, got.BranchOwned, headSHA)
	}

	// Once: a second run changes nothing, even if setBase moved the base.
	if err := m.store.SetTaskBase(task.ID, "develop", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if n := m.BackfillTaskBases(); n != 0 {
		t.Fatalf("second BackfillTaskBases() = %d, want 0", n)
	}
	if got := getTask(t, m, task.ID); got.BaseSha != strings.Repeat("a", 40) {
		t.Fatalf("second backfill overwrote the base: %+v", got)
	}
}

func TestMigrate_BackfillBranchOwned_ByPrefixOnly(t *testing.T) {
	m, wsID, repo, _, _, headSHA := newTaskRefEnv(t)
	cases := map[string]bool{
		"feat/x":           false,
		"smind/task-mine":  true, // the one heuristic: smind's own prefixes
		"smind/pr-5-thing": true,
		"smind-other/x":    false,
		"mysmind/task-x":   false,
	}
	ids := map[string]int64{}
	for branch := range cases {
		runGitT(t, repo, "branch", branch, headSHA)
		task, err := m.CreateTask(wsID, nil, "t "+branch, WithExistingBranch(branch))
		if err != nil {
			t.Fatalf("CreateTask(%s) error = %v", branch, err)
		}
		wipeBase(t, m, task.ID)
		ids[branch] = task.ID
	}
	if n := m.BackfillTaskBases(); n != len(cases) {
		t.Fatalf("BackfillTaskBases() = %d, want %d", n, len(cases))
	}
	for branch, want := range cases {
		if got := getTask(t, m, ids[branch]); got.BranchOwned != want || got.BaseSha == "" {
			t.Fatalf("%s: owned=%v sha=%q, want owned=%v and a sha", branch, got.BranchOwned, got.BaseSha, want)
		}
	}
}

func TestMigrate_UnrecoverableBase_LeavesEmpty(t *testing.T) {
	m, wsID, repo, _, _, _ := newTaskRefEnv(t)
	task, err := m.CreateTask(wsID, nil, "Gone")
	if err != nil {
		t.Fatal(err)
	}
	wipeBase(t, m, task.ID)
	// Worktree and branch (hence its reflog) are gone.
	runGitT(t, repo, "worktree", "remove", "--force", *task.WorktreePath)
	runGitT(t, repo, "branch", "-D", *task.Branch)

	if n := m.BackfillTaskBases(); n != 0 {
		t.Fatalf("BackfillTaskBases() = %d, want 0", n)
	}
	got := getTask(t, m, task.ID)
	if got.BaseSha != "" || got.BaseRef != "" || got.BranchOwned {
		t.Fatalf("unrecoverable task was touched: %+v", got)
	}
}
