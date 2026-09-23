package runs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestRegistry_SetApprovalPolicy_TakesEffectForSubsequentDecideCalls proves
// docs/plans/active/mid-run-approval-and-retry-effort.md's Item A core
// behavior: switching a live run's approvalPolicy changes what
// runPermissionDecider.Decide does with the *next* request it evaluates, in
// both directions (manual->auto-safe and back). Uses the "hang" fakeagent
// scenario so the run stays StatusRunning for the whole test (SetApprovalPolicy
// rejects a finished run -- see its own test below), and drives Decide
// directly (rather than through the hung subprocess, which never itself
// issues a permission request) the same way the existing AutoSafe decider
// tests already do.
func TestRegistry_SetApprovalPolicy_TakesEffectForSubsequentDecideCalls(t *testing.T) {
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

	reg.mu.Lock()
	r := reg.runs[runID]
	reg.mu.Unlock()
	if r == nil {
		t.Fatalf("run %q not found in registry", runID)
	}
	decider := runPermissionDecider{reg: reg, r: r}
	opts := []taskrunner.PermissionOption{
		{ID: "allow-1", Label: "Allow", Kind: "allow_once"},
		{ID: "deny-1", Label: "Deny", Kind: "reject_once"},
	}
	const cmd = "go test ./..."

	type result struct {
		optionID string
		err      error
	}
	decideInBackground := func() <-chan result {
		ch := make(chan result, 1)
		go func() {
			optionID, err := decider.Decide(context.Background(), "run "+cmd, cmd, opts)
			ch <- result{optionID, err}
		}()
		return ch
	}
	waitForPending := func() string {
		t.Helper()
		var requestID string
		deadline := time.Now().Add(2 * time.Second)
		for requestID == "" && time.Now().Before(deadline) {
			r.mu.Lock()
			for id := range r.pendingPermissions {
				requestID = id
			}
			r.mu.Unlock()
			if requestID == "" {
				time.Sleep(5 * time.Millisecond)
			}
		}
		if requestID == "" {
			t.Fatalf("Decide(%q) never became pending", cmd)
		}
		return requestID
	}

	// Under manual (Start's default), an allowlisted command still blocks
	// for a human -- proves the baseline before any switch happens.
	resCh := decideInBackground()
	requestID := waitForPending()
	if err := reg.RespondPermission(runID, requestID, "deny-1"); err != nil {
		t.Fatalf("RespondPermission() error = %v", err)
	}
	select {
	case res := <-resCh:
		if res.err != nil || res.optionID != "deny-1" {
			t.Fatalf("Decide() = (%q, %v), want (%q, nil)", res.optionID, res.err, "deny-1")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Decide() did not unblock after RespondPermission")
	}

	// Switch to auto-safe: the very next Decide call for the same
	// allowlisted command now auto-allows, with no pending state at all.
	if err := reg.SetApprovalPolicy(runID, taskrunner.ApprovalPolicyAutoSafe); err != nil {
		t.Fatalf("SetApprovalPolicy(auto-safe) error = %v", err)
	}
	optionID, err := decider.Decide(context.Background(), "run "+cmd, cmd, opts)
	if err != nil {
		t.Fatalf("Decide() after switch to auto-safe error = %v", err)
	}
	if optionID != "allow-1" {
		t.Fatalf("Decide() after switch to auto-safe = %q, want %q (auto-allowed)", optionID, "allow-1")
	}

	// Switch back to manual: auto-safe's allowlist stops applying to a new
	// request immediately.
	if err := reg.SetApprovalPolicy(runID, taskrunner.ApprovalPolicyManual); err != nil {
		t.Fatalf("SetApprovalPolicy(manual) error = %v", err)
	}
	resCh = decideInBackground()
	requestID = waitForPending()
	if err := reg.RespondPermission(runID, requestID, "allow-1"); err != nil {
		t.Fatalf("RespondPermission() error = %v", err)
	}
	select {
	case res := <-resCh:
		if res.err != nil || res.optionID != "allow-1" {
			t.Fatalf("Decide() = (%q, %v), want (%q, nil)", res.optionID, res.err, "allow-1")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Decide() did not unblock after RespondPermission")
	}
}

// TestRegistry_SetApprovalPolicy_SwitchDoesNotRetroactivelyAffectAlreadyPendingRequest
// proves the ordering guarantee the plan doc calls out explicitly: a
// request already waiting on a human decision when a mid-run switch happens
// keeps whatever behavior was already in flight (it was already past the
// auto-safe check, blocked in Decide's own select) -- only requests raised
// after the switch see the new policy.
func TestRegistry_SetApprovalPolicy_SwitchDoesNotRetroactivelyAffectAlreadyPendingRequest(t *testing.T) {
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

	reg.mu.Lock()
	r := reg.runs[runID]
	reg.mu.Unlock()
	if r == nil {
		t.Fatalf("run %q not found in registry", runID)
	}
	decider := runPermissionDecider{reg: reg, r: r}
	opts := []taskrunner.PermissionOption{
		{ID: "allow-1", Label: "Allow", Kind: "allow_once"},
		{ID: "deny-1", Label: "Deny", Kind: "reject_once"},
	}
	const cmd = "go test ./..."

	type result struct {
		optionID string
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		optionID, err := decider.Decide(context.Background(), "run "+cmd, cmd, opts)
		resultCh <- result{optionID, err}
	}()

	var requestID string
	deadline := time.Now().Add(2 * time.Second)
	for requestID == "" && time.Now().Before(deadline) {
		r.mu.Lock()
		for id := range r.pendingPermissions {
			requestID = id
		}
		r.mu.Unlock()
		if requestID == "" {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if requestID == "" {
		t.Fatalf("Decide(%q) never became pending", cmd)
	}

	if err := reg.SetApprovalPolicy(runID, taskrunner.ApprovalPolicyAutoSafe); err != nil {
		t.Fatalf("SetApprovalPolicy() error = %v", err)
	}

	select {
	case res := <-resultCh:
		t.Fatalf("Decide() resolved on its own after the switch (got %+v) -- an already-pending request must not be retroactively auto-resolved", res)
	case <-time.After(200 * time.Millisecond):
		// Still pending, as expected -- only a real human answer resolves it.
	}

	if err := reg.RespondPermission(runID, requestID, "deny-1"); err != nil {
		t.Fatalf("RespondPermission() error = %v", err)
	}
	select {
	case res := <-resultCh:
		if res.err != nil || res.optionID != "deny-1" {
			t.Fatalf("Decide() = (%q, %v), want (%q, nil)", res.optionID, res.err, "deny-1")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Decide() did not unblock after RespondPermission")
	}
}

// TestRegistry_SetApprovalPolicy_RejectsFullAccessAsTarget proves
// full-access is rejected as a live-switch target with a clear error, never
// silently ignored or applied -- entering/leaving it mid-run would require
// respawning the provider's client, out of scope for this plan.
func TestRegistry_SetApprovalPolicy_RejectsFullAccessAsTarget(t *testing.T) {
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

	if err := reg.SetApprovalPolicy(runID, taskrunner.ApprovalPolicyFullAccess); err == nil {
		t.Fatalf("SetApprovalPolicy(full-access) error = nil, want a clear rejection")
	}

	// Rejected as a no-op, not applied then rejected: the run's policy must
	// still read back as whatever it was before the attempt.
	reg.mu.Lock()
	r := reg.runs[runID]
	reg.mu.Unlock()
	r.mu.Lock()
	policy := r.approvalPolicy
	r.mu.Unlock()
	if policy != taskrunner.ApprovalPolicyManual {
		t.Fatalf("approvalPolicy after rejected full-access attempt = %q, want unchanged %q", policy, taskrunner.ApprovalPolicyManual)
	}
}

// TestRegistry_SetApprovalPolicy_RejectsInvalidPolicy proves an unrecognized
// policy string is rejected with a clear error rather than silently
// defaulting or applying, mirroring taskrunner.ApprovalPolicy.IsValid's own
// reject-over-silent-fallback rationale.
func TestRegistry_SetApprovalPolicy_RejectsInvalidPolicy(t *testing.T) {
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

	if err := reg.SetApprovalPolicy(runID, taskrunner.ApprovalPolicy("bogus")); err == nil {
		t.Fatalf("SetApprovalPolicy(%q) error = nil, want a clear rejection", "bogus")
	}
}

// TestRegistry_SetApprovalPolicy_RejectsFinishedRun proves switching a run
// that has already reached a terminal state is a clear error, not a
// dangling mutation of dead state.
func TestRegistry_SetApprovalPolicy_RejectsFinishedRun(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

	if err := reg.SetApprovalPolicy(runID, taskrunner.ApprovalPolicyAutoSafe); err == nil {
		t.Fatalf("SetApprovalPolicy() on a finished run error = nil, want a clear rejection")
	}
}

// TestRegistry_SetApprovalPolicy_UnknownRun_ReturnsErrNotFound proves a
// nonexistent runID is reported the same way every other Registry lookup
// reports it.
func TestRegistry_SetApprovalPolicy_UnknownRun_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	_, st := newTestWorkspaceManager(t)
	reg := newTestRegistry(t, st)

	err := reg.SetApprovalPolicy("does-not-exist", taskrunner.ApprovalPolicyAutoSafe)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetApprovalPolicy() error = %v, want ErrNotFound", err)
	}
}

// waitForTextChunk polls runID's history for a text event whose Text
// equals want, failing the test if it never appears within timeout.
func waitForTextChunk(t *testing.T, reg *Registry, runID, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		hist, _, err := reg.History(runID)
		if err != nil {
			t.Fatalf("History(%q) error = %v", runID, err)
		}
		for _, e := range hist {
			if e.Type == taskrunner.EventTypeText && e.Text == want {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for run %q to record text chunk %q, history so far: %+v", runID, want, hist)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestRegistry_SetApprovalPolicy_MidRunSwitchEnablesACPFileEditAutoAllow
// proves a real gap found in code review of this plan's Item A: ACP's
// (GLM/Kimi's) file-edit auto-allow fast path
// (taskrunner.acpDeciderAdapter.Decide's autoAllowACPFileEdit check) has
// its own frozen approvalPolicy, captured once when runACP constructs the
// adapter, entirely separate from the run struct's own live, mutex-guarded
// approvalPolicy that SetApprovalPolicy updates and
// runPermissionDecider.Decide already reads live for its own
// AllowlistedCommand (shell-command) check. Without wiring
// taskrunner.LivePolicyDecider through (see runPermissionDecider's
// CurrentApprovalPolicy), a mid-run switch to auto-safe would correctly
// unlock the shell-command allowlist but silently leave every in-worktree
// file edit still asking a human -- this test drives a real ACP (GLM
// fakeagent) subprocess through the "permission-edit" scenario to prove
// the fix: starting manual, switching mid-run, then a subsequent
// worktree-confined edit-kind request auto-allows without ever becoming a
// pending permission request at all (see acpDeciderAdapter.Decide's fast
// path, which never reaches runPermissionDecider on this route).
func TestRegistry_SetApprovalPolicy_MidRunSwitchEnablesACPFileEditAutoAllow(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "permission-edit")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, taskrunner.ProviderGLM, "hi", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// The scenario's "ready" chunk is causally ordered before its
	// session/request_permission call (see fakeagent's "permission-edit"
	// doc comment, and the real delay between them) -- so switching right
	// after observing it leaves comfortable headroom before the edit
	// request actually goes out.
	waitForTextChunk(t, reg, runID, "ready", 5*time.Second)

	if err := reg.SetApprovalPolicy(runID, taskrunner.ApprovalPolicyAutoSafe); err != nil {
		t.Fatalf("SetApprovalPolicy() error = %v", err)
	}

	status := waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	if status.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", status.StopReason, "end_turn")
	}

	hist, _, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	var sawFinalChunk bool
	for _, e := range hist {
		if e.Type == taskrunner.EventTypePermissionRequest || e.Type == taskrunner.EventTypePermissionResolved {
			t.Fatalf("history recorded a permission request/resolution event (%+v) -- acpDeciderAdapter's auto-allow fast path should short-circuit before the edit ever becomes a pending (or even auto-safe-resolved-by-the-decider) request", e)
		}
		if e.Type == taskrunner.EventTypeText && e.Text == "chose:allow-1" {
			sawFinalChunk = true
		}
	}
	if !sawFinalChunk {
		t.Fatalf("history does not include the agent's post-decision chunk reflecting the auto-allowed edit (the run would otherwise have hung waiting on a human, as it did before this fix): %+v", hist)
	}
}
