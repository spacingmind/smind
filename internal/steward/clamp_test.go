package steward

import (
	"strings"
	"testing"
)

// gatesWith returns base()'s gates with the named ones overridden whole.
func gatesWith(overrides ...Gate) []Gate {
	gs := ComputeGates(base())
	for _, o := range overrides {
		for i := range gs {
			if gs[i].ID == o.ID {
				gs[i] = o
			}
		}
	}
	return gs
}

var (
	ciFail     = Gate{ID: "ci", State: GateFail, Detail: "unit"}
	ciUnknown  = Gate{ID: "ci", State: GateUnknown, Detail: "no CI result yet"}
	stacked    = Gate{ID: "not_stacked", State: GateFail, Detail: "carries 2 commit(s) of another PR"}
	secretLeak = Gate{ID: "no_secrets", State: GateFail, Detail: "1 hit(s): token=1"}
)

func TestClamp_FailingGateCapsAtChanges(t *testing.T) {
	got := Clamp(VerdictAccepted, gatesWith(ciFail))
	if got.Hold {
		t.Error("Hold = true, want false for a decided failing gate")
	}
	if got.Verdict != VerdictChanges {
		t.Errorf("Verdict = %q, want CHANGES", got.Verdict)
	}
	if !got.Capped {
		t.Error("Capped = false, want true (ACCEPTED was lowered)")
	}
	if len(got.Reasons) != 1 || !strings.Contains(got.Reasons[0], "gate ci failed: unit") {
		t.Errorf("Reasons = %v, want [gate ci failed: unit]", got.Reasons)
	}

	// A CHANGES seat with the same gate stays CHANGES, not capped.
	got = Clamp(VerdictChanges, gatesWith(ciFail))
	if got.Verdict != VerdictChanges || got.Capped {
		t.Errorf("Clamp(CHANGES, ci fail) = %+v, want Verdict CHANGES, Capped false", got)
	}
}

func TestClamp_StackedAndSecretCapAtBlocked(t *testing.T) {
	cases := []struct {
		name  string
		seat  string
		gates []Gate
	}{
		{"not_stacked alone, seat ACCEPTED", VerdictAccepted, gatesWith(stacked)},
		{"no_secrets alone, seat ACCEPTED", VerdictAccepted, gatesWith(secretLeak)},
		{"not_stacked alone, seat CHANGES", VerdictChanges, gatesWith(stacked)},
		{"no_secrets alone, seat CHANGES", VerdictChanges, gatesWith(secretLeak)},
		{"not_stacked with another failing gate", VerdictAccepted, gatesWith(stacked, ciFail)},
		{"no_secrets with another failing gate", VerdictChanges, gatesWith(secretLeak, ciFail)},
	}
	for _, tc := range cases {
		got := Clamp(tc.seat, tc.gates)
		if got.Verdict != VerdictBlocked {
			t.Errorf("%s: Verdict = %q, want BLOCKED", tc.name, got.Verdict)
		}
		if !got.Capped {
			t.Errorf("%s: Capped = false, want true", tc.name)
		}
		if got.Hold {
			t.Errorf("%s: Hold = true, want false", tc.name)
		}
		joined := strings.Join(got.Reasons, "; ")
		if !strings.Contains(joined, "failed") || !strings.Contains(joined, "not_stacked") && !strings.Contains(joined, "no_secrets") {
			t.Errorf("%s: Reasons = %v, want the blocking gate named", tc.name, got.Reasons)
		}
	}

	// Reasons follow gate order (ci comes before no_secrets in GateIDs).
	got := Clamp(VerdictAccepted, gatesWith(ciFail, secretLeak))
	joined := strings.Join(got.Reasons, "; ")
	if strings.Index(joined, "gate ci failed") > strings.Index(joined, "gate no_secrets failed") {
		t.Errorf("Reasons = %v, want ci before no_secrets (gate order)", got.Reasons)
	}
}

func TestClamp_SeatCannotRaiseAboveGates(t *testing.T) {
	cases := []struct {
		name   string
		seat   string
		gates  []Gate
		want   string
		capped bool
	}{
		{"seat BLOCKED with all gates pass stays BLOCKED", VerdictBlocked, gatesWith(), VerdictBlocked, false},
		{"seat CHANGES with all gates pass stays CHANGES, never ACCEPTED", VerdictChanges, gatesWith(), VerdictChanges, false},
		{"seat ACCEPTED with all gates pass is ACCEPTED", VerdictAccepted, gatesWith(), VerdictAccepted, false},
		{"seat BLOCKED with a failing gate stays BLOCKED", VerdictBlocked, gatesWith(ciFail), VerdictBlocked, false},
		{"seat CHANGES with a failing gate stays CHANGES", VerdictChanges, gatesWith(ciFail), VerdictChanges, false},
		{"seat ACCEPTED with a failing gate is lowered", VerdictAccepted, gatesWith(ciFail), VerdictChanges, true},
	}
	for _, tc := range cases {
		got := Clamp(tc.seat, tc.gates)
		if got.Verdict != tc.want {
			t.Errorf("%s: Verdict = %q, want %q", tc.name, got.Verdict, tc.want)
		}
		if got.Capped != tc.capped {
			t.Errorf("%s: Capped = %v, want %v", tc.name, got.Capped, tc.capped)
		}
	}
}

func TestClamp_CIPendingHoldsReviewPending(t *testing.T) {
	got := Clamp(VerdictAccepted, gatesWith(ciUnknown))
	if !got.Hold {
		t.Error("Hold = false, want true for an unknown gate")
	}
	if got.Verdict != "" {
		t.Errorf("Verdict = %q, want empty while held (nothing published)", got.Verdict)
	}
	if len(got.Reasons) == 0 || !strings.Contains(got.Reasons[0], "gate ci unknown") {
		t.Errorf("Reasons = %v, want the unknown gate named", got.Reasons)
	}

	// A held review holds whatever the seat said.
	if got := Clamp(VerdictBlocked, gatesWith(ciUnknown)); !got.Hold || got.Verdict != "" {
		t.Errorf("Clamp(BLOCKED, ci unknown) = %+v, want held with no verdict", got)
	}
	if got := Clamp(VerdictChanges, gatesWith(Gate{ID: "validator", State: GateUnknown})); !got.Hold || got.Verdict != "" {
		t.Errorf("Clamp(CHANGES, validator unknown) = %+v, want held with no verdict", got)
	}
}

func TestClamp_UnknownSeatValueFailsClosed(t *testing.T) {
	for _, seat := range []string{"", "MAYBE", "accepted", "Accepted"} {
		got := Clamp(seat, gatesWith())
		if got.Verdict != VerdictBlocked {
			t.Errorf("Clamp(%q, all pass) Verdict = %q, want BLOCKED", seat, got.Verdict)
		}
		if !got.Capped {
			t.Errorf("Clamp(%q, all pass) Capped = false, want true", seat)
		}
		if got := Clamp(seat, gatesWith(ciFail)); got.Verdict != VerdictBlocked {
			t.Errorf("Clamp(%q, ci fail) Verdict = %q, want BLOCKED", seat, got.Verdict)
		}
	}

	ranks := map[string]int{VerdictBlocked: 0, VerdictChanges: 1, VerdictAccepted: 2, "MAYBE": -1, "": -1, "accepted": -1}
	for in, want := range ranks {
		if got := Rank(in); got != want {
			t.Errorf("Rank(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestDaemonNeverWritesAccepted(t *testing.T) {
	if len(DaemonBlockedReasons) != 5 {
		t.Fatalf("DaemonBlockedReasons = %v, want exactly the five daemon reasons", DaemonBlockedReasons)
	}
	for _, r := range []string{"timeout", "budget", "crashed", "cancelled", "scope_violation"} {
		if !IsDaemonBlockedReason(r) {
			t.Errorf("IsDaemonBlockedReason(%q) = false, want true", r)
		}
	}
	for _, r := range []string{"", "ACCEPTED", "pass", "stale"} {
		if IsDaemonBlockedReason(r) {
			t.Errorf("IsDaemonBlockedReason(%q) = true, want false", r)
		}
	}

	sets := []struct {
		name  string
		gates []Gate
	}{
		{"all pass", gatesWith()},
		{"ci fail", gatesWith(ciFail)},
		{"not_stacked fail", gatesWith(stacked)},
		{"no_secrets fail", gatesWith(secretLeak)},
		{"ci and no_secrets fail", gatesWith(ciFail, secretLeak)},
		{"ci pending", gatesWith(ciUnknown)},
		{"validator pending", gatesWith(Gate{ID: "validator", State: GateUnknown, Detail: "waiting"})},
	}
	seats := []string{VerdictAccepted, VerdictChanges, VerdictBlocked, "", "MAYBE", "accepted"}
	for _, seat := range seats {
		for _, set := range sets {
			got := Clamp(seat, set.gates)
			if got.Verdict != VerdictAccepted {
				continue
			}
			if seat != VerdictAccepted {
				t.Errorf("Clamp(%q, %s) = ACCEPTED without the seat saying so", seat, set.name)
			}
			for _, g := range set.gates {
				if g.State == GateFail || g.State == GateUnknown {
					t.Errorf("Clamp(%q, %s) = ACCEPTED despite gate %s = %s", seat, set.name, g.ID, g.State)
				}
			}
		}
	}
}
