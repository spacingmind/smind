package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/taskrunner/fakeclaude"
	"github.com/spacingmind/smind/internal/workspace"
)

// fakeClaudeEnv switches the test binary into the fake claude CLI; the
// Runner is pointed at the binary itself (taskrunner.WithClaudeCLIPath).
const fakeClaudeEnv = "LEARNING_EXECUTOR_FAKE_CLAUDE"

func TestMain(m *testing.M) {
	if os.Getenv(fakeClaudeEnv) == "1" {
		fakeclaude.Run()
		return
	}
	os.Setenv(fakeClaudeEnv, "1")
	os.Exit(m.Run())
}

type env struct {
	st   *store.Store
	wm   *workspace.Manager
	reg  *runs.Registry
	ex   *Executor
	home string
	ws   store.Workspace
}

// newEnv builds the executor over the fake CLI running scenario. The
// scenario travels in the CLI's env, because the executor's cwd (where the
// fake would otherwise read a scenario file) is cleaned before every run.
func newEnv(t *testing.T, scenario string) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("store.Close: %v", err)
		}
	})
	wm := workspace.New(st)
	reg, err := runs.New(st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.CloseAll) // runs before the store closes and TempDir is removed
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner := taskrunner.New(wm, taskrunner.WithClaudeCLIPath(self, fakeclaude.ScenarioEnv+"="+scenario))
	ws, err := st.CreateWorkspace(store.Workspace{Path: t.TempDir(), Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	return &env{st: st, wm: wm, reg: reg, ex: New(st, wm, runner, reg, home), home: home, ws: ws}
}

func (e *env) req() Request {
	return Request{WorkspaceID: e.ws.ID, Purpose: "reflect", SystemPrompt: "you reflect", UserPrompt: "window", Timeout: 30 * time.Second}
}

func TestExecutor_RunsHiddenIsolatedSystemRun(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "echo-args")
	res, err := e.ex.Run(context.Background(), e.req())
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalText != "ok" {
		t.Fatalf("FinalText = %q, want ok", res.FinalText)
	}
	_, status, err := e.reg.History(res.RunID)
	if err != nil || status.Purpose != "reflect" || status.Status != runs.StatusDone {
		t.Fatalf("status = %+v, %v", status, err)
	}
	for _, r := range e.reg.List(0) {
		t.Fatalf("system run visible in List: %+v", r)
	}
	tasks, err := e.st.ListTasksByWorkspace(e.ws.ID)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("visible tasks = %+v, %v; want none", tasks, err)
	}
	sys, ok, err := e.st.GetSystemTask(e.ws.ID)
	if err != nil || !ok || sys.Kind != store.TaskKindSystem {
		t.Fatalf("system task = %+v ok %v err %v", sys, ok, err)
	}
	wantCwd := filepath.Join(e.home, "learning", "cwd")
	if sys.WorktreePath == nil || !strings.HasPrefix(*sys.WorktreePath, wantCwd+string(filepath.Separator)) {
		t.Fatalf("cwd = %v, want under %s", sys.WorktreePath, wantCwd)
	}
	data, err := os.ReadFile(filepath.Join(*sys.WorktreePath, "args"))
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(string(data), "\n")
	foundTools := false
	for i, a := range args {
		if a == "--tools" && i+1 < len(args) && args[i+1] == "" {
			foundTools = true
		}
		if a == "--tools=" {
			foundTools = true
		}
	}
	if !foundTools {
		t.Fatalf(`argv lacks --tools "": %q`, args)
	}
	fi, err := os.Stat(*sys.WorktreePath)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("cwd mode = %v, %v; want 0700", fi, err)
	}
}

func TestExecutor_ReusesSystemTaskPerWorkspace(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "reply")
	r1, err := e.ex.Run(context.Background(), e.req())
	if err != nil {
		t.Fatal(err)
	}
	if r1.FinalText != "hello from claude" {
		t.Fatalf("FinalText = %q", r1.FinalText)
	}
	req := e.req()
	req.Purpose = "curate"
	r2, err := e.ex.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if r1.RunID == r2.RunID {
		t.Fatal("same run id twice")
	}
	all, err := e.st.ListAllTasksByWorkspace(e.ws.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("tasks = %+v, %v; want exactly one system task", all, err)
	}
	// A second workspace gets its own task and cwd.
	ws2, err := e.st.CreateWorkspace(store.Workspace{Path: t.TempDir(), Title: "w2", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	req.WorkspaceID = ws2.ID
	if _, err := e.ex.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	t2, ok, _ := e.st.GetSystemTask(ws2.ID)
	if !ok || t2.ID == all[0].ID || *t2.WorktreePath == *all[0].WorktreePath {
		t.Fatalf("second workspace task = %+v", t2)
	}
}

func TestExecutor_FailedRunReturnsError(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "tool-attempt")
	res, err := e.ex.Run(context.Background(), e.req())
	if err == nil {
		t.Fatalf("Run = %+v, nil; want error", res)
	}
	if res.FinalText != "" {
		t.Fatalf("FinalText = %q, want empty for a violated run", res.FinalText)
	}
	if !strings.Contains(err.Error(), "isolated run attempted a tool call") {
		t.Fatalf("error = %v, want the run's message", err)
	}
}

func TestExecutor_CtxCancelStopsRun(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "init-hang")
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	resCh := make(chan Result, 1)
	go func() {
		res, err := e.ex.Run(ctx, e.req())
		resCh <- res
		errCh <- err
	}()
	waitRunning(t, e)
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	res := <-resCh
	_, status, err := e.reg.History(res.RunID)
	if err != nil || status.Status != runs.StatusStopped {
		t.Fatalf("run status = %+v, %v; want stopped", status, err)
	}
}

func TestExecutor_BusyPerWorkspace(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "init-hang")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.ex.Run(ctx, e.req())
	}()
	waitRunning(t, e)
	if _, err := e.ex.Run(context.Background(), e.req()); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Run error = %v, want ErrBusy", err)
	}
	cancel()
	<-done
	// Released: the workspace accepts a run again (it fails fast here only
	// because ctx is already done, not because of ErrBusy).
	cctx, ccancel := context.WithCancel(context.Background())
	ccancel()
	if _, err := e.ex.Run(cctx, e.req()); errors.Is(err, ErrBusy) {
		t.Fatalf("workspace still busy after the first run ended: %v", err)
	}
}

func TestExecutor_CleansStrayCwdEntries(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "reply")
	if _, err := e.ex.Run(context.Background(), e.req()); err != nil {
		t.Fatal(err)
	}
	sys, _, _ := e.st.GetSystemTask(e.ws.ID)
	cwd := *sys.WorktreePath

	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(cwd, ".claude", "nested"), 0o755))
	must(os.WriteFile(filepath.Join(cwd, ".claude", "nested", "f"), []byte("x"), 0o600))
	must(os.WriteFile(filepath.Join(cwd, "CLAUDE.md"), []byte("x"), 0o600))
	must(os.Symlink(outside, filepath.Join(cwd, "link")))

	if res, err := e.ex.Run(context.Background(), e.req()); err != nil || res.FinalText != "hello from claude" {
		t.Fatalf("Run after stray entries = %+v, %v", res, err)
	}
	entries, err := os.ReadDir(cwd)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cwd entries = %v, %v; want empty", entries, err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("file outside cwd was touched through the symlink: %v", err)
	}
}

func TestExecutor_RefusesSymlinkedCwd(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "reply")
	if _, err := e.ex.Run(context.Background(), e.req()); err != nil {
		t.Fatal(err)
	}
	sys, _, _ := e.st.GetSystemTask(e.ws.ID)
	cwd := *sys.WorktreePath
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(cwd); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, cwd); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ex.Run(context.Background(), e.req()); err == nil {
		t.Fatal("Run with a symlinked cwd succeeded")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("file behind the symlinked cwd was removed: %v", err)
	}
}

func TestExecutor_UsageFromRunUsage(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "usage")
	res, err := e.ex.Run(context.Background(), e.req())
	if err != nil {
		t.Fatal(err)
	}
	row, err := e.st.GetRunUsage(res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if res.InputTokens == nil || res.OutputTokens == nil || res.CostUSD == nil {
		t.Fatalf("usage = %+v, want all reported", res)
	}
	if *res.InputTokens != *row.InputTokens || *res.OutputTokens != *row.OutputTokens || *res.CostUSD != *row.CostUSD {
		t.Fatalf("usage %v/%v/%v differs from the run_usage row %+v", *res.InputTokens, *res.OutputTokens, *res.CostUSD, row)
	}

	// No usage reported: nil pointers, not zeros.
	e2 := newEnv(t, "usage-no-model")
	res2, err := e2.ex.Run(context.Background(), e2.req())
	if err != nil {
		t.Fatal(err)
	}
	if res2.InputTokens != nil || res2.OutputTokens != nil || res2.CostUSD != nil {
		t.Fatalf("usage = %+v, want nil pointers when unreported", res2)
	}
}

// waitRunning blocks until the executor's run has been registered and has
// produced its first event (the init-hang scenario's text chunk).
func waitRunning(t *testing.T, e *env) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := e.st.ListRunsByPurpose("reflect", 10)
		if err == nil && len(rows) == 1 {
			if hist, _, err := e.reg.History(rows[0].ID); err == nil && len(hist) > 0 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run never started")
}
