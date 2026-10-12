package quota

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Observed rate-limit headers (ADR-0028 §7.2, D14). Display data only: nothing
// here is read by routing. Mirrors refs/cliproxyapi/sdk/cliproxy/auth/
// quota_signals.go (ObserveResponseHeadersForProvider, isQuotaSignalHeader
// ForProvider), but stores typed values, never raw header text.

const (
	observeMaxHeaders  = 64
	observeMaxValueLen = 512

	windowSourceObserved = "observed"

	statusAllowed        = "allowed"
	statusAllowedWarning = "allowed_warning"
	statusRejected       = "rejected"
)

// Observation is the typed quota signal of one upstream response. It carries
// no raw header text.
type Observation struct {
	At             time.Time
	Windows        []Window
	UnifiedStatus  string
	Representative string
	OverageStatus  string
	PlanType       string
}

// collectSignals returns the allowlisted, bounded, lower-cased header values.
// Names are filtered by allow, sorted, capped at 64, and only then are values
// checked (non-empty, at most 512 characters, no control character). The last
// value of a repeated header wins, and the result does not depend on map
// iteration order.
func collectSignals(h http.Header, allow func(lowerName string) bool) map[string]string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	last := map[string]string{}
	for _, k := range keys {
		lower := strings.ToLower(strings.TrimSpace(k))
		if !allow(lower) {
			continue
		}
		vals := h[k]
		if len(vals) == 0 {
			continue
		}
		last[lower] = vals[len(vals)-1]
	}
	names := make([]string, 0, len(last))
	for n := range last {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > observeMaxHeaders {
		names = names[:observeMaxHeaders]
	}
	out := make(map[string]string, len(names))
	for _, n := range names {
		if v := strings.TrimSpace(last[n]); validSignalValue(v) {
			out[n] = v
		}
	}
	return out
}

func validSignalValue(v string) bool {
	if v == "" || len(v) > observeMaxValueLen {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] == 0x7f {
			return false
		}
	}
	return true
}

func isAnthropicSignalHeader(lower string) bool {
	return lower == "retry-after" || strings.HasPrefix(lower, "anthropic-ratelimit-unified-")
}

var anthropicRepresentativeClaims = map[string]bool{
	"five_hour": true, "seven_day": true, "seven_day_opus": true,
	"seven_day_sonnet": true, "seven_day_oi": true, "overage": true,
}

// ParseAnthropicObservation reads the typed signals of one response. ok is
// false when it carries no usable quota signal.
func ParseAnthropicObservation(h http.Header, at time.Time) (Observation, bool) {
	sig := collectSignals(h, isAnthropicSignalHeader)
	const p = "anthropic-ratelimit-unified-"
	o := Observation{
		At:            at,
		UnifiedStatus: enumStatus(sig[p+"status"]),
		OverageStatus: enumStatus(sig[p+"overage-status"]),
	}
	if rep := strings.ToLower(sig[p+"representative-claim"]); anthropicRepresentativeClaims[rep] {
		o.Representative = rep
	}
	specs := []struct {
		key string
		win Window
	}{
		{"5h", Window{ID: "five_hour", Label: "5-hour", ShortLabel: "5h", Summary: true}},
		{"7d", Window{ID: "weekly", Label: "Weekly", ShortLabel: "7d"}},
		{"7d_oi", Window{ID: "weekly_oi", Label: "Weekly (model-specific)", ShortLabel: "7d oi",
			Scope: &Scope{Kind: ScopeFeature, Name: "7d_oi"}}},
	}
	for _, s := range specs {
		status := enumStatus(sig[p+s.key+"-status"])
		used := fractionToPct(sig[p+s.key+"-utilization"])
		var resets *time.Time
		if t, ok := parseResetTime(sig[p+s.key+"-reset"]); ok {
			resets = &t
		}
		if status == "" && used == nil && resets == nil {
			continue
		}
		w := newWindow(s.win.ID, s.win.Label, s.win.ShortLabel, s.win.Summary, used, resets)
		w.Scope = s.win.Scope
		o.Windows = append(o.Windows, observedWindow(w, status))
	}
	ok := len(o.Windows) > 0 || o.UnifiedStatus != "" || o.Representative != "" || o.OverageStatus != ""
	if !ok {
		return Observation{}, false
	}
	return o, true
}

// observedWindow stamps the observed source and status and derives the tone.
func observedWindow(w Window, status string) Window {
	w.Source = windowSourceObserved
	w.Status = status
	switch status {
	case statusRejected:
		w.Tone = ToneDanger
	case statusAllowedWarning:
		w.Tone = ToneWarning
	}
	return w
}

func enumStatus(v string) string {
	switch v = strings.ToLower(strings.TrimSpace(v)); v {
	case statusAllowed, statusAllowedWarning, statusRejected:
		return v
	}
	return ""
}

// fractionToPct converts a 0..1 utilization fraction to a clamped percent.
func fractionToPct(raw string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return clampPct(f * 100)
}

func clampPct(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	v = math.Max(0, math.Min(100, v))
	return &v
}

// Equal compares the parsed values, ignoring At.
func (o Observation) Equal(p Observation) bool {
	if o.UnifiedStatus != p.UnifiedStatus || o.Representative != p.Representative ||
		o.OverageStatus != p.OverageStatus || o.PlanType != p.PlanType || len(o.Windows) != len(p.Windows) {
		return false
	}
	for i := range o.Windows {
		if !windowEqual(o.Windows[i], p.Windows[i]) {
			return false
		}
	}
	return true
}

func windowEqual(a, b Window) bool {
	return a.ID == b.ID && a.Label == b.Label && a.ShortLabel == b.ShortLabel && a.Summary == b.Summary &&
		a.Source == b.Source && a.Status == b.Status && a.Tone == b.Tone &&
		scopeEqual(a.Scope, b.Scope) &&
		ptrEqual(a.UsedPct, b.UsedPct) && ptrEqual(a.RemainingPct, b.RemainingPct) &&
		ptrEqual(a.DurationSeconds, b.DurationSeconds) &&
		((a.ResetsAt == nil && b.ResetsAt == nil) || (a.ResetsAt != nil && b.ResetsAt != nil && a.ResetsAt.Equal(*b.ResetsAt)))
}

func scopeEqual(a, b *Scope) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Verdict is the worst status among the windows and the unified status:
// rejected > allowed_warning > allowed > "".
func (o Observation) Verdict() string {
	rank := func(s string) int {
		switch s {
		case statusRejected:
			return 3
		case statusAllowedWarning:
			return 2
		case statusAllowed:
			return 1
		}
		return 0
	}
	worst := o.UnifiedStatus
	for _, w := range o.Windows {
		if rank(w.Status) > rank(worst) {
			worst = w.Status
		}
	}
	return worst
}

// Report is the display form of an observation (stored as the observed
// snapshot row).
func (o Observation) Report() Report {
	r := Report{
		Status:    StatusAvailable,
		PlanLabel: o.PlanType,
		Windows:   o.Windows,
		FetchedAt: o.At,
	}
	if o.Representative != "" {
		r.Details = append(r.Details, Detail{ID: "representative_claim", Label: "Binding limit", Value: o.Representative})
	}
	if o.OverageStatus != "" {
		r.Details = append(r.Details, Detail{ID: "overage_status", Label: "Overage", Value: o.OverageStatus})
	}
	return r
}

// ObservationFromReport inverts Report. ok is false when r carries no window,
// detail or plan. UnifiedStatus is not part of the report and comes back "".
func ObservationFromReport(r Report) (Observation, bool) {
	o := Observation{At: r.FetchedAt, Windows: r.Windows, PlanType: r.PlanLabel}
	for _, d := range r.Details {
		switch d.ID {
		case "representative_claim":
			o.Representative = d.Value
		case "overage_status":
			o.OverageStatus = d.Value
		}
	}
	if len(o.Windows) == 0 && o.Representative == "" && o.OverageStatus == "" && o.PlanType == "" {
		return Observation{}, false
	}
	return o, true
}
