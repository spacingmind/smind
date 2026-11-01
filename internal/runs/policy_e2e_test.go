package runs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestPolicyE2E_PushOwnBranchAllowedStashDenied drives the policy engine
// end to end over a real task: a real registry/run (policyRun), the real
// worktree and task branch read back from the store (the same fields
// runPermissionDecider.policyRequest feeds the engine), a rule that allows
// pushing the task's own branch, the compiled-in no-stash floor, and an
// ask that escalates to a human -- all of it landing in the policy audit.
func TestPolicyE2E_PushOwnBranchAllowedStashDenied(t *testing.T) {
	t.Parallel()
	// This is permpolicy's pushOwn rule (internal/permpolicy/decide_test.go)
	// with its `seats: [lead]` line removed: the run below has profileID 0,
	// so policyRequest leaves req.Seat empty and matchRule's exact seat
	// match would turn every push into an ask.
	const policy = `version: 1
rules:
  - id: lead-push-own-branch
    action: allow
    why: Lead pushes its own task branch
    match:
      tool: Bash
      command:
        argv0: git
        subcommand: push
        args:
          allow_only: ["origin", "{taskBranch}"]
          deny_flags: ["-f", "--force", "--force-with-lease", "--delete", "--mirror"]
      paths:
        within: ["{worktree}"]
`
	reg, r, runID, n := policyRun(t, taskrunner.PermissionSettings{}, policy, true)
	task, err := reg.st.GetTask(r.taskID)
	if err != nil {
		t.Fatalf("GetTask(%d) error = %v", r.taskID, err)
	}
	if task.Branch == nil || *task.Branch == "" {
		t.Fatalf("task %d has no branch", r.taskID)
	}
	branch := *task.Branch
	d := runPermissionDecider{reg: reg, r: r}
	// Bound every Decide call in this test: if a floor or rule regression
	// turns one into an ask, the decider would otherwise park the request
	// and wait out the full human timeout. A deadline makes the ask path's
	// ctx.Done() case return an error within 10 s instead.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// (1) Pushing the task's own branch is allowed by the rule, with no
	// human involved: no pending request, no notification, and a resolved
	// event tagged with the rule.
	got, err := d.DecideDetailed(ctx, "run Bash", bashDetail("git push origin "+branch), allowDenyOptions)
	if err != nil || got != "allow-1" {
		t.Fatalf("push own branch = %q, %v; want allow-1", got, err)
	}
	if pendingCount(r) != 0 || n.pending.Load() != 0 {
		t.Fatalf("pending = %d, notifications = %d; want 0 and 0", pendingCount(r), n.pending.Load())
	}
	reqs, resolved := permEvents(r)
	if len(reqs) != 1 || len(resolved) != 1 {
		t.Fatalf("events: %d requests, %d resolved; want 1 and 1", len(reqs), len(resolved))
	}
	if resolved[0].PermissionResolution != taskrunner.PermissionResolvedByPolicyAllow ||
		resolved[0].PermissionRuleID != "lead-push-own-branch" ||
		resolved[0].PermissionRequestID != reqs[0].PermissionRequestID {
		t.Fatalf("resolved = %+v", resolved[0])
	}

	// (2) The compiled-in no-stash floor denies `git stash` even though no
	// rule mentions it, and never parks a request.
	got, reason, err := d.DecideDetailedWithReason(ctx, "run Bash", bashDetail("git stash"), allowDenyOptions)
	if err != nil || got != "deny-1" {
		t.Fatalf("git stash = %q, %v; want deny-1", got, err)
	}
	if !strings.Contains(reason, "no-stash") {
		t.Fatalf("reason = %q, want the floor id", reason)
	}
	if pendingCount(r) != 0 || n.pending.Load() != 0 {
		t.Fatalf("after stash: pending = %d, notifications = %d; want 0 and 0", pendingCount(r), n.pending.Load())
	}
	_, resolved = permEvents(r)
	if len(resolved) != 2 || resolved[1].PermissionResolution != taskrunner.PermissionResolvedByPolicyDeny ||
		resolved[1].PermissionRuleID != "no-stash" {
		t.Fatalf("stash resolved = %+v", resolved)
	}

	// (3) Pushing some other branch is not auto-allowed: the engine asks,
	// the request parks for a human, and a real RespondPermission denies it.
	out, reqID := decideAsync(t, d, bashDetail("git push origin some-other-branch"))
	if err := reg.RespondPermission(runID, reqID, "deny-1"); err != nil {
		t.Fatalf("RespondPermission(%q) error = %v", reqID, err)
	}
	if got := <-out; got != "deny-1" {
		t.Fatalf("push other branch = %q, want the human's deny-1", got)
	}

	// (4) The audit recorded every evaluation against the real task: the
	// allow with its rule and the branch in the target, the floor deny, and
	// the escalated ask (whose human denial lands as a fourth row).
	var sawAllow, sawDeny, sawAsk bool
	for _, row := range policyAudit(t, reg) {
		switch row.Decision {
		case "allow":
			sawAllow = true
			if row.RuleID != "lead-push-own-branch" {
				t.Errorf("allow row RuleID = %q, want lead-push-own-branch", row.RuleID)
			}
			if !strings.Contains(row.Target, branch) {
				t.Errorf("allow row Target = %q, want it to contain the task branch %q", row.Target, branch)
			}
		case "deny":
			sawDeny = true
			if !strings.Contains(row.RuleID, "no-stash") {
				t.Errorf("deny row RuleID = %q, want the floor id no-stash", row.RuleID)
			}
		case "ask":
			sawAsk = true
			if !strings.Contains(row.Target, "some-other-branch") {
				t.Errorf("ask row Target = %q, want it to contain the pushed ref", row.Target)
			}
		}
	}
	if !sawAllow || !sawDeny || !sawAsk {
		t.Fatalf("audit decisions: allow=%v deny=%v ask=%v, want all three", sawAllow, sawDeny, sawAsk)
	}
}
