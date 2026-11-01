package runs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const goTestAllowPolicy = `version: 1
rules:
  - id: go-test
    action: allow
    why: go test inside the worktree
    match:
      tool: Bash
      command:
        argv0: go
        subcommand: test
      paths:
        within: ["{worktree}"]
`

// pendingCounter counts NotifyPermissionPending calls on top of the
// resolvedRecorder.
type pendingCounter struct {
	*resolvedRecorder
	pending atomic.Int32
}

func (p *pendingCounter) NotifyPermissionPending(string, int64, int64, string, string, []taskrunner.PermissionOption) {
	p.pending.Add(1)
}

// policyRun starts a run and installs a policy engine (nil when policy is
// "-"; otherwise the file content, "" = no file) with the given switch.
func policyRun(t *testing.T, perm taskrunner.PermissionSettings, policy string, enabled bool) (*Registry, *run, string, *pendingCounter) {
	t.Helper()
	reg, r, runID := startedRun(t, perm)
	n := &pendingCounter{resolvedRecorder: newResolvedRecorder()}
	reg.SetNotifier(n)
	if policy != "-" {
		path := filepath.Join(t.TempDir(), "policy.yaml")
		if policy != "" {
			if err := os.WriteFile(path, []byte(policy), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		reg.SetPolicyEngine(permpolicy.NewEngine(permpolicy.Options{
			Path: path, Enabled: func() bool { return enabled }, Audit: reg.st,
			Home: t.TempDir(), SmindHome: t.TempDir(),
		}))
	}
	return reg, r, runID, n
}

func bashDetail(cmd string) *taskrunner.PermissionDetail {
	return &taskrunner.PermissionDetail{Kind: "execute", Command: cmd}
}

func permEvents(r *run) (reqs, resolved []taskrunner.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.history {
		switch e.Type {
		case taskrunner.EventTypePermissionRequest:
			reqs = append(reqs, e)
		case taskrunner.EventTypePermissionResolved:
			resolved = append(resolved, e)
		}
	}
	return
}

func pendingCount(r *run) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pendingPermissions)
}

func policyAudit(t *testing.T, reg *Registry) []store.PolicyAuditRow {
	t.Helper()
	rows, err := reg.st.ListPolicyAudit(store.PolicyAuditFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// decideAsync runs Decide on its own goroutine and returns the result
// channel plus the request id once it is pending.
func decideAsync(t *testing.T, d runPermissionDecider, detail *taskrunner.PermissionDetail) (<-chan string, string) {
	t.Helper()
	out := make(chan string, 1)
	go func() {
		id, err := d.DecideDetailed(context.Background(), "run Bash", detail, allowDenyOptions)
		if err != nil {
			id = "error: " + err.Error()
		}
		out <- id
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d.r.mu.Lock()
		for id := range d.r.pendingPermissions {
			d.r.mu.Unlock()
			return out, id
		}
		d.r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no pending permission request")
	return nil, ""
}

func TestDecider_PolicyAllowResolvesWithoutHuman(t *testing.T) {
	t.Parallel()
	reg, r, _, n := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
	got, err := (runPermissionDecider{reg: reg, r: r}).DecideDetailed(context.Background(), "run Bash", bashDetail("go test ./..."), allowDenyOptions)
	if err != nil || got != "allow-1" {
		t.Fatalf("DecideDetailed = %q, %v; want allow-1", got, err)
	}
	reqs, resolved := permEvents(r)
	if len(reqs) != 1 || len(resolved) != 1 {
		t.Fatalf("events: %d requests, %d resolved", len(reqs), len(resolved))
	}
	if resolved[0].PermissionResolution != taskrunner.PermissionResolvedByPolicyAllow || resolved[0].PermissionRuleID != "go-test" ||
		resolved[0].PermissionRequestID != reqs[0].PermissionRequestID {
		t.Fatalf("resolved = %+v", resolved[0])
	}
	if p := reqs[0].PermissionDetail.Policy; p == nil || p.Action != "allow" || p.RuleID != "go-test" {
		t.Fatalf("request policy = %+v", p)
	}
	if pendingCount(r) != 0 || n.pending.Load() != 0 {
		t.Fatalf("pending = %d, notifications = %d, want 0", pendingCount(r), n.pending.Load())
	}
}

func TestDecider_PolicyDenyResolvesToDenyOption(t *testing.T) {
	t.Parallel()
	reg, r, _, n := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
	d := runPermissionDecider{reg: reg, r: r}
	got, reason, err := d.DecideDetailedWithReason(context.Background(), "run Bash", bashDetail("git stash"), allowDenyOptions)
	if err != nil || got != "deny-1" {
		t.Fatalf("DecideDetailedWithReason = %q, %v; want deny-1", got, err)
	}
	if !strings.Contains(reason, "no-stash") {
		t.Fatalf("reason = %q, want the floor id", reason)
	}
	_, resolved := permEvents(r)
	if len(resolved) != 1 || resolved[0].PermissionResolution != taskrunner.PermissionResolvedByPolicyDeny || resolved[0].PermissionRuleID != "no-stash" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if pendingCount(r) != 0 || n.pending.Load() != 0 {
		t.Fatal("a policy deny must not park a request")
	}
}

func TestDecider_AskFallsThroughToHumanPath(t *testing.T) {
	t.Parallel()
	reg, r, runID, n := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
	d := runPermissionDecider{reg: reg, r: r}
	out, reqID := decideAsync(t, d, bashDetail("make build"))
	if err := reg.RespondPermission(runID, reqID, "allow-1"); err != nil {
		t.Fatalf("RespondPermission: %v", err)
	}
	if got := <-out; got != "allow-1" {
		t.Fatalf("Decide = %q", got)
	}
	reqs, resolved := permEvents(r)
	if len(resolved) != 1 || resolved[0].PermissionResolution != taskrunner.PermissionResolvedByHuman {
		t.Fatalf("resolved = %+v", resolved)
	}
	if p := reqs[0].PermissionDetail.Policy; p == nil || p.Action != "ask" {
		t.Fatalf("request policy = %+v, want ask", p)
	}
	if n.pending.Load() != 1 {
		t.Fatalf("pending notifications = %d, want 1", n.pending.Load())
	}
	decisions := map[string]bool{}
	for _, row := range policyAudit(t, reg) {
		decisions[row.Decision] = true
		if row.RequestID != reqID {
			t.Errorf("row request id = %q, want %q", row.RequestID, reqID)
		}
	}
	if len(decisions) != 2 || !decisions["ask"] || !decisions["ask_human_allow"] {
		t.Fatalf("audit decisions = %v", decisions)
	}
}

func TestDecider_AskTimeoutDeniesAndAudits(t *testing.T) {
	t.Parallel()
	reg, r, _, _ := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
	reg.SetPermissionTimeout(50 * time.Millisecond)
	got, err := (runPermissionDecider{reg: reg, r: r}).DecideDetailed(context.Background(), "run Bash", bashDetail("make build"), allowDenyOptions)
	if err != nil || got != "deny-1" {
		t.Fatalf("DecideDetailed = %q, %v; want deny-1", got, err)
	}
	_, resolved := permEvents(r)
	if len(resolved) != 1 || resolved[0].PermissionResolution != taskrunner.PermissionResolvedByTimeout {
		t.Fatalf("resolved = %+v", resolved)
	}
	decisions := map[string]bool{}
	for _, row := range policyAudit(t, reg) {
		decisions[row.Decision] = true
	}
	if len(decisions) != 2 || !decisions["ask"] || !decisions["ask_timeout_deny"] || decisions["allow"] {
		t.Fatalf("audit decisions = %v", decisions)
	}
}

// TestDecider_PolicyOffMatchesGolden pins the pre-policy behaviour: with no
// engine, or one that is switched off, the event sequence, the options
// returned and the (absent) audit are exactly what the decider did before.
func TestDecider_PolicyOffMatchesGolden(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		policy  string
		enabled bool
	}{"no engine": {"-", false}, "engine off": {goTestAllowPolicy, false}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Human path: pending, announced, resolved human.
			reg, r, runID, n := policyRun(t, taskrunner.PermissionSettings{}, tc.policy, tc.enabled)
			d := runPermissionDecider{reg: reg, r: r}
			out, reqID := decideAsync(t, d, bashDetail("go test ./..."))
			if err := reg.RespondPermission(runID, reqID, "deny-1"); err != nil {
				t.Fatal(err)
			}
			if got := <-out; got != "deny-1" {
				t.Fatalf("human path = %q", got)
			}
			// Timeout path.
			reg.SetPermissionTimeout(30 * time.Millisecond)
			if got, err := d.DecideDetailed(context.Background(), "run Bash", bashDetail("git stash"), allowDenyOptions); err != nil || got != "deny-1" {
				t.Fatalf("timeout path = %q, %v", got, err)
			}
			reqs, resolved := permEvents(r)
			if len(reqs) != 2 || len(resolved) != 2 ||
				resolved[0].PermissionResolution != taskrunner.PermissionResolvedByHuman ||
				resolved[1].PermissionResolution != taskrunner.PermissionResolvedByTimeout {
				t.Fatalf("events: %d requests, resolved %+v", len(reqs), resolved)
			}
			for _, e := range append(reqs, resolved...) {
				if e.PermissionRuleID != "" || (e.PermissionDetail != nil && e.PermissionDetail.Policy != nil) {
					t.Fatalf("policy field leaked into %+v", e)
				}
			}
			if n.pending.Load() != 2 {
				t.Fatalf("pending notifications = %d, want 2", n.pending.Load())
			}
			if rows := policyAudit(t, reg); len(rows) != 0 {
				t.Fatalf("audit rows = %d, want 0", len(rows))
			}
			// AutoAccept path.
			reg2, r2, _, _ := policyRun(t, taskrunner.PermissionSettings{AutoAccept: true}, tc.policy, tc.enabled)
			if got, err := (runPermissionDecider{reg: reg2, r: r2}).DecideDetailed(context.Background(), "run Bash", bashDetail("go test ./..."), allowDenyOptions); err != nil || got != "allow-1" {
				t.Fatalf("auto path = %q, %v", got, err)
			}
			if _, res := permEvents(r2); len(res) != 1 || res[0].PermissionResolution != taskrunner.PermissionResolvedByAutoAccept {
				t.Fatalf("auto resolved = %+v", res)
			}
		})
	}
}

func TestDecider_BypassModeNeverAsksSoPolicyNotConsulted(t *testing.T) {
	t.Parallel()
	reg, r, _, _ := policyRun(t, taskrunner.PermissionSettings{AutoAccept: true}, goTestAllowPolicy, true)
	got, err := (runPermissionDecider{reg: reg, r: r}).DecideDetailed(context.Background(), "run Bash", bashDetail("git stash"), allowDenyOptions)
	if err != nil || got != "allow-1" {
		t.Fatalf("DecideDetailed = %q, %v", got, err)
	}
	_, resolved := permEvents(r)
	if len(resolved) != 1 || resolved[0].PermissionResolution != taskrunner.PermissionResolvedByAutoAccept {
		t.Fatalf("resolved = %+v", resolved)
	}
	if rows := policyAudit(t, reg); len(rows) != 0 {
		t.Fatalf("audit rows = %d, want 0", len(rows))
	}
}

// A detail built by a provider adapter carries the elided command: the pending
// event (what the card, inbox and relay render) never holds the body, with the
// policy engine on or off, while the engine still judges the raw command.
func TestDecider_HeredocBodyNeverOnPendingEvent(t *testing.T) {
	t.Parallel()
	cmd := "cat <<EOF > f\nkey=SECRET_TOKEN_123\nEOF\n"
	for _, tc := range []struct {
		name   string
		policy string
		on     bool
	}{{"policy on", goTestAllowPolicy, true}, {"policy off", "-", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg, r, runID, _ := policyRun(t, taskrunner.PermissionSettings{}, tc.policy, tc.on)
			d := runPermissionDecider{reg: reg, r: r}
			tcJSON, _ := json.Marshal(map[string]any{"kind": "execute", "rawInput": map[string]any{"command": cmd}})
			detail := taskrunner.BuildPermissionDetail(t.TempDir(), tcJSON)
			out, reqID := decideAsync(t, d, detail)
			if err := reg.RespondPermission(runID, reqID, "allow-1"); err != nil {
				t.Fatal(err)
			}
			<-out
			reqs, _ := permEvents(r)
			raw, _ := json.Marshal(reqs[0])
			if strings.Contains(string(raw), "SECRET_TOKEN_123") || !strings.Contains(string(raw), "[heredoc ") {
				t.Errorf("pending event = %s", raw)
			}
			for _, row := range policyAudit(t, reg) {
				if strings.Contains(row.Target, "SECRET_TOKEN_123") {
					t.Errorf("audit target = %q", row.Target)
				}
			}
		})
	}
}

// A command whose raw text exceeded the detail cap still asks, even though its
// elided form is short: the elision changes what humans see, never what is
// decided.
func TestDecider_ElidedLongBodyStillAsks(t *testing.T) {
	t.Parallel()
	reg, r, runID, n := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
	d := runPermissionDecider{reg: reg, r: r}
	cmd := "go test ./... <<EOF\n" + strings.Repeat("x", 3000) + "\nEOF\n"
	tcJSON, _ := json.Marshal(map[string]any{"kind": "execute", "rawInput": map[string]any{"command": cmd}})
	detail := taskrunner.BuildPermissionDetail(t.TempDir(), tcJSON)
	out, reqID := decideAsync(t, d, detail)
	if err := reg.RespondPermission(runID, reqID, "allow-1"); err != nil {
		t.Fatal(err)
	}
	<-out
	if n.pending.Load() != 1 {
		t.Fatalf("pending = %d: a raw command over the cap must still ask", n.pending.Load())
	}
}
