package runs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

type reasonResult struct {
	optionID, reason string
	err              error
}

// decideWithReasonAsync parks a request the way the Claude Code adapter does
// (DecideDetailedWithReason) and returns its result channel and request id.
func decideWithReasonAsync(t *testing.T, d runPermissionDecider, withReason bool) (<-chan reasonResult, string) {
	t.Helper()
	out := make(chan reasonResult, 1)
	go func() {
		var res reasonResult
		if withReason {
			res.optionID, res.reason, res.err = d.DecideDetailedWithReason(context.Background(), "run Bash", bashDetail("make build"), allowDenyOptions)
		} else {
			res.optionID, res.err = d.DecideDetailed(context.Background(), "run Bash", bashDetail("make build"), allowDenyOptions)
		}
		out <- res
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

func TestDenyReason_DeliveredToTheModelWhenTheProviderCanCarryIt(t *testing.T) {
	t.Parallel()
	reg, r, runID, _ := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
	out, reqID := decideWithReasonAsync(t, runPermissionDecider{reg: reg, r: r}, true)
	delivered, err := reg.RespondPermissionWithReason(runID, reqID, "deny-1", "  use make test instead \x07 ")
	if err != nil || !delivered {
		t.Fatalf("RespondPermissionWithReason = %v, %v; want delivered", delivered, err)
	}
	got := <-out
	if got.optionID != "deny-1" || got.reason != "use make test instead" {
		t.Fatalf("decide = %+v; want deny-1 with the sanitized reason", got)
	}
}

func TestDenyReason_NotDeliveredWhereTheProviderCannotCarryIt(t *testing.T) {
	t.Parallel()
	reg, r, runID, _ := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
	out, reqID := decideWithReasonAsync(t, runPermissionDecider{reg: reg, r: r}, false)
	delivered, err := reg.RespondPermissionWithReason(runID, reqID, "deny-1", "nope")
	if err != nil || delivered {
		t.Fatalf("RespondPermissionWithReason = %v, %v; want the deny sent, not delivered", delivered, err)
	}
	if got := <-out; got.optionID != "deny-1" || got.reason != "" {
		t.Fatalf("decide = %+v; want a plain deny", got)
	}
}

func TestDenyReason_EmptyReasonBehavesExactlyLikeRespondPermission(t *testing.T) {
	t.Parallel()
	reg, r, runID, _ := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
	out, reqID := decideWithReasonAsync(t, runPermissionDecider{reg: reg, r: r}, true)
	if err := reg.RespondPermission(runID, reqID, "deny-1"); err != nil {
		t.Fatal(err)
	}
	if got := <-out; got.optionID != "deny-1" || got.reason != "" || got.err != nil {
		t.Fatalf("decide = %+v", got)
	}
	// A second answer is still refused as already resolved.
	if _, err := reg.RespondPermissionWithReason(runID, reqID, "deny-1", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second answer = %v; want ErrNotFound", err)
	}
}

func TestDenyReason_RefusedWithAnAllowOptionOrOverTheCapAndRequestStaysPending(t *testing.T) {
	t.Parallel()
	reg, r, runID, _ := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
	out, reqID := decideWithReasonAsync(t, runPermissionDecider{reg: reg, r: r}, true)

	if _, err := reg.RespondPermissionWithReason(runID, reqID, "allow-1", "sneaky"); !errors.Is(err, ErrDenyReason) {
		t.Fatalf("allow with a reason = %v; want ErrDenyReason", err)
	}
	if _, err := reg.RespondPermissionWithReason(runID, reqID, "no-such-option", "x"); !errors.Is(err, ErrDenyReason) {
		t.Fatalf("unknown option with a reason = %v; want ErrDenyReason", err)
	}
	if _, err := reg.RespondPermissionWithReason(runID, reqID, "deny-1", strings.Repeat("é", MaxDenyReasonRunes+1)); !errors.Is(err, ErrDenyReason) {
		t.Fatalf("over the cap = %v; want ErrDenyReason", err)
	}
	if _, err := reg.RespondPermissionWithReason(runID, reqID, "deny-1", "bad \xff utf8"); !errors.Is(err, ErrDenyReason) {
		t.Fatalf("invalid utf-8 = %v; want ErrDenyReason", err)
	}
	if pendingCount(r) != 1 {
		t.Fatal("a refused reason must leave the request pending")
	}
	// Exactly at the cap is accepted.
	if _, err := reg.RespondPermissionWithReason(runID, reqID, "deny-1", strings.Repeat("é", MaxDenyReasonRunes)); err != nil {
		t.Fatalf("at the cap = %v", err)
	}
	if got := <-out; got.optionID != "deny-1" || len([]rune(got.reason)) != MaxDenyReasonRunes {
		t.Fatalf("decide = %+v", got)
	}
}

func TestSanitizeDenyReason(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"  hi  ", "hi"},
		{"line1\nline2", "line1\nline2"},
		{"tab\there\x1b[31m", "tab here [31m"},
	}
	for _, c := range cases {
		got, err := SanitizeDenyReason(c.in)
		if err != nil || got != c.want {
			t.Fatalf("SanitizeDenyReason(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestSanitizeDenyReason_RefusesNUL(t *testing.T) {
	t.Parallel()
	if _, err := SanitizeDenyReason("a\x00b"); !errors.Is(err, ErrDenyReason) {
		t.Fatalf("NUL = %v; want ErrDenyReason", err)
	}
}

func TestDenyReason_BlankReasonOnADenyIsAPlainDeny(t *testing.T) {
	t.Parallel()
	reg, r, runID, _ := policyRun(t, taskrunner.PermissionSettings{}, goTestAllowPolicy, true)
	out, reqID := decideWithReasonAsync(t, runPermissionDecider{reg: reg, r: r}, true)
	delivered, err := reg.RespondPermissionWithReason(runID, reqID, "deny-1", " \x07 ")
	if err != nil || delivered {
		t.Fatalf("blank reason = %v, %v; want a plain deny, not delivered", delivered, err)
	}
	if got := <-out; got.optionID != "deny-1" || got.reason != "" {
		t.Fatalf("decide = %+v", got)
	}
}
