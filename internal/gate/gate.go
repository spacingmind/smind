// Package gate derives, for a provider (and optionally a model), why a picker
// row cannot run right now and the concrete way out: the preventive twin of a
// run failure class (internal/runs/failure, docs/plans/active/picker-gate-reasons.md).
// One derivation serves provider.list, the non-human send guard and the MCP
// text; no client computes a gate.
//
// Reads are memory/DB only. The package imports neither internal/quota nor
// internal/accounts: a usage fetch cannot be reached from here (ADR-0028), and
// the account windows arrive as a routing.Verdict.
package gate

import (
	"time"

	"github.com/spacingmind/smind/internal/runs/failure"
)

// The fixed reason vocabulary.
const (
	ReasonNotSignedIn          = "not_signed_in"
	ReasonOutOfPlan            = "out_of_plan"
	ReasonCoolingDown          = "cooling_down"
	ReasonUnsupportedByHarness = "unsupported_by_harness"
	ReasonModelNotInPlan       = "model_not_in_plan"
)

// Gate says why a row cannot run and what to do about it (additive wire).
type Gate struct {
	Reason string `json:"reason"`
	// Until is when the block ends (UTC), absent when unknown.
	Until  *time.Time      `json:"until,omitempty"`
	Action *failure.Action `json:"action,omitempty"`
	// AccountID is the account the gate talks about, when one is known.
	AccountID int64 `json:"accountId,omitempty"`
	// Advisory marks a gate resting on inferred or cached evidence (an account's
	// cached usage window, a recent failed run): the row warns but stays
	// selectable, because smind runs on the CLI's own login (ADR-0020) and the
	// evidence may be about another account. A hard gate (no field) is a fact the
	// daemon enforces for non-human callers.
	Advisory bool `json:"advisory,omitempty"`
}

// Hard reports whether the daemon refuses a non-human send on this gate.
func (g *Gate) Hard() bool { return g != nil && !g.Advisory }

var twins = map[string]string{
	ReasonNotSignedIn:    failure.KindAuthExpired,
	ReasonOutOfPlan:      failure.KindPlanWindowExhausted,
	ReasonCoolingDown:    failure.KindPlanWindowExhausted,
	ReasonModelNotInPlan: failure.KindModelNotInPlan,
}

// TwinOf is the run failure class that is the reactive twin of a picker
// reason; ok is false for a reason with none (unsupported_by_harness).
func TwinOf(reason string) (class string, ok bool) {
	class, ok = twins[reason]
	return class, ok
}

// ReasonFor is the picker reason that prevents a failure class; ok is false
// for a class a picker row never prevents (transient_429, rate_limited, cli_exited).
func ReasonFor(class string) (reason string, ok bool) {
	switch class {
	case failure.KindAuthExpired:
		return ReasonNotSignedIn, true
	case failure.KindPlanWindowExhausted:
		return ReasonOutOfPlan, true
	case failure.KindModelNotInPlan:
		return ReasonModelNotInPlan, true
	}
	return "", false
}
