package quota

import (
	"net/http"
	"time"
)

// Passive rejection cooldown and observed headers (ADR-0028 §7, D14). The
// proxy learns from responses it already receives; nothing here can reach a
// usage endpoint. Two strictly separate things come out of a response:
//
//   - a routing signal: a hard rejection starts a Cooldown (read by routing
//     through Reader.Cooldown);
//   - an observation: rate-limit headers stored as typed display data, never
//     read by routing.

// Response is what the proxy hands the Observer for one upstream response.
// Header is parsed synchronously and never retained.
type Response struct {
	AccountID int64
	Provider  string // "anthropic"; any other provider is ignored
	Model     string // the model of the request, "" when unknown
	Status    int
	Header    http.Header
	At        time.Time
}

// Observer is the write-only side handed to the proxy. It has no method that
// reads state or reaches the network.
type Observer interface {
	Observe(Response)
}

// CooldownScope says what a cooldown blocks.
type CooldownScope string

const (
	CooldownAccount CooldownScope = "account" // every model of the account
	CooldownModel   CooldownScope = "model"   // only Cooldown.Model
)

// CooldownReason says where a cooldown's end came from.
type CooldownReason string

const (
	// ReasonRejected: a declared rejection with a valid future reset (plus grace).
	ReasonRejected CooldownReason = "rejected"
	// ReasonFallback: a declared rejection with no valid future reset; the
	// end is the generic exponential cooldown (1, 2, 4 ... 30 min).
	ReasonFallback CooldownReason = "fallback"
	// ReasonBackoff: a bare 429 with Retry-After; a transient per-(account,
	// model) routing back-off of at most 60 s. It is not exhaustion and
	// surfaces must not show it as a rate limit.
	ReasonBackoff CooldownReason = "backoff"
)

// Cooldown is a passive routing block on an account, or on one model of it.
type Cooldown struct {
	Until  time.Time
	Scope  CooldownScope
	Model  string // set when Scope is CooldownModel
	Reason CooldownReason
	Level  int // fallback level (1 = 1 min ...); 0 unless Reason is ReasonFallback
}

// Transient reports whether c is only a short routing back-off (bare 429),
// not a rate-limit state to show.
func (c Cooldown) Transient() bool { return c.Reason == ReasonBackoff }

// CooldownSource answers whether a cooldown applies. It is read-only and
// never does network I/O.
type CooldownSource interface {
	Cooldown(accountID int64, model string, now time.Time) (Cooldown, bool)
}

// WithCooldowns attaches the passive cooldown source the Poller's
// Reader.Cooldown answers from. Nil (the default) means no cooldowns.
func WithCooldowns(src CooldownSource) Option { return func(p *Poller) { p.cooldowns = src } }

// Cooldown implements Reader. It never touches the network or the store of
// usage reports, and answers (zero, false) when no source is attached or the
// poller is disabled (accountUsage.enabled: false).
func (p *Poller) Cooldown(accountID int64, model string, now time.Time) (Cooldown, bool) {
	if !p.enabled || p.cooldowns == nil {
		return Cooldown{}, false
	}
	return p.cooldowns.Cooldown(accountID, model, now)
}
