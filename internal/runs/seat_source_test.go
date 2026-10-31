package runs

import (
	"context"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// denyAllFloor denies every tool with the given reason.
func denyAllFloor(reason string) func(string, []string) (bool, string) {
	return func(string, []string) (bool, string) { return false, reason }
}

// seatRun is policyRun plus a seat source installed for the run's task.
func seatRun(t *testing.T, perm taskrunner.PermissionSettings, policy string, enabled bool, spec SeatSpec, ok bool) (*Registry, *run, string, *pendingCounter) {
	t.Helper()
	reg, r, runID, n := policyRun(t, perm, policy, enabled)
	reg.SetSeatSource(func(int64) (SeatSpec, bool) { return spec, ok })
	return reg, r, runID, n
}

// TestDecider_SeatFloorDeniesBashEvenWhenPolicyOff: with no policy engine,
// or one that is switched off, the seat floor alone resolves a Bash request
// to the deny option -- nothing parked, no notification, no audit rows.
func TestDecider_SeatFloorDeniesBashEvenWhenPolicyOff(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		policy  string
		enabled bool
	}{"no engine": {"-", false}, "engine off": {goTestAllowPolicy, false}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg, r, _, n := seatRun(t, taskrunner.PermissionSettings{}, tc.policy, tc.enabled, SeatSpec{Floor: denyAllFloor("the steward seat has no shell")}, true)
			d := runPermissionDecider{reg: reg, r: r}
			got, reason, err := d.DecideDetailedWithReason(context.Background(), "run Bash", bashDetail("ls"), allowDenyOptions)
			if err != nil || got != "deny-1" {
				t.Fatalf("DecideDetailedWithReason = %q, %v; want deny-1", got, err)
			}
			if !strings.HasPrefix(reason, "steward seat floor") {
				t.Fatalf("reason = %q, want the steward seat floor prefix", reason)
			}
			reqs, resolved := permEvents(r)
			if len(reqs) != 1 || len(resolved) != 1 ||
				resolved[0].PermissionResolution != taskrunner.PermissionResolvedByPolicyDeny ||
				resolved[0].PermissionRuleID != "steward-seat-floor" {
				t.Fatalf("resolved = %+v", resolved)
			}
			if p := reqs[0].PermissionDetail.Policy; p == nil || p.Action != "deny" || p.RuleID != "steward-seat-floor" {
				t.Fatalf("request policy = %+v", p)
			}
			if pendingCount(r) != 0 || n.pending.Load() != 0 {
				t.Fatalf("pending = %d, notifications = %d, want 0", pendingCount(r), n.pending.Load())
			}
			if rows := policyAudit(t, reg); len(rows) != 0 {
				t.Fatalf("audit rows = %d, want 0: the seat floor never audits", len(rows))
			}
		})
	}
}

// TestDecider_SeatFloorAllowsReadInScratch: a floor that allows Read
// resolves to allow_once with a policy_allow footprint; only allow_once is
// ever picked, never an "always" option.
func TestDecider_SeatFloorAllowsReadInScratch(t *testing.T) {
	t.Parallel()
	var sawTool string
	var sawPaths []string
	floor := func(tool string, paths []string) (bool, string) {
		sawTool, sawPaths = tool, paths
		return tool == "Read", "reading the review scratch"
	}
	reg, r, _, n := seatRun(t, taskrunner.PermissionSettings{}, "-", false, SeatSpec{Floor: floor}, true)
	d := runPermissionDecider{reg: reg, r: r}
	got, reason, err := d.DecideDetailedWithReason(context.Background(), "run Read notes.md", nil, allowDenyOptions)
	if err != nil || got != "allow-1" {
		t.Fatalf("DecideDetailedWithReason = %q, %v; want allow-1", got, err)
	}
	if reason != "" {
		t.Fatalf("reason = %q, want empty on an allow", reason)
	}
	if sawTool != "Read" || sawPaths != nil {
		t.Fatalf("floor saw (%q, %v), want (Read, nil)", sawTool, sawPaths)
	}
	_, resolved := permEvents(r)
	if len(resolved) != 1 || resolved[0].PermissionResolution != taskrunner.PermissionResolvedByPolicyAllow ||
		resolved[0].PermissionRuleID != "steward-seat-floor" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if pendingCount(r) != 0 || n.pending.Load() != 0 {
		t.Fatalf("pending = %d, notifications = %d, want 0", pendingCount(r), n.pending.Load())
	}
}

// TestDecider_SeatFloorBeforeAutoAccept: AutoAccept is the human's choice,
// not the seat's; the floor still denies.
func TestDecider_SeatFloorBeforeAutoAccept(t *testing.T) {
	t.Parallel()
	reg, r, _, _ := seatRun(t, taskrunner.PermissionSettings{AutoAccept: true}, "-", false, SeatSpec{Floor: denyAllFloor("no shell")}, true)
	d := runPermissionDecider{reg: reg, r: r}
	got, _, err := d.DecideDetailedWithReason(context.Background(), "run Bash", bashDetail("git stash"), allowDenyOptions)
	if err != nil || got != "deny-1" {
		t.Fatalf("DecideDetailedWithReason = %q, %v; want deny-1", got, err)
	}
	_, resolved := permEvents(r)
	if len(resolved) != 1 || resolved[0].PermissionResolution != taskrunner.PermissionResolvedByPolicyDeny {
		t.Fatalf("resolved = %+v, want the seat floor deny, not AutoAccept", resolved)
	}
}

// TestDecider_SeatFloorNotConsultedForOtherTasks: a task the source does not
// own follows the normal human path exactly like before: parked, one pending
// notification, resolved by the human.
func TestDecider_SeatFloorNotConsultedForOtherTasks(t *testing.T) {
	t.Parallel()
	reg, r, runID, n := seatRun(t, taskrunner.PermissionSettings{}, "-", false, SeatSpec{}, false)
	d := runPermissionDecider{reg: reg, r: r}
	out, reqID := decideAsync(t, d, bashDetail("go test ./..."))
	if err := reg.RespondPermission(runID, reqID, "allow-1"); err != nil {
		t.Fatalf("RespondPermission: %v", err)
	}
	if got := <-out; got != "allow-1" {
		t.Fatalf("human path = %q, want allow-1", got)
	}
	_, resolved := permEvents(r)
	if len(resolved) != 1 || resolved[0].PermissionResolution != taskrunner.PermissionResolvedByHuman {
		t.Fatalf("resolved = %+v", resolved)
	}
	if pendingCount(r) != 0 || n.pending.Load() != 1 {
		t.Fatalf("pending = %d, notifications = %d, want 0/1", pendingCount(r), n.pending.Load())
	}
}

// TestDecider_SeatFloorDenyWithoutRejectOptionErrors: a deny the provider
// offered no reject option for is an error, never an allow.
func TestDecider_SeatFloorDenyWithoutRejectOptionErrors(t *testing.T) {
	t.Parallel()
	reg, r, _, _ := seatRun(t, taskrunner.PermissionSettings{}, "-", false, SeatSpec{Floor: denyAllFloor("no shell")}, true)
	d := runPermissionDecider{reg: reg, r: r}
	allowOnly := []taskrunner.PermissionOption{{ID: "allow-1", Label: "Allow", Kind: "allow_once"}}
	got, _, err := d.DecideDetailedWithReason(context.Background(), "run Bash", bashDetail("ls"), allowOnly)
	if err == nil || !strings.Contains(err.Error(), "seat floor") {
		t.Fatalf("err = %v, want the seat floor error", err)
	}
	if got == "allow-1" {
		t.Fatal("a deny without a reject option must never resolve to the allow option")
	}
	if pendingCount(r) != 0 {
		t.Fatal("a failed floor deny must not park a request")
	}
}

// seatContract builds the contract the way drive does for a run of a
// profile with role text, with the given seat wiring.
func seatContract(t *testing.T, reg *Registry, st *store.Store) taskrunner.RunContract {
	t.Helper()
	p, err := st.CreateAgentProfile(store.AgentProfile{Name: "seat-" + t.Name(), Provider: "glm", Instructions: "ROLE"})
	if err != nil {
		t.Fatal(err)
	}
	r := &run{id: "run-42", taskID: 7, chatID: 9, profileID: p.ID}
	return taskrunner.RunContractFrom(reg.withRunContract(context.Background(), r))
}

// TestRunContract_SeatBranchCarriesEnvAndDisallowedTools: a seat task's
// contract carries the base MCP env plus the seat's own env, and the seat's
// deny list.
func TestRunContract_SeatBranchCarriesEnvAndDisallowedTools(t *testing.T) {
	t.Parallel()
	_, st := newTestWorkspaceManager(t)
	reg := newTestRegistry(t, st)
	reg.SetSmindHome("/daemon/home")
	reg.SetSeatSource(func(int64) (SeatSpec, bool) {
		return SeatSpec{
			Env:             map[string]string{"SMIND_REVIEW_ID": "rev-9"},
			DisallowedTools: []string{"Bash", "Edit"},
			Floor:           denyAllFloor("no shell"),
		}, true
	})
	c := seatContract(t, reg, st)
	if c.Mcp == nil || !c.Mcp.Orchestration {
		t.Fatalf("Mcp = %+v, want the forced built-in entry", c.Mcp)
	}
	want := map[string]string{
		"SMIND_HOME": "/daemon/home", "SMIND_RUN_ID": "run-42",
		"SMIND_TASK_ID": "7", "SMIND_CHAT_ID": "9", "SMIND_REVIEW_ID": "rev-9",
	}
	if len(c.Mcp.Env) != len(want) {
		t.Fatalf("env = %v, want exactly %v", c.Mcp.Env, want)
	}
	for k, v := range want {
		if c.Mcp.Env[k] != v {
			t.Fatalf("env[%s] = %q, want %q", k, c.Mcp.Env[k], v)
		}
	}
	if len(c.DisallowedTools) != 2 || c.DisallowedTools[0] != "Bash" || c.DisallowedTools[1] != "Edit" {
		t.Fatalf("DisallowedTools = %v, want [Bash Edit]", c.DisallowedTools)
	}
}

// TestRunContract_NoSeatSourceUnchanged: without a seat source the contract
// is what a plain profile always produced: instructions only.
func TestRunContract_NoSeatSourceUnchanged(t *testing.T) {
	t.Parallel()
	_, st := newTestWorkspaceManager(t)
	reg := newTestRegistry(t, st)
	c := seatContract(t, reg, st)
	if c.Mcp != nil || c.DisallowedTools != nil || c.Instructions != "ROLE" {
		t.Fatalf("contract = %+v, want instructions only", c)
	}
}
