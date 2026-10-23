//go:build !windows

package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/ghctx"
	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/store"
)

// prViewStub answers gh for PR 7 of o/r: a fork PR or not, counting `pr view`
// calls. With fail set every call fails like a daemon host without gh.
type prViewStub struct {
	fork  bool
	fail  bool
	views atomic.Int32
}

func (s *prViewStub) run(_ context.Context, _ string, args ...string) (string, error) {
	if s.fail {
		return "", context.DeadlineExceeded
	}
	switch {
	case args[0] == "repo":
		return `{"nameWithOwner":"o/r","url":"https://github.com/o/r"}`, nil
	case args[0] == "pr" && args[1] == "view":
		s.views.Add(1)
		fork := "false"
		if s.fork {
			fork = "true"
		}
		return `{"number":7,"url":"https://github.com/o/r/pull/7","title":"Fix","state":"OPEN","isDraft":false,` +
			`"headRefName":"feat/x","headRefOid":"abc","baseRefName":"develop","isCrossRepository":` + fork + `,` +
			`"mergeable":"MERGEABLE","reviewDecision":"","statusCheckRollup":[]}`, nil
	}
	return "", context.DeadlineExceeded
}

func TestTaskCreate_ForkPR_Gate_EndToEnd(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	repo := newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "develop")
	bare := t.TempDir()
	runGitT(t, bare, "init", "--bare")
	runGitT(t, repo, "remote", "add", "origin", bare)
	runGitT(t, repo, "push", "origin", "develop")
	runGitT(t, repo, "push", "origin", "HEAD:refs/pull/7/head")
	w, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	setup := `echo ran > setup.marker`
	if _, err := db.SetWorkspaceScripts(w.ID, &setup, nil); err != nil {
		t.Fatal(err)
	}
	stub := &prViewStub{fork: true}
	api, srv := newGitTestAPI(t, wm, db, nil)
	api.SetGithubClient(ghctx.New(ghctx.WithRunner(stub.run)))
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	var task store.Task
	ec.call("1", "task.create", map[string]any{"workspaceId": w.ID, "title": "Fork", "prNumber": 7}, &task)
	if !task.UntrustedSource || task.SetupStatus != "blocked" {
		t.Fatalf("created task untrusted=%v setup_status=%q, want true/blocked", task.UntrustedSource, task.SetupStatus)
	}
	if task.PRNumber != 7 || task.PRRepo != "o/r" || task.PRHeadRef != "feat/x" || task.BaseRef != "develop" {
		t.Fatalf("PR identity/base = %d %q %q base %q", task.PRNumber, task.PRRepo, task.PRHeadRef, task.BaseRef)
	}
	if stub.views.Load() != 1 {
		t.Fatalf("gh pr view calls = %d, want exactly 1", stub.views.Load())
	}

	var g gitstate.TaskGit
	ec.call("2", "task.git", map[string]any{"taskId": task.ID}, &g)
	if !g.Untrusted {
		t.Fatal("task.git untrusted = false for a fork task")
	}
	if _, err := os.Stat(filepath.Join(*task.WorktreePath, "setup.marker")); err == nil {
		t.Fatal("setup ran for a fork PR before the user asked")
	}

	// An agent (run-scoped caller) cannot un-gate the task.
	raw, _ := json.Marshal(map[string]any{"id": task.ID})
	if _, err := api.hs["task.retrySetup"](context.Background(), &requestContext{runScoped: true}, raw); err == nil {
		t.Fatal("run-scoped caller ran setup")
	} else if c := codedError(err); c.Code != codeForbidden {
		t.Fatalf("code = %q, want %q", c.Code, codeForbidden)
	}
	if got, _ := db.GetTask(task.ID); !got.UntrustedSource {
		t.Fatal("run-scoped caller cleared untrusted_source")
	}

	// The user's retry runs it and lifts the gate.
	var retried store.Task
	ec.call("3", "task.retrySetup", map[string]any{"id": task.ID}, &retried)
	if retried.UntrustedSource || retried.SetupStatus != "running" {
		t.Fatalf("after retry untrusted=%v setup_status=%q", retried.UntrustedSource, retried.SetupStatus)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if got, _ := db.GetTask(task.ID); got.SetupStatus == "ok" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("setup never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(*task.WorktreePath, "setup.marker")); err != nil {
		t.Fatalf("setup did not run: %v", err)
	}
	// The recompute after task.retrySetup is asynchronous (and publishes a
	// task.git event); a read converges on the cleared flag.
	for i := 0; ; i++ {
		ec.call("4-"+strconv.Itoa(i), "task.git", map[string]any{"taskId": task.ID}, &g)
		if !g.Untrusted {
			break
		}
		if time.Now().After(deadline.Add(15 * time.Second)) {
			t.Fatal("task.git untrusted stayed true after the user ran setup")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestTaskCreate_PR_ForgeUnavailable_FailsClosed(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	repo := newTestRepo(t)
	runGitT(t, repo, "branch", "-M", "develop")
	bare := t.TempDir()
	runGitT(t, bare, "init", "--bare")
	runGitT(t, repo, "remote", "add", "origin", bare)
	runGitT(t, repo, "push", "origin", "develop")
	runGitT(t, repo, "push", "origin", "HEAD:refs/pull/7/head")
	w, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	api, srv := newGitTestAPI(t, wm, db, nil)
	api.SetGithubClient(ghctx.New(ghctx.WithRunner((&prViewStub{fail: true}).run)))
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	var task store.Task
	ec.call("1", "task.create", map[string]any{"workspaceId": w.ID, "title": "Unknown", "prNumber": 7}, &task)
	if !task.UntrustedSource || task.PRNumber != 0 || !strings.HasPrefix(*task.Branch, "smind/pr-7-") {
		t.Fatalf("untrusted=%v prNumber=%d branch=%q, want untrusted, no identity, PR branch", task.UntrustedSource, task.PRNumber, *task.Branch)
	}
}
