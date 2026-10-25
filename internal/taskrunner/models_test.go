package taskrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/workspace"
)

func TestModel_ValidationRejectsBadStrings(t *testing.T) {
	t.Parallel()
	for name, bad := range map[string]string{
		"leading dash": "-x",
		"flag":         "--model",
		"space":        "claude opus",
		"newline":      "opus\n--evil",
		"too long":     strings.Repeat("a", 101),
		"semicolon":    "a;b",
		"quote":        `a"b`,
		"unicode":      "mödel",
	} {
		if err := ValidateModelSyntax(bad); err == nil {
			t.Errorf("%s: ValidateModelSyntax(%q) = nil, want error", name, bad)
		}
		claude, _ := ProviderInfoFor(ProviderClaudeNative)
		if err := ValidateModel(claude, bad); err == nil {
			t.Errorf("%s: ValidateModel(claude, %q) = nil, want error", name, bad)
		}
	}
	for _, ok := range []string{"", "sonnet", "claude-opus-5-5", "glm-4.6", "a/b:c@d[1m]", strings.Repeat("a", 100), "gpt_5.1-codex"} {
		if err := ValidateModelSyntax(ok); err != nil {
			t.Errorf("ValidateModelSyntax(%q) = %v, want nil", ok, err)
		}
	}
}

func TestProviderList_ClaudeStaticModels(t *testing.T) {
	t.Parallel()
	r := New(nil)
	p, ok := r.ProviderCatalogFor(ProviderClaudeNative)
	if !ok {
		t.Fatal("no claude-native provider")
	}
	if p.ModelsDiscovered {
		t.Fatal("claude catalog is static: modelsDiscovered must be false")
	}
	ids := map[string]bool{}
	for _, m := range p.Models {
		ids[m.ID] = true
	}
	for _, want := range []string{"sonnet", "opus", "haiku", "claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"} {
		if !ids[want] {
			t.Errorf("claude models missing %q: %+v", want, p.Models)
		}
	}
	// Not an allowlist: a model released after this build is accepted.
	if err := ValidateModel(p, "claude-future-9-9"); err != nil {
		t.Errorf("ValidateModel(claude, future id) = %v, want nil", err)
	}
}

func waitForDiscoveredModels(t *testing.T, r *Runner, provider Provider) ProviderInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if p, _ := r.ProviderCatalogFor(provider); p.ModelsDiscovered {
			return p
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s models never discovered", provider)
	return ProviderInfo{}
}

func TestProviderList_ACPModelsFromProbe(t *testing.T) {
	t.Parallel()
	r := New(nil, WithACPModeProbe(), WithACPCommand(ProviderGLM, []string{fakeACPAgentPath, "models"}), WithACPCommand(ProviderKimi, []string{"/nonexistent/kimi"}))
	p := waitForDiscoveredModels(t, r, ProviderGLM)
	if len(p.Models) != 2 || p.Models[0].ID != "fake-model-a" || !p.Models[0].Default || p.Models[1].ID != "fake-model-b" || p.Models[1].Default {
		t.Fatalf("glm models = %+v, want fake-model-a (default), fake-model-b", p.Models)
	}
	c, _ := r.acpModelCatalog(ProviderGLM)
	if c.configID != "model" {
		t.Fatalf("model configID = %q, want model", c.configID)
	}
	if err := ValidateModel(p, "fake-model-b"); err != nil {
		t.Errorf("ValidateModel(known) = %v", err)
	}
	err := ValidateModel(p, "nope")
	if err == nil || !strings.Contains(err.Error(), "fake-model-a, fake-model-b") {
		t.Errorf("ValidateModel(unknown) = %v, want an error listing the valid ids", err)
	}
}

func TestProviderList_UndiscoveredAcceptsString(t *testing.T) {
	t.Parallel()
	// No probe enabled: nothing is discovered for any provider.
	r := New(nil)
	for _, id := range []Provider{ProviderGLM, ProviderKimi, ProviderCodexNative} {
		p, _ := r.ProviderCatalogFor(id)
		if p.ModelsDiscovered || len(p.Models) != 0 {
			t.Fatalf("%s catalog = %+v, want no discovered models", id, p)
		}
		if err := ValidateModel(p, "whatever-the-agent-knows"); err != nil {
			t.Errorf("%s: ValidateModel(undiscovered) = %v, want nil", id, err)
		}
		if err := ValidateModel(p, "-bad"); err == nil {
			t.Errorf("%s: syntax is still checked when undiscovered", id)
		}
	}
}

func TestProviderList_CodexModelsFromModelList(t *testing.T) {
	t.Parallel()
	r := New(nil, WithACPModeProbe(), WithCodexCommand([]string{fakeCodexAgentPath}), WithACPCommand(ProviderGLM, []string{"/nonexistent/glm"}), WithACPCommand(ProviderKimi, []string{"/nonexistent/kimi"}))
	p := waitForDiscoveredModels(t, r, ProviderCodexNative)
	var ids []string
	for _, m := range p.Models {
		ids = append(ids, m.ID)
	}
	// Both pages, hidden model skipped.
	if got := strings.Join(ids, ","); got != "gpt-fake-a,gpt-fake-b" {
		t.Fatalf("codex models = %s, want gpt-fake-a,gpt-fake-b", got)
	}
	if !p.Models[0].Default || p.Models[0].Label != "GPT Fake A" {
		t.Fatalf("codex first model = %+v", p.Models[0])
	}
	if err := ValidateModel(p, "gpt-unknown"); err == nil {
		t.Error("discovered codex catalog should reject an unknown id")
	}
}

// An app-server without model/list leaves the provider with no models and
// any syntax-valid string accepted.
func TestProviderList_CodexModelListUnavailableFallback(t *testing.T) {
	t.Parallel()
	r := New(nil, WithACPModeProbe(), WithCodexCommand([]string{fakeCodexAgentPath, "nomodellist"}), WithACPCommand(ProviderGLM, []string{"/nonexistent/glm"}), WithACPCommand(ProviderKimi, []string{"/nonexistent/kimi"}))
	p, _ := r.ProviderCatalogFor(ProviderCodexNative) // kicks the probe
	time.Sleep(500 * time.Millisecond)
	p, _ = r.ProviderCatalogFor(ProviderCodexNative)
	if p.ModelsDiscovered || len(p.Models) != 0 {
		t.Fatalf("catalog = %+v, want no models", p)
	}
	if err := ValidateModel(p, "gpt-anything"); err != nil {
		t.Fatalf("ValidateModel() = %v, want the string accepted", err)
	}
}

// runModelPrompt runs one RunPrompt to completion and returns its error.
func runModelPrompt(t *testing.T, r *Runner, taskID int64, provider Provider, model string) error {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), taskID, taskID, provider, "hi", nil, PermissionSettings{}, "", model, events)
	}()
	drainEvents(events)
	return <-errCh
}

func glmRunnerArgs(wm *workspace.Manager, args ...string) *Runner {
	r := New(wm)
	r.newACPClient = func(_ []string, opts ...acp.Option) (acpBackend, error) {
		return acp.New(append([]string{fakeACPAgentPath}, args...), opts...)
	}
	return r
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.TrimSpace(string(data))
}

func TestRunClaudeNative_WithModelOption(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		model, want string
	}{
		{"claude-opus-5-5", "claude-opus-5-5"},
		{"sonnet", "sonnet"},
		{ClaudeModelDefault, ""}, // the synthetic default means no flag
	} {
		t.Run("model="+tc.model, func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "echo-args")
			r := claudeNativeRunner(t, wm)
			if err := runModelPrompt(t, r, task.ID, ProviderClaudeNative, tc.model); err != nil {
				t.Fatalf("RunPrompt() error = %v", err)
			}
			args := strings.Split(readArgsFile(t, *task.WorktreePath), "\n")
			got, has := flagValue(args, "--model")
			if tc.want == "" {
				if has {
					t.Fatalf("--model present for model %q: %q", tc.model, args)
				}
				return
			}
			if !has || got != tc.want {
				t.Fatalf("--model = %q (present %v), want %q; args %q", got, has, tc.want, args)
			}
		})
	}
}

func TestRunACP_AppliesModelConfigOption(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, model, wantLog string
	}{
		{"switches", "fake-model-b", "set_config_option:model:fake-model-b"},
		{"already current", "fake-model-a", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "reply")
			r := glmRunnerArgs(wm, "models")
			if err := runModelPrompt(t, r, task.ID, ProviderGLM, tc.model); err != nil {
				t.Fatalf("RunPrompt() error = %v", err)
			}
			if got := readLog(t, filepath.Join(*task.WorktreePath, "session-model")); got != tc.wantLog {
				t.Fatalf("session-model log = %q, want %q", got, tc.wantLog)
			}
		})
	}
}

// The agent's own rejection of an (undiscovered) model fails the run
// start, and the prompt never runs.
func TestRunACP_ModelRejectedFailsStart(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "reply")
	r := glmRunnerArgs(wm, "models")
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", "bad-model", events)
	}()
	got := drainEvents(events)
	err := <-errCh
	if err == nil || !strings.Contains(err.Error(), "unsupported model: bad-model") {
		t.Fatalf("RunPrompt() error = %v, want the agent's own rejection", err)
	}
	if texts := eventTexts(got); len(texts) != 0 {
		t.Fatalf("prompt ran despite model rejection: %v", texts)
	}
}

// Once a provider's models are discovered, an unknown id is rejected up
// front (no spawn), listing the valid ids.
func TestRunACP_UnknownModelRejectedWhenDiscovered(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "reply")
	r := glmRunnerArgs(wm, "models")
	// A first run discovers the catalog from its session/new.
	if err := runModelPrompt(t, r, task.ID, ProviderGLM, ""); err != nil {
		t.Fatalf("discovery run error = %v", err)
	}
	spawned := false
	inner := r.newACPClient
	r.newACPClient = func(cmd []string, opts ...acp.Option) (acpBackend, error) {
		spawned = true
		return inner(cmd, opts...)
	}
	err := runModelPrompt(t, r, task.ID, ProviderGLM, "nope")
	if err == nil || !strings.Contains(err.Error(), "fake-model-a, fake-model-b") {
		t.Fatalf("RunPrompt() error = %v, want an invalid-model error listing valid ids", err)
	}
	if spawned {
		t.Fatal("an agent was spawned for a model rejected up front")
	}
}

func TestRunCodex_ModelInThreadStart(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "reply")
	r := codexRunner(wm)
	if err := runModelPrompt(t, r, task.ID, ProviderCodexNative, "gpt-fake-b"); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if got := readLog(t, filepath.Join(*task.WorktreePath, "thread-model")); got != "gpt-fake-b" {
		t.Fatalf("thread/start model = %q, want gpt-fake-b", got)
	}
	// A resumed thread (second run on the same chat) carries it too.
	if err := runModelPrompt(t, r, task.ID, ProviderCodexNative, "gpt-fake-a"); err != nil {
		t.Fatalf("second RunPrompt() error = %v", err)
	}
	if got := readLog(t, filepath.Join(*task.WorktreePath, "thread-model")); got != "gpt-fake-a" {
		t.Fatalf("thread/resume model = %q, want gpt-fake-a", got)
	}
}

// An empty model changes nothing about an ACP or Codex spawn: no
// set_config_option for the model, no `model` in thread/start. A claude launch
// with none is refused (TestUnpinnedClaude_*), never started unpinned.
func TestRunStart_ModelEmptyIsDefault(t *testing.T) {
	t.Parallel()
	t.Run("claude", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "echo-args")
		var nm *NoModelError
		if err := runModelPrompt(t, claudeNativeRunner(t, wm), task.ID, ProviderClaudeNative, ""); !errors.As(err, &nm) {
			t.Fatalf("RunPrompt() error = %v, want *NoModelError", err)
		}
		if _, err := os.Stat(filepath.Join(*task.WorktreePath, "args")); err == nil {
			t.Fatal("the CLI was spawned for a refused run")
		}
	})
	t.Run("acp", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "reply")
		if err := runModelPrompt(t, glmRunnerArgs(wm, "models"), task.ID, ProviderGLM, ""); err != nil {
			t.Fatalf("RunPrompt() error = %v", err)
		}
		if got := readLog(t, filepath.Join(*task.WorktreePath, "session-model")); got != "" {
			t.Fatalf("session-model log = %q, want none", got)
		}
	})
	t.Run("codex", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "reply")
		if err := runModelPrompt(t, codexRunner(wm), task.ID, ProviderCodexNative, ""); err != nil {
			t.Fatalf("RunPrompt() error = %v", err)
		}
		if got := readLog(t, filepath.Join(*task.WorktreePath, "thread-model")); got != "" {
			t.Fatalf("thread/start model = %q, want none", got)
		}
	})
}

func TestConfigOptions_ClaudeSyntheticModelOption(t *testing.T) {
	t.Parallel()
	r := New(nil)

	// No live session: nothing to switch, nothing listed.
	if got := r.ConfigOptions(7, ProviderClaudeNative); len(got) != 0 {
		t.Fatalf("ConfigOptions() with no live session = %+v, want none", got)
	}

	r.trackClaudeClient(7, &fakeClaudeModeClient{})
	r.setClaudeModel(7, "claude-opus-5-5")
	got := r.ConfigOptions(7, ProviderClaudeNative)
	if len(got) != 1 {
		t.Fatalf("ConfigOptions() = %+v, want exactly the synthetic model option", got)
	}
	o := got[0]
	if o.ConfigID != "model" || o.Category != "model" || o.Type != "select" {
		t.Fatalf("option = %+v, want configId/category model, select", o)
	}
	if cur := configCurrentValue(o.CurrentValue); cur != "claude-opus-5-5" {
		t.Fatalf("currentValue = %q, want claude-opus-5-5", cur)
	}
	var ids []string
	for _, c := range o.Options {
		ids = append(ids, c.Value)
	}
	for _, want := range []string{ClaudeModelDefault, "sonnet", "opus", "haiku", "claude-opus-5-5"} {
		found := false
		for _, id := range ids {
			found = found || id == want
		}
		if !found {
			t.Errorf("choices %v missing %q", ids, want)
		}
	}

	// An unset model reports the default choice as current.
	r.setClaudeModel(7, "")
	if cur := configCurrentValue(r.ConfigOptions(7, ProviderClaudeNative)[0].CurrentValue); cur != ClaudeModelDefault {
		t.Fatalf("currentValue with no model = %q, want %q", cur, ClaudeModelDefault)
	}

	// The turn ending removes it again.
	r.untrackClaudeClient(7)
	if got := r.ConfigOptions(7, ProviderClaudeNative); len(got) != 0 {
		t.Fatalf("ConfigOptions() after the turn = %+v, want none", got)
	}
}

func TestSetConfigOption_ClaudeModelCallsSetModel(t *testing.T) {
	t.Parallel()
	r := New(nil)
	fake := &fakeClaudeModeClient{}
	r.trackClaudeClient(7, fake)

	opts, err := r.SetSessionConfigOption(context.Background(), 7, ProviderClaudeNative, "model", "sonnet")
	if err != nil {
		t.Fatalf("SetSessionConfigOption(model=sonnet) error = %v", err)
	}
	if len(fake.modelCalls) != 1 || fake.modelCalls[0] == nil || *fake.modelCalls[0] != "sonnet" {
		t.Fatalf("SetModel calls = %v, want one call with sonnet", fake.modelCalls)
	}
	if len(opts) != 1 || configCurrentValue(opts[0].CurrentValue) != "sonnet" {
		t.Fatalf("returned options = %+v, want current sonnet", opts)
	}
	if cur := configCurrentValue(r.ConfigOptions(7, ProviderClaudeNative)[0].CurrentValue); cur != "sonnet" {
		t.Fatalf("listed currentValue = %q, want sonnet", cur)
	}

	// The catalog default (and empty) select the configured default, never a
	// reset to the CLI's own (SetModel(nil)); with none configured they are
	// refused and the client is not called.
	for _, v := range []string{ClaudeModelDefault, ""} {
		fake.modelCalls = nil
		var nm *NoModelError
		if _, err := r.SetSessionConfigOption(context.Background(), 7, ProviderClaudeNative, "model", v); !errors.As(err, &nm) {
			t.Fatalf("SetSessionConfigOption(model=%q) with no default error = %v, want *NoModelError", v, err)
		}
		if len(fake.modelCalls) != 0 {
			t.Fatalf("SetModel called for an unresolvable default: %v", fake.modelCalls)
		}
	}
	r.SetDefaultModel(ProviderClaudeNative, "claude-haiku-5-5")
	for _, v := range []string{ClaudeModelDefault, ""} {
		fake.modelCalls = nil
		if _, err := r.SetSessionConfigOption(context.Background(), 7, ProviderClaudeNative, "model", v); err != nil {
			t.Fatalf("SetSessionConfigOption(model=%q) error = %v", v, err)
		}
		if len(fake.modelCalls) != 1 || fake.modelCalls[0] == nil || *fake.modelCalls[0] != "claude-haiku-5-5" {
			t.Fatalf("SetModel calls for %q = %v, want one call with the configured default", v, fake.modelCalls)
		}
	}

	// Bad syntax never reaches the client; neither does another option id.
	fake.modelCalls = nil
	if _, err := r.SetSessionConfigOption(context.Background(), 7, ProviderClaudeNative, "model", "--evil"); err == nil {
		t.Error("model=--evil accepted")
	}
	if _, err := r.SetSessionConfigOption(context.Background(), 7, ProviderClaudeNative, "thinking-level", "low"); err == nil {
		t.Error("unknown claude config id accepted")
	}
	if len(fake.modelCalls) != 0 {
		t.Fatalf("SetModel called for rejected input: %v", fake.modelCalls)
	}

	// No live session: an error, not a silent no-op.
	r.untrackClaudeClient(7)
	if _, err := r.SetSessionConfigOption(context.Background(), 7, ProviderClaudeNative, "model", "opus"); err == nil {
		t.Error("model switch with no live session = nil error")
	}
}

func TestSetConfigOption_CodexModelRejected(t *testing.T) {
	t.Parallel()
	r := New(nil)
	if got := r.ConfigOptions(1, ProviderCodexNative); len(got) != 0 {
		t.Fatalf("codex ConfigOptions() = %+v, want none (no synthetic option)", got)
	}
	_, err := r.SetSessionConfigOption(context.Background(), 1, ProviderCodexNative, "model", "gpt-fake-a")
	if err == nil || !strings.Contains(err.Error(), "applies from the next run: pass `model` to task_send") {
		t.Fatalf("SetSessionConfigOption(codex model) = %v, want the next-run refusal", err)
	}
	// Other options keep today's not-supported answer.
	if _, err := r.SetSessionConfigOption(context.Background(), 1, ProviderCodexNative, "thinking-level", "low"); !errors.Is(err, ErrConfigOptionsNotSupported) {
		t.Fatalf("codex thinking-level err = %v, want ErrConfigOptionsNotSupported", err)
	}
}
