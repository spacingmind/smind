package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/tasktree"
)

// Tree budget wire (ADR-0041 A5). The task payload is store.Task, whose
// budgetUsd is the opt-in cap; taskView adds the computed spend fields for
// a budgeted ROOT task. Nothing is computed for an unbudgeted tree.
type taskView struct {
	store.Task
	BudgetSpentUSD    *float64 `json:"budgetSpentUsd,omitempty"`
	BudgetPartial     bool     `json:"budgetPartial,omitempty"`
	BudgetUnknownRuns int      `json:"budgetUnknownRuns,omitempty"`
	OverBudget        bool     `json:"overBudget,omitempty"`

	// LatestRunProfileID/Name: the profile of the task's most recent run,
	// only on task.list withLatestRun; omitted when it has none (Name also
	// when the profile row is gone).
	LatestRunProfileID   *int64  `json:"latestRunProfileId,omitempty"`
	LatestRunProfileName *string `json:"latestRunProfileName,omitempty"`

	// AutomationID/Name: the automation that started the task, omitted when
	// none did.
	AutomationID   *int64 `json:"automationId,omitempty"`
	AutomationName string `json:"automationName,omitempty"`
}

// annotateLatestRun fills LatestRunProfile* on views from two queries.
func annotateLatestRun(db *store.Store, workspaceID int64, views []taskView) error {
	byTask, err := db.LatestRunProfileByTask(workspaceID)
	if err != nil {
		return err
	}
	names := map[int64]*string{}
	for i := range views {
		id := byTask[views[i].ID]
		if id == 0 {
			continue
		}
		views[i].LatestRunProfileID = &id
		name, seen := names[id]
		if !seen {
			if p, err := db.GetAgentProfile(id); err == nil {
				n := p.Name
				name = &n
			}
			names[id] = name
		}
		views[i].LatestRunProfileName = name
	}
	return nil
}

// budgetSpend is one budgeted root's reported spend.
type budgetSpend struct {
	spent   float64
	unknown int
	over    bool
}

// rootBudgetSpend computes the spend of root's tree, or nil when root has no
// budget or is not a root (one TreeKnownCost query otherwise).
func rootBudgetSpend(db *store.Store, id int64, parent *int64, budget *float64) (*budgetSpend, error) {
	if budget == nil || (parent != nil && *parent != 0) {
		return nil, nil
	}
	known, unknown, err := db.TreeKnownCost(id)
	if err != nil {
		return nil, err
	}
	return &budgetSpend{spent: known, unknown: unknown, over: known >= *budget}, nil
}

// viewTask wraps t with its budget spend fields and its automation link.
func viewTask(db *store.Store, t store.Task) (taskView, error) {
	views, err := viewTasks(db, []store.Task{t})
	if err != nil {
		return taskView{}, err
	}
	return views[0], nil
}

func viewTasks(db *store.Store, tasks []store.Task) ([]taskView, error) {
	out := make([]taskView, len(tasks))
	ids := make([]int64, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
		v := taskView{Task: t}
		sp, err := rootBudgetSpend(db, t.ID, t.ParentTaskID, t.BudgetUSD)
		if err != nil {
			return nil, err
		}
		if sp != nil {
			v.BudgetSpentUSD, v.BudgetPartial, v.BudgetUnknownRuns, v.OverBudget = &sp.spent, sp.unknown > 0, sp.unknown, sp.over
		}
		out[i] = v
	}
	links, err := db.AutomationLinksForTasks(ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if l, ok := links[out[i].ID]; ok {
			id := l.AutomationID
			out[i].AutomationID, out[i].AutomationName = &id, l.Name
		}
	}
	return out, nil
}

// annotateTreeBudgets fills the spend fields of every root node that carries
// a budget: one cost query per budgeted root, none for an unbudgeted tree.
func annotateTreeBudgets(db *store.Store, nodes []tasktree.Node) error {
	for i := range nodes {
		n := &nodes[i]
		sp, err := rootBudgetSpend(db, n.ID, n.ParentID, n.BudgetUSD)
		if err != nil {
			return err
		}
		if sp != nil {
			n.BudgetSpentUSD, n.BudgetPartial, n.BudgetUnknownRuns, n.OverBudget = &sp.spent, sp.unknown > 0, sp.unknown, sp.over
		}
	}
	return nil
}

// parseBudget validates a budgetUsd wire value: JSON null clears (nil), else
// a finite number > 0 rounded to 2 decimals.
func parseBudget(raw json.RawMessage) (*float64, error) {
	if len(raw) == 0 {
		return nil, errors.New("budgetUsd is required (a number, or null to clear)")
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, errors.New("budgetUsd must be a number or null")
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, errors.New("budgetUsd must be a finite number")
	}
	v = math.Round(v*100) / 100
	if v <= 0 {
		return nil, errors.New("budgetUsd must be greater than 0 (use null to clear the budget)")
	}
	return &v, nil
}

func budgetInvalid(method string, err error) error {
	return &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error()}
}

// handleTaskSetBudget is task.setBudget {taskId, budgetUsd|null}: a HUMAN
// sets, raises, lowers or clears the tree budget on a root task. It is not
// in RunScopedMethods and refuses run-scoped callers too: an agent must never
// be able to defeat its own cap (ADR-0041 A5). Raising or clearing re-arms
// the once-per-crossing parent notification.
func handleTaskSetBudget(db *store.Store, reg *runs.Registry) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if err := refuseRunScoped("task.setBudget", rc); err != nil {
			return nil, err
		}
		var p struct {
			TaskID    int64           `json:"taskId"`
			BudgetUSD json.RawMessage `json:"budgetUsd"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, budgetInvalid("task.setBudget", fmt.Errorf("invalid params: %w", err))
		}
		if p.TaskID <= 0 {
			return nil, budgetInvalid("task.setBudget", errors.New("taskId is required"))
		}
		budget, err := parseBudget(p.BudgetUSD)
		if err != nil {
			return nil, budgetInvalid("task.setBudget", err)
		}
		t, err := db.GetTask(p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("task.setBudget: task %d: %w", p.TaskID, err)
		}
		if t.ParentTaskID != nil && *t.ParentTaskID != 0 {
			return nil, budgetInvalid("task.setBudget", fmt.Errorf("task %d is not a root task (set the budget on its tree root #%d or above)", p.TaskID, *t.ParentTaskID))
		}
		if err := db.SetTaskBudget(p.TaskID, budget); err != nil {
			return nil, fmt.Errorf("task.setBudget: %w", err)
		}
		reg.RearmBudgetNotify(p.TaskID)
		t.BudgetUSD = budget
		return viewTask(db, t)
	}
}
