package runs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/acp"
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

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderCodexNative, "hi", taskrunner.ApprovalPolicyManual, "")
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

// TestRegistry_ConfigOptions_GLM_RealSelectRoundTrip proves ListConfigOptions
// and SetConfigOption carry a real ACP select option's enumerated choices
// (fakeagent's scripted "thinking-level" option, mirroring GLM's real
// thinking-level tiers -- see internal/taskrunner/fakeagent/main.go) all
// the way from the agent through Runner/Registry, matching what the
// frontend's live-view control (task-move-approval-thinking.md's Item 3)
// needs to render real named choices instead of a bare id field.
//
// The "hang" scenario keeps the turn live (blocks after its first chunk)
// so SetConfigOption -- which requires a still-running session, see its
// own doc comment -- has something to reach; waitForHistoryLen's first
// recorded chunk is proof the session (and so its config options) already
// exists by the time these calls run.
func TestRegistry_ConfigOptions_GLM_RealSelectRoundTrip(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = reg.Stop(runID) })

	waitForHistoryLen(t, reg, runID, 1, 5*time.Second)

	opts, err := reg.ListConfigOptions(runID)
	if err != nil {
		t.Fatalf("ListConfigOptions() error = %v", err)
	}
	if len(opts) != 1 || opts[0].ConfigID != "thinking-level" {
		t.Fatalf("ListConfigOptions() = %+v, want the thinking-level select option", opts)
	}
	wantChoices := []acp.ConfigSelectOption{
		{Value: "minimal", Name: "Minimal"},
		{Value: "low", Name: "Low"},
		{Value: "medium", Name: "Medium"},
		{Value: "high", Name: "High"},
	}
	if len(opts[0].Options) != len(wantChoices) {
		t.Fatalf("ListConfigOptions()[0].Options = %+v, want %+v", opts[0].Options, wantChoices)
	}
	for i, want := range wantChoices {
		if opts[0].Options[i] != want {
			t.Errorf("ListConfigOptions()[0].Options[%d] = %+v, want %+v", i, opts[0].Options[i], want)
		}
	}

	set, err := reg.SetConfigOption(context.Background(), runID, "thinking-level", "high")
	if err != nil {
		t.Fatalf("SetConfigOption() error = %v", err)
	}
	if len(set) != 1 || string(set[0].CurrentValue) != `{"type":"id","value":"high"}` {
		t.Fatalf("SetConfigOption() = %+v, want currentValue reflecting the new value %q", set, "high")
	}
}
