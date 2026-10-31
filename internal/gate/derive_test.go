package gate

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/runs/failure"
)

var t0 = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

type fakeWindows map[int64]routing.Verdict

func (f fakeWindows) Exhaustion(id int64, _ string, _ time.Time) routing.Verdict { return f[id] }

type fixture struct {
	provider  Provider
	accounts  []Account
	windows   fakeWindows
	provWin   *time.Time
	cooldown  *time.Time
	recent    *Recent
	fallbacks bool
}

func (f fixture) inputs() Inputs {
	return Inputs{
		Provider: func(id string) (Provider, bool) { return f.provider, id == f.provider.ID },
		Accounts: func(string) []Account { return f.accounts },
		Windows:  f.windows,
		ProviderWindow: func(string, time.Time) (time.Time, bool) {
			if f.provWin == nil {
				return time.Time{}, false
			}
			return *f.provWin, true
		},
		Cooldown: func(string, time.Time) (time.Time, bool) {
			if f.cooldown == nil {
				return time.Time{}, false
			}
			return *f.cooldown, true
		},
		Recent: f.recent,
	}
}

func base() fixture {
	return fixture{
		provider: Provider{ID: "claude-native", Label: "Claude Code", Harness: "claude-code", AccountProvider: "anthropic"},
		windows:  fakeWindows{},
		recent:   NewRecent(),
	}
}

func at(d time.Duration) *time.Time { u := t0.Add(d); return &u }

func derive(f fixture, model string) *Gate { return Derive(f.inputs(), "claude-native", model, t0) }

func TestDerive_NoGateWhenNothingKnown(t *testing.T) {
	f := base()
	f.accounts = nil // no smind account: runs use the CLI's own login (ADR-0020), never a gate
	if g := derive(f, ""); g != nil {
		t.Fatalf("gate = %+v, want none", g)
	}
}

func TestDerive_NotSignedIn_UnsetSecretIsHard(t *testing.T) {
	f := base()
	f.provider.UnsetSecrets = []string{"ANTHROPIC_AUTH_TOKEN"}
	g := derive(f, "")
	if g == nil || g.Reason != ReasonNotSignedIn || g.Advisory {
		t.Fatalf("gate = %+v, want hard not_signed_in", g)
	}
	if g.Action == nil || g.Action.Kind != failure.ActionSignIn || g.Action.Target != "settings:providers?provider=claude-native" {
		t.Fatalf("action = %+v", g.Action)
	}
}

func TestDerive_NotSignedIn_RecentAuthFailureIsAdvisory(t *testing.T) {
	f := base()
	f.recent.Mark("claude-native", "", ReasonNotSignedIn, 7, t0.Add(-time.Minute))
	g := derive(f, "")
	if g == nil || g.Reason != ReasonNotSignedIn || !g.Advisory || g.AccountID != 7 {
		t.Fatalf("gate = %+v, want advisory not_signed_in", g)
	}
	f.recent.Clear("claude-native")
	if g := derive(f, ""); g != nil {
		t.Fatalf("after Clear gate = %+v", g)
	}
	// The flag expires.
	f.recent.Mark("claude-native", "", ReasonNotSignedIn, 0, t0.Add(-2*RecentTTL))
	if g := derive(f, ""); g != nil {
		t.Fatalf("expired flag gated: %+v", g)
	}
}

func TestDerive_OutOfPlan_AllAccountsExhaustedIsAdvisoryWithReset(t *testing.T) {
	f := base()
	f.accounts = []Account{{ID: 1}, {ID: 2}}
	f.windows = fakeWindows{1: {Exhausted: true, Until: t0.Add(3 * time.Hour)}, 2: {Exhausted: true, Until: t0.Add(2 * time.Hour)}}
	g := derive(f, "")
	if g == nil || g.Reason != ReasonOutOfPlan || !g.Advisory {
		t.Fatalf("gate = %+v", g)
	}
	if g.Until == nil || !g.Until.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("until = %v, want the earliest end (first account to recover)", g.Until)
	}
	if g.Action == nil || g.Action.Kind != failure.ActionWaitUntil {
		t.Fatalf("action = %+v", g.Action)
	}
}

func TestDerive_OutOfPlan_OneHealthyAccountUngates(t *testing.T) {
	f := base()
	f.accounts = []Account{{ID: 1}, {ID: 2}}
	f.windows = fakeWindows{1: {Exhausted: true, Until: t0.Add(time.Hour)}}
	if g := derive(f, ""); g != nil {
		t.Fatalf("gate = %+v, want none", g)
	}
}

func TestDerive_OutOfPlan_TransientBackoffIsNotAGate(t *testing.T) {
	f := base()
	f.accounts = []Account{{ID: 1}}
	f.windows = fakeWindows{1: {Exhausted: true, TransientOnly: true, Until: t0.Add(30 * time.Second)}}
	if g := derive(f, ""); g != nil {
		t.Fatalf("gate = %+v, want none", g)
	}
}

func TestDerive_OutOfPlan_ModelScopedWindowGatesOnlyThatModel(t *testing.T) {
	f := base()
	f.accounts = []Account{{ID: 1}}
	in := f.inputs()
	in.Windows = modelWindows{"opus": routing.Verdict{Exhausted: true, Until: t0.Add(time.Hour)}}
	if g := Derive(in, "claude-native", "opus", t0); g == nil || g.Reason != ReasonOutOfPlan {
		t.Fatalf("opus gate = %+v", g)
	}
	if g := Derive(in, "claude-native", "sonnet", t0); g != nil {
		t.Fatalf("sonnet gate = %+v", g)
	}
}

type modelWindows map[string]routing.Verdict

func (m modelWindows) Exhaustion(_ int64, model string, _ time.Time) routing.Verdict { return m[model] }

func TestDerive_OutOfPlan_ProviderInstanceWindowIsHard(t *testing.T) {
	f := base()
	f.provWin = at(time.Hour)
	f.cooldown = at(time.Hour) // the merged cooldown contains the same window
	g := derive(f, "")
	if g == nil || g.Reason != ReasonOutOfPlan || g.Advisory {
		t.Fatalf("gate = %+v, want hard out_of_plan", g)
	}
}

func TestDerive_CoolingDown_StoredCooldown(t *testing.T) {
	f := base()
	f.cooldown = at(90 * time.Minute)
	g := derive(f, "")
	if g == nil || g.Reason != ReasonCoolingDown || g.Advisory {
		t.Fatalf("gate = %+v", g)
	}
	if g.Until == nil || !g.Until.Equal(*f.cooldown) {
		t.Fatalf("until = %v", g.Until)
	}
	if g.Action == nil || g.Action.Kind != failure.ActionClearCooldown || g.Action.Target != "claude-native" {
		t.Fatalf("action = %+v", g.Action)
	}
}

func TestDerive_CoolingDown_ExpiredCooldownIsNoGate(t *testing.T) {
	f := base()
	f.cooldown = at(-time.Minute)
	if g := derive(f, ""); g != nil {
		t.Fatalf("gate = %+v", g)
	}
}

func TestDerive_UnsupportedByHarness_CodexChatOnlyEndpoint(t *testing.T) {
	f := base()
	f.provider = Provider{ID: "claude-native", Label: "X", Harness: "codex-app-server", EndpointKind: "chat-completions"}
	g := derive(f, "")
	if g == nil || g.Reason != ReasonUnsupportedByHarness || g.Advisory {
		t.Fatalf("gate = %+v", g)
	}
	if g.Action == nil || g.Action.Kind != failure.ActionPickProvider {
		t.Fatalf("action = %+v", g.Action)
	}
	f.provider.EndpointKind = "openai-responses"
	if g := derive(f, ""); g != nil {
		t.Fatalf("responses endpoint gated: %+v", g)
	}
}

func TestDerive_ModelNotInPlan_RecentFailureIsAdvisoryPerModel(t *testing.T) {
	f := base()
	f.recent.Mark("claude-native", "opus", ReasonModelNotInPlan, 0, t0)
	g := derive(f, "opus")
	if g == nil || g.Reason != ReasonModelNotInPlan || !g.Advisory {
		t.Fatalf("gate = %+v", g)
	}
	if g.Action == nil || g.Action.Kind != failure.ActionPickModel {
		t.Fatalf("action = %+v", g.Action)
	}
	if g := derive(f, "sonnet"); g != nil {
		t.Fatalf("other model gated: %+v", g)
	}
	if g := derive(f, ""); g != nil {
		t.Fatalf("provider gated by a model failure: %+v", g)
	}
}

// TestDerive_PrecedenceMatrix runs every combination of the five conditions and
// checks the winner follows the documented order.
func TestDerive_PrecedenceMatrix(t *testing.T) {
	order := []string{ReasonUnsupportedByHarness, ReasonNotSignedIn, ReasonModelNotInPlan, ReasonOutOfPlan, ReasonCoolingDown}
	for mask := 0; mask < 1<<len(order); mask++ {
		f := base()
		f.accounts = []Account{{ID: 1}}
		want := ""
		for i, r := range order {
			if mask&(1<<i) == 0 {
				continue
			}
			if want == "" {
				want = r
			}
			switch r {
			case ReasonUnsupportedByHarness:
				f.provider.Harness, f.provider.EndpointKind = "codex-app-server", "chat-completions"
			case ReasonNotSignedIn:
				f.provider.UnsetSecrets = []string{"K"}
			case ReasonModelNotInPlan:
				f.recent.Mark("claude-native", "m", ReasonModelNotInPlan, 0, t0)
			case ReasonOutOfPlan:
				f.windows = fakeWindows{1: {Exhausted: true, Until: t0.Add(time.Hour)}}
			case ReasonCoolingDown:
				f.cooldown = at(time.Hour)
			}
		}
		g := derive(f, "m")
		got := ""
		if g != nil {
			got = g.Reason
		}
		if got != want {
			t.Fatalf("mask %05b: reason = %q, want %q", mask, got, want)
		}
	}
}

func TestDerive_UntilIsLaterOfWindowAndCooldown(t *testing.T) {
	f := base()
	f.provWin = at(time.Hour)
	f.cooldown = at(2 * time.Hour)
	g := derive(f, "")
	if g == nil || g.Until == nil || !g.Until.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("gate = %+v, want until = the later end", g)
	}
}

func TestModelGates_OnlyModelSpecificGates(t *testing.T) {
	f := base()
	f.recent.Mark("claude-native", "opus", ReasonModelNotInPlan, 0, t0)
	in := f.inputs()
	got := ModelGates(in, "claude-native", []string{"opus", "sonnet"}, t0)
	if len(got) != 1 || got["opus"].Reason != ReasonModelNotInPlan {
		t.Fatalf("modelGates = %+v", got)
	}
	f.cooldown = at(time.Hour) // provider-level gate: models inherit it client-side, not repeated
	if got := ModelGates(f.inputs(), "claude-native", []string{"sonnet"}, t0); len(got) != 0 {
		t.Fatalf("provider gate repeated per model: %+v", got)
	}
}

func TestTwinTable_CoversEveryHumanFixAndLimitClass(t *testing.T) {
	// Every class a picker row can prevent needs a twin; the rest are listed.
	noTwin := map[string]bool{failure.KindTransient429: true, failure.KindRateLimited: true, failure.KindCLIExited: true}
	for _, kind := range []string{failure.KindAuthExpired, failure.KindModelNotInPlan, failure.KindPlanWindowExhausted,
		failure.KindTransient429, failure.KindRateLimited, failure.KindCLIExited} {
		if noTwin[kind] {
			if _, ok := ReasonFor(kind); ok {
				t.Fatalf("%s must have no picker twin", kind)
			}
			continue
		}
		if _, ok := ReasonFor(kind); !ok {
			t.Fatalf("failure class %s has no picker twin", kind)
		}
	}
	for _, r := range []string{ReasonNotSignedIn, ReasonOutOfPlan, ReasonCoolingDown, ReasonModelNotInPlan} {
		if k, ok := TwinOf(r); !ok || !failure.IsKnown(k) {
			t.Fatalf("reason %s twin = %q, %v", r, k, ok)
		}
	}
	if _, ok := TwinOf(ReasonUnsupportedByHarness); ok {
		t.Fatal("unsupported_by_harness has no run-failure twin")
	}
}

// The gate is cache-only: it must not be able to reach a fetcher (ADR-0028).
func TestGate_PackageImportsNoFetcher(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasSuffix(p, "internal/quota") || strings.HasSuffix(p, "internal/accounts") || strings.Contains(p, "/fetch") {
				t.Fatalf("%s imports %s: the gate may only read through routing.Verdict and small func inputs", f, p)
			}
		}
	}
}

// Nobody but this package builds a Gate (AC3: one derivation).
func TestGate_NoSecondDerivation(t *testing.T) {
	root := "../.."
	var hits []string
	_ = filepath.Walk(root+"/internal", func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "internal/gate/") {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), "gate.Gate{") {
			hits = append(hits, p)
		}
		return nil
	})
	if len(hits) > 0 {
		t.Fatalf("gate.Gate built outside internal/gate: %v", hits)
	}
}
