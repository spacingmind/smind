package taskrunner

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A failure to find a native claude.exe (Windows) must fail the turn at
// start with the actionable message, not hang or fall back to a .cmd shim.
func TestRunner_RunPrompt_ClaudeNative_LaunchErrorFailsTheTurn(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := claudeNativeRunner(t, wm)
	r.claudeLaunch = func() (string, []string, error) {
		return "", nil, errors.New("could not find a native claude.exe (tried: C:\\x)")
	}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", testClaudeModel, events)
	}()
	_ = drainEvents(events)

	err := <-errCh
	if err == nil {
		t.Fatal("RunPrompt() error = nil, want the launch error")
	}
	for _, want := range []string{"claude code cli", "could not find a native claude.exe"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestRunner_claudeLaunchSpec_NilMeansSDKDefaults(t *testing.T) {
	t.Parallel()
	var r Runner
	path, env, err := r.claudeLaunchSpec()
	if path != "" || env != nil || err != nil {
		t.Fatalf("claudeLaunchSpec() on a zero Runner = (%q, %v, %v), want empty", path, env, err)
	}
}
