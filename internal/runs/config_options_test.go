package runs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestRegistry_ConfigOptions_NotSupportedForNonACPProvider proves the
// config-option surface's documented non-ACP outcome (the plan's Test
// Scenarios): on a codex-native run, ListConfigOptions returns an empty
// list with no error, and SetConfigOption fails with a clear
// "not supported for this provider" error (taskrunner's sentinel) rather
// than attempting an ACP call or silently no-op'ing.
//
// The run is driven with a command path that cannot exist, so the turn
// fails fast and fully offline -- no real Codex CLI -- while still
// registering a genuine codex-native run (with runner and provider) in
// the Registry, which is all these calls read.
func TestRegistry_ConfigOptions_NotSupportedForNonACPProvider(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	reg := newTestRegistry(t, st)
	runner := taskrunner.New(wm, taskrunner.WithCodexCommand([]string{"/nonexistent/smind-test-codex-agent"}))

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderCodexNative, "hi", taskrunner.ApprovalPolicyManual)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusError, 5*time.Second)

	opts, err := reg.ListConfigOptions(runID)
	if err != nil {
		t.Fatalf("ListConfigOptions() error = %v, want nil (empty list, not an error)", err)
	}
	if len(opts) != 0 {
		t.Fatalf("ListConfigOptions() = %+v, want empty for a non-ACP provider", opts)
	}

	_, err = reg.SetConfigOption(context.Background(), runID, "thinking-level", "low")
	if err == nil {
		t.Fatal("SetConfigOption() error = nil, want a clear not-supported error")
	}
	if !errors.Is(err, taskrunner.ErrConfigOptionsNotSupported) {
		t.Fatalf("SetConfigOption() error = %v, want it to wrap taskrunner.ErrConfigOptionsNotSupported", err)
	}
}
