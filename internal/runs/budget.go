package runs

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

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

// budgetedOrigin reports an origin the tree budget applies to: an agent, and
// a plugin's task.send (ADR-0042 PB.3), which spends the tree's money like one.
func budgetedOrigin(origin string) bool {
	if origin == store.ChatQueueSourceAgent {
		return true
	}
	_, plugin := store.PluginSourceID(origin)
	return plugin
}

// checkBudget is the tree budget's admission check. It must be called BEFORE
// reg.mu is taken (it reads the store). Only an agent-sourced launch into a
// tree whose root has a budget is checked, with one TreeKnownCost query;
// every other origin (human, orchestrator, child_event, unknown) and every
// unbudgeted tree costs no query. A budget is reached when known >= budget.
func (reg *Registry) checkBudget(origin string, root store.Task) error {
	if !budgetedOrigin(origin) || root.BudgetUSD == nil {
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
		be := &BudgetExceededError{RootTaskID: root.ID, SpentUSD: known, BudgetUSD: *root.BudgetUSD, UnknownRuns: unknown}
		reg.notifyBudgetReached(be)
		return be
	}
	reg.RearmBudgetNotify(root.ID)
	return nil
}

// RearmBudgetNotify lets the root's next budget crossing notify its chat
// again. task.setBudget calls it whenever the budget is raised or cleared;
// checkBudget calls it when it finds the tree back under the cap.
func (reg *Registry) RearmBudgetNotify(rootID int64) {
	reg.budgetMu.Lock()
	delete(reg.budgetNotified, rootID)
	reg.budgetMu.Unlock()
}

// notifyBudgetReached tells the root task's chat, once per crossing, that
// its tree reached its budget: one child_event style message through the
// parent notifier's queue (same debounce, coalescing and delivery at a turn
// boundary), so a Lead hears it without being interrupted. Further refusals
// while the flag is set do nothing. The enqueue is synchronous; delivery
// is started in the background because checkBudget can run inside deliverOne,
// which holds deliverMu.
func (reg *Registry) notifyBudgetReached(be *BudgetExceededError) {
	reg.budgetMu.Lock()
	if reg.budgetNotified[be.RootTaskID] {
		reg.budgetMu.Unlock()
		return
	}
	if reg.budgetNotified == nil {
		reg.budgetNotified = make(map[int64]bool)
	}
	reg.budgetNotified[be.RootTaskID] = true
	now := time.Now
	if reg.budgetNow != nil {
		now = reg.budgetNow
	}
	reg.budgetMu.Unlock()

	reg.notifyMu.Lock()
	notifyOn, debounce, maxHold := reg.notifyParent, reg.notifyDebounce, reg.notifyMaxHold
	reg.notifyMu.Unlock()
	if !notifyOn || reg.closing.Load() {
		return
	}
	chats, err := reg.st.ListChatsByTask(be.RootTaskID, false)
	if err != nil || len(chats) == 0 {
		return
	}
	target := chats[0].ID
	prev, ok, err := reg.st.LatestRunForChat(target)
	if err != nil || !ok {
		return // nothing to copy run settings from, as for a child event
	}
	cfg, err := json.Marshal(chatQueueRunConfig{
		Provider: prev.Provider, PermissionMode: prev.PermissionMode, AutoAccept: prev.AutoAccept, Model: prev.Model,
		ProfileID: prev.ProfileID,
	})
	if err != nil {
		return
	}
	_, _, err = reg.st.EnqueueChildEvent(store.ChildEventInput{
		ChatID: target, Block: budgetReachedText(be), RunConfig: string(cfg),
		Now: now(), Debounce: debounce, MaxHold: maxHold,
	})
	if err != nil {
		log.Printf("runs: budget notify task %d: %v", be.RootTaskID, err)
		return
	}
	reg.notifyQueueUpdated(target)
	reg.armQueueWake()
	if debounce <= 0 {
		go reg.deliverOne(target)
	}
}

// budgetReachedText is the notification text: the facts, in the style of
// the child event status line, with no untrusted content.
func budgetReachedText(be *BudgetExceededError) string {
	text := fmt.Sprintf("task tree #%d reached its budget ($%.2f of $%.2f reported cost)", be.RootTaskID, be.SpentUSD, be.BudgetUSD)
	if be.UnknownRuns > 0 {
		text += fmt.Sprintf("; %d %s reported no cost", be.UnknownRuns, pluralRuns(be.UnknownRuns))
	}
	return text
}

// CheckTreeBudget is checkBudget for a launch that is not a run: an agent
// creating a task under parentTaskID (task.create from a seat). It resolves
// the tree root and refuses with *BudgetExceededError exactly as run
// admission does; a nil error means the tree has no budget or is under it.
func (reg *Registry) CheckTreeBudget(parentTaskID int64, origin string) error {
	if !budgetedOrigin(origin) {
		return nil
	}
	_, root, err := reg.treeRootTask(parentTaskID)
	if err != nil {
		return err
	}
	return reg.checkBudget(origin, root)
}

// RPCCode is the wire code of a budget refusal (wsapi's rpcCoder).
func (e *BudgetExceededError) RPCCode() string { return "budget_exceeded" }
