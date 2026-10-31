package tasktree

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/runs/failure"
)

func task(id int64, parent *int64, archived bool) Task {
	return Task{ID: id, ParentID: parent, WorkspaceID: 1, Title: fmt.Sprintf("t%d", id), TaskStatus: "open", Archived: archived}
}

func pid(id int64) *int64 { return &id }

func i64(v int64) *int64 { return &v }

func find(t *testing.T, nodes []Node, id int64) Node {
	t.Helper()
	for _, n := range nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %d not found in %+v", id, ids(nodes))
	return Node{}
}

func ids(nodes []Node) []int64 {
	out := make([]int64, len(nodes))
	for i, n := range nodes {
		out[i] = n.ID
	}
	return out
}

// TestBuild_ThreeTierOrderAndDepth: supervisor -> leaders (id order) ->
// coders; flat pre-order emission with depth from the queried root and
// tiers derived from position.
func TestBuild_ThreeTierOrderAndDepth(t *testing.T) {
	tasks := []Task{
		task(1, nil, false),
		task(2, pid(1), false),
		task(3, pid(1), false),
		task(4, pid(2), false),
		task(5, pid(2), false),
		task(6, pid(3), false),
	}
	nodes, trunc := Build(tasks, nil, nil, Root{WorkspaceID: 1})
	if trunc {
		t.Fatal("truncated = true, want false")
	}
	want := []int64{1, 2, 4, 5, 3, 6}
	if got := ids(nodes); !equalIDs(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for _, tc := range []struct {
		id    int64
		depth int
		tier  string
		child int
	}{
		{1, 0, "supervisor", 2},
		{2, 1, "leader", 2},
		{4, 2, "coder", 0},
		{3, 1, "leader", 1},
		{6, 2, "coder", 0},
	} {
		n := find(t, nodes, tc.id)
		if n.Depth != tc.depth {
			t.Errorf("node %d depth = %d, want %d", tc.id, n.Depth, tc.depth)
		}
		if n.Tier == nil || *n.Tier != tc.tier {
			t.Errorf("node %d tier = %v, want %q", tc.id, n.Tier, tc.tier)
		}
		if n.ChildCount != tc.child {
			t.Errorf("node %d childCount = %d, want %d", tc.id, n.ChildCount, tc.child)
		}
	}
	// Rooted at a non-root task: depth restarts from it.
	nodes, _ = Build(tasks, nil, nil, Root{TaskID: 2})
	if got := ids(nodes); !equalIDs(got, []int64{2, 4, 5}) {
		t.Fatalf("rooted order = %v, want [2 4 5]", got)
	}
	if n := find(t, nodes, 2); n.Depth != 0 || n.Tier == nil || *n.Tier != "leader" {
		t.Fatalf("rooted root node = %+v, want depth 0 tier leader", n)
	}
}

// TestBuild_OrphanOfArchivedParentIsRoot: a live child of an archived
// parent is emitted as a top-level node in workspace mode, and the
// archived parent's own subtree is not.
func TestBuild_OrphanOfArchivedParentIsRoot(t *testing.T) {
	tasks := []Task{
		task(1, nil, false),
		task(2, pid(1), false),
		task(3, pid(2), true),  // archived middle
		task(4, pid(3), false), // live orphan
		task(5, nil, false),
	}
	nodes, _ := Build(tasks, nil, nil, Root{WorkspaceID: 1})
	want := []int64{1, 2, 4, 5}
	if got := ids(nodes); !equalIDs(got, want) {
		t.Fatalf("order = %v, want %v (orphan 4 as root, archived 3 absent)", got, want)
	}
	n := find(t, nodes, 4)
	if n.ParentID == nil || *n.ParentID != 3 {
		t.Fatalf("orphan parentId = %v, want preserved 3", n.ParentID)
	}
	if n.Depth != 0 {
		t.Fatalf("orphan depth = %d, want 0 (root)", n.Depth)
	}
	if n.Tier != nil {
		t.Fatalf("orphan tier = %q, want nil (untiered)", *n.Tier)
	}
	// Tier of a child of the orphan: parent exists but is not archived
	// (it is live), so it is a leader with children.
	tasks = append(tasks, task(6, pid(4), false))
	nodes, _ = Build(tasks, nil, nil, Root{WorkspaceID: 1})
	n = find(t, nodes, 6)
	if n.Tier == nil || *n.Tier != "coder" {
		t.Fatalf("child of orphan tier = %v, want coder", n.Tier)
	}
}

// TestBuild_SubtreeUsageCountsEachRunOnce: a->b->c subtree usage equals
// the direct sum of all three own usages; adding a sibling task under a
// never changes b's subtree.
func TestBuild_SubtreeUsageCountsEachRunOnce(t *testing.T) {
	tasks := []Task{task(1, nil, false), task(2, pid(1), false), task(3, pid(2), false)}
	usage := map[int64]UsageTotals{
		1: {Runs: 1, InputTokens: i64(10), CostUSD: costPtr(0.5)},
		2: {Runs: 2, OutputTokens: i64(20)},
		3: {Runs: 3, InputTokens: i64(30), OutputTokens: i64(40), CostUSD: costPtr(1.5)},
	}
	nodes, _ := Build(tasks, nil, usage, Root{WorkspaceID: 1})
	a := find(t, nodes, 1)
	sub := a.Usage.Subtree
	if sub.Runs != 6 {
		t.Errorf("a.subtree.runs = %d, want 6", sub.Runs)
	}
	if sub.InputTokens == nil || *sub.InputTokens != 40 {
		t.Errorf("a.subtree.inputTokens = %v, want 40", sub.InputTokens)
	}
	if sub.OutputTokens == nil || *sub.OutputTokens != 60 {
		t.Errorf("a.subtree.outputTokens = %v, want 60", sub.OutputTokens)
	}
	if sub.CostUSD == nil || *sub.CostUSD != 2.0 {
		t.Errorf("a.subtree.costUsd = %v, want 2.0", sub.CostUSD)
	}
	b := find(t, nodes, 2)
	if b.Usage.Subtree.Runs != 5 || b.Usage.Subtree.InputTokens == nil || *b.Usage.Subtree.InputTokens != 30 {
		t.Errorf("b.subtree = %+v, want runs 5 input 30", b.Usage.Subtree)
	}

	// Adding a sibling under a leaves b's subtree unchanged.
	tasks = append(tasks, task(4, pid(1), false))
	usage[4] = UsageTotals{Runs: 100, InputTokens: i64(999)}
	nodes, _ = Build(tasks, nil, usage, Root{WorkspaceID: 1})
	if b := find(t, nodes, 2); b.Usage.Subtree.Runs != 5 {
		t.Fatalf("after adding sibling, b.subtree.runs = %d, want still 5", b.Usage.Subtree.Runs)
	}
}

// TestBuild_ArchivedDescendantUsageIncludedCountsExcluded: usage.subtree
// and archivedDescendants include archived descendants; counts and the
// node list do not.
func TestBuild_ArchivedDescendantUsageIncludedCountsExcluded(t *testing.T) {
	tasks := []Task{
		task(1, nil, false),
		task(2, pid(1), true),  // archived child
		task(3, pid(2), false), // live grandchild via archived intermediary
		task(4, pid(1), false),
	}
	facts := map[int64]RunFact{
		1: {State: StateDone},
		2: {State: StateDone},
		3: {State: StateRunning},
		4: {State: StateWaitingPermission},
	}
	usage := map[int64]UsageTotals{
		1: {Runs: 1, InputTokens: i64(1)},
		2: {Runs: 2, InputTokens: i64(2)},
		3: {Runs: 3, InputTokens: i64(3)},
	}
	nodes, _ := Build(tasks, facts, usage, Root{WorkspaceID: 1})
	// Archived 2 is not a node and emission follows live children only,
	// so 3 (a live child of an archived parent) is itself a root.
	if got := ids(nodes); !equalIDs(got, []int64{1, 4, 3}) {
		t.Fatalf("order = %v, want [1 4 3]", got)
	}
	a := find(t, nodes, 1)
	if a.Usage.Subtree.Runs != 6 || a.Usage.Subtree.InputTokens == nil || *a.Usage.Subtree.InputTokens != 6 {
		t.Fatalf("a.subtree = %+v, want runs 6 input 6 (archived included)", a.Usage.Subtree)
	}
	if a.Usage.ArchivedDescendants != 1 {
		t.Fatalf("a.archivedDescendants = %d, want 1", a.Usage.ArchivedDescendants)
	}
	// Counts are live-only: 3 running, 4 waiting; 2 (done, archived) not
	// counted.
	if a.Counts.Running != 1 || a.Counts.WaitingPermission != 1 || a.Counts.Done != 0 {
		t.Fatalf("a.counts = %+v, want running 1 waiting 1 done 0", a.Counts)
	}
	// 3 is emitted as a root (its own subtree, no descendants).
	c := find(t, nodes, 3)
	if c.Depth != 0 {
		t.Fatalf("3 depth = %d, want 0 (root via archived parent)", c.Depth)
	}
	if c.Usage.Subtree.Runs != 3 || c.Usage.ArchivedDescendants != 0 {
		t.Fatalf("c usage = %+v, want subtree runs 3 archived 0", c.Usage)
	}
}

// TestBuild_UnreportedFieldsStayNullNotZero: a usage field no run reported
// stays nil through the rollup -- a reported 0 sums to 0, never reported
// stays nil.
func TestBuild_UnreportedFieldsStayNullNotZero(t *testing.T) {
	tasks := []Task{task(1, nil, false), task(2, pid(1), false)}
	usage := map[int64]UsageTotals{
		1: {Runs: 1, InputTokens: i64(0)}, // reported zero
		2: {Runs: 1},                      // nothing reported but runs
	}
	nodes, _ := Build(tasks, nil, usage, Root{WorkspaceID: 1})
	a := find(t, nodes, 1)
	if a.Usage.Subtree.InputTokens == nil || *a.Usage.Subtree.InputTokens != 0 {
		t.Errorf("subtree.inputTokens = %v, want reported 0", a.Usage.Subtree.InputTokens)
	}
	if a.Usage.Subtree.OutputTokens != nil {
		t.Errorf("subtree.outputTokens = %v, want nil (never reported)", a.Usage.Subtree.OutputTokens)
	}
	if a.Usage.Subtree.CacheReadTokens != nil {
		t.Errorf("subtree.cacheReadTokens = %v, want nil", a.Usage.Subtree.CacheReadTokens)
	}
	if a.Usage.Subtree.CacheWriteTokens != nil {
		t.Errorf("subtree.cacheWriteTokens = %v, want nil", a.Usage.Subtree.CacheWriteTokens)
	}
	if a.Usage.Subtree.ReasoningTokens != nil {
		t.Errorf("subtree.reasoningTokens = %v, want nil", a.Usage.Subtree.ReasoningTokens)
	}
	if a.Usage.Subtree.CostUSD != nil {
		t.Errorf("subtree.costUsd = %v, want nil", a.Usage.Subtree.CostUSD)
	}
	// The wire encoding must render null, not 0.
	data, err := json.Marshal(a.Usage.Subtree)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"runs":2,"inputTokens":0,"outputTokens":null,"cacheReadTokens":null,"cacheWriteTokens":null,"reasoningTokens":null,"costUsd":null}`
	if string(data) != want {
		t.Fatalf("wire = %s, want %s", data, want)
	}
}

// TestBuild_CountsRunningWaitingFailedDone: counts bucket each live
// descendant by latest-run state; stopped counts as done, idle for no
// runs, error as failed.
func TestBuild_CountsRunningWaitingFailedDone(t *testing.T) {
	tasks := []Task{
		task(1, nil, false),
		task(2, pid(1), false), task(3, pid(1), false), task(4, pid(1), false),
		task(5, pid(1), false), task(6, pid(1), false),
	}
	facts := map[int64]RunFact{
		1: {State: StateRunning},
		2: {State: StateRunning, RunningRunIDs: []string{"r2"}, PendingPermissions: 1},
		3: {State: StateWaitingPermission},
		4: {State: StateError},
		5: {State: StateStopped},
		// 6 absent -> idle
	}
	nodes, _ := Build(tasks, facts, nil, Root{WorkspaceID: 1})
	a := find(t, nodes, 1)
	want := Counts{Running: 1, WaitingPermission: 1, Failed: 1, Done: 1, Idle: 1}
	if a.Counts != want {
		t.Fatalf("counts = %+v, want %+v", a.Counts, want)
	}
	if a.Counts.Done != 1 {
		t.Fatalf("stopped must count as done exactly once")
	}
	b := find(t, nodes, 2)
	if b.Run.State != StateRunning || len(b.Run.RunningRunIDs) != 1 || b.Run.RunningRunIDs[0] != "r2" || b.Run.PendingPermissions != 1 {
		t.Fatalf("b.run = %+v", b.Run)
	}
	if (Counts{}) != b.Counts {
		t.Fatalf("leaf counts = %+v, want zero", b.Counts)
	}
}

// TestBuild_CycleTerminates: a corrupt parent_task_id cycle neither loops
// in aggregation nor in emission.
func TestBuild_CycleTerminates(t *testing.T) {
	tasks := []Task{
		task(1, pid(2), false), // 1 <-> 2 cycle
		task(2, pid(1), false),
		task(3, pid(2), false),
		task(4, nil, false),
	}
	usage := map[int64]UsageTotals{1: {Runs: 1}, 2: {Runs: 1}, 3: {Runs: 1}, 4: {Runs: 1}}
	done := make(chan struct{})
	var nodes []Node
	go func() {
		nodes, _ = Build(tasks, nil, usage, Root{WorkspaceID: 1})
		close(done)
	}()
	<-done
	// In workspace mode the cycle members' parents both exist and are
	// live, so neither is a root and only 4 is emitted -- terminating, not
	// looping, is the contract.
	if got := ids(nodes); !equalIDs(got, []int64{4}) {
		t.Fatalf("workspace nodes = %v, want [4]", got)
	}
	// Rooted inside the cycle: 1 -> 2 -> (1 visited, skipped) -> 3, each
	// exactly once, and cycle-aggregated usage does not explode.
	nodes, _ = Build(tasks, nil, usage, Root{TaskID: 1})
	if got := ids(nodes); !equalIDs(got, []int64{1, 2, 3}) {
		t.Fatalf("rooted nodes = %v, want [1 2 3]", got)
	}
	if n := find(t, nodes, 2); n.Usage.Subtree.Runs != 2 {
		t.Fatalf("cycle node 2 subtree.runs = %d, want 2 (no double count)", n.Usage.Subtree.Runs)
	}
}

// TestBuild_TruncatesAt500: more than MaxNodes live tasks truncates with
// truncated=true and exactly MaxNodes nodes.
func TestBuild_TruncatesAt500(t *testing.T) {
	tasks := make([]Task, 0, MaxNodes+10)
	tasks = append(tasks, task(1, nil, false))
	for i := 2; i <= MaxNodes+10; i++ {
		tasks = append(tasks, task(int64(i), pid(1), false))
	}
	nodes, trunc := Build(tasks, nil, nil, Root{WorkspaceID: 1})
	if !trunc {
		t.Fatal("truncated = false, want true")
	}
	if len(nodes) != MaxNodes {
		t.Fatalf("len(nodes) = %d, want %d", len(nodes), MaxNodes)
	}
	// Under the cap: no truncation.
	nodes, trunc = Build(tasks[:100], nil, nil, Root{WorkspaceID: 1})
	if trunc || len(nodes) != 100 {
		t.Fatalf("len = %d trunc = %v, want 100 false", len(nodes), trunc)
	}
}

// TestTierOf_Table pins the tier derivation table.
func TestTierOf_Table(t *testing.T) {
	cases := []struct {
		hasParent, liveChildren bool
		want                    string
	}{
		{false, true, "supervisor"},
		{true, true, "leader"},
		{true, false, "coder"},
		{false, false, ""},
	}
	for _, tc := range cases {
		if got := TierOf(tc.hasParent, tc.liveChildren); got != tc.want {
			t.Errorf("TierOf(%v, %v) = %q, want %q", tc.hasParent, tc.liveChildren, got, tc.want)
		}
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func costPtr(v float64) *float64 { return &v }

// TestBuild_NodeRunCarriesTheDisposition (D13): a node whose latest run is
// "done" at a cap says so, and one without says nothing (the key is omitted).
func TestBuild_NodeRunCarriesTheDisposition(t *testing.T) {
	tasks := []Task{task(1, nil, false), task(2, pid(1), false), task(3, pid(1), false)}
	facts := map[int64]RunFact{
		2: {State: StateDone, Disposition: "needs-continue"},
		3: {State: StateDone},
	}
	nodes, _ := Build(tasks, facts, nil, Root{WorkspaceID: 1})
	if got := find(t, nodes, 2).Run; got.State != StateDone || got.Disposition != "needs-continue" {
		t.Fatalf("capped node run = %+v", got)
	}
	plain, err := json.Marshal(find(t, nodes, 3).Run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "disposition") {
		t.Fatalf("a node without a disposition must omit the key: %s", plain)
	}
}

// TestTaskTree_NodeCarriesLatestRunFailure (ADR-0041 R2): the latest run's
// typed failure rides on the node; a running node carries none.
func TestTaskTree_NodeCarriesLatestRunFailure(t *testing.T) {
	tasks := []Task{task(1, nil, false), task(2, pid(1), false), task(3, pid(1), false)}
	facts := map[int64]RunFact{
		2: {State: StateError, FailureKind: "rate_limited", Window: "5h", ResetAt: "2026-10-22T10:35:18Z", ResetInferred: true},
		3: {State: StateRunning},
	}
	nodes, _ := Build(tasks, facts, nil, Root{WorkspaceID: 1})
	got := find(t, nodes, 2).Run
	if got.FailureKind != "rate_limited" || got.Window != "5h" || got.ResetAt != "2026-10-22T10:35:18Z" || !got.ResetInferred {
		t.Fatalf("failed node run = %+v", got)
	}
	run := find(t, nodes, 3).Run
	if run.FailureKind != "" || run.Window != "" || run.ResetAt != "" {
		t.Fatalf("running node run = %+v, want no failure", run)
	}
	b, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "failureKind") || strings.Contains(string(b), "resetAt") || strings.Contains(string(b), "resetInferred") {
		t.Fatalf("running node must omit failure keys: %s", b)
	}
	b, _ = json.Marshal(got)
	if !strings.Contains(string(b), `"failureKind":"rate_limited"`) || !strings.Contains(string(b), `"resetAt":"2026-10-22T10:35:18Z"`) || !strings.Contains(string(b), `"resetInferred":true`) {
		t.Fatalf("failed node json = %s", b)
	}
}

func TestTaskTreeShowsRemoteChildrenAndRollups(t *testing.T) {
	tasks := []Task{task(1, nil, false), task(2, pid(1), false)}
	usage := map[int64]UsageTotals{
		1: {Runs: 1, CostUSD: costPtr(1.0)},
		2: {Runs: 1, CostUSD: costPtr(2.0)},
	}
	remotes := []RemoteTask{
		{HostID: "h2", Alias: "win", RemoteTaskID: "7", ParentID: 1, Title: "w", State: "done", CostUSD: costPtr(0.25)},
		{HostID: "h1", Alias: "box", RemoteTaskID: "7", ParentID: 1, Title: "b", State: "running", CostUSD: costPtr(0.5)},
	}
	nodes, trunc := BuildWithRemote(tasks, nil, usage, remotes, Root{WorkspaceID: 1})
	if trunc {
		t.Fatal("truncated")
	}
	if got := ids(nodes); !equalIDs(got, []int64{1, 2, -1, -2}) {
		t.Fatalf("order = %v, want [1 2 -1 -2]", got)
	}
	box, win := nodes[2], nodes[3]
	for _, tc := range []struct {
		n      Node
		host   string
		hostID string
		gid    string
		state  State
	}{
		{box, "box", "h1", "box:7", StateRunning},
		{win, "win", "h2", "win:7", StateDone},
	} {
		n := tc.n
		if !n.Remote || n.Host != tc.host || n.HostID != tc.hostID || n.GlobalID != tc.gid {
			t.Errorf("remote node = %+v", n)
		}
		if n.ID >= 0 || n.ParentID == nil || *n.ParentID != 1 || n.Depth != 1 || n.Run.State != tc.state {
			t.Errorf("remote node shape = %+v", n)
		}
		if n.Tier != nil || n.ChildCount != 0 {
			t.Errorf("remote node tier/childCount = %v/%d", n.Tier, n.ChildCount)
		}
	}
	root := nodes[0]
	if root.Usage.Subtree.CostUSD == nil || *root.Usage.Subtree.CostUSD != 3.75 {
		t.Fatalf("subtree cost = %v, want 3.75", root.Usage.Subtree.CostUSD)
	}
	if root.Usage.Subtree.Runs != 4 {
		t.Fatalf("subtree runs = %d, want 4", root.Usage.Subtree.Runs)
	}
	if root.Counts.Running != 1 || root.Counts.Done != 1 {
		t.Fatalf("counts = %+v", root.Counts)
	}
	if root.Tier == nil || *root.Tier != "supervisor" || root.ChildCount != 3 {
		t.Fatalf("root tier/childCount = %v/%d", root.Tier, root.ChildCount)
	}
}

func TestTaskTreeRemoteNilCostStaysNil(t *testing.T) {
	tasks := []Task{task(1, nil, false)}
	r1 := RemoteTask{HostID: "h1", Alias: "box", RemoteTaskID: "1", ParentID: 1, State: "running"}
	nodes, _ := BuildWithRemote(tasks, nil, nil, []RemoteTask{r1}, Root{TaskID: 1})
	if nodes[0].Usage.Subtree.CostUSD != nil || nodes[1].Usage.Own.CostUSD != nil {
		t.Fatalf("nil cost became %v / %v", nodes[0].Usage.Subtree.CostUSD, nodes[1].Usage.Own.CostUSD)
	}
	r2 := RemoteTask{HostID: "h1", Alias: "box", RemoteTaskID: "2", ParentID: 1, State: "done", CostUSD: costPtr(0.4)}
	nodes, _ = BuildWithRemote(tasks, nil, nil, []RemoteTask{r1, r2}, Root{TaskID: 1})
	if c := nodes[0].Usage.Subtree.CostUSD; c == nil || *c != 0.4 {
		t.Fatalf("subtree cost = %v, want 0.4", c)
	}
	if nodes[1].Usage.Own.CostUSD != nil {
		t.Fatalf("first remote own cost = %v, want nil", nodes[1].Usage.Own.CostUSD)
	}
}

func TestTaskTreeRemoteIgnoredWithoutVisibleParent(t *testing.T) {
	tasks := []Task{task(1, nil, false), task(2, pid(1), true)}
	remotes := []RemoteTask{
		{HostID: "h1", Alias: "box", RemoteTaskID: "1", ParentID: 2, State: "running", CostUSD: costPtr(0.7)},
		{HostID: "h1", Alias: "box", RemoteTaskID: "2", ParentID: 99, State: "running", CostUSD: costPtr(5)},
	}
	nodes, _ := BuildWithRemote(tasks, nil, nil, remotes, Root{WorkspaceID: 1})
	if got := ids(nodes); !equalIDs(got, []int64{1}) {
		t.Fatalf("nodes = %v, want [1]", got)
	}
	root := nodes[0]
	if root.ChildCount != 0 || root.Usage.Subtree.Runs != 1 {
		t.Fatalf("root = %+v", root)
	}
	// The archived parent's remote still rolls up; the unknown parent's does not.
	if c := root.Usage.Subtree.CostUSD; c == nil || *c != 0.7 {
		t.Fatalf("subtree cost = %v, want 0.7", c)
	}
}

func TestBuildEqualsBuildWithRemoteNil(t *testing.T) {
	tasks := []Task{task(1, nil, false), task(2, pid(1), false), task(3, pid(2), true)}
	a, ta := Build(tasks, nil, nil, Root{WorkspaceID: 1})
	b, tb := BuildWithRemote(tasks, nil, nil, nil, Root{WorkspaceID: 1})
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if ta != tb || string(ja) != string(jb) {
		t.Fatalf("Build != BuildWithRemote(nil)\n%s\n%s", ja, jb)
	}
}

// TestTreeNode_FailureActionSerialises: the latest run's next action rides on
// the node as `action`; a node without a failure omits the key.
func TestTreeNode_FailureActionSerialises(t *testing.T) {
	tasks := []Task{task(1, nil, false), task(2, pid(1), false)}
	facts := map[int64]RunFact{
		2: {State: StateError, FailureKind: "auth_expired", Action: &failure.Action{Kind: failure.ActionSignIn, Target: "settings:providers?provider=glm", Label: "Sign in again to glm"}},
	}
	nodes, _ := Build(tasks, facts, nil, Root{WorkspaceID: 1})
	b, err := json.Marshal(find(t, nodes, 2).Run)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"action":{"kind":"sign_in","target":"settings:providers?provider=glm","label":"Sign in again to glm"}`) {
		t.Fatalf("failed node json = %s", b)
	}
	b, _ = json.Marshal(find(t, nodes, 1).Run)
	if strings.Contains(string(b), "action") {
		t.Fatalf("a node without a failure must omit action: %s", b)
	}
}
