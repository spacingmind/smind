package serve

import (
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
)

// checkBudget enforces the per-origin grant budget (ADR-0048 D3) before a
// write effect runs. It queries per ORIGIN HOST only, never per trace: a
// host's live agents and spend count across every trace id, so a caller
// cannot reset its budget by opening a new trace. A nil return means the
// call is within budget; a budget failure is a *Refusal with
// wire.CodeBudgetExceeded, while a ledger failure is a wrapped error -- a
// broken store must never be mistaken for a refusal the caller caused.
func (s *Server) checkBudget(principal string, g registry.Grant) error {
	// The wall is checked first and without the ledger: zero wall means
	// zero, no remote run may start at all.
	if g.MaxWallSeconds <= 0 {
		return budgetRefusal(wire.BudgetWall, 0, g.MaxWallSeconds)
	}
	live, err := s.opts.Ledger.ListLiveOriginRuns(principal)
	if err != nil {
		return fmt.Errorf("budget: live runs of %s: %w", principal, err)
	}
	if len(live) >= g.MaxLiveAgents {
		return budgetRefusal(wire.BudgetLiveAgents, len(live), g.MaxLiveAgents)
	}
	cost, err := s.opts.Ledger.OriginCostSince(principal, s.opts.Now().Add(-24*time.Hour))
	if err != nil {
		return fmt.Errorf("budget: 24h cost of %s: %w", principal, err)
	}
	if cost >= g.MaxCostUSD {
		return budgetRefusal(wire.BudgetCost, cost, g.MaxCostUSD)
	}
	return nil
}

// budgetRefusal is the one shape of a budget refusal: the code never says
// why, the message names which limit was hit and by how much.
func budgetRefusal(which string, n, limit any) *Refusal {
	return refuse(wire.CodeBudgetExceeded, fmt.Sprintf("budget exceeded: %s (%v of %v)", which, n, limit))
}
