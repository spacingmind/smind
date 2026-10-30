package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
)

// Sentinel values, never real keys (the same rule the fake Anthropic spike
// in ADR-0046 section 7 followed).
const (
	zaiSentinel  = "sentinel-NOT-A-REAL-KEY-12345"
	passSentinel = "sentinel-PASS-NOT-REAL-45678"
)

// fakeSecretSource is a tiny in-test ProviderSecretSource backed by a map;
// PruneProviderSecrets records the keep list it was handed.
type fakeSecretSource struct {
	secrets map[string]string // instanceID + "\x1f" + secretName -> value
	keepLog [][]string
}

func (f *fakeSecretSource) GetProviderSecret(instanceID, name string) (string, bool, error) {
	v, ok := f.secrets[instanceID+"\x1f"+name]
	return v, ok, nil
}

func (f *fakeSecretSource) ListProviderSecrets(instanceID string) (map[string]store.ProviderSecretInfo, error) {
	out := map[string]store.ProviderSecretInfo{}
	for k, v := range f.secrets {
		if id, name, _ := strings.Cut(k, "\x1f"); id == instanceID {
			out[name] = store.ProviderSecretInfo{Fingerprint: store.SecretFingerprint(v)}
		}
	}
	return out, nil
}

func (f *fakeSecretSource) PruneProviderSecrets(keepInstanceIDs []string) (int, error) {
	keep := append([]string(nil), keepInstanceIDs...)
	sort.Strings(keep)
	f.keepLog = append(f.keepLog, keep)
	return 0, nil
}

// secretInstanceRunner wires a Runner (fake claude CLI, echo-args scenario)
// at one claude-code instance spec, with src as its secret store (nil: none).
func secretInstanceRunner(t *testing.T, spec providers.Spec, src ProviderSecretSource) (*Runner, store.Task) {
	t.Helper()
	r, _, task := newClaudeMcpEnv(t)
	reg := providers.New()
	if err := reg.SetCustom([]providers.Spec{spec}); err != nil {
		t.Fatal(err)
	}
	WithProviderRegistry(reg)(r)
	if src != nil {
		WithProviderSecrets(src)(r)
	}
	return r, task
}

// runInstance drives one turn of provider through the fake CLI.
func runInstance(t *testing.T, r *Runner, task store.Task, provider Provider) error {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, provider, "hi", denyAllDecider{}, PermissionSettings{}, "", testClaudeModel, events)
	}()
	drainEvents(events)
	return <-errCh
}

// readLaunchEnv returns the whole environment the fake CLI saw.
func readLaunchEnv(t *testing.T, task store.Task) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "env-all"))
	if err != nil {
		t.Fatalf("read env-all: %v", err)
	}
	return string(data)
}

// readLaunchArgv returns the argv the fake CLI recorded, one element per line.
func readLaunchArgv(t *testing.T, task store.Task) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "args"))
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	return strings.Split(string(data), "\n")
}

func TestProviderEnv_SecretResolvedLastWins(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "daemon-value")
	r, task := secretInstanceRunner(t,
		providers.Spec{ID: "zai", Extends: providers.IDClaudeNative,
			SecretEnv: map[string]string{"ANTHROPIC_AUTH_TOKEN": "zai-key"}},
		&fakeSecretSource{secrets: map[string]string{"zai\x1fzai-key": zaiSentinel}})

	entries, err := r.launchEnvEntries("zai")
	if err != nil {
		t.Fatalf("launchEnvEntries() error = %v", err)
	}
	if last := entries[len(entries)-1]; last != "ANTHROPIC_AUTH_TOKEN="+zaiSentinel {
		t.Fatalf("launchEnvEntries() = %v, want it to end with the resolved secret", entries)
	}
	if err := runInstance(t, r, task, "zai"); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if got := readLaunchEnv(t, task); !strings.Contains(got, "ANTHROPIC_AUTH_TOKEN="+zaiSentinel) {
		t.Fatalf("CLI environment lacks the resolved secret (last wins over the daemon env)")
	}
}

func TestProviderEnv_PassthroughCopiesDaemonEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", passSentinel)
	r, _ := secretInstanceRunner(t,
		providers.Spec{ID: "zai", Extends: providers.IDClaudeNative, PassthroughEnv: []string{"ANTHROPIC_API_KEY"}},
		&fakeSecretSource{})

	entries, err := r.launchEnvEntries("zai")
	if err != nil {
		t.Fatalf("launchEnvEntries() error = %v", err)
	}
	if len(entries) != 1 || entries[0] != "ANTHROPIC_API_KEY="+passSentinel {
		t.Fatalf("launchEnvEntries() = %v, want only the passthrough entry", entries)
	}

	os.Unsetenv("ANTHROPIC_API_KEY")
	entries, err = r.launchEnvEntries("zai")
	if err != nil {
		t.Fatalf("launchEnvEntries() after unset: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Fatalf("absent passthrough still produced %q in %v", e, entries)
		}
	}
}

func TestProviderEnv_NotInArgv(t *testing.T) {
	r, task := secretInstanceRunner(t,
		providers.Spec{ID: "zai", Extends: providers.IDClaudeNative,
			SecretEnv: map[string]string{"ANTHROPIC_AUTH_TOKEN": "zai-key"}},
		&fakeSecretSource{secrets: map[string]string{"zai\x1fzai-key": zaiSentinel}})

	if err := runInstance(t, r, task, "zai"); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if got := readLaunchEnv(t, task); !strings.Contains(got, "ANTHROPIC_AUTH_TOKEN="+zaiSentinel) {
		t.Fatal("CLI environment lacks the resolved secret")
	}
	for i, arg := range readLaunchArgv(t, task) {
		if strings.Contains(arg, zaiSentinel) {
			t.Fatalf("argv[%d] carries the secret value: %q", i, arg)
		}
	}
}

func TestProviderEnv_MissingSecretErrorNamesNotValue(t *testing.T) {
	r, task := secretInstanceRunner(t,
		providers.Spec{ID: "zai", Extends: providers.IDClaudeNative,
			SecretEnv: map[string]string{"ANTHROPIC_AUTH_TOKEN": "zai-key"}},
		&fakeSecretSource{}) // nothing stored

	_, err := r.launchEnvEntries("zai")
	if err == nil {
		t.Fatal("launchEnvEntries() = nil error, want the missing-secret error")
	}
	for _, want := range []string{"zai-key", "ANTHROPIC_AUTH_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q names neither the secret nor the env name: missing %q", err, want)
		}
	}

	if err := runInstance(t, r, task, "zai"); err == nil || !strings.Contains(err.Error(), "is not set") {
		t.Fatalf("RunPrompt() = %v, want the launch to fail with the missing-secret error", err)
	}
	if _, statErr := os.Stat(filepath.Join(*task.WorktreePath, "args")); statErr == nil {
		t.Fatal("the CLI was spawned although the launch failed")
	}
}

func TestScrubEnv_SetOnlyWhenPolicySaysSo(t *testing.T) {
	claude := providers.Spec{Runner: providers.RunnerClaude, SecretEnv: map[string]string{"ANTHROPIC_AUTH_TOKEN": "k"}}
	if got := scrubEnvEntries(claude, true); len(got) != 1 || got[0] != "CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1" {
		t.Fatalf("scrubEnvEntries(claude, on) = %v, want the scrub entry", got)
	}
	if got := scrubEnvEntries(claude, false); got != nil {
		t.Fatalf("scrubEnvEntries(policy off) = %v, want nil", got)
	}
	if got := scrubEnvEntries(providers.Spec{Runner: providers.RunnerClaude}, true); got != nil {
		t.Fatalf("scrubEnvEntries(no secret) = %v, want nil", got)
	}
	codex := providers.Spec{Runner: providers.RunnerCodex, SecretEnv: map[string]string{"OPENAI_API_KEY": "k"}}
	if got := scrubEnvEntries(codex, true); got != nil {
		t.Fatalf("scrubEnvEntries(codex harness) = %v, want nil (claude-code only)", got)
	}
	if got := scrubEnvEntries(providers.Spec{Runner: providers.RunnerClaude, PassthroughEnv: []string{"X"}}, true); len(got) != 1 {
		t.Fatalf("scrubEnvEntries(passthrough only) = %v, want the scrub entry", got)
	}
	if New(nil).scrubSubprocessEnv {
		t.Fatal("scrub on by default: ADR-0046 section 7 decided OFF (it forces the permission mode back to default)")
	}
}

func TestClaudeSettings_DenyRulesForSecretEnvNames(t *testing.T) {
	rules := denyList(t, claudeSettings(ClaudeModeAuto, "/srv/smind-home", []string{"ANTHROPIC_AUTH_TOKEN"}))
	for _, want := range []string{"Bash(echo $ANTHROPIC_AUTH_TOKEN*)", "Read(//proc/self/environ)"} {
		if !hasRule(rules, want) {
			t.Fatalf("missing %s in %v", want, rules)
		}
	}
	if hasRule(denyList(t, claudeSettings(ClaudeModeAuto, "/srv/smind-home", nil)), "Bash(printenv*)") {
		t.Fatal("nil names must not add the env-dumper rules")
	}
}

func TestRunner_PruneProviderSecretsKeepsCustomInstances(t *testing.T) {
	src := &fakeSecretSource{}
	r, _ := instanceRunner(t,
		providers.Spec{ID: "acp-x", Command: []string{"x"}},
		providers.Spec{ID: "inst", Extends: providers.IDClaudeNative})
	WithProviderSecrets(src)(r)

	if n, err := r.PruneProviderSecrets(); err != nil || n != 0 {
		t.Fatalf("PruneProviderSecrets() = %d, %v", n, err)
	}
	if len(src.keepLog) != 1 {
		t.Fatalf("prune ran %d times, want once", len(src.keepLog))
	}
	if got := strings.Join(src.keepLog[0], ","); got != "acp-x,inst" {
		t.Fatalf("keep list = %q, want only the custom specs (never the built-ins)", got)
	}
}
