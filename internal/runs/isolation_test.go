package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// fakeClaudeEnv switches the test binary into the fake claude CLI (see
// TestMain); the isolated runner is pointed at the binary itself.
const fakeClaudeEnv = "RUNS_FAKE_CLAUDE_CLI"

type isolatedEnv struct {
	wm     *workspace.Manager
	st     *store.Store
	task   store.Task
	runner *taskrunner.Runner
	reg    *Registry
}

// newIsolatedEnv is a registry + a claude-native runner on the fake CLI,
// with a task whose worktree passes the isolation cwd guard (.git removed).
func newIsolatedEnv(t *testing.T, scenario string) *isolatedEnv {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, scenario)
	if err := os.RemoveAll(filepath.Join(*task.WorktreePath, ".git")); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner := taskrunner.New(wm, taskrunner.WithDefaultModel(taskrunner.ProviderClaudeNative, "claude-haiku-5-5"), taskrunner.WithDefaultModel(taskrunner.ProviderClaudeNative, "claude-haiku-5-5"), taskrunner.WithClaudeCLIPath(self))
	return &isolatedEnv{wm: wm, st: st, task: task, runner: runner, reg: newTestRegistry(t, st)}
}

func (e *isolatedEnv) start(opts ...StartOption) (string, error) {
	return e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderClaudeNative, "hi", taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified, opts...)
}

var testIso = taskrunner.IsolationOptions{SystemPrompt: "you reflect"}

func TestRegistry_IsolatedStartRequiresSystemPurpose(t *testing.T) {
	t.Parallel()
	e := newIsolatedEnv(t, "reply")
	if id, err := e.start(WithIsolation(testIso)); err == nil {
		t.Fatalf("Start(WithIsolation) without purpose = %q, nil; want error", id)
	}
	rows, err := e.st.ListRecentRuns(10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("persisted runs = %+v, %v; want none", rows, err)
	}
	if got := e.reg.List(0); len(got) != 0 {
		t.Fatalf("registry runs = %+v, want none", got)
	}
	// StartWhenBusy cannot carry isolation either.
	if _, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderClaudeNative, "hi", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0, WithPurpose("reflect"), WithIsolation(testIso)); err == nil {
		t.Fatal("StartWhenBusy(WithIsolation) succeeded, want error")
	}
}

func TestRegistry_IsolatedStartRefusesAcpProviders(t *testing.T) {
	t.Parallel()
	e := newIsolatedEnv(t, "reply")
	for _, p := range []taskrunner.Provider{taskrunner.ProviderGLM, taskrunner.ProviderKimi, taskrunner.ProviderCodexNative} {
		_, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, p, "hi", taskrunner.PermissionSettings{}, "", WithPurpose("reflect"), WithIsolation(testIso))
		if !errors.Is(err, taskrunner.ErrIsolationUnsupported) {
			t.Fatalf("Start(%s) error = %v, want ErrIsolationUnsupported", p, err)
		}
	}
	rows, _ := e.st.ListRunsByPurpose("reflect", 10)
	if len(rows) != 0 {
		t.Fatalf("persisted reflect runs = %+v, want none", rows)
	}
	if got := e.reg.ListFiltered(0, 0, ""); len(got) != 0 {
		t.Fatalf("registry runs = %+v, want none", got)
	}
	chat, err := e.wm.DefaultChat(e.task.ID)
	if err != nil || chat.Provider != nil {
		t.Fatalf("chat provider = %v, %v; refused start must not bind the chat", chat.Provider, err)
	}
}

func TestRegistry_IsolatedRunUsesIsolatedRunner(t *testing.T) {
	t.Parallel()
	e := newIsolatedEnv(t, "echo-args")
	id, err := e.start(WithPurpose("reflect"), WithIsolation(testIso), WithOrigin(""))
	if err != nil {
		t.Fatal(err)
	}
	st := waitForStatus(t, e.reg, id, StatusDone, 10*time.Second)
	if st.Purpose != "reflect" {
		t.Fatalf("Purpose = %q, want reflect", st.Purpose)
	}
	for _, r := range e.reg.List(0) {
		if r.ID == id {
			t.Fatal("system run visible in List")
		}
	}
	data, err := os.ReadFile(filepath.Join(*e.task.WorktreePath, "args"))
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(string(data), "\n")
	has := func(f string) bool {
		for _, a := range args {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
		return false
	}
	for _, f := range []string{"--tools", "--strict-mcp-config", "--system-prompt"} {
		if !has(f) {
			t.Errorf("%s missing from argv %q", f, args)
		}
	}
}

func TestRegistry_IsolatedViolationFailsRun(t *testing.T) {
	t.Parallel()
	e := newIsolatedEnv(t, "tool-attempt")
	id, err := e.start(WithPurpose("reflect"), WithIsolation(testIso))
	if err != nil {
		t.Fatal(err)
	}
	st := waitForStatus(t, e.reg, id, StatusError, 10*time.Second)
	if !strings.Contains(st.Err, "isolated run attempted a tool call") {
		t.Fatalf("Err = %q, want the violation message", st.Err)
	}
	row, err := e.st.GetRun(id)
	if err != nil || row.Status != string(StatusError) || row.ErrMsg == "" {
		t.Fatalf("row = %+v, %v", row, err)
	}
}

func TestClipErrMsg(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", 400)
	got := clipErrMsg(long)
	if len(got) > maxIsolatedErrMsg || !strings.HasPrefix(long, got) {
		t.Fatalf("clipErrMsg len = %d", len(got))
	}
}
