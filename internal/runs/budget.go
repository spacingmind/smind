package runs

import (
	"fmt"

	"github.com/spacingmind/smind/internal/store"
)

// BudgetExceededError is the tree budget's admission refusal (ADR-0041 A5):
// the tree rooted at RootTaskID has reported SpentUSD of its BudgetUSD cap,
// so an agent may not launch another run or task in it. Only reported cost
// is counted (ADR-0020); UnknownRuns finished runs reported none and are not
// in SpentUSD. Its message is model-readable.
type BudgetExceededError struct {
	RootTaskID  int64
	SpentUSD    float64
	BudgetUSD   float64
	UnknownRuns int
}

func (e *BudgetExceededError) Error() string {
	partial := ""
	if e.UnknownRuns > 0 {
		partial = fmt.Sprintf("; %d %s reported no cost and %s not counted", e.UnknownRuns, pluralRuns(e.UnknownRuns), isAre(e.UnknownRuns))
	}
	return fmt.Sprintf("runs: start: task tree #%d reached its budget ($%.2f of $%.2f reported cost%s): no new agent-launched runs or tasks are allowed in this tree. Ask the human to raise it (smind task budget %d <usd>, or the budget control in the tree header); do not retry.",
		e.RootTaskID, e.SpentUSD, e.BudgetUSD, partial, e.RootTaskID)
}

func pluralRuns(n int) string {
	if n == 1 {
		return "run"
	}
	return "runs"
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

// checkBudget is the tree budget's admission check. It must be called BEFORE
// reg.mu is taken (it reads the store). Only an agent-sourced launch into a
// tree whose root has a budget is checked, with one TreeKnownCost query;
// every other origin (human, orchestrator, child_event, unknown) and every
// unbudgeted tree costs no query. A budget is reached when known >= budget.
func (reg *Registry) checkBudget(origin string, root store.Task) error {
	if origin != store.ChatQueueSourceAgent || root.BudgetUSD == nil {
		return nil
	}
	if reg.budgetCostRead != nil {
		reg.budgetCostRead()
	}
	known, unknown, err := reg.st.TreeKnownCost(root.ID)
	if err != nil {
		return fmt.Errorf("runs: start: %w", err)
	}
	if known >= *root.BudgetUSD {
		return &BudgetExceededError{RootTaskID: root.ID, SpentUSD: known, BudgetUSD: *root.BudgetUSD, UnknownRuns: unknown}
	}
	return nil
}
