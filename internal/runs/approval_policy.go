package runs

import (
	"fmt"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// SetApprovalPolicy switches runID's live approvalPolicy between
// taskrunner.ApprovalPolicyManual and taskrunner.ApprovalPolicyAutoSafe,
// taking effect for the next pending-or-later permission request
// runPermissionDecider.Decide evaluates (see approvalPolicy's doc comment
// on the run struct) -- a request already past that check when the switch
// happens (blocked in its own select, waiting on a human/timeout/provider
// cancellation) is not retroactively affected.
//
// taskrunner.ApprovalPolicyFullAccess is rejected as a target, not
// silently ignored: unlike manual<->auto-safe (which only changes what
// runPermissionDecider.Decide does with a request it already intercepts),
// full-access skips installing a decider entirely and instead configures
// the provider client at construction time with its own native
// auto-approve mechanism (see policy.go's ApprovalPolicyFullAccess doc
// comment) -- entering or leaving it mid-run would require tearing down
// and respawning that client, which is out of scope here (see
// docs/plans/active/mid-run-approval-and-retry-effort.md's Decisions).
//
// A finished run's approvalPolicy has nothing left to affect (its decider,
// if any, is done deciding), so SetApprovalPolicy fails on one rather than
// mutating dead state silently. An unknown runID returns ErrNotFound (via
// reg.get), same as every other Registry lookup.
func (reg *Registry) SetApprovalPolicy(runID string, policy taskrunner.ApprovalPolicy) error {
	if policy == taskrunner.ApprovalPolicyFullAccess {
		return fmt.Errorf("runs: set approval policy: switching to %q mid-run is not supported (full-access requires respawning the provider client)", policy)
	}
	if !policy.IsValid() {
		return fmt.Errorf("runs: set approval policy: invalid approval policy %q", policy)
	}

	r, err := reg.get(runID)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status != StatusRunning {
		return fmt.Errorf("runs: set approval policy: run %q has already finished", runID)
	}
	r.approvalPolicy = policy
	return nil
}
