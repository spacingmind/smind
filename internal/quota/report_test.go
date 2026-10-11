package quota

import (
	"encoding/json"
	"testing"
	"time"
)

func TestReportRoundtripAndVersioning(t *testing.T) {
	t.Parallel()

	expires := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	used := 42.5
	overage := true
	full := Report{
		Status:    StatusAvailable,
		PlanLabel: "pro",
		Windows:   []Window{newWindow("weekly", "Weekly", "wk", true, &used, &expires)},
		Balances:  []Balance{{ID: "credits", Label: "Credits", Remaining: &used, Unit: "credits"}},
		Details:   []Detail{{ID: "region", Label: "Region", Value: "us"}},
		FetchedAt: expires,
	}
	full.OverageEnabled = &overage
	full.Problem = &Problem{Kind: ProblemExpired, ExpiresAt: &expires, Detail: detailExpired}

	encoded, err := EncodeReport(full)
	if err != nil {
		t.Fatalf("EncodeReport() error = %v", err)
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(encoded), &probe); err != nil {
		t.Fatalf("encoded payload is not JSON: %v", err)
	}
	if probe["v"] != float64(ReportVersion) {
		t.Errorf("EncodeReport() v = %v, want %d", probe["v"], ReportVersion)
	}

	decoded, ok := DecodeReport(encoded)
	if !ok {
		t.Fatal("DecodeReport() ok = false, want true")
	}
	if !decoded.FetchedAt.Equal(full.FetchedAt) {
		t.Errorf("FetchedAt = %v, want %v", decoded.FetchedAt, full.FetchedAt)
	}
	if len(decoded.Windows) != 1 || decoded.Windows[0].ID != "weekly" {
		t.Errorf("Windows = %+v, want one weekly window", decoded.Windows)
	}
	if rem := decoded.Windows[0].RemainingPct; rem == nil || *rem != 57.5 {
		t.Errorf("RemainingPct = %v, want 57.5", rem)
	}
	if len(decoded.Balances) != 1 || decoded.Balances[0].ID != "credits" {
		t.Errorf("Balances = %+v", decoded.Balances)
	}
	if len(decoded.Details) != 1 || decoded.Details[0].Value != "us" {
		t.Errorf("Details = %+v", decoded.Details)
	}
	if decoded.OverageEnabled == nil || !*decoded.OverageEnabled {
		t.Errorf("OverageEnabled = %v, want true", decoded.OverageEnabled)
	}
	if decoded.Problem == nil || decoded.Problem.Kind != ProblemExpired || decoded.Problem.ExpiresAt == nil || !decoded.Problem.ExpiresAt.Equal(expires) {
		t.Errorf("Problem = %+v, want expired with ExpiresAt %v", decoded.Problem, expires)
	}

	bad := []struct {
		name string
		data string
	}{
		{"legacy usage shape", `{"tokens_used":0,"tokens_limit":0,"resets_at":"0001-01-01T00:00:00Z"}`},
		{"newer version", `{"v":3,"status":"available"}`},
		{"version one", `{"v":1}`},
		{"garbage", "{not json"},
		{"empty", ""},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := DecodeReport(tt.data); ok {
				t.Errorf("DecodeReport(%q) ok = true, want false", tt.data)
			}
		})
	}
}

func TestToneThresholds(t *testing.T) {
	t.Parallel()

	pct := func(v float64) *float64 { return &v }
	tests := []struct {
		name string
		in   *float64
		want Tone
	}{
		{"nil default", nil, ToneDefault},
		{"zero ok", pct(0), ToneOK},
		{"69.9 ok", pct(69.9), ToneOK},
		{"70 warning", pct(70), ToneWarning},
		{"90 warning", pct(90), ToneWarning},
		{"90.1 danger", pct(90.1), ToneDanger},
		{"100 danger", pct(100), ToneDanger},
		{"150 danger", pct(150), ToneDanger},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ToneFromUsedPct(tt.in); got != tt.want {
				t.Errorf("ToneFromUsedPct(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWindowAppliesTo(t *testing.T) {
	t.Parallel()

	modelScope := func(name string) *Scope { return &Scope{Kind: ScopeModel, Name: name} }
	tests := []struct {
		name  string
		scope *Scope
		model string
		want  bool
	}{
		{"opus matches opus id", modelScope("Opus"), "claude-opus-4-1", true},
		{"opus not sonnet", modelScope("Opus"), "claude-sonnet-4-5", false},
		{"case insensitive", modelScope("Opus"), "CLAUDE-OPUS-4", true},
		{"scope name with number", &Scope{Kind: ScopeModel, Name: "Sonnet 4"}, "claude-sonnet-4-5", true},
		{"surface never applies", &Scope{Kind: ScopeSurface, Name: "Opus"}, "claude-opus-4-1", false},
		{"feature never applies", &Scope{Kind: ScopeFeature, Name: "Opus"}, "claude-opus-4-1", false},
		{"nil scope always applies", nil, "claude-opus-4-1", true},
		{"nil scope empty model", nil, "", true},
		{"model scope empty model", modelScope("Opus"), "", false},
		{"model scope empty name", modelScope(""), "claude-opus-4-1", false},
		{"model scope whitespace name", modelScope("   "), "claude-opus-4-1", false},
		{"model scope punctuation name", modelScope("---"), "claude-opus-4-1", false},
		{"unrelated word", modelScope("Omelette"), "claude-opus-4-1", false},
		{"unrelated word gpt", modelScope("Omelette"), "gpt-5", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := Window{Scope: tt.scope}
			if got := w.AppliesTo(tt.model); got != tt.want {
				t.Errorf("Window{Scope:%+v}.AppliesTo(%q) = %v, want %v", tt.scope, tt.model, got, tt.want)
			}
		})
	}
}

func TestNewWindowDerivesFields(t *testing.T) {
	t.Parallel()

	pct := func(v float64) *float64 { return &v }
	reset := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	t.Run("remaining derived", func(t *testing.T) {
		t.Parallel()
		w := newWindow("five_hour", "5-hour", "5h", true, pct(30), &reset)
		if w.RemainingPct == nil || *w.RemainingPct != 70 {
			t.Errorf("RemainingPct = %v, want 70", w.RemainingPct)
		}
		if w.Tone != ToneOK {
			t.Errorf("Tone = %q, want ok", w.Tone)
		}
	})

	t.Run("nil used", func(t *testing.T) {
		t.Parallel()
		w := newWindow("five_hour", "5-hour", "5h", true, nil, &reset)
		if w.RemainingPct != nil {
			t.Errorf("RemainingPct = %v, want nil", w.RemainingPct)
		}
		if w.Tone != ToneDefault {
			t.Errorf("Tone = %q, want default", w.Tone)
		}
	})

	t.Run("overuse clamps to zero", func(t *testing.T) {
		t.Parallel()
		w := newWindow("five_hour", "5-hour", "5h", true, pct(130), &reset)
		if w.RemainingPct == nil || *w.RemainingPct != 0 {
			t.Errorf("RemainingPct = %v, want 0", w.RemainingPct)
		}
	})
}

func TestNewWindowRemainingIsClamped(t *testing.T) {
	t.Parallel()
	neg := -50.0
	if w := newWindow("w", "W", "", false, &neg, nil); w.RemainingPct == nil || *w.RemainingPct != 100 {
		t.Errorf("RemainingPct for -50%% used = %v, want 100", w.RemainingPct)
	}
}
