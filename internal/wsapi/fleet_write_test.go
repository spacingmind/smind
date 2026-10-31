package wsapi

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/fleet/pairing"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/inbox"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsclient"
)

// fakeFleetModel is the model the fake ACP agent advertises (with its
// "models" argument): a fleet create must name a model the callee's own
// agent knows.
const fakeFleetModel = "fake-model-a"

// fleetWriteEnv is newFleetEnv with the ONE store every side shares: the
// write path's task and run rows must be visible to serve's TaskLookup, the
// API's workspace manager and the inbox alike, which the split-store
// newFleetEnv cannot give.
type fleetWriteEnv struct {
	*fleetEnv
	wm *workspace.Manager
	st *store.Store
}

func newFleetWriteEnv(t *testing.T) *fleetWriteEnv {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	// The "models" argument makes the fake agent advertise its two model
	// options, so the wire-required model field can name a real one and the
	// started runs actually turn.
	runner := taskrunner.New(wm, taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath, "models"}))
	api, err := New(wm, newTestAccountsRegistry(t), runner, st, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	key, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &fleetWriteEnv{
		fleetEnv: &fleetEnv{api: api, srv: srv, reg: registry.New(st, 8), key: key, window: pairing.NewWindow(nil), links: &fakeFleetLinks{}},
		wm:       wm,
		st:       st,
	}
	api.SetFleet(FleetOptions{Key: key, Registry: e.reg, Window: e.window, Links: e.links, Version: "test", Store: st, Workspaces: fakeFleetWorkspaces{st}})
	return e
}

// grantWrite narrows the approved caller's grant to the write shape the
// fleet write methods need: every op, this workspace writable, and the
// plan's budget numbers.
func (e *fleetWriteEnv) grantWrite(t *testing.T, caller *identity.Key, wsID int64) {
	t.Helper()
	g := registry.DefaultForwardGrant()
	g.WorkspacesWrite = []int64{wsID}
	g.MaxLiveAgents = 2
	g.MaxWallSeconds = 3600
	g.MaxCostUSD = 5
	if err := e.reg.SetGrant(caller.HostID(), g); err != nil {
		t.Fatal(err)
	}
}

// fleetRunStatusWS reads fleet.run.status once for the caller.
func fleetRunStatusWS(t *testing.T, cl *wsclient.Client, caller *identity.Key, runID string) wire.RunStatus {
	t.Helper()
	var st wire.RunStatus
	if err := cl.Call(callCtx(), wire.MethodRunStatus, wire.RunStatusParams{Prov: okProv(caller), RunID: runID}, &st); err != nil {
		t.Fatalf("run.status %s: %v", runID, err)
	}
	return st
}

func TestFleetIdemSweeperRemovesExpired(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now().UTC().Add(10 * time.Minute) // a fixed clock: only age decides
	old := store.FleetIdem{HostID: "h", Key: "old", Method: wire.MethodTaskCreate, CreatedAt: now.Add(-wire.IdempotencyWindow - time.Hour)}
	fresh := store.FleetIdem{HostID: "h", Key: "fresh", Method: wire.MethodTaskCreate, CreatedAt: now}
	for _, i := range []store.FleetIdem{old, fresh} {
		if _, _, err := st.PutFleetIdem(i); err != nil {
			t.Fatal(err)
		}
	}

	stop := startFleetIdemSweeper(st, func() time.Time { return now })
	stop() // the sweep at start has already run; stop is idempotent
	stop()

	if _, ok, err := st.GetFleetIdem("h", "old"); err != nil || ok {
		t.Fatalf("expired row still present: ok=%v err=%v, want gone", ok, err)
	}
	if _, ok, err := st.GetFleetIdem("h", "fresh"); err != nil || !ok {
		t.Fatalf("fresh row = ok %v err %v, want kept", ok, err)
	}
}

func TestFleetCreateBranchFromOwnOrigin(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	e := newFleetWriteEnv(t)
	dir := t.TempDir()

	// A bare "origin", a workspace clone of it with only the seed branch,
	// and a second clone that pushes feat/remote so the workspace clone has
	// no such local branch: the callee must fetch it from its own origin.
	origin := filepath.Join(dir, "origin.git")
	runGitT(t, dir, "init", "--bare", "origin.git")
	seed := filepath.Join(dir, "seed")
	runGitT(t, dir, "clone", origin, seed)
	gitSeed := func(args ...string) { t.Helper(); runGitT(t, seed, args...) }
	gitSeed("config", "user.email", "t@example.com")
	gitSeed("config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitSeed("add", "README.md")
	gitSeed("commit", "-m", "seed")
	gitSeed("push", "origin", "HEAD")

	wsRepo := filepath.Join(dir, "ws")
	runGitT(t, dir, "clone", origin, wsRepo)
	second := filepath.Join(dir, "second")
	runGitT(t, dir, "clone", origin, second)
	gitSecond := func(args ...string) { t.Helper(); runGitT(t, second, args...) }
	gitSecond("config", "user.email", "t@example.com")
	gitSecond("config", "user.name", "T")
	gitSecond("checkout", "-b", "feat/remote")
	unique := "fleet-remote-file.txt"
	if err := os.WriteFile(filepath.Join(second, unique), []byte("from origin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitSecond("add", unique)
	gitSecond("commit", "-m", "remote")
	gitSecond("push", "origin", "feat/remote")

	ws, err := e.wm.CreateWorkspace(wsRepo, "repo", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	cl, caller := approvedConn(t, e.fleetEnv)
	e.grantWrite(t, caller, ws.ID)

	taskCount := func() int {
		rows, err := e.st.ListTasksByWorkspace(ws.ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	create := func(key, branch string) error {
		p := wire.TaskCreateParams{
			Prov: okProv(caller), IdempotencyKey: key, WorkspaceID: ws.ID,
			Provider: "glm", Model: fakeFleetModel, Title: "remote task", Prompt: "hi",
		}
		if branch != "" {
			p.Branch = branch
		}
		return cl.Call(callCtx(), wire.MethodTaskCreate, p, nil)
	}

	var res wire.TaskCreateResult
	p := wire.TaskCreateParams{
		Prov: okProv(caller), IdempotencyKey: "k1", WorkspaceID: ws.ID,
		Provider: "glm", Model: fakeFleetModel, Title: "remote task", Prompt: "hi", Branch: "feat/remote",
	}
	if err := cl.Call(callCtx(), wire.MethodTaskCreate, p, &res); err != nil {
		t.Fatalf("task.create: %v", err)
	}
	if res.TaskID == 0 || res.RunID == "" || res.Branch != "feat/remote" {
		t.Fatalf("create result = %+v", res)
	}
	task, err := e.st.GetTask(res.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.WorktreePath == nil {
		t.Fatal("task has no worktree")
	}
	if b, err := os.ReadFile(filepath.Join(*task.WorktreePath, unique)); err != nil || string(b) != "from origin\n" {
		t.Fatalf("worktree file = %q, %v; want the branch's own content", b, err)
	}
	host, trace, err := e.st.TaskOrigin(res.TaskID)
	if err != nil || host != caller.HostID() || trace != "t1" {
		t.Fatalf("task origin = %q %q, %v; want the caller host and its trace", host, trace, err)
	}

	// A branch origin does not have is branch_not_found, with no task row.
	before := taskCount()
	wantFleetCode(t, create("k2", "nope"), wire.CodeBranchNotFound)
	if got := taskCount(); got != before {
		t.Fatalf("task count after a missing branch = %d, want %d", got, before)
	}
	// Hostile names die at the wire, before anything runs.
	for _, bad := range []string{"-x", "a..b"} {
		wantFleetCode(t, create("k3", bad), wire.CodeInvalid)
	}
	if got := taskCount(); got != before {
		t.Fatalf("task count after bad branch names = %d, want %d", got, before)
	}
}

func TestFleetRemotePermissionGoesToCalleeInbox(t *testing.T) {
	e := newFleetWriteEnv(t)
	ws, err := e.wm.CreateWorkspace(newTestRepo(t), "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	cl, caller := approvedConn(t, e.fleetEnv)
	e.grantWrite(t, caller, ws.ID)

	var res wire.TaskCreateResult
	p := wire.TaskCreateParams{
		Prov: okProv(caller), IdempotencyKey: "c1", WorkspaceID: ws.ID,
		Provider: "glm", Model: fakeFleetModel, Prompt: "hi",
	}
	if err := cl.Call(callCtx(), wire.MethodTaskCreate, p, &res); err != nil {
		t.Fatalf("task.create: %v", err)
	}
	// The fake agent's scenario file is per worktree: park the second run
	// on the permission scenario once the first has finished with it.
	deadline := time.Now().Add(15 * time.Second)
	for {
		st := fleetRunStatusWS(t, cl, caller, res.RunID)
		if st.State != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first run still %q after 15s", st.State)
		}
		time.Sleep(50 * time.Millisecond)
	}
	task, err := e.st.GetTask(res.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("permission"), 0o644); err != nil {
		t.Fatal(err)
	}

	var send wire.TaskSendResult
	sp := wire.TaskSendParams{Prov: okProv(caller), IdempotencyKey: "s1", TaskID: res.TaskID, Prompt: "again"}
	if err := cl.Call(callCtx(), wire.MethodTaskSend, sp, &send); err != nil {
		t.Fatalf("task.send: %v", err)
	}

	// The caller learns a permission waits, and nothing more.
	var st wire.RunStatus
	for {
		st = fleetRunStatusWS(t, cl, caller, send.RunID)
		if st.WaitingPermission && st.State == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run never waited on a permission: %+v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The request itself sits in the callee's (local human's) inbox.
	var in struct {
		Items []inbox.Item `json:"items"`
	}
	if err := e.dial(t).Call(callCtx(), "inbox.list", nil, &in); err != nil {
		t.Fatalf("inbox.list: %v", err)
	}
	found := false
	for _, it := range in.Items {
		if it.Kind == inbox.KindPermission && it.TaskID == res.TaskID {
			found = true
		}
	}
	if !found {
		t.Fatalf("no permission item for task %d in %+v", res.TaskID, in.Items)
	}

	// No fleet read or write method can answer one.
	for _, m := range append(slices.Clone(wire.ReadMethods), wire.WriteMethods...) {
		if strings.Contains(m, "permission") || strings.Contains(m, "approve") {
			t.Errorf("method %q answers or describes a permission", m)
		}
	}
}

func TestFleetWriteOverWsapi(t *testing.T) {
	e := newFleetWriteEnv(t)
	ws, err := e.wm.CreateWorkspace(newTestRepo(t), "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	cl, caller := approvedConn(t, e.fleetEnv)
	e.grantWrite(t, caller, ws.ID)

	var res wire.TaskCreateResult
	p := wire.TaskCreateParams{
		Prov: okProv(caller), IdempotencyKey: "c1", WorkspaceID: ws.ID,
		Provider: "glm", Model: fakeFleetModel, Prompt: "hi",
	}
	if err := cl.Call(callCtx(), wire.MethodTaskCreate, p, &res); err != nil {
		t.Fatalf("task.create: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	waitDone := func(runID string) wire.RunStatus {
		for {
			st := fleetRunStatusWS(t, cl, caller, runID)
			if st.State != "running" {
				return st
			}
			if time.Now().After(deadline) {
				t.Fatalf("run %s still running after 20s", runID)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitDone(res.RunID)

	// A second prompt on the same task, then the wait loop drains it.
	var send wire.TaskSendResult
	sp := wire.TaskSendParams{Prov: okProv(caller), IdempotencyKey: "s1", TaskID: res.TaskID, Prompt: "again"}
	if err := cl.Call(callCtx(), wire.MethodTaskSend, sp, &send); err != nil {
		t.Fatalf("task.send: %v", err)
	}
	var events int
	cursor := int64(0)
	for {
		var w wire.RunWaitResult
		wp := wire.RunWaitParams{Prov: okProv(caller), RunID: send.RunID, Cursor: cursor, TimeoutSeconds: 3}
		if err := cl.Call(callCtx(), wire.MethodRunWait, wp, &w); err != nil {
			t.Fatalf("run.wait: %v", err)
		}
		events += len(w.Events)
		cursor = w.NextCursor
		if w.Done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run.wait never finished: %d events so far", events)
		}
	}
	if events == 0 {
		t.Fatal("run.wait returned done with no events")
	}

	// Stopping a finished run reports nothing was stopped.
	var stop wire.TaskStopResult
	stopP := wire.TaskStopParams{Prov: okProv(caller), TaskID: res.TaskID}
	if err := cl.Call(callCtx(), wire.MethodTaskStop, stopP, &stop); err != nil {
		t.Fatalf("task.stop: %v", err)
	}
	if stop.Stopped {
		t.Fatal("stop of a finished run = true, want false")
	}

	// Another approved host may not drive the first one's task.
	other := newCallerKey(t)
	e.pairApproved(t, other, "other")
	e.grantWrite(t, other, ws.ID)
	ocl := e.dial(t)
	if _, err := e.doHello(t, ocl, other); err != nil {
		t.Fatal(err)
	}
	op := wire.TaskSendParams{Prov: okProv(other), IdempotencyKey: "o1", TaskID: res.TaskID, Prompt: "mine now"}
	wantFleetCode(t, ocl.Call(callCtx(), wire.MethodTaskSend, op, nil), wire.CodeNotOwner)

	// A grant that cannot write this workspace never reaches the effect.
	nb := newCallerKey(t)
	e.pairApproved(t, nb, "nb")
	nbg := registry.DefaultForwardGrant()
	nbg.WorkspacesWrite = []int64{ws.ID + 1000}
	if err := e.reg.SetGrant(nb.HostID(), nbg); err != nil {
		t.Fatal(err)
	}
	ncl := e.dial(t)
	if _, err := e.doHello(t, ncl, nb); err != nil {
		t.Fatal(err)
	}
	np := wire.TaskCreateParams{
		Prov: okProv(nb), IdempotencyKey: "n1", WorkspaceID: ws.ID,
		Provider: "glm", Model: fakeFleetModel, Prompt: "hi",
	}
	wantFleetCode(t, ncl.Call(callCtx(), wire.MethodTaskCreate, np, nil), wire.CodeNotGranted)
}

func TestFleetBackendRefusesUnsafeRefNames(t *testing.T) {
	for _, ref := range []string{"a:b", "a..b", "-x", "x.lock", "a b", "a^", "refs/heads/ok/..", "a/", "", "a\x7fb", "a~1", "a?", "a*", "a[", `a\b`, strings.Repeat("a", 201)} {
		if safeRefName(ref) {
			t.Errorf("safeRefName(%q) = true, want false", ref)
		}
	}
	for _, ref := range []string{"feat/x", "main", "release-1.2"} {
		if !safeRefName(ref) {
			t.Errorf("safeRefName(%q) = false, want true", ref)
		}
	}
}
