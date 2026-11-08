package quota

import (
	cryptorand "crypto/rand"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Passive rejection detection (ADR-0028 §7.1, D14). Everything here is pure:
// no clock reads, no I/O, no logging of header values. The rules
// follow Anthropic's unified rate-limit headers: which responses are declared
// rejections, how a reset deadline is read, and the random grace added to it.

// MaxCooldown bounds any cooldown end, so a garbage header cannot park an
// account.
const MaxCooldown = 8 * 24 * time.Hour

const (
	rejectFallbackBase    = time.Minute
	rejectFallbackCap     = 30 * time.Minute
	bare429BackoffCap     = 60 * time.Second
	passiveGraceMinSec    = 1
	passiveGraceMaxSec    = 30
	maxResetEpochSec      = 1e12 // far beyond any real reset; keeps int64/Duration math safe
	maxRetryAfterDeltaSec = 1e9
)

// Rejection is the verdict on one upstream response.
type Rejection struct {
	// Rejected: the headers explicitly declare a hard rejection.
	Rejected bool
	// ModelOnly: the rejection is an overage-only or 7d_oi (Fable-only) shape
	// (see unifiedStatuses.modelOnly), which cools down only the model of the
	// rejected request, never the whole account.
	ModelOnly bool
	// Window names the rejected window for display (one of the Window*
	// constants): weekly when the 7d window is rejected (also when the 5h one
	// is too: the weekly reset is the later one, so it is what binds), else
	// five_hour when the 5h one is, weekly_model for the ModelOnly shape, else
	// unified (only the overall status said rejected). Empty when not Rejected.
	// This is the verdict before any deadline is known: the label of a stored
	// cooldown comes from RejectionDeadlineWindow.
	Window string
}

// Rejected-window names carried by Rejection.Window and Cooldown.Window. They
// are the wire values of the cooldown's window field.
const (
	WindowFiveHour    = "five_hour"
	WindowWeekly      = "weekly"
	WindowWeeklyModel = "weekly_model"
	WindowUnified     = "unified"
)

// ClassifyRejection reports whether a response is a declared hard rejection.
// Only a 429 can be one; header names are case-insensitive.
func ClassifyRejection(status int, h http.Header) Rejection {
	if status != http.StatusTooManyRequests {
		return Rejection{}
	}
	s := readUnifiedStatuses(h)
	switch {
	case s.sevenD == "rejected":
		return Rejection{Rejected: true, Window: WindowWeekly}
	case s.fiveH == "rejected":
		return Rejection{Rejected: true, Window: WindowFiveHour}
	case s.unified != "rejected":
		return Rejection{}
	case s.modelOnly():
		return Rejection{Rejected: true, ModelOnly: true, Window: WindowWeeklyModel}
	}
	return Rejection{Rejected: true, Window: WindowUnified}
}

type unifiedStatuses struct {
	unified, fiveH, sevenD, sevenDOI string
	// overage signals, read only to tell an overage-only rejection apart
	overage, overageDisabledReason, claim string
	// utilization headers: the raw value and whether the header was sent at all
	util5h, util7d       string
	has5hUtil, has7dUtil bool
}

func readUnifiedStatuses(h http.Header) unifiedStatuses {
	const p = "Anthropic-Ratelimit-Unified-"
	get := func(name string) string { return strings.TrimSpace(headerFold(h, p+name)) }
	low := func(name string) string { return strings.ToLower(get(name)) }
	has := func(name string) bool { return headerPresent(h, p+name) }
	return unifiedStatuses{
		unified:               low("Status"),
		fiveH:                 low("5h-Status"),
		sevenD:                low("7d-Status"),
		sevenDOI:              low("7d_oi-Status"),
		overage:               low("Overage-Status"),
		overageDisabledReason: get("Overage-Disabled-Reason"),
		claim:                 low("Representative-Claim"),
		util5h:                get("5h-Utilization"),
		util7d:                get("7d-Utilization"),
		has5hUtil:             has("5h-Utilization"),
		has7dUtil:             has("7d-Utilization"),
	}
}

// windowAllowed: a shared window is not exhausted ("allowed_warning" is a
// warning, not a rejection).
func windowAllowed(status string) bool { return status == "allowed" || status == "allowed_warning" }

// utilizationHealthy: a parsable utilization in [0, 1).
func utilizationHealthy(raw string) bool {
	u, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return err == nil && !math.IsNaN(u) && !math.IsInf(u, 0) && u >= 0 && u < 1
}

// modelOnly reports a rejection that is only the account's overage budget or its Fable (7d_oi)
// window, while the shared 5h and 7d windows are not exhausted. It must cool
// down at most the rejected model, never the account. The shared windows count
// as not exhausted when each is allowed (or allowed_warning) or, when its
// status header is omitted, when its own utilization is healthy (< 1) or, for
// an overage-claim rejection, absent: the API leaves a window out when it did
// not evaluate it. A bare overage-disabled-reason header (many accounts send it
// on every response) narrows only when both shared windows are explicitly
// allowed: it is not enough to excuse an omitted window. Anything that does not
// match stays a whole-account rejection: an unknown shape never fails open. The
// window label of such a cooldown is weekly_model, whichever signal declared it.
func (s unifiedStatuses) modelOnly() bool {
	if s.fiveH == "rejected" || s.sevenD == "rejected" {
		return false
	}
	declared := s.sevenDOI == "rejected" || s.overage == "rejected" || strings.Contains(s.claim, "overage")
	fiveOK, sevenOK := windowAllowed(s.fiveH), windowAllowed(s.sevenD)
	if fiveOK && sevenOK {
		return declared || s.overageDisabledReason != ""
	}
	if !declared {
		return false
	}
	if sevenOK && s.fiveH == "" {
		if utilizationHealthy(s.util5h) || (s.unified == "rejected" && !s.has5hUtil) {
			return true
		}
	}
	if fiveOK && s.sevenD == "" && utilizationHealthy(s.util7d) {
		return true
	}
	// both shared windows omitted: only a rejected overage claim relies on that
	if s.unified == "rejected" && s.fiveH == "" && s.sevenD == "" && strings.Contains(s.claim, "overage") {
		absentOrHealthy := func(raw string) bool { return strings.TrimSpace(raw) == "" || utilizationHealthy(raw) }
		return absentOrHealthy(s.util5h) && absentOrHealthy(s.util7d)
	}
	return false
}

// RejectionDeadline returns the latest future reset among the candidates that
// apply to r (exactly ParseClaudeRateLimitReset's rules). No grace, no clamp.
func RejectionDeadline(h http.Header, now time.Time, r Rejection) (time.Time, bool) {
	t, _, ok := RejectionDeadlineWindow(h, now, r)
	return t, ok
}

// RejectionDeadlineWindow is RejectionDeadline plus the window to name for it:
// when the winning deadline comes from a window's own reset header (5h-Reset or
// 7d-Reset, each considered only while that window is rejected) that window
// (five_hour or weekly), so the label never contradicts the end time when both
// are rejected; when it comes from Retry-After, the 7d_oi reset or the unified
// reset, r.Window. A tie between the 5h and 7d resets is weekly. window is
// r.Window when there is no deadline.
func RejectionDeadlineWindow(h http.Header, now time.Time, r Rejection) (deadline time.Time, window string, ok bool) {
	if !r.Rejected {
		return time.Time{}, "", false
	}
	s := readUnifiedStatuses(h)
	modelOnly := s.modelOnly()
	var latest time.Time
	window = r.Window
	// own is the window a candidate belongs to ("" = none in particular).
	consider := func(t time.Time, ok bool, own string) {
		if !ok || !t.After(now) {
			return
		}
		tie := own == WindowWeekly && t.Equal(latest)
		if !t.After(latest) && !tie {
			return
		}
		latest = t
		window = r.Window
		if own != "" {
			window = own
		}
	}
	reset := func(name string) (time.Time, bool) {
		return parseResetTime(headerFold(h, "Anthropic-Ratelimit-Unified-"+name))
	}

	if raw := headerFold(h, "Retry-After"); raw != "" {
		t, ok := parseRetryAfterTime(raw, now)
		consider(t, ok, "")
	}
	if s.fiveH == "rejected" {
		t, ok := reset("5h-Reset")
		consider(t, ok, WindowFiveHour)
	}
	if s.sevenD == "rejected" {
		t, ok := reset("7d-Reset")
		consider(t, ok, WindowWeekly)
	}
	if s.sevenDOI == "rejected" && !modelOnly {
		t, ok := reset("7d_oi-Reset")
		consider(t, ok, "")
	}
	unifiedRejected := !modelOnly && (s.unified == "rejected" || s.fiveH == "rejected" || s.sevenD == "rejected" ||
		s.sevenDOI == "rejected" || (s.unified == "" && s.fiveH != "allowed" && s.sevenD != "allowed"))
	if unifiedRejected {
		t, ok := reset("Reset")
		consider(t, ok, "")
	}
	return latest, window, !latest.IsZero()
}

// CooldownEnd is min(deadline + max(grace, 0), now + MaxCooldown).
func CooldownEnd(deadline, now time.Time, grace time.Duration) time.Time {
	if grace < 0 {
		grace = 0
	}
	end := deadline.Add(grace)
	if limit := now.Add(MaxCooldown); end.After(limit) {
		return limit
	}
	return end
}

// GraceFunc yields the random grace added to a cooldown end.
type GraceFunc func() time.Duration

// DefaultGrace is a uniform integer 1 to 30 seconds from a cryptographic RNG.
func DefaultGrace() time.Duration {
	return graceFrom(func(max int64) (int64, error) {
		n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(max))
		if err != nil {
			return 0, err
		}
		return n.Int64(), nil
	})
}

// graceFrom draws from [0, max) and maps it to 1..30 s; a failing source gives
// the minimum.
func graceFrom(draw func(max int64) (int64, error)) time.Duration {
	span := int64(passiveGraceMaxSec - passiveGraceMinSec + 1)
	n, err := draw(span)
	if err != nil || n < 0 || n >= span {
		return passiveGraceMinSec * time.Second
	}
	return time.Duration(passiveGraceMinSec+n) * time.Second
}

// FallbackDuration is the generic exponential cooldown for a declared
// rejection with no valid reset: 1, 2, 4 ... capped at 30 min. level < 1 is 1.
func FallbackDuration(level int) time.Duration {
	if level < 1 {
		level = 1
	}
	// 1m << 5 = 32m already exceeds the cap; avoid any overflow.
	if level > 6 {
		return rejectFallbackCap
	}
	d := rejectFallbackBase << uint(level-1)
	if d > rejectFallbackCap {
		return rejectFallbackCap
	}
	return d
}

// RetryAfterBackoff is the bare-429 routing back-off: min(Retry-After, 60 s).
// ok is false when Retry-After is absent, unparseable or not positive.
func RetryAfterBackoff(h http.Header, now time.Time) (time.Duration, bool) {
	raw := headerFold(h, "Retry-After")
	if raw == "" {
		return 0, false
	}
	t, ok := parseRetryAfterTime(raw, now)
	if !ok {
		return 0, false
	}
	d := t.Sub(now)
	if d <= 0 {
		return 0, false
	}
	if d > bare429BackoffCap {
		d = bare429BackoffCap
	}
	return d, true
}

// headerPresent reports whether the header was sent, whatever its value.
func headerPresent(h http.Header, name string) bool {
	for k := range h {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

// headerFold is a case-insensitive header read: http.Header.Get cannot
// find a non-canonical key that contains '_' (7d_oi), so fall back to a scan.
func headerFold(h http.Header, name string) string {
	if h == nil {
		return ""
	}
	if v := h.Get(name); v != "" {
		return v
	}
	for k, v := range h {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

func positiveSeconds(raw string, limit float64) (float64, bool) {
	sec, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(sec) || math.IsInf(sec, 0) || sec <= 0 {
		return 0, false
	}
	return math.Min(sec, limit), true
}

// parseResetTime reads a reset value: epoch seconds (fractions allowed, > 0),
// RFC 3339 or HTTP-date (ref parseUnixOrTimestamp).
func parseResetTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if sec, ok := positiveSeconds(raw, maxResetEpochSec); ok {
		whole := int64(sec)
		return time.Unix(whole, int64((sec-float64(whole))*1e9)), true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	if t, err := http.ParseTime(raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// parseRetryAfterTime reads Retry-After as an absolute time: delta seconds
// (fractions allowed), HTTP-date or RFC 3339 (ref parseRetryAfterHeader).
func parseRetryAfterTime(raw string, now time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if sec, ok := positiveSeconds(raw, maxRetryAfterDeltaSec); ok {
		return now.Add(time.Duration(sec * float64(time.Second))), true
	}
	if t, err := http.ParseTime(raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}
