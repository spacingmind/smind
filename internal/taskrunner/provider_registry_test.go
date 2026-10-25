package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// customRunner is a Runner over its own registry holding specs, spawning real
// processes from the registry's commands (no client seam).
func customRunner(t *testing.T, wm *workspace.Manager, specs ...providers.Spec) (*Runner, *providers.Registry) {
	t.Helper()
	reg := providers.New()
	if err := reg.SetCustom(specs); err != nil {
		t.Fatal(err)
	}
	return New(wm, WithProviderRegistry(reg)), reg
}

func runPlain(t *testing.T, r *Runner, task store.Task, provider Provider, perm PermissionSettings, model string) []Event {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, provider, "hi", denyAllDecider{}, perm, "", model, events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt(%s) error = %v", provider, err)
	}
	return got
}

func TestCustomProvider_RunsViaACP_UsageAndEvents(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "usage in=100 out=20")
	r, _ := customRunner(t, wm, providers.Spec{
		ID: "my-agent", Label: "My agent", Command: []string{fakeACPAgentPath},
		Env: map[string]string{"FAKE_CUSTOM_VAR": "from-config"},
	})
	got := runPlain(t, r, task, "my-agent", PermissionSettings{}, "")

	var text, usage, done int
	for _, e := range got {
		switch e.Type {
		case EventTypeText:
			text++
		case EventTypeUsage:
			usage++
			if e.Usage == nil || e.Usage.Input == nil || *e.Usage.Input != 100 || *e.Usage.Output != 20 {
				t.Fatalf("usage = %+v, want in=100 out=20", e.Usage)
			}
		case EventTypeDone:
			done++
		}
	}
	if text == 0 || usage != 1 || done != 1 {
		t.Fatalf("events text=%d usage=%d done=%d: %+v", text, usage, done, got)
	}
	// The configured command environment reached the agent process.
	b, err := os.ReadFile(filepath.Join(*task.WorktreePath, "env-fake-custom"))
	if err != nil || string(b) != "from-config" {
		t.Fatalf("agent env FAKE_CUSTOM_VAR = %q, %v", b, err)
	}
}

func TestCustomProvider_ModelSelectionAndModes(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r, _ := customRunner(t, wm, providers.Spec{
		ID: "my-agent", Command: []string{fakeACPAgentPath, "models", "modes:session"},
		Models:      []providers.Model{{ID: "fake-model-a", Label: "A", Default: true}, {ID: "fake-model-b", Label: "B"}},
		Modes:       []providers.Mode{{ID: "plan", Label: "Plan"}, {ID: "yolo", Label: "Yolo", AutoApproves: true}},
		DefaultMode: "plan",
	})

	// The static catalogs are served before any discovery.
	cat, ok := r.ProviderCatalogFor("my-agent")
	if !ok || !cat.Custom || cat.Runner != "acp" || cat.DefaultMode != "plan" || len(cat.Modes) != 2 || len(cat.Models) != 2 {
		t.Fatalf("catalog = %+v", cat)
	}
	if !ModeAutoApproves(cat, PermissionSettings{Mode: "yolo"}) || ModeAutoApproves(cat, PermissionSettings{Mode: "plan"}) {
		t.Fatal("custom mode AutoApproves flag not honoured")
	}
	if err := ValidatePermissionSettings(cat, PermissionSettings{AutoAccept: true}); err != nil {
		t.Fatalf("custom ACP providers take autoAccept: %v", err)
	}

	// A run applies the chosen model and mode on the agent.
	runPlain(t, r, task, "my-agent", PermissionSettings{Mode: "accept_edits"}, "fake-model-b")
	if got := readLog(t, filepath.Join(*task.WorktreePath, "session-model")); !strings.Contains(got, "set_config_option:model:fake-model-b") {
		t.Fatalf("agent model log = %q", got)
	}
	if got := readLog(t, filepath.Join(*task.WorktreePath, "session-mode")); !strings.Contains(got, "accept_edits") {
		t.Fatalf("agent mode log = %q", got)
	}
}

func claudeInstanceRunner(t *testing.T, wm *workspace.Manager, specs ...providers.Spec) *Runner {
	t.Helper()
	r := claudeNativeRunner(t, wm)
	reg := providers.New()
	if err := reg.SetCustom(specs); err != nil {
		t.Fatal(err)
	}
	r.registry = reg
	return r
}

func newScenarioTask(t *testing.T, wm *workspace.Manager) store.Task {
	t.Helper()
	task := newTestTaskUnder(t, wm)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("echo-args"), 0o644); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestProviderInstance_ExtendsClaudeNative_OwnConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	_, wm, _ := newTestStoreAndManager(t)
	r := claudeInstanceRunner(t, wm, providers.Spec{ID: "claude-s", Label: "Claude S", Extends: providers.IDClaudeNative,
		Env: map[string]string{"CLAUDE_CONFIG_DIR": "/inst/claude-s"}})
	task := newScenarioTask(t, wm)

	runPlain(t, r, task, "claude-s", PermissionSettings{}, "")
	if got := readEnvDump(t, *task.WorktreePath); got != "/inst/claude-s" {
		t.Fatalf("instance CLI CLAUDE_CONFIG_DIR = %q", got)
	}
	// The session handle belongs to the instance, so the base never resumes it.
	if h, ok := r.sessionStore.Get(task.ID); !ok || h.Provider != "claude-s" {
		t.Fatalf("session handle = %+v, %v", h, ok)
	}
	// Same code, same catalog: an instance has Claude's modes and models.
	cat, _ := r.ProviderCatalogFor("claude-s")
	if cat.Extends != providers.IDClaudeNative || cat.Runner != "claude-native" || cat.DefaultMode != ClaudeModeAcceptEdits || len(cat.Models) == 0 || !cat.Custom {
		t.Fatalf("instance catalog = %+v", cat)
	}
	if cat.AccountProvider != "" || cat.CredentialKind != "" {
		t.Fatalf("an instance must not borrow the base's smind account row: %+v", cat)
	}
}

func TestProviderInstance_SideBySideWithBase(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	_, wm, _ := newTestStoreAndManager(t)
	r := claudeInstanceRunner(t, wm,
		providers.Spec{ID: "claude-a", Extends: providers.IDClaudeNative, Env: map[string]string{"CLAUDE_CONFIG_DIR": "/inst/a"}},
		providers.Spec{ID: "claude-b", Extends: providers.IDClaudeNative, Env: map[string]string{"CLAUDE_CONFIG_DIR": "/inst/b"}},
	)
	r.SetProviderEnv(ProviderClaudeNative, "CLAUDE_CONFIG_DIR", "/base")

	want := map[Provider]string{ProviderClaudeNative: "/base", "claude-a": "/inst/a", "claude-b": "/inst/b"}
	for provider, dir := range want {
		task := newScenarioTask(t, wm)
		runPlain(t, r, task, provider, PermissionSettings{}, "")
		if got := readEnvDump(t, *task.WorktreePath); got != dir {
			t.Errorf("%s: CLAUDE_CONFIG_DIR = %q, want %q", provider, got, dir)
		}
	}
	// Setting the base's env does not leak into an instance and vice versa.
	if r.ProviderEnv("claude-a")["CLAUDE_CONFIG_DIR"] != "/inst/a" || r.ProviderEnv(ProviderClaudeNative)["CLAUDE_CONFIG_DIR"] != "/base" {
		t.Fatalf("env a=%v base=%v", r.ProviderEnv("claude-a"), r.ProviderEnv(ProviderClaudeNative))
	}
	var ids []string
	for _, p := range r.ProviderCatalog() {
		ids = append(ids, string(p.ID))
	}
	if got := strings.Join(ids, ","); got != "claude-native,glm,kimi,codex-native,claude-a,claude-b" {
		t.Fatalf("catalog ids = %s", got)
	}
}
