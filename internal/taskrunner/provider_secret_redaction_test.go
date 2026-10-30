package taskrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
)

// collectRedactedRun drives one turn of provider to completion and returns
// every event marshaled to JSON, every session-note text, and the run error
// -- the three surfaces a provider secret value must never reach
// (ADR-0046 section 4).
func collectRedactedRun(t *testing.T, r *Runner, task store.Task, provider Provider, model string) (events, notes []string, err error) {
	t.Helper()
	eventCh := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, provider, "hi", denyAllDecider{}, PermissionSettings{}, "", model, eventCh)
	}()
	for e := range eventCh {
		raw, merr := json.Marshal(e)
		if merr != nil {
			t.Fatalf("marshal event %+v: %v", e, merr)
		}
		events = append(events, string(raw))
		if e.Type == EventTypeSessionNote {
			notes = append(notes, e.Text)
		}
	}
	return events, notes, <-errCh
}

// assertNoSentinel fails when sentinel (a stored provider secret value)
// surfaces anywhere in the run's observable output.
func assertNoSentinel(t *testing.T, sentinel string, parts ...string) {
	t.Helper()
	all := strings.Join(parts, "\n")
	if strings.Contains(all, sentinel) {
		t.Fatalf("provider secret value leaked into the run's output:\n%s", all)
	}
}

// TestProviderSecret_RedactedInEventsAndLogs_Claude: a claude-native
// instance whose stored ANTHROPIC_AUTH_TOKEN is echoed back by the CLI on
// stderr (which then exits non-zero) must keep the value out of the run's
// events, session notes and error text; the redacted stderr tail carries
// the redaction placeholder instead.
func TestProviderSecret_RedactedInEventsAndLogs_Claude(t *testing.T) {
	// t.Setenv: the fake CLI inherits the test process's environment, and
	// the name (never the value) tells it what to echo.
	t.Setenv("FAKE_CLAUDE_STDERR_ECHO_ENV", "ANTHROPIC_AUTH_TOKEN")
	wm, task := newTestTask(t, "cli-exit-nonzero")
	r := claudeNativeRunner(t, wm)
	reg := providers.New()
	if err := reg.SetCustom([]providers.Spec{{
		ID: "zai", Extends: providers.IDClaudeNative,
		SecretEnv: map[string]string{"ANTHROPIC_AUTH_TOKEN": "zai-key"},
	}}); err != nil {
		t.Fatal(err)
	}
	WithProviderRegistry(reg)(r)
	WithProviderSecrets(&fakeSecretSource{secrets: map[string]string{"zai\x1fzai-key": zaiSentinel}})(r)

	events, notes, err := collectRedactedRun(t, r, task, "zai", testClaudeModel)
	if err == nil {
		t.Fatal("RunPrompt() = nil error, want the CLI-exit failure")
	}
	var cliExit *CLIExitError
	if !errors.As(err, &cliExit) {
		t.Fatalf("RunPrompt() error = %v, want a *CLIExitError", err)
	}
	// The CLI really had and echoed the token (the fake records what it
	// printed), so its absence below proves the scrub, not a missing launch.
	echoed, rerr := os.ReadFile(filepath.Join(*task.WorktreePath, "stderr-echoed"))
	if rerr != nil || string(echoed) != zaiSentinel {
		t.Fatalf("fake CLI echoed %q (%v), want the stored sentinel", echoed, rerr)
	}
	assertNoSentinel(t, zaiSentinel, append(append([]string{err.Error()}, events...), notes...)...)
	if !strings.Contains(cliExit.Stderr, "[REDACTED]") {
		t.Fatalf("stderr tail = %q, want the redaction placeholder", cliExit.Stderr)
	}
}

// TestProviderSecret_RedactedInEventsAndLogs_ACP: a custom ACP instance
// whose stored *_KEY secret is echoed back in the agent's session/new error
// must keep the value out of the run error, events and session notes; the
// error shows the [redacted] placeholder instead.
func TestProviderSecret_RedactedInEventsAndLogs_ACP(t *testing.T) {
	wm, task := newTestTask(t, "")
	reg := providers.New()
	if err := reg.SetCustom([]providers.Spec{{
		ID: "acme", Command: []string{fakeACPAgentPath},
		SecretEnv: map[string]string{"ACME_API_KEY": "acme-key"},
	}}); err != nil {
		t.Fatal(err)
	}
	r := New(wm, WithProviderRegistry(reg),
		WithProviderSecrets(&fakeSecretSource{secrets: map[string]string{"acme\x1facme-key": zaiSentinel}}))
	r.newACPClient = func([]string, ...acp.Option) (acpBackend, error) {
		return scrubStubACP{secret: zaiSentinel}, nil
	}

	events, notes, err := collectRedactedRun(t, r, task, "acme", "")
	if err == nil || !strings.Contains(err.Error(), "new failed with") {
		t.Fatalf("RunPrompt() = %v, want the new-session failure", err)
	}
	assertNoSentinel(t, zaiSentinel, append(append([]string{err.Error()}, events...), notes...)...)
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("run error = %v, want the [redacted] placeholder", err)
	}
}

// TestProviderSecret_ValuesHelper: providerSecretValues resolves the stored
// SecretEnv values and the PassthroughEnv names present in the daemon env,
// skipping empty and absent ones -- and without a secret source configured
// contributes only the passthrough values.
func TestProviderSecret_ValuesHelper(t *testing.T) {
	t.Setenv("SMIND_TEST_PRESENT", "passthrough-value")
	spec := providers.Spec{
		ID: "vals", Command: []string{"x"},
		SecretEnv:      map[string]string{"A_KEY": "sec-a", "B_KEY": "sec-empty", "C_KEY": "sec-missing"},
		PassthroughEnv: []string{"SMIND_TEST_ABSENT", "SMIND_TEST_PRESENT"},
	}
	r, _ := instanceRunner(t, spec)
	WithProviderSecrets(&fakeSecretSource{secrets: map[string]string{
		"vals\x1fsec-a":     "value-a",
		"vals\x1fsec-empty": "",
	}})(r)

	if got, want := r.providerSecretValues("vals"), []string{"value-a", "passthrough-value"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("providerSecretValues() = %q, want %q", got, want)
	}

	bare, _ := instanceRunner(t, spec)
	if got := bare.providerSecretValues("vals"); !reflect.DeepEqual(got, []string{"passthrough-value"}) {
		t.Fatalf("providerSecretValues() without a secret source = %q, want only the passthrough value", got)
	}
}

// captureDaemonLog redirects the stdlib log package's output (the daemon
// log) into a buffer for the rest of the test, restoring the previous
// writer and flags afterwards. Only for tests that do not run in
// parallel: the log's output writer is process-global.
func captureDaemonLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevWriter, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// TestProviderSecret_NotInDaemonLog_Run: the claude-native and the ACP
// runs of the redaction tests above must keep the stored secret value out
// of the daemon log (stdlib log output) as well, not just out of the run's
// events, notes and error. Not parallel: log output is process-global.
func TestProviderSecret_NotInDaemonLog_Run(t *testing.T) {
	t.Run("claude", func(t *testing.T) {
		t.Setenv("FAKE_CLAUDE_STDERR_ECHO_ENV", "ANTHROPIC_AUTH_TOKEN")
		wm, task := newTestTask(t, "cli-exit-nonzero")
		r := claudeNativeRunner(t, wm)
		reg := providers.New()
		if err := reg.SetCustom([]providers.Spec{{
			ID: "zai", Extends: providers.IDClaudeNative,
			SecretEnv: map[string]string{"ANTHROPIC_AUTH_TOKEN": "zai-key"},
		}}); err != nil {
			t.Fatal(err)
		}
		WithProviderRegistry(reg)(r)
		WithProviderSecrets(&fakeSecretSource{secrets: map[string]string{"zai\x1fzai-key": zaiSentinel}})(r)

		logs := captureDaemonLog(t)
		events, notes, err := collectRedactedRun(t, r, task, "zai", testClaudeModel)
		if err == nil {
			t.Fatal("RunPrompt() = nil error, want the CLI-exit failure")
		}
		assertNoSentinel(t, zaiSentinel, append(append([]string{logs.String(), err.Error()}, events...), notes...)...)
	})

	t.Run("acp", func(t *testing.T) {
		wm, task := newTestTask(t, "")
		reg := providers.New()
		if err := reg.SetCustom([]providers.Spec{{
			ID: "acme", Command: []string{fakeACPAgentPath},
			SecretEnv: map[string]string{"ACME_API_KEY": "acme-key"},
		}}); err != nil {
			t.Fatal(err)
		}
		r := New(wm, WithProviderRegistry(reg),
			WithProviderSecrets(&fakeSecretSource{secrets: map[string]string{"acme\x1facme-key": zaiSentinel}}))
		r.newACPClient = func([]string, ...acp.Option) (acpBackend, error) {
			return scrubStubACP{secret: zaiSentinel}, nil
		}

		logs := captureDaemonLog(t)
		events, notes, err := collectRedactedRun(t, r, task, "acme", "")
		if err == nil || !strings.Contains(err.Error(), "new failed with") {
			t.Fatalf("RunPrompt() = %v, want the new-session failure", err)
		}
		assertNoSentinel(t, zaiSentinel, append(append([]string{logs.String(), err.Error()}, events...), notes...)...)
	})
}

// TestProviderSecret_NotInDaemonLog_ModeProbe: the ACP mode probe spawns
// the agent with the secret in its launch environment, so an agent error
// echoing it back must reach neither the probe's returned error nor the
// daemon log line the caller writes that error to.
func TestProviderSecret_NotInDaemonLog_ModeProbe(t *testing.T) {
	reg := providers.New()
	if err := reg.SetCustom([]providers.Spec{{
		ID: "acme", Command: []string{fakeACPAgentPath},
		SecretEnv: map[string]string{"ACME_API_KEY": "acme-key"},
	}}); err != nil {
		t.Fatal(err)
	}
	r := New(nil, WithProviderRegistry(reg),
		WithProviderSecrets(&fakeSecretSource{secrets: map[string]string{"acme\x1facme-key": zaiSentinel}}))
	r.newACPClient = func([]string, ...acp.Option) (acpBackend, error) {
		return scrubStubACP{secret: zaiSentinel}, nil
	}

	logs := captureDaemonLog(t)
	err := r.probeACPModes(context.Background(), "acme")
	if err == nil || !strings.Contains(err.Error(), "session/new") {
		t.Fatalf("probeACPModes() = %v, want the new-session failure", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("probeACPModes() = %v, want the [redacted] placeholder", err)
	}
	assertNoSentinel(t, zaiSentinel, err.Error(), logs.String())
}
