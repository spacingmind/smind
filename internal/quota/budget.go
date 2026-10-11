package quota

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
)

// The call-budget constants (ADR-0028 §3). They are deliberately not
// configurable: no config key, option or caller can loosen them.
const (
	touchTTLFloor     = 5 * time.Minute  // minimum TTL, and the Touch pace
	refreshFloor      = 30 * time.Second // min gap between attempts for a forced Refresh
	refreshBudget     = 6                // forced attempts per rolling hour
	refreshWindow     = time.Hour
	maxInFlight       = 2 // global concurrent fetches
	retryAfterDefault = 5 * time.Minute
	retryAfterCap     = 6 * time.Hour
	backoffBase       = time.Minute
	backoffCap        = 30 * time.Minute
	jitterFrac        = 0.2
	// nextAllowedCap bounds a persisted NextAllowedAt (clock jumps).
	nextAllowedCap = retryAfterCap
)

// fingerprint identifies a credential without revealing it: the first 8
// bytes of the SHA-256 of the OAuth access token (or API key), hex. It is
// compared against the stored rejected fingerprint and never logged or sent.
func fingerprint(a accounts.Account) string {
	var secret string
	switch {
	case a.OAuth != nil:
		secret = a.OAuth.AccessToken
	case a.APIKey != nil:
		secret = a.APIKey.Key
	default:
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:8])
}

// clampState bounds persisted values that a clock jump could have left in the
// future, so a skewed clock can neither park an account nor open its budget.
func clampState(st *store.QuotaPollState, now time.Time) {
	if !st.NextAllowedAt.IsZero() && st.NextAllowedAt.After(now.Add(nextAllowedCap)) {
		st.NextAllowedAt = now.Add(nextAllowedCap)
	}
	if st.LastAttemptAt.After(now) {
		st.LastAttemptAt = now
	}
	nowMs := now.UnixMilli()
	for i, ms := range st.ForcedAt {
		if ms > nowMs {
			st.ForcedAt[i] = nowMs
		}
	}
}

// forcedGate is the one place that decides whether a forced Refresh made at
// now would reach the network. blocked == "" means it would. Otherwise at is
// the earliest time it could (nil when never: rejected). Peek, Touch and
// Refresh all report from this, so the three cannot disagree.
//
// The latest applicable time wins; ties go backoff > budget > floor.
func forcedGate(st store.QuotaPollState, fp string, now time.Time) (blocked BlockReason, at *time.Time) {
	if st.RejectedFP != "" && st.RejectedFP == fp {
		return BlockRejected, nil
	}
	var best time.Time
	var reason BlockReason
	consider := func(t time.Time, r BlockReason) {
		if reason == "" || t.After(best) {
			best, reason = t, r
		}
	}
	if now.Before(st.NextAllowedAt) {
		consider(st.NextAllowedAt, BlockBackoff)
	}
	if oldest, n := forcedInWindow(st.ForcedAt, now); n >= refreshBudget {
		if t := oldest.Add(refreshWindow); now.Before(t) {
			consider(t, BlockBudget)
		}
	}
	if !st.LastAttemptAt.IsZero() {
		if t := st.LastAttemptAt.Add(refreshFloor); now.Before(t) {
			consider(t, BlockFloor)
		}
	}
	if reason == "" {
		return "", nil
	}
	return reason, &best
}

// forcedInWindow returns the oldest forced attempt within (now-1h, now] and
// how many there are.
func forcedInWindow(forced []int64, now time.Time) (oldest time.Time, n int) {
	lo := now.Add(-refreshWindow).UnixMilli()
	hi := now.UnixMilli()
	for _, ms := range forced {
		if ms > lo && ms <= hi {
			if n == 0 || time.UnixMilli(ms).Before(oldest) {
				oldest = time.UnixMilli(ms)
			}
			n++
		}
	}
	return oldest, n
}

// appendForced records a forced attempt at now, dropping entries older than
// the window and keeping at most refreshBudget. It returns a new slice.
func appendForced(forced []int64, now time.Time) []int64 {
	lo := now.Add(-refreshWindow).UnixMilli()
	out := make([]int64, 0, refreshBudget)
	for _, ms := range forced {
		if ms > lo {
			out = append(out, ms)
		}
	}
	out = append(out, now.UnixMilli())
	if len(out) > refreshBudget {
		out = out[len(out)-refreshBudget:]
	}
	return out
}

// backoffWait is the wait after the failures-th consecutive failure:
// min(30m, 1m<<(n-1)) plus additive jitter of 0–20% (jit is in [0,1)).
func backoffWait(failures int, jit float64) time.Duration {
	if failures < 1 {
		failures = 1
	}
	b := backoffCap
	if failures <= 6 {
		if d := backoffBase << (failures - 1); d < backoffCap {
			b = d
		}
	}
	return b + time.Duration(float64(b)*jitterFrac*jit)
}

// retryAfterWait applies the 429 rule: Retry-After clamped to [5m, 6h], then
// the larger of that and the backoff wait, so jitter never shortens it.
func retryAfterWait(retryAfter, backoff time.Duration) time.Duration {
	if retryAfter < retryAfterDefault {
		retryAfter = retryAfterDefault
	}
	if retryAfter > retryAfterCap {
		retryAfter = retryAfterCap
	}
	if backoff > retryAfter {
		return backoff
	}
	return retryAfter
}
