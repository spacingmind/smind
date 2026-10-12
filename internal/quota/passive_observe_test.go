package quota

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// quota_signals_test.go:68-110 header set.
func anthropicFullHeaders() http.Header {
	return http.Header{
		"Anthropic-Ratelimit-Unified-5h-Status":               {"allowed"},
		"Anthropic-Ratelimit-Unified-5h-Utilization":          {"0.0"},
		"Anthropic-Ratelimit-Unified-5h-Reset":                {"1787296800"},
		"Anthropic-Ratelimit-Unified-7d-Status":               {"allowed"},
		"Anthropic-Ratelimit-Unified-7d-Utilization":          {"0.53"},
		"Anthropic-Ratelimit-Unified-7d-Reset":                {"1787695200"},
		"Anthropic-Ratelimit-Unified-Fallback-Percentage":     {"0.5"},
		"Anthropic-Ratelimit-Unified-Overage-Disabled-Reason": {"member_zero_credit_limit"},
		"Anthropic-Ratelimit-Unified-Overage-Status":          {"rejected"},
		"Anthropic-Ratelimit-Unified-Representative-Claim":    {"five_hour"},
		"Anthropic-Ratelimit-Unified-Reset":                   {"1787296800"},
		"Anthropic-Ratelimit-Unified-Status":                  {"allowed"},
		"Anthropic-Workspace-Id":                              {"workspace-must-not-be-quota-signal"},
	}
}

func windowByID(o Observation, id string) (Window, bool) {
	for _, w := range o.Windows {
		if w.ID == id {
			return w, true
		}
	}
	return Window{}, false
}

func Test_observed_anthropic_full_header_set_typed(t *testing.T) {
	t.Parallel()
	at := time.Unix(1787279282, 0)
	h := anthropicFullHeaders()
	h["anthropic-ratelimit-unified-7d_oi-status"] = []string{"allowed_warning"}
	h["anthropic-ratelimit-unified-7d_oi-utilization"] = []string{"0.91"}
	h["anthropic-ratelimit-unified-7d_oi-reset"] = []string{"1787695200"}
	o, ok := ParseAnthropicObservation(h, at)
	if !ok {
		t.Fatal("no observation")
	}
	five, _ := windowByID(o, "five_hour")
	if five.UsedPct == nil || *five.UsedPct != 0 || five.Source != "observed" || five.Status != "allowed" || !five.Summary || five.Label != "5-hour" {
		t.Fatalf("five_hour = %+v", five)
	}
	week, _ := windowByID(o, "weekly")
	if week.UsedPct == nil || *week.UsedPct < 52.99 || *week.UsedPct > 53.01 || week.Source != "observed" {
		t.Fatalf("weekly = %+v", week)
	}
	if week.ResetsAt == nil || week.ResetsAt.Unix() != 1787695200 {
		t.Fatalf("weekly reset = %v", week.ResetsAt)
	}
	oi, ok := windowByID(o, "weekly_oi")
	if !ok || oi.Scope == nil || oi.Scope.Kind != ScopeFeature || oi.Scope.Name != "7d_oi" || oi.Tone != ToneWarning || oi.AppliesTo("claude-opus-4") {
		t.Fatalf("weekly_oi = %+v", oi)
	}
	if o.UnifiedStatus != "allowed" || o.Representative != "five_hour" || o.OverageStatus != "rejected" || !o.At.Equal(at) {
		t.Fatalf("scalars = %+v", o)
	}
	if v := o.Verdict(); v != "allowed_warning" {
		t.Fatalf("verdict %q", v)
	}
}

func Test_observed_window_values(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, util string
		want       *float64
	}{
		{"clamped high", "1.7", ptr(100.0)},
		{"clamped low", "-0.2", ptr(0.0)},
		{"nan", "NaN", nil},
		{"inf", "+Inf", nil},
		{"garbage", "lots", nil},
	}
	for _, tc := range tests {
		h := http.Header{"Anthropic-Ratelimit-Unified-5h-Status": {"allowed"}, "Anthropic-Ratelimit-Unified-5h-Utilization": {tc.util}}
		o, ok := ParseAnthropicObservation(h, passiveNow)
		if !ok {
			t.Fatalf("%s: not ok", tc.name)
		}
		got := o.Windows[0].UsedPct
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
	// rejected → danger regardless of percent; bogus status → no status.
	o, _ := ParseAnthropicObservation(http.Header{"Anthropic-Ratelimit-Unified-5h-Status": {"rejected"}, "Anthropic-Ratelimit-Unified-5h-Utilization": {"0.1"}}, passiveNow)
	if o.Windows[0].Tone != ToneDanger {
		t.Fatalf("tone %v", o.Windows[0].Tone)
	}
	if _, ok := ParseAnthropicObservation(http.Header{"Anthropic-Ratelimit-Unified-5h-Status": {"bogus"}, "Anthropic-Ratelimit-Unified-Representative-Claim": {"nope"}}, passiveNow); ok {
		t.Fatal("bogus values produced an observation")
	}
}

func ptr[T any](v T) *T { return &v }

func Test_observed_bounds_64_headers_512_chars_no_control_chars(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	for i := 0; i < 100; i++ {
		h[fmt.Sprintf("Anthropic-Ratelimit-Unified-Zz%03d", i)] = []string{"x"}
	}
	got := collectSignals(h, isAnthropicSignalHeader)
	if len(got) != 64 {
		t.Fatalf("kept %d headers", len(got))
	}
	if _, ok := got["anthropic-ratelimit-unified-zz000"]; !ok {
		t.Fatal("not deterministic: first sorted name missing")
	}
	if _, ok := got["anthropic-ratelimit-unified-zz064"]; ok {
		t.Fatal("65th name kept")
	}

	h = http.Header{
		"Anthropic-Ratelimit-Unified-Status":               {"allowed"},
		"Anthropic-Ratelimit-Unified-Representative-Claim": {"five_hour\r\nforged"},
		"Anthropic-Ratelimit-Unified-Overage-Status":       {"rejected\x7f"},
		"Anthropic-Ratelimit-Unified-5h-Status":            {strings.Repeat("a", 513)},
		"Anthropic-Ratelimit-Unified-7d-Status":            {strings.Repeat("a", 512)},
		"Anthropic-Ratelimit-Unified-Reset":                {""},
		"Retry-After":                                      {"1", "2", "3"},
	}
	got = collectSignals(h, isAnthropicSignalHeader)
	if len(got) != 3 || got["anthropic-ratelimit-unified-status"] != "allowed" ||
		got["anthropic-ratelimit-unified-7d-status"] == "" || got["retry-after"] != "3" {
		t.Fatalf("got %v", got)
	}
	o, ok := ParseAnthropicObservation(h, passiveNow)
	if !ok || o.Representative != "" || o.OverageStatus != "" {
		t.Fatalf("control char value used: %+v", o)
	}
}

func Test_observed_unrelated_headers_dropped(t *testing.T) {
	t.Parallel()
	got := collectSignals(anthropicFullHeaders(), isAnthropicSignalHeader)
	if _, ok := got["anthropic-workspace-id"]; ok {
		t.Fatal("workspace id kept")
	}
	if _, ok := ParseAnthropicObservation(http.Header{"Anthropic-Workspace-Id": {"w"}, "Content-Type": {"x"}, "Retry-After": {"30"}}, passiveNow); ok {
		t.Fatal("Retry-After alone is not a quota signal")
	}
}

func Test_observed_provider_scoped(t *testing.T) {
	t.Parallel()
	codexOnly := http.Header{"X-Codex-Primary-Used-Percent": {"51"}, "X-Codex-Primary-Window-Minutes": {"300"}, "X-Codex-Plan-Type": {"pro"}}
	if _, ok := ParseAnthropicObservation(codexOnly, passiveNow); ok {
		t.Fatal("anthropic parser read x-codex-*")
	}
	mixed := anthropicFullHeaders()
	mixed["X-Codex-Plan-Type"] = []string{"pro"}
	o, _ := ParseAnthropicObservation(mixed, passiveNow)
	if o.PlanType != "" {
		t.Fatalf("plan type leaked: %q", o.PlanType)
	}
}

func Test_observed_x_ratelimit_headers_ignored(t *testing.T) {
	t.Parallel()
	h := http.Header{"X-Ratelimit-Remaining-Requests": {"3"}, "X-Ratelimit-Limit-Requests": {"10"}, "Retry-After": {"5"}}
	if _, ok := ParseAnthropicObservation(h, passiveNow); ok {
		t.Fatal("anthropic read x-ratelimit-*")
	}
}

func Test_observed_no_raw_headers_in_report(t *testing.T) {
	t.Parallel()
	const sentinel = "SENTINEL-raw-value-7f3a"
	h := anthropicFullHeaders()
	h["Anthropic-Ratelimit-Unified-Overage-Disabled-Reason"] = []string{sentinel}
	h["Anthropic-Ratelimit-Unified-Fallback-Percentage"] = []string{sentinel}
	h["Anthropic-Ratelimit-Unified-Representative-Claim"] = []string{sentinel}
	h["Anthropic-Ratelimit-Unified-Overage-Status"] = []string{sentinel}
	h["Anthropic-Ratelimit-Unified-5h-Status"] = []string{sentinel}
	h["Anthropic-Ratelimit-Unified-Unknown"] = []string{sentinel}
	o, ok := ParseAnthropicObservation(h, passiveNow)
	if !ok {
		t.Fatal("no observation")
	}
	enc, err := EncodeReport(o.Report())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enc, sentinel) {
		t.Fatalf("raw header value in report: %s", enc)
	}
}

func Test_observed_report_roundtrip(t *testing.T) {
	t.Parallel()
	h := anthropicFullHeaders()
	h["anthropic-ratelimit-unified-7d_oi-status"] = []string{"rejected"}
	o, _ := ParseAnthropicObservation(h, passiveNow)
	o.UnifiedStatus = "" // not carried by a Report
	enc, err := EncodeReport(o.Report())
	if err != nil {
		t.Fatal(err)
	}
	dec, ok := DecodeReport(enc)
	if !ok || dec.Status != StatusAvailable || !dec.FetchedAt.Equal(passiveNow) {
		t.Fatalf("decode: %+v %v", dec, ok)
	}
	back, ok := ObservationFromReport(dec)
	if !ok || !back.Equal(o) || !back.At.Equal(o.At) {
		t.Fatalf("round trip mismatch:\n%+v\n%+v", back, o)
	}
	if len(dec.Details) != 2 || dec.Details[0].ID != "representative_claim" || dec.Details[1].ID != "overage_status" {
		t.Fatalf("details %+v", dec.Details)
	}
	if _, ok := ObservationFromReport(Report{}); ok {
		t.Fatal("empty report is not an observation")
	}
}

func Test_observed_equal_and_verdict(t *testing.T) {
	t.Parallel()
	h := anthropicFullHeaders()
	a, _ := ParseAnthropicObservation(h, passiveNow)
	b, _ := ParseAnthropicObservation(h, passiveNow.Add(time.Hour))
	if !a.Equal(b) {
		t.Fatal("At must be ignored")
	}
	h2 := anthropicFullHeaders()
	h2["Anthropic-Ratelimit-Unified-7d-Utilization"] = []string{"0.54"}
	c, _ := ParseAnthropicObservation(h2, passiveNow)
	if a.Equal(c) {
		t.Fatal("different utilization equal")
	}
	h3 := anthropicFullHeaders()
	delete(h3, "Anthropic-Ratelimit-Unified-Representative-Claim")
	d, _ := ParseAnthropicObservation(h3, passiveNow)
	if a.Equal(d) {
		t.Fatal("missing representative claim equal")
	}
	tests := []struct {
		name string
		o    Observation
		want string
	}{
		{"empty", Observation{}, ""},
		{"unified allowed", Observation{UnifiedStatus: "allowed"}, "allowed"},
		{"window warning beats unified allowed", Observation{UnifiedStatus: "allowed", Windows: []Window{{Status: "allowed_warning"}}}, "allowed_warning"},
		{"window rejected beats all", Observation{UnifiedStatus: "allowed_warning", Windows: []Window{{Status: "allowed"}, {Status: "rejected"}}}, "rejected"},
		{"unified rejected", Observation{UnifiedStatus: "rejected", Windows: []Window{{Status: "allowed"}}}, "rejected"},
	}
	for _, tc := range tests {
		if got := tc.o.Verdict(); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}
