package taskrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/providers"
)

// ReplayACPSession: the agent is started like a run's, asked to load, and always
// closed; nothing but initialize and session/load is sent.

const replayJSON = `[
 {"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"first"}},
 {"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}},
 {"sessionUpdate":"tool_call","toolCallId":"t1","kind":"execute","title":"ls","status":"completed"},
 {"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"second"}},
 {"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"bye"}}
]`

type closeCounter struct {
	acpBackend
	closed *atomic.Int32
}

func (c closeCounter) Close() error { c.closed.Add(1); return c.acpBackend.Close() }

func replayRunner(caps string, closed *atomic.Int32) *Runner {
	r := New(nil, WithACPCommand(ProviderGLM, []string{fakeACPAgentPath, caps}))
	inner := r.newACPClient
	r.newACPClient = func(cmd []string, opts ...acp.Option) (acpBackend, error) {
		c, err := inner(cmd, opts...)
		if err != nil {
			return nil, err
		}
		return closeCounter{c, closed}, nil
	}
	return r
}

func replayDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "replay.json"), []byte(replayJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "methods.log"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReplayACPSessionCollectsTheReplayAndSendsNoPrompt(t *testing.T) {
	var closed atomic.Int32
	r := replayRunner("loadSession", &closed)
	dir := replayDir(t)
	evs, err := r.ReplayACPSession(context.Background(), ProviderGLM, "fake-session-1", dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 5 || evs[0].Type != EventTypeUserMessage || evs[0].Text != "first" || evs[2].Type != EventTypeToolCall || evs[2].ToolCallID != "t1" || evs[4].Text != "bye" {
		t.Fatalf("events: %+v", evs)
	}
	if closed.Load() != 1 {
		t.Fatalf("the agent was closed %d times", closed.Load())
	}
	log, _ := os.ReadFile(filepath.Join(dir, "methods.log"))
	for _, bad := range []string{"session/prompt", "session/new", "session/set_mode", "session/set_config_option", "session/resume"} {
		if strings.Contains(string(log), bad) {
			t.Fatalf("the replay sent %s: %s", bad, log)
		}
	}
	if !strings.Contains(string(log), "session/load") {
		t.Fatalf("no session/load in %q", log)
	}
}

func TestReplayACPSessionRefusesWithoutLoadSession(t *testing.T) {
	var closed atomic.Int32
	r := replayRunner("resume", &closed) // advertises resume only
	dir := replayDir(t)
	_, err := r.ReplayACPSession(context.Background(), ProviderGLM, "fake-session-1", dir, 1<<20)
	if !errors.Is(err, ErrACPNoLoadSession) {
		t.Fatalf("err = %v", err)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "methods.log"))
	if strings.Contains(string(log), "session/load") || closed.Load() != 1 {
		t.Fatalf("session/load was called without the capability (log %q) or the agent was left open (%d closes)", log, closed.Load())
	}
}

func TestReplayACPSessionErrors(t *testing.T) {
	var closed atomic.Int32
	r := replayRunner("loadSession", &closed)
	if _, err := r.ReplayACPSession(context.Background(), ProviderGLM, "nope", replayDir(t), 1<<20); !errors.Is(err, ErrACPSessionUnknown) {
		t.Fatalf("unknown session: %v", err)
	}
	if _, err := r.ReplayACPSession(context.Background(), ProviderGLM, "fake-session-1", replayDir(t), 50); !errors.Is(err, ErrACPReplayTooLarge) {
		t.Fatalf("over the byte bound: %v", err)
	}
	if closed.Load() != 2 {
		t.Fatalf("closes = %d, want one per call", closed.Load())
	}
	// a cancelled context ends the call and still closes the agent
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	closed.Store(0)
	if _, err := r.ReplayACPSession(ctx, ProviderGLM, "fake-session-1", replayDir(t), 1<<20); err == nil {
		t.Fatal("a cancelled call succeeded")
	}
	if closed.Load() != 1 {
		t.Fatalf("cancelled call closes = %d", closed.Load())
	}
	if _, err := New(nil).ReplayACPSession(context.Background(), ProviderGLM, "s", t.TempDir(), 1); err == nil {
		t.Fatal("a provider with no command must fail")
	}
}

// The agent gets the provider's own launch environment, as a run's does (the
// configured values reach it; the probe's NO_BROWSER default too).
func TestReplayACPSessionUsesTheProvidersLaunchEnv(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "env-dump")
	t.Setenv("FAKEAGENT_ENV_DUMP", dump)
	r, _ := customRunner(t, nil, providers.Spec{
		ID: "replay-agent", Command: []string{fakeACPAgentPath, "loadSession"},
		Env: map[string]string{"REPLAY_TEST_VAR": "from-provider"},
	})
	if _, err := r.ReplayACPSession(context.Background(), "replay-agent", "fake-session-1", replayDir(t), 1<<20); err != nil {
		t.Fatal(err)
	}
	env := readAgentEnvDump(t, dump)
	if env["REPLAY_TEST_VAR"] != "from-provider" {
		t.Fatalf("the provider's env did not reach the agent: %v", env["REPLAY_TEST_VAR"])
	}
}
