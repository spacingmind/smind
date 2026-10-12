package quota

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func Test_observed_codex_http_headers_primary_secondary_additional_credits_plan(t *testing.T) {
	t.Parallel()
	at := time.Unix(1787279282, 0)
	// codex-rs/core/tests/suite/client.rs:3206-3222 plus quota_signals_test.go.
	h := http.Header{
		"X-Codex-Plan-Type":                          {"pro"},
		"X-Codex-Primary-Used-Percent":               {"12.5"},
		"X-Codex-Primary-Window-Minutes":             {"300"},
		"X-Codex-Primary-Reset-At":                   {"1787588999"},
		"X-Codex-Secondary-Used-Percent":             {"40.0"},
		"X-Codex-Secondary-Window-Minutes":           {"10080"},
		"X-Codex-Secondary-Reset-After-Seconds":      {"3600"},
		"X-Codex-Bengalfox-Limit-Name":               {"GPT-5.3-Codex-Spark"},
		"X-Codex-Bengalfox-Secondary-Used-Percent":   {"35"},
		"X-Codex-Bengalfox-Secondary-Window-Minutes": {"90"},
		"X-Codex-Bengalfox-Secondary-Reset-At":       {"1787590000"},
		"X-Codex-Bengalfox-Limit-Reached":            {"true"},
		"X-Codex-Credits-Has-Credits":                {"False"},
		"X-Codex-Credits-Balance":                    {"0"},
		"X-Ratelimit-Remaining":                      {"1"},
		"Retry-After":                                {"30"},
		"X-Codex-Turn-State":                         {"nope"},
	}
	o, ok := ParseCodexObservation(h, at)
	if !ok || o.PlanType != "pro" {
		t.Fatalf("o=%+v ok=%v", o, ok)
	}
	if len(o.Windows) != 3 {
		t.Fatalf("windows = %+v", o.Windows)
	}
	five := o.Windows[0]
	if five.ID != "five_hour" || *five.UsedPct != 12.5 || five.ResetsAt.Unix() != 1787588999 || *five.DurationSeconds != 18000 || five.Source != "observed" {
		t.Fatalf("primary = %+v", five)
	}
	week := o.Windows[1]
	if week.ID != "weekly" || *week.UsedPct != 40 || !week.ResetsAt.Equal(at.Add(time.Hour)) || *week.DurationSeconds != 604800 {
		t.Fatalf("secondary = %+v", week)
	}
	extra := o.Windows[2]
	if extra.ID != "bengalfox_window_90m" || extra.Scope == nil || extra.Scope.Kind != ScopeFeature || extra.Scope.Name != "bengalfox" ||
		extra.Status != "rejected" || extra.Tone != ToneDanger || !strings.Contains(extra.Label, "GPT-5.3-Codex-Spark") || extra.AppliesTo("gpt-5") {
		t.Fatalf("additional = %+v", extra)
	}
	enc, _ := EncodeReport(o.Report())
	if strings.Contains(enc, "Credits") || strings.Contains(strings.ToLower(enc), "turn-state") {
		t.Fatalf("credits or unrelated header leaked: %s", enc)
	}

	// Allowed true → allowed; no window data at all → not ok.
	o, _ = ParseCodexObservation(http.Header{"x-codex-allowed": {"true"}, "x-codex-primary-used-percent": {"1"}}, at)
	if len(o.Windows) != 1 || o.Windows[0].Status != "allowed" || o.Windows[0].ID != "primary" {
		t.Fatalf("allowed: %+v", o)
	}
	if _, ok := ParseCodexObservation(http.Header{"x-codex-primary-window-minutes": {"300"}, "x-codex-credits-balance": {"5"}}, at); ok {
		t.Fatal("window without used-percent or reset accepted")
	}
	// Provider scoping and header bounds.
	if _, ok := ParseCodexObservation(anthropicFullHeaders(), at); ok {
		t.Fatal("codex parser read anthropic headers")
	}
	if _, ok := ParseCodexObservation(http.Header{"X-Ratelimit-Remaining-Requests": {"3"}, "Retry-After": {"5"}}, at); ok {
		t.Fatal("codex read x-ratelimit-* or retry-after")
	}
	if _, ok := ParseCodexObservation(http.Header{"X-Codex-Primary-Used-Percent": {"5\r\n"}, "X-Codex-Plan-Type": {"a\nb"}}, at); !ok {
		// "5\r\n" trims to "5" (valid); plan type with inner LF is dropped.
		t.Fatal("trimmed value should parse")
	}
	o, _ = ParseCodexObservation(http.Header{"X-Codex-Plan-Type": {"a\nb"}}, at)
	if o.PlanType != "" {
		t.Fatalf("control char plan type kept: %q", o.PlanType)
	}
}

func Test_observed_codex_websocket_quota_event_normalized(t *testing.T) {
	t.Parallel()
	at := time.Unix(1787279282, 0)
	ws := CodexQuotaEventHeaders([]byte(`{
		"type":"codex.rate_limits",
		"plan_type":"pro",
		"metered_limit_name":"codex_bengalfox",
		"rate_limits":{"allowed":true,"limit_reached":false,
			"primary":{"used_percent":81,"window_minutes":10080,"reset_after_seconds":137160,"reset_at":1787588999},"secondary":null},
		"code_review_rate_limits":{"primary":{"used_percent":4,"window_minutes":300,"reset_after_seconds":900}},
		"additional_rate_limits":{"GPT-5.3-Codex-Spark":{"allowed":true,"limit_reached":false,
			"primary":{"used_percent":0,"window_minutes":300,"reset_after_seconds":18000}}},
		"credits":{"has_credits":false,"unlimited":false,"balance":"0"},
		"promo":null
	}`))
	for key, want := range map[string]string{
		"X-Codex-Plan-Type":                                           "pro",
		"X-Codex-Active-Limit":                                        "codex_bengalfox",
		"X-Codex-Primary-Used-Percent":                                "81",
		"X-Codex-Primary-Reset-At":                                    "1787588999",
		"X-Codex-Code-Review-Primary-Used-Percent":                    "4",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Limit-Name":           "GPT-5.3-Codex-Spark",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Primary-Used-Percent": "0",
		"X-Codex-Credits-Balance":                                     "0",
		"X-Codex-Credits-Has-Credits":                                 "false",
	} {
		if got := ws.Get(key); got != want {
			t.Fatalf("header %s = %q, want %q; %v", key, got, want, ws)
		}
	}
	if ws.Get("X-Codex-Secondary-Used-Percent") != "" {
		t.Fatal("null secondary materialized")
	}
	o, ok := ParseCodexObservation(ws, at)
	if !ok || o.PlanType != "pro" {
		t.Fatalf("observation from ws headers: %+v %v", o, ok)
	}
	var scopes []string
	for _, w := range o.Windows {
		if w.Scope != nil {
			scopes = append(scopes, w.Scope.Name)
		}
	}
	if len(o.Windows) != 3 || strings.Join(scopes, ",") != "code-review,gpt-5.3-codex-spark" {
		t.Fatalf("windows/scopes = %+v / %v", o.Windows, scopes)
	}

	// Array shape and camelCase give the same data as the object shape.
	arr := CodexQuotaEventHeaders([]byte(`{
		"type":"codex.rate_limits","planType":"pro",
		"rateLimit":{"allowed":true,"limitReached":false,"primary":{"usedPercent":71,"windowMinutes":10080,"resetAfterSeconds":60}},
		"additionalRateLimits":[{"limitName":"GPT-5.3-Codex-Spark","rateLimit":{
			"primary":{"usedPercent":13,"windowMinutes":300,"resetAt":1787399817},
			"secondary":{"usedPercent":51,"windowMinutes":10080,"resetAfterSeconds":120}}}]
	}`))
	for key, want := range map[string]string{
		"X-Codex-Primary-Used-Percent":                                  "71",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Primary-Reset-At":       "1787399817",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Secondary-Used-Percent": "51",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Limit-Name":             "GPT-5.3-Codex-Spark",
	} {
		if got := arr.Get(key); got != want {
			t.Fatalf("array header %s = %q, want %q", key, got, want)
		}
	}

	// HTTP spelling (short name) and websocket spelling are both feature windows.
	httpO, _ := ParseCodexObservation(http.Header{"X-Codex-Bengalfox-Primary-Used-Percent": {"3"}, "X-Codex-Bengalfox-Primary-Window-Minutes": {"300"}, "X-Codex-Bengalfox-Primary-Reset-After-Seconds": {"5"}}, at)
	if len(httpO.Windows) != 1 || httpO.Windows[0].Scope.Name != "bengalfox" {
		t.Fatalf("http additional: %+v", httpO.Windows)
	}

	// 8-cap, in document order.
	var sb strings.Builder
	sb.WriteString(`{"type":"codex.rate_limits","additional_rate_limits":{`)
	for i := 0; i < 12; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `"z%02d":{"primary":{"used_percent":1,"window_minutes":300,"reset_after_seconds":60}}`, 11-i)
	}
	sb.WriteString("}}")
	capped := CodexQuotaEventHeaders([]byte(sb.String()))
	count := 0
	for k := range capped {
		if strings.HasSuffix(k, "-Limit-Name") {
			count++
		}
	}
	if count != 8 || capped.Get("X-Codex-Additional-z11-Limit-Name") != "z11" || capped.Get("X-Codex-Additional-z04-Limit-Name") != "z04" || capped.Get("X-Codex-Additional-z03-Limit-Name") != "" {
		t.Fatalf("cap: count=%d %v", count, capped)
	}

	// Error-event headers: only quota names survive.
	errH := CodexQuotaEventHeaders([]byte(`{"type":"error","status_code":429,"headers":{
		"X-Codex-Primary-Used-Percent":"100","X-Codex-Primary-Window-Minutes":"10080",
		"X-Codex-Primary-Reset-After-Seconds":"437380","X-Codex-Credits-Balance":"0",
		"X-Codex-Turn-State":"no","Set-Cookie":"no"}}`))
	if errH.Get("X-Codex-Primary-Used-Percent") != "100" || errH.Get("X-Codex-Credits-Balance") != "0" || errH.Get("Set-Cookie") != "" || errH.Get("X-Codex-Turn-State") != "" {
		t.Fatalf("error headers: %v", errH)
	}
	eo, ok := ParseCodexObservation(errH, at)
	if !ok || eo.Windows[0].ID != "weekly" || *eo.Windows[0].UsedPct != 100 {
		t.Fatalf("error observation: %+v", eo)
	}

	// Rejections of the ref: incomplete, out of range, non-quota, control chars, bad JSON.
	for name, p := range map[string]string{
		"incomplete":  `{"type":"codex.rate_limits","rate_limits":{"primary":{"used_percent":42}}}`,
		"over 100":    `{"type":"codex.rate_limits","rate_limits":{"primary":{"used_percent":101,"window_minutes":300,"reset_after_seconds":60}}}`,
		"other event": `{"type":"response.created","rate_limits":{"primary":{"used_percent":1,"window_minutes":300,"reset_after_seconds":60}}}`,
		"bad json":    `{"type":"codex.rate_limits",`,
		"empty":       ``,
	} {
		if got := CodexQuotaEventHeaders([]byte(p)); got != nil {
			t.Errorf("%s: got %v", name, got)
		}
	}
	evil := CodexQuotaEventHeaders([]byte(`{"type":"codex.rate_limits","additional_rate_limits":[{"limit_name":"evil\r\nX-Injected: 1","primary":{"used_percent":3,"window_minutes":300,"reset_after_seconds":60}}]}`))
	if evil == nil {
		t.Fatal("window did not survive")
	}
	for k, vs := range evil {
		for _, v := range vs {
			if strings.ContainsAny(v, "\r\n") {
				t.Fatalf("%s carries control chars: %q", k, v)
			}
		}
	}
	keep := CodexQuotaEventHeaders([]byte(`{"type":"codex.rate_limits","metered_limit_name":"bad limit","rate_limits":{"primary":{"used_percent":1,"window_minutes":300,"reset_after_seconds":60}}}`))
	if keep.Get("X-Codex-Primary-Used-Percent") != "1" || keep.Get("X-Codex-Active-Limit") != "" {
		t.Fatalf("invalid active limit handling: %v", keep)
	}
}
