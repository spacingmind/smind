//go:build !windows

package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// fakePRProvider answers Provider.PR with a canned PR or error and counts
// calls; every other Provider method is the nil embedded interface (never
// called by CreateTask).
type fakePRProvider struct {
	forge.Provider
	pr    forge.PR
	err   error
	calls atomic.Int32
}

func (f *fakePRProvider) PR(_ context.Context, _ string, _ int) (forge.PR, error) {
	f.calls.Add(1)
	return f.pr, f.err
}

// newPRTaskEnv is newTaskRefEnv plus a real PR head at refs/pull/7/head on
// origin and a workspace setup script that drops setup.marker into the
// worktree.
func newPRTaskEnv(t *testing.T) (m *Manager, wsID int64) {
	t.Helper()
	m, wsID, repo, bare, _, _ := newTaskRefEnv(t)
	prSHA := strings.TrimSpace(runGitOutputT(t, repo, "rev-parse", "HEAD"))
	runGitPushRef(t, repo, bare, prSHA, "refs/pull/7/head")
	setup, teardown := `echo ran > setup.marker`, `echo ran > "$SMIND_SOURCE_CHECKOUT_PATH/teardown.marker"`
	if _, err := m.store.SetWorkspaceScripts(wsID, &setup, &teardown); err != nil {
		t.Fatal(err)
	}
	return m, wsID
}

func sameRepoPR() forge.PR {
	return forge.PR{
		Number: 7, Repo: "o/r", URL: "https://github.com/o/r/pull/7", State: forge.PRStateOpen,
		HeadRef: "feat/x", BaseRef: "develop",
	}
}

func useProvider(m *Manager, p forge.Provider) {
	m.SetForge(func() forge.Provider { return p })
}

func TestCreateTask_SameRepoPR_RunsSetupAndStoresIdentity(t *testing.T) {
	m, wsID := newPRTaskEnv(t)
	prov := &fakePRProvider{pr: sameRepoPR()}
	useProvider(m, prov)

	task, err := m.CreateTask(wsID, nil, "Same repo PR", WithPullRequest(7))
	if err != nil {
		t.Fatal(err)
	}
	if task.UntrustedSource {
		t.Fatal("same-repo PR task is untrusted")
	}
	if task.PRForge != "github" || task.PRRepo != "o/r" || task.PRNumber != 7 ||
		task.PRURL != "https://github.com/o/r/pull/7" || task.PRHeadRef != "feat/x" {
		t.Fatalf("PR identity = %q %q %d %q %q", task.PRForge, task.PRRepo, task.PRNumber, task.PRURL, task.PRHeadRef)
	}
	if task.SetupStatus != SetupRunning {
		t.Fatalf("setup_status = %q, want running", task.SetupStatus)
	}
	waitSetup(t, m, task.ID, SetupOK)
	if _, err := os.Stat(filepath.Join(*task.WorktreePath, "setup.marker")); err != nil {
		t.Fatalf("setup did not run: %v", err)
	}
	if n := prov.calls.Load(); n != 1 {
		t.Fatalf("Provider.PR calls = %d, want exactly 1", n)
	}
}

func TestCreateTask_PR_BaseRefFromPR(t *testing.T) {
	m, wsID, repo, bare, _, _ := newTaskRefEnv(t)
	prSHA := strings.TrimSpace(runGitOutputT(t, repo, "rev-parse", "HEAD"))
	runGitPushRef(t, repo, bare, prSHA, "refs/pull/7/head")
	// The PR targets release/1, a branch the checkout has already fetched.
	runGitT(t, repo, "push", "origin", "HEAD:refs/heads/release/1")
	runGitT(t, repo, "fetch", "origin")
	pr := sameRepoPR()
	pr.BaseRef = "release/1"
	useProvider(m, &fakePRProvider{pr: pr})

	task, err := m.CreateTask(wsID, nil, "Targets release", WithPullRequest(7))
	if err != nil {
		t.Fatal(err)
	}
	if task.BaseRef != "release/1" {
		t.Fatalf("base_ref = %q, want release/1 (the PR's baseRefName)", task.BaseRef)
	}
	if task.BaseSha == "" {
		t.Fatal("base_sha empty, want the merge base with origin/release/1")
	}
}

func TestCreateTask_PR_BaseRefFetchedWhenMissing(t *testing.T) {
	m, wsID, repo, bare, _, _ := newTaskRefEnv(t)
	prSHA := strings.TrimSpace(runGitOutputT(t, repo, "rev-parse", "HEAD"))
	runGitPushRef(t, repo, bare, prSHA, "refs/pull/7/head")
	// release/2 exists only on the bare origin; the checkout never fetched it.
	runGitPushRef(t, repo, bare, prSHA, "refs/heads/release/2")
	pr := sameRepoPR()
	pr.BaseRef = "release/2"
	useProvider(m, &fakePRProvider{pr: pr})

	task, err := m.CreateTask(wsID, nil, "Targets unfetched", WithPullRequest(7))
	if err != nil {
		t.Fatal(err)
	}
	if task.BaseRef != "release/2" || task.BaseSha == "" {
		t.Fatalf("base = %q @ %q, want release/2 with a resolved merge base", task.BaseRef, task.BaseSha)
	}
}

func TestCreateTask_PR_InvalidBaseFallsBackToDefault(t *testing.T) {
	m, wsID := newPRTaskEnv(t)
	pr := sameRepoPR()
	pr.BaseRef = "--upload-pack=evil"
	useProvider(m, &fakePRProvider{pr: pr})

	task, err := m.CreateTask(wsID, nil, "Bad base", WithPullRequest(7))
	if err != nil {
		t.Fatal(err)
	}
	if task.BaseRef != "develop" {
		t.Fatalf("base_ref = %q, want the workspace default base develop", task.BaseRef)
	}
}

func TestCreateTask_ForkPR_Untrusted_SetupBlocked(t *testing.T) {
	m, wsID := newPRTaskEnv(t)
	pr := sameRepoPR()
	pr.FromFork = true
	useProvider(m, &fakePRProvider{pr: pr})

	task, err := m.CreateTask(wsID, nil, "Fork PR", WithPullRequest(7))
	if err != nil {
		t.Fatal(err)
	}
	if !task.UntrustedSource || task.SetupStatus != SetupBlocked {
		t.Fatalf("untrusted=%v setup_status=%q, want true/blocked", task.UntrustedSource, task.SetupStatus)
	}
	if task.PRNumber != 7 || task.PRHeadRef != "feat/x" {
		t.Fatalf("fork PR identity = %d %q, want stored too", task.PRNumber, task.PRHeadRef)
	}
	time.Sleep(300 * time.Millisecond) // a wrongly started script would have written its marker by now
	if _, err := os.Stat(filepath.Join(*task.WorktreePath, "setup.marker")); err == nil {
		t.Fatal("setup script ran for a fork PR")
	}
	got, _ := m.GetTask(task.ID)
	if got.SetupStatus != SetupBlocked {
		t.Fatalf("stored setup_status = %q, want blocked", got.SetupStatus)
	}
}

func TestCreateTask_PRForgeUnavailable_FailsClosed(t *testing.T) {
	for name, wire := range map[string]func(*Manager){
		"no provider wired": func(*Manager) {},
		"nil provider":      func(m *Manager) { m.SetForge(func() forge.Provider { return nil }) },
		"unauthenticated": func(m *Manager) {
			useProvider(m, &fakePRProvider{err: forge.NewError(forge.CodeUnauthenticated, "not signed in")})
		},
		"cli missing": func(m *Manager) {
			useProvider(m, &fakePRProvider{err: forge.NewError(forge.CodeCLIMissing, "no gh")})
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, wsID := newPRTaskEnv(t)
			wire(m)
			task, err := m.CreateTask(wsID, nil, "Unknown forge", WithPullRequest(7))
			if err != nil {
				t.Fatal(err)
			}
			if !task.UntrustedSource || task.SetupStatus != SetupBlocked {
				t.Fatalf("untrusted=%v setup_status=%q, want true/blocked", task.UntrustedSource, task.SetupStatus)
			}
			if task.PRNumber != 0 || task.PRRepo != "" || task.PRURL != "" {
				t.Fatalf("PR identity stored without a forge answer: %+v", task)
			}
			if task.BaseRef != "develop" {
				t.Fatalf("base_ref = %q, want the workspace default base", task.BaseRef)
			}
		})
	}
}

func TestCreateTask_NonPRModes_DoNotAskTheForge(t *testing.T) {
	m, wsID := newPRTaskEnv(t)
	prov := &fakePRProvider{err: errors.New("must not be called")}
	useProvider(m, prov)
	task, err := m.CreateTask(wsID, nil, "Plain")
	if err != nil {
		t.Fatal(err)
	}
	if task.UntrustedSource || prov.calls.Load() != 0 {
		t.Fatalf("untrusted=%v calls=%d, want a trusted task and no forge call", task.UntrustedSource, prov.calls.Load())
	}
	waitSetup(t, m, task.ID, SetupOK)
}

func TestCreateTask_ForkPR_TeardownSkipped(t *testing.T) {
	m, wsID := newPRTaskEnv(t)
	pr := sameRepoPR()
	pr.FromFork = true
	useProvider(m, &fakePRProvider{pr: pr})
	task, err := m.CreateTask(wsID, nil, "Fork teardown", WithPullRequest(7))
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := m.store.GetWorkspace(wsID)
	marker := filepath.Join(ws.Path, "teardown.marker")

	if _, err := m.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("teardown ran for an untrusted task")
	}
	got, _ := m.GetTask(task.ID)
	if !strings.Contains(got.SetupLog, "teardown skipped") {
		t.Fatalf("setup_log = %q, want it to say teardown was skipped", got.SetupLog)
	}
}

func TestRetrySetup_ClearsUntrusted(t *testing.T) {
	m, wsID := newPRTaskEnv(t)
	pr := sameRepoPR()
	pr.FromFork = true
	useProvider(m, &fakePRProvider{pr: pr})
	task, err := m.CreateTask(wsID, nil, "Fork retry", WithPullRequest(7))
	if err != nil {
		t.Fatal(err)
	}

	got, err := m.RetrySetup(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UntrustedSource || got.SetupStatus != SetupRunning {
		t.Fatalf("after retry untrusted=%v setup_status=%q, want false/running", got.UntrustedSource, got.SetupStatus)
	}
	waitSetup(t, m, task.ID, SetupOK)
	if _, err := os.Stat(filepath.Join(*task.WorktreePath, "setup.marker")); err != nil {
		t.Fatalf("setup did not run after the user's retry: %v", err)
	}

	// Cleared means teardown runs again at archive.
	if _, err := m.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}
	ws, _ := m.store.GetWorkspace(wsID)
	if _, err := os.Stat(filepath.Join(ws.Path, "teardown.marker")); err != nil {
		t.Fatalf("teardown did not run after the flag was cleared: %v", err)
	}
}

func TestUntrusted_OnlyUserClears(t *testing.T) {
	m, wsID := newPRTaskEnv(t)
	useProvider(m, &fakePRProvider{err: forge.NewError(forge.CodeUnauthenticated, "not signed in")})
	task, err := m.CreateTask(wsID, nil, "Stays untrusted", WithPullRequest(7))
	if err != nil {
		t.Fatal(err)
	}
	// Nothing short of RetrySetup clears the flag: not a status write, not a
	// base change, not the PR poller's identity write.
	if _, err := m.store.UpdateTaskSetup(task.ID, SetupFailed, "x", "y"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetTaskBase(task.ID, "develop"); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetTaskPR(task.ID, "github", "o/r", 7, "https://github.com/o/r/pull/7", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.GetTask(task.ID); !got.UntrustedSource {
		t.Fatal("untrusted_source cleared by something other than the user's retry")
	}
	if _, err := m.RetrySetup(task.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.GetTask(task.ID); got.UntrustedSource {
		t.Fatal("RetrySetup did not clear untrusted_source")
	}
}

func TestRetrySetup_UntrustedWithoutScript_ClearsFlag(t *testing.T) {
	m, wsID, repo, bare, _, _ := newTaskRefEnv(t)
	prSHA := strings.TrimSpace(runGitOutputT(t, repo, "rev-parse", "HEAD"))
	runGitPushRef(t, repo, bare, prSHA, "refs/pull/7/head")
	task, err := m.CreateTask(wsID, nil, "No script", WithPullRequest(7)) // no forge: fail closed
	if err != nil {
		t.Fatal(err)
	}
	if !task.UntrustedSource || task.SetupStatus != "" {
		t.Fatalf("untrusted=%v setup_status=%q, want true and no status (no script to block)", task.UntrustedSource, task.SetupStatus)
	}
	got, err := m.RetrySetup(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UntrustedSource {
		t.Fatal("flag not cleared for a workspace without a setup script")
	}
}
