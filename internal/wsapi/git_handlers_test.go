package wsapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// countingGit wraps the real hardened runner and counts subcommands.
type countingGit struct {
	mu     sync.Mutex
	counts map[string]int
}

func (c *countingGit) run(ctx context.Context, dir string, args ...string) (string, error) {
	c.mu.Lock()
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	c.counts[args[0]]++
	c.mu.Unlock()
	return gitstate.DefaultRunner(ctx, dir, args...)
}

func (c *countingGit) count(sub string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[sub]
}

func newGitTestAPI(t *testing.T, wm *workspace.Manager, db *store.Store, run gitstate.Runner) (*API, *httptest.Server) {
	t.Helper()
	api, err := newAPI(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok", gitOptions{Runner: run})
	if err != nil {
		t.Fatalf("newAPI() error = %v", err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	return api, srv
}

func (c *eventConn) call(id, method string, params any, out any) {
	c.t.Helper()
	sendRequest(c.t, c.ws, id, method, params)
	resp := c.nextResponse(id, 5*time.Second)
	if resp.Error != nil {
		c.t.Fatalf("%s error = %v", method, resp.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			c.t.Fatalf("decode %s result: %v", method, err)
		}
	}
}

func TestKnownTopics_IncludesTaskGit(t *testing.T) {
	t.Parallel()
	if !knownTopics[TopicTaskGit] || TopicTaskGit != "task.git" {
		t.Fatalf("task.git is not a known topic")
	}
	bus := newEventBus()
	if bus.HasSubscribers(TopicTaskGit) {
		t.Fatal("empty bus reports a subscriber")
	}
	s := newSubscriber()
	bus.register(s)
	if bus.HasSubscribers(TopicTaskGit) {
		t.Fatal("subscriber without the topic counted")
	}
	s.subscribe([]string{TopicTaskGit})
	if !bus.HasSubscribers(TopicTaskGit) {
		t.Fatal("subscribed connection not counted")
	}
	bus.unregister(s)
	if bus.HasSubscribers(TopicTaskGit) {
		t.Fatal("unregistered subscriber still counted")
	}
}

func TestTaskGit_RPC(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	_, srv := newGitTestAPI(t, wm, db, nil)
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	var g gitstate.TaskGit
	ec.call("1", "task.git", map[string]any{"taskId": task.ID}, &g)
	if g.TaskID != task.ID || g.Branch == nil || g.Branch.Name != *task.Branch || g.Worktree == nil || g.Base == nil || !g.Base.Known {
		t.Fatalf("task.git = %+v", g)
	}

	var list taskGitListResult
	ec.call("2", "task.gitList", map[string]any{"workspaceId": task.WorkspaceID}, &list)
	if len(list.Items) != 1 || list.Items[0].TaskID != task.ID {
		t.Fatalf("task.gitList = %+v", list.Items)
	}

	ec.call("3", "task.gitFocus", map[string]any{"taskId": task.ID}, nil)
	ec.call("4", "task.gitFocus", map[string]any{"taskId": nil}, nil)

	sendRequest(t, ec.ws, "5", "task.git", map[string]any{"taskId": 99999})
	if resp := ec.nextResponse("5", 5*time.Second); resp.Error == nil {
		t.Fatal("task.git for an unknown task succeeded")
	}
}

func TestTaskGit_ReadNeverRunsNetworkGit(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	cg := &countingGit{}
	_, srv := newGitTestAPI(t, wm, db, cg.run)
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicTaskGit)

	ec.call("1", "task.gitFocus", map[string]any{"taskId": task.ID}, nil)
	ec.call("2", "task.git", map[string]any{"taskId": task.ID}, nil)
	ec.call("3", "task.gitList", map[string]any{"workspaceId": task.WorkspaceID}, nil)
	if cg.count("rev-parse") == 0 {
		t.Fatal("counting runner saw no local git: the test is not exercising the read path")
	}
	if n := cg.count("fetch"); n != 0 {
		t.Fatalf("reads ran git fetch %d times, want 0", n)
	}
}

func TestTaskGitFocus_DroppedOnDisconnect(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	api, srv := newGitTestAPI(t, wm, db, nil)
	ws := dialWS(t, srv, "tok")
	ec := newEventConn(t, ws)

	ec.call("1", "task.gitFocus", map[string]any{"taskId": task.ID}, nil)
	if got := api.focus.ids(); len(got) != 1 || got[0] != task.ID {
		t.Fatalf("focus after gitFocus = %v, want [%d]", got, task.ID)
	}
	ws.Close()
	deadline := time.Now().Add(5 * time.Second)
	for len(api.focus.ids()) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("focus not dropped after disconnect: %v", api.focus.ids())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitGit reads task.git events until one satisfies pred.
func waitGit(t *testing.T, ec *eventConn, what string, pred func(gitstate.TaskGit) bool) gitstate.TaskGit {
	t.Helper()
	for range 10 {
		var p taskGitPayload
		decodePayload(t, ec.nextEvent(8*time.Second), &p)
		if pred(p.Git) {
			return p.Git
		}
	}
	t.Fatalf("no task.git event matched: %s", what)
	return gitstate.TaskGit{}
}

func TestTaskGitEvent_TriggersRunEndCommitStageSetBase(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	ws, err := wm.GetWorkspace(task.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	runGitT(t, ws.Path, "branch", "other")
	api, srv := newGitTestAPI(t, wm, db, nil)
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	ec.call("prime", "task.git", map[string]any{"taskId": task.ID}, nil) // caches the snapshot
	ec.subscribe("sub", TopicTaskGit)
	// A second connection for the negative check: a read deadline that expires
	// poisons a gorilla connection, so ec must never be the one that waits.
	quiet := newEventConn(t, dialWS(t, srv, "tok"))
	quiet.subscribe("sub", TopicTaskGit)

	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notifier := busRunNotifier{bus: api.bus, db: db, git: api.git}

	// A run that is still running is not a trigger.
	notifier.NotifyRunStatus(runs.RunStatus{ID: "r1", TaskID: task.ID, Status: runs.StatusRunning})
	quiet.expectNoEvent(300 * time.Millisecond)

	// Run end is.
	notifier.NotifyRunStatus(runs.RunStatus{ID: "r1", TaskID: task.ID, Status: runs.StatusDone})
	waitGit(t, ec, "untracked after run end", func(g gitstate.TaskGit) bool {
		return g.Worktree != nil && g.Worktree.Untracked == 1
	})

	ec.call("stage", "task.stage", map[string]any{"taskId": task.ID, "path": "x.txt", "staged": true}, nil)
	waitGit(t, ec, "staged after task.stage", func(g gitstate.TaskGit) bool {
		return g.Worktree != nil && g.Worktree.Staged == 1
	})

	ec.call("commit", "task.commit", map[string]any{"taskId": task.ID, "message": "add x", "author": "human"}, nil)
	waitGit(t, ec, "commit after task.commit", func(g gitstate.TaskGit) bool {
		return g.Commits != nil && g.Commits.SinceFork != nil && *g.Commits.SinceFork == 1 && g.Worktree != nil && !g.Worktree.Dirty
	})

	ec.call("base", "task.setBase", map[string]any{"taskId": task.ID, "baseRef": "other"}, nil)
	waitGit(t, ec, "base.ref after task.setBase", func(g gitstate.TaskGit) bool {
		return g.Base != nil && g.Base.Ref == "other"
	})
}
