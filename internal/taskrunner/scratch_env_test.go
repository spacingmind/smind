package taskrunner

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/scratch"
	"github.com/spacingmind/smind/internal/store"
)

// lastEnvEntry returns the last "NAME=" line of an environment dump the
// fake agents write (os.Environ() joined by newlines): duplicate names are
// possible when a launch prepends entries after the inherited environment,
// and the last one is the value that wins.
func lastEnvEntry(t *testing.T, env, name string) string {
	t.Helper()
	last := ""
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, name+"=") {
			last = line
		}
	}
	if last == "" {
		t.Fatalf("agent environment has no %s entry:\n%s", name, env)
	}
	return last
}

// AC1, claude-native: the launch env points TMPDIR at the task's scratch
// dir, and the dir exists before the CLI runs.
func TestLaunchEnv_ScratchTmpdir_Claude(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	WithScratch(scratch.New(t.TempDir()))(r)

	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	dir := r.scratch.Dir(task.ID)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("scratch dir %s missing or not a directory: %v", dir, err)
	}
	if got := lastEnvEntry(t, readLaunchEnv(t, task), "TMPDIR"); got != "TMPDIR="+dir {
		t.Fatalf("TMPDIR = %q, want the scratch dir %q", got, dir)
	}
}

// AC1: a TMPDIR pinned in the provider env wins over the scratch entry
// (scratch entries are prepended; last entry wins).
func TestLaunchEnv_PinnedTmpdirWins(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	WithScratch(scratch.New(t.TempDir()))(r)
	r.SetProviderEnv(ProviderClaudeNative, "TMPDIR", "/pinned/tmp")

	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if got := lastEnvEntry(t, readLaunchEnv(t, task), "TMPDIR"); got != "TMPDIR=/pinned/tmp" {
		t.Fatalf("TMPDIR = %q, want the pinned /pinned/tmp (last wins)", got)
	}
}

// AC2: configured shared caches reach the launch env, scratch or not.
func TestSharedCaches_On(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	WithSharedCaches(scratch.SharedCaches{"GOCACHE": "/shared/gocache", "GOMODCACHE": "/shared/mod"})(r)

	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	env := readLaunchEnv(t, task)
	for _, want := range []string{"GOCACHE=/shared/gocache", "GOMODCACHE=/shared/mod"} {
		if !strings.Contains(env, want) {
			t.Fatalf("agent environment lacks %q:\n%s", want, env)
		}
	}
}

// AC2: with no WithSharedCaches option nothing is added, even when the
// daemon's own environment sets the name.
func TestSharedCaches_Off(t *testing.T) {
	os.Unsetenv("GOCACHE")
	r, _, task := newClaudeMcpEnv(t)

	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	env := readLaunchEnv(t, task)
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, "GOCACHE=") {
			t.Fatalf("agent environment has %q with shared caches off:\n%s", line, env)
		}
	}
}

// AC2: a provider-pinned cache var wins over the shared-cache entry.
func TestSharedCaches_DoesNotOverrideExisting(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	WithSharedCaches(scratch.SharedCaches{"GOCACHE": "/shared/gocache"})(r)
	r.SetProviderEnv(ProviderClaudeNative, "GOCACHE", "/mine")

	if err := runInstance(t, r, task, ProviderClaudeNative); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if got := lastEnvEntry(t, readLaunchEnv(t, task), "GOCACHE"); got != "GOCACHE=/mine" {
		t.Fatalf("GOCACHE = %q, want the pinned /mine (last wins)", got)
	}
}

// runScratchTurn drives one provider-agnostic RunPrompt turn of provider
// (no decider, no model) and fails the test on error.
func runScratchTurn(t *testing.T, r *Runner, task store.Task, provider Provider) {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, provider, "hi", nil, PermissionSettings{}, "", "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
}

// assertScratchTmpdir checks the env-all the agent dumped in the worktree
// points TMPDIR at the task's scratch dir (FAKE_DUMP_ENV=1 makes the fake
// agents write it at session creation).
func assertScratchTmpdir(t *testing.T, r *Runner, task store.Task) {
	t.Helper()
	dir := r.scratch.Dir(task.ID)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("scratch dir %s missing or not a directory: %v", dir, err)
	}
	if got := lastEnvEntry(t, readLaunchEnv(t, task), "TMPDIR"); got != "TMPDIR="+dir {
		t.Fatalf("TMPDIR = %q, want the scratch dir %q", got, dir)
	}
}

// AC1, codex-native: thread/start's cwd dump shows the scratch TMPDIR.
func TestLaunchEnv_ScratchTmpdir_Codex(t *testing.T) {
	t.Setenv("FAKE_DUMP_ENV", "1")
	wm, task := newTestTask(t, "")
	r := codexRunner(wm)
	WithScratch(scratch.New(t.TempDir()))(r)

	runScratchTurn(t, r, task, ProviderCodexNative)
	assertScratchTmpdir(t, r, task)
}

// AC1, ACP: the same launch env reaches any ACP-speaking provider
// (ProviderKimi here; runACP is provider-agnostic past the command).
func TestLaunchEnv_ScratchTmpdir_ACP(t *testing.T) {
	t.Setenv("FAKE_DUMP_ENV", "1")
	wm, task := newTestTask(t, "")
	r := glmRunner(wm)
	WithScratch(scratch.New(t.TempDir()))(r)

	runScratchTurn(t, r, task, ProviderKimi)
	assertScratchTmpdir(t, r, task)
}

// AC1, GLM: driven through the real acpCommands lookup (WithACPCommand,
// not the newACPClient seam), so the command spawn itself carries the env.
func TestLaunchEnv_ScratchTmpdir_GLM(t *testing.T) {
	t.Setenv("FAKE_DUMP_ENV", "1")
	wm, task := newTestTask(t, "")
	r := New(wm, WithACPCommand(ProviderGLM, []string{fakeACPAgentPath}))
	WithScratch(scratch.New(t.TempDir()))(r)

	runScratchTurn(t, r, task, ProviderGLM)
	assertScratchTmpdir(t, r, task)
}
