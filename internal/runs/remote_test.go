package runs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestFleetRemoteTaskUnderCalleeFloorAndMode pins ADR-0048's callee-side
// admission rules for a prompt from a paired host: never AutoAccept, never
// an auto-approving mode, and the ADR-0047 floor plus standing rules see
// every permission request in every mode -- while a human-origin run keeps
// its AutoAccept shortcut.
func TestFleetRemoteTaskUnderCalleeFloorAndMode(t *testing.T) {
	t.Parallel()

	t.Run("autoaccept_forced_off_and_origin_recorded", func(t *testing.T) {
		t.Parallel()
		e := newQueuedTestEnv(t, "")
		out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{AutoAccept: true}, "", WhenBusyReject, store.ChatQueueSourceRemote, 0, 0, WithOriginHost("hostA"))
		if err != nil || out.Queued {
			t.Fatalf("StartWhenBusy = %+v, %v", out, err)
		}
		waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
		row, err := e.st.GetRun(out.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if row.AutoAccept {
			t.Fatal("stored run row AutoAccept = true, want forced off for a remote source")
		}
		if row.Origin != store.ChatQueueSourceRemote {
			t.Fatalf("row origin = %q, want %q", row.Origin, store.ChatQueueSourceRemote)
		}
		host, err := e.st.RunOrigin(out.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if host != "hostA" {
			t.Fatalf("RunOrigin = %q, want hostA", host)
		}
	})

	t.Run("auto_approving_mode_refused", func(t *testing.T) {
		t.Parallel()
		e := newQueuedTestEnv(t, "hang")
		runID := e.startRun(t, "hi")
		chatID := e.chatIDOf(t, runID)
		t.Cleanup(func() { _ = e.reg.Stop(runID) })

		_, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "x", taskrunner.PermissionSettings{Mode: "bypass_permissions"}, "", WhenBusyQueue, store.ChatQueueSourceRemote, 0, 0)
		if err == nil || !strings.Contains(err.Error(), "auto-approving") {
			t.Fatalf("remote bypass error = %v, want the non-human auto-approving rejection", err)
		}
		_, err = e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chatID, taskrunner.ProviderGLM, "x", taskrunner.PermissionSettings{AutoAccept: true}, "", WhenBusyQueue, store.ChatQueueSourceRemote, 0, 0)
		if err != nil {
			t.Fatalf("remote autoAccept error = %v, want admitted with AutoAccept forced off, not refused", err)
		}
	})

	t.Run("floor_applies_without_autoaccept_shortcut", func(t *testing.T) {
		t.Parallel()
		reg, r, runID, n := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
		r.mu.Lock()
		r.origin = store.ChatQueueSourceRemote
		r.perm.AutoAccept = true
		r.mu.Unlock()
		d := runPermissionDecider{reg: reg, r: r}

		// The floor denies git stash whatever any rule says; a remote run
		// must reach that deny even with AutoAccept set on the run.
		got, reason, err := d.DecideDetailedWithReason(context.Background(), "run Bash", bashDetail("git stash"), allowDenyOptions)
		if err != nil || got != "deny-1" {
			t.Fatalf("floor deny = %q, %v; want deny-1", got, err)
		}
		if !strings.Contains(reason, "no-stash") {
			t.Fatalf("reason = %q, want the floor id", reason)
		}
		_, resolved := permEvents(r)
		if len(resolved) != 1 || resolved[0].PermissionResolution != taskrunner.PermissionResolvedByPolicyDeny {
			t.Fatalf("resolved = %+v, want PermissionResolvedByPolicyDeny", resolved)
		}

		// A request no rule matches parks for the callee's human instead
		// of being auto-allowed.
		out, reqID := decideAsync(t, d, bashDetail("make build"))
		if pendingCount(r) != 1 || n.pending.Load() != 1 {
			t.Fatalf("pending = %d, notifications = %d; want the request parked for the local human", pendingCount(r), n.pending.Load())
		}
		if err := reg.RespondPermission(runID, reqID, "deny-1"); err != nil {
			t.Fatalf("RespondPermission: %v", err)
		}
		if got := <-out; got != "deny-1" {
			t.Fatalf("Decide = %q, want deny-1", got)
		}
		_, resolved = permEvents(r)
		for _, e := range resolved {
			if e.PermissionResolution == taskrunner.PermissionResolvedByAutoAccept {
				t.Fatalf("resolved = %+v; an AutoAccept resolution must never appear for a remote run", resolved)
			}
		}
	})

	t.Run("local_run_autoaccept_unchanged", func(t *testing.T) {
		t.Parallel()
		reg, r, _, _ := policyRun(t, taskrunner.PermissionSettings{AutoAccept: true}, goTestAllowPolicy, true)
		r.mu.Lock()
		r.origin = store.ChatQueueSourceHuman
		r.mu.Unlock()
		got, err := (runPermissionDecider{reg: reg, r: r}).DecideDetailed(context.Background(), "run Bash", bashDetail("git stash"), allowDenyOptions)
		if err != nil || got != "allow-1" {
			t.Fatalf("DecideDetailed = %q, %v; want allow-1", got, err)
		}
		_, resolved := permEvents(r)
		if len(resolved) != 1 || resolved[0].PermissionResolution != taskrunner.PermissionResolvedByAutoAccept {
			t.Fatalf("resolved = %+v, want the human run's AutoAccept shortcut", resolved)
		}
	})
}
