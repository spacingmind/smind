// Package tasktree is the pure computation behind the task.tree read model
// (docs/plans/active/task-hierarchy-tiers.md §4.1): it turns a flat set of
// tasks, per-task run facts, and per-task usage totals into the flat
// depth-first pre-order node list the wire contract specifies, with subtree
// usage rollups (archived descendants included) and status counts (live
// descendants only). It imports nothing from store/runs so both the wsapi
// layer and tests can feed it small input structs.
package tasktree

import "sort"

// MaxNodes is the cap on the returned node list; a tree larger than this
// is truncated and Build's second return value reports true.
const MaxNodes = 500

// Task is the slice of store.Task Build needs. Archived tasks are valid
// input: they are never emitted as nodes, but their usage rolls up into
// their ancestors and a live child of an archived parent becomes a root.
type Task struct {
	ID          int64
	ParentID    *int64
	WorkspaceID int64
	Title       string
	TaskStatus  string
	Archived    bool
	// BudgetUSD is the opt-in tree budget (ADR-0041 A5); only a root's
	// counts. Build copies it onto a root's node; the spend comes from the
	// caller (it needs a cost query Build must not make).
	BudgetUSD *float64
}

// State is the task.tree node run-state vocabulary (see RunFact.State).
type State string

// RunFact is one task's latest-run snapshot: State is one of "running",
// "waiting_permission", "done", "error", "stopped", "idle" (absent facts
// mean "idle"), RunningRunIDs the task's own running runs (oldest-started
// first), and PendingPermissions the task's own pending permission count.
type RunFact struct {
	State              State
	RunningRunIDs      []string
	PendingPermissions int
	// Disposition is "needs-continue" when the latest finished run ended done
	// at a turn/token/budget cap (runs.DispositionFor); empty otherwise.
	Disposition string
	// FailureKind/Window/ResetAt are the latest finished run's typed failure
	// (ADR-0041 B1): "rate_limited", the window name, and the reset time as
	// RFC 3339 UTC; empty when none.
	FailureKind string
	Window      string
	ResetAt     string
	// ResetInferred: ResetAt was read in an assumed zone.
	ResetInferred bool
}

// Run state constants for RunFact.State / Node.Run.State.
const (
	StateRunning           State = "running"
	StateWaitingPermission State = "waiting_permission"
	StateDone              State = "done"
	StateError             State = "error"
	StateStopped           State = "stopped"
	StateIdle              State = "idle"
)

// UsageTotals is a sum of usage over runs. Every numeric field except Runs
// is nil when nothing reported it -- nil never means 0 (ADR-0020).
type UsageTotals struct {
	Runs             int64    `json:"runs"`
	InputTokens      *int64   `json:"inputTokens"`
	OutputTokens     *int64   `json:"outputTokens"`
	CacheReadTokens  *int64   `json:"cacheReadTokens"`
	CacheWriteTokens *int64   `json:"cacheWriteTokens"`
	ReasoningTokens  *int64   `json:"reasoningTokens"`
	CostUSD          *float64 `json:"costUsd"`
}

// Root selects what Build emits: TaskID != 0 (taking precedence) emits
// that task and its live descendants; otherwise WorkspaceID != 0 emits
// every live root and orphan of that workspace with its subtree. The
// wsapi layer enforces the exactly-one-param rule; Build just reads the
// struct.
type Root struct {
	TaskID      int64
	WorkspaceID int64
}

// Counts are a node's live-descendant (not self) status counts. "stopped"
// counts as done.
type Counts struct {
	Running           int64 `json:"running"`
	WaitingPermission int64 `json:"waitingPermission"`
	Failed            int64 `json:"failed"`
	Done              int64 `json:"done"`
	Idle              int64 `json:"idle"`
}

// NodeRun is a node's own run snapshot.
type NodeRun struct {
	State              State    `json:"state"`
	RunningRunIDs      []string `json:"runningRunIds"`
	PendingPermissions int      `json:"pendingPermissions"`
	// Disposition is "needs-continue" for a node whose latest run is "done"
	// but was cut off at a cap (additive, omitted otherwise).
	Disposition string `json:"disposition,omitempty"`
	// FailureKind/Window/ResetAt carry the latest run's typed failure
	// (ADR-0041 B1); omitted when empty. ResetAt is RFC 3339 UTC.
	FailureKind string `json:"failureKind,omitempty"`
	Window      string `json:"window,omitempty"`
	ResetAt     string `json:"resetAt,omitempty"`
	// ResetInferred: ResetAt was read in an assumed zone (clients show "≈").
	ResetInferred bool `json:"resetInferred,omitempty"`
}

// NodeUsage bundles a node's own usage, its subtree rollup (every
// descendant reachable via parent links, archived included, each task's
// own counted exactly once), and how many archived descendants it has.
type NodeUsage struct {
	Own                 UsageTotals `json:"own"`
	Subtree             UsageTotals `json:"subtree"`
	ArchivedDescendants int64       `json:"archivedDescendants"`
}

// Node is one entry of the task.tree result: the wire shape of §4.1.
// Tier is nil for an untiered plain task. ChildCount counts live direct
// children.
type Node struct {
	ID          int64     `json:"id"`
	ParentID    *int64    `json:"parentId"`
	WorkspaceID int64     `json:"workspaceId"`
	Title       string    `json:"title"`
	Depth       int       `json:"depth"`
	TaskStatus  string    `json:"taskStatus"`
	Tier        *string   `json:"tier"`
	ChildCount  int       `json:"childCount"`
	Run         NodeRun   `json:"run"`
	Counts      Counts    `json:"counts"`
	Usage       NodeUsage `json:"usage"`
	// BudgetUSD is set on a ROOT node whose task has a tree budget
	// (additive, omitted otherwise). The remaining budget* fields are
	// filled by the wsapi layer from one cost query per budgeted root:
	// BudgetSpentUSD is the reported cost, BudgetPartial/BudgetUnknownRuns
	// say how many finished runs reported none (never counted, ADR-0020),
	// OverBudget is spent >= budget.
	BudgetUSD         *float64 `json:"budgetUsd,omitempty"`
	BudgetSpentUSD    *float64 `json:"budgetSpentUsd,omitempty"`
	BudgetPartial     bool     `json:"budgetPartial,omitempty"`
	BudgetUnknownRuns int      `json:"budgetUnknownRuns,omitempty"`
	OverBudget        bool     `json:"overBudget,omitempty"`
}

// TierOf derives a task's tier from its position (plan §3.1): no parent +
// live children = "supervisor"; parent + live children = "leader"; parent +
// no live children = "coder"; no parent and no children = "" (untiered).
// hasParent means the parent exists and is not archived.
func TierOf(hasParent, liveChildren bool) string {
	switch {
	case !hasParent && liveChildren:
		return "supervisor"
	case hasParent && liveChildren:
		return "leader"
	case hasParent && !liveChildren:
		return "coder"
	default:
		return ""
	}
}

// aggRec is the memoized post-order aggregate for one task: its subtree's
// usage (including itself), its live-strict-descendant counts, and its
// archived-descendant count. Computed once per task over the full forest
// (archived tasks included), so Build is O(n).
type aggRec struct {
	usage    UsageTotals
	counts   Counts
	archived int64
}

// Build returns the flat DFS pre-order node list for root (siblings by
// task id, depth from the queried root(s)), truncated at MaxNodes --
// the bool reports truncation. Cycle-safe: corrupt parent_task_id cycles
// terminate via visited sets, both in aggregation and in emission.
func Build(tasks []Task, facts map[int64]RunFact, usage map[int64]UsageTotals, root Root) ([]Node, bool) {
	byID := make(map[int64]*Task, len(tasks))
	children := make(map[int64][]*Task, len(tasks))
	for i := range tasks {
		t := &tasks[i]
		byID[t.ID] = t
		if t.ParentID != nil {
			children[*t.ParentID] = append(children[*t.ParentID], t)
		}
	}
	// children lists must be id-ascending; sort defensively so the
	// contract does not depend on input order.
	for _, kids := range children {
		sortTasks(kids)
	}

	// Memoized post-order aggregation over the full forest.
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := make(map[int64]int, len(tasks))
	memo := make(map[int64]*aggRec, len(tasks))
	var visit func(t *Task) *aggRec
	visit = func(t *Task) *aggRec {
		switch color[t.ID] {
		case grey:
			return nil // cycle: this edge contributes nothing
		case black:
			return memo[t.ID]
		}
		color[t.ID] = grey
		rec := &aggRec{usage: cloneUsage(usage[t.ID])}
		for _, c := range children[t.ID] {
			crec := visit(c)
			if crec == nil {
				continue
			}
			rec.usage = addUsage(rec.usage, crec.usage)
			// Counts aggregate through archived intermediaries (the
			// live descendants under them), but the archived task itself
			// is never counted.
			rec.counts = addCounts(rec.counts, crec.counts)
			rec.archived += crec.archived
			if c.Archived {
				rec.archived++
			} else {
				rec.counts = addCounts(rec.counts, countOne(factState(facts, c.ID)))
			}
		}
		color[t.ID] = black
		memo[t.ID] = rec
		return rec
	}
	for i := range tasks {
		visit(&tasks[i])
	}

	emit := func(t *Task, depth int) Node {
		rec := memo[t.ID]
		hasParent := false
		if t.ParentID != nil {
			if p, ok := byID[*t.ParentID]; ok && !p.Archived {
				hasParent = true
			}
		}
		liveKids := 0
		for _, c := range children[t.ID] {
			if !c.Archived {
				liveKids++
			}
		}
		f := facts[t.ID]
		runIDs := f.RunningRunIDs
		if runIDs == nil {
			runIDs = []string{}
		}
		var tier *string
		if s := TierOf(hasParent, liveKids > 0); s != "" {
			tier = &s
		}
		n := Node{
			ID: t.ID, ParentID: t.ParentID, WorkspaceID: t.WorkspaceID,
			Title: t.Title, Depth: depth, TaskStatus: t.TaskStatus, Tier: tier,
			ChildCount: liveKids,
			Run: NodeRun{
				State: factState(facts, t.ID), RunningRunIDs: runIDs,
				PendingPermissions: f.PendingPermissions, Disposition: f.Disposition,
				FailureKind: f.FailureKind, Window: f.Window, ResetAt: f.ResetAt, ResetInferred: f.ResetInferred,
			},
			Counts: rec.counts,
			Usage:  NodeUsage{Own: usage[t.ID], Subtree: rec.usage, ArchivedDescendants: rec.archived},
		}
		if t.ParentID == nil {
			n.BudgetUSD = t.BudgetUSD
		}
		return n
	}

	nodes := make([]Node, 0)
	visited := make(map[int64]bool, len(tasks))
	truncated := false
	var walk func(t *Task, depth int)
	walk = func(t *Task, depth int) {
		if visited[t.ID] || truncated {
			return
		}
		visited[t.ID] = true
		if len(nodes) >= MaxNodes {
			truncated = true
			return
		}
		nodes = append(nodes, emit(t, depth))
		for _, c := range children[t.ID] {
			if !c.Archived {
				walk(c, depth+1)
			}
		}
	}

	if root.TaskID != 0 {
		if t, ok := byID[root.TaskID]; ok && !t.Archived {
			walk(t, 0)
		}
		return nodes, truncated
	}

	// Workspace mode: every live root and orphan (parent missing or
	// archived), id order.
	roots := make([]*Task, 0, len(tasks))
	for i := range tasks {
		t := &tasks[i]
		if t.Archived {
			continue
		}
		if t.ParentID == nil {
			roots = append(roots, t)
			continue
		}
		p, ok := byID[*t.ParentID]
		if !ok || p.Archived {
			roots = append(roots, t)
		}
	}
	sortTasks(roots)
	for _, r := range roots {
		walk(r, 0)
	}
	return nodes, truncated
}

func factState(facts map[int64]RunFact, id int64) State {
	if s := facts[id].State; s != "" {
		return s
	}
	return StateIdle
}

func countOne(state State) Counts {
	switch state {
	case StateRunning:
		return Counts{Running: 1}
	case StateWaitingPermission:
		return Counts{WaitingPermission: 1}
	case StateError:
		return Counts{Failed: 1}
	case StateDone, StateStopped:
		return Counts{Done: 1}
	default:
		return Counts{Idle: 1}
	}
}

func addCounts(a, b Counts) Counts {
	return Counts{
		Running:           a.Running + b.Running,
		WaitingPermission: a.WaitingPermission + b.WaitingPermission,
		Failed:            a.Failed + b.Failed,
		Done:              a.Done + b.Done,
		Idle:              a.Idle + b.Idle,
	}
}

func cloneUsage(u UsageTotals) UsageTotals {
	out := u
	out.InputTokens = cpI64(u.InputTokens)
	out.OutputTokens = cpI64(u.OutputTokens)
	out.CacheReadTokens = cpI64(u.CacheReadTokens)
	out.CacheWriteTokens = cpI64(u.CacheWriteTokens)
	out.ReasoningTokens = cpI64(u.ReasoningTokens)
	out.CostUSD = cpF64(u.CostUSD)
	return out
}

func addUsage(a, b UsageTotals) UsageTotals {
	return UsageTotals{
		Runs:             a.Runs + b.Runs,
		InputTokens:      addI64(a.InputTokens, b.InputTokens),
		OutputTokens:     addI64(a.OutputTokens, b.OutputTokens),
		CacheReadTokens:  addI64(a.CacheReadTokens, b.CacheReadTokens),
		CacheWriteTokens: addI64(a.CacheWriteTokens, b.CacheWriteTokens),
		ReasoningTokens:  addI64(a.ReasoningTokens, b.ReasoningTokens),
		CostUSD:          addF64(a.CostUSD, b.CostUSD),
	}
}

func cpI64(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cpF64(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// addI64 sums two optional values: nil + nil stays nil (nothing reported),
// anything else is the sum of the reported values.
func addI64(a, b *int64) *int64 {
	if a == nil && b == nil {
		return nil
	}
	var out int64
	if a != nil {
		out += *a
	}
	if b != nil {
		out += *b
	}
	return &out
}

func addF64(a, b *float64) *float64 {
	if a == nil && b == nil {
		return nil
	}
	var out float64
	if a != nil {
		out += *a
	}
	if b != nil {
		out += *b
	}
	return &out
}

func sortTasks(ts []*Task) {
	sort.Slice(ts, func(i, j int) bool { return ts[i].ID < ts[j].ID })
}
