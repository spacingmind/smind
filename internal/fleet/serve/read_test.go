package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const canary = "sk-CANARY-abcdefghijklmnopqrstuvwxyz0123456789"

type memSource struct {
	workspaces []WorkspaceInfo
	tasks      map[int64]store.Task
	runs       map[string]store.Run
	events     map[string][]store.RunEvent
	costs      map[string]*float64
	origins    map[int64][2]string
}

func newMemSource() *memSource {
	return &memSource{
		tasks:   map[int64]store.Task{},
		runs:    map[string]store.Run{},
		events:  map[string][]store.RunEvent{},
		costs:   map[string]*float64{},
		origins: map[int64][2]string{},
	}
}

func (m *memSource) ListWorkspaces() ([]WorkspaceInfo, error) { return m.workspaces, nil }

func (m *memSource) ListTasks(ws int64) ([]store.Task, error) {
	var out []store.Task
	for id := int64(1); id < 10000; id++ {
		if t, ok := m.tasks[id]; ok && t.WorkspaceID == ws {
			out = append(out, t)
		}
		if len(out) == len(m.tasks) {
			break
		}
	}
	return out, nil
}

func (m *memSource) GetTask(id int64) (store.Task, error) {
	if t, ok := m.tasks[id]; ok {
		return t, nil
	}
	return store.Task{}, fmt.Errorf("task %d: %w", id, ErrNotFound)
}

func (m *memSource) TaskOrigin(id int64) (string, string, error) {
	o := m.origins[id]
	return o[0], o[1], nil
}

func (m *memSource) GetRun(id string) (store.Run, error) {
	if r, ok := m.runs[id]; ok {
		return r, nil
	}
	return store.Run{}, fmt.Errorf("run %s: %w", id, ErrNotFound)
}

func (m *memSource) LatestRun(task int64) (store.Run, bool, error) {
	var best store.Run
	found := false
	for _, r := range m.runs {
		if r.TaskID == task && (!found || r.StartedAt.After(best.StartedAt)) {
			best, found = r, true
		}
	}
	return best, found, nil
}

func (m *memSource) RunCostUSD(id string) (*float64, error) { return m.costs[id], nil }

func (m *memSource) RunEvents(id string, after int64, limit int) ([]store.RunEvent, error) {
	var out []store.RunEvent
	for _, e := range m.events[id] {
		if e.Seq > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *memSource) addEvent(run string, v map[string]any) {
	b, _ := json.Marshal(v)
	seq := int64(len(m.events[run]) + 1)
	m.events[run] = append(m.events[run], store.RunEvent{RunID: run, Seq: seq, EventData: string(b), CreatedAt: time.Unix(1_800_000_000+seq, 0)})
}

func allGrant() registry.Grant {
	return registry.Grant{Ops: []string{registry.OpRead}, WorkspacesRead: registry.WorkspaceScope{All: true}}
}

func ws1Grant() registry.Grant {
	return registry.Grant{Ops: []string{registry.OpRead}, WorkspacesRead: registry.WorkspaceScope{IDs: []int64{1}}}
}

func str(s string) *string { return &s }

func fixture() *memSource {
	m := newMemSource()
	m.workspaces = []WorkspaceInfo{
		{ID: 1, Title: "one", RepoURL: "https://user:ghp_secretsecretsecretsecretsecretsecret@github.com/o/r.git?token=abc", DefaultBranch: "main"},
		{ID: 2, Title: "two", RepoURL: "/Users/x/repo", DefaultBranch: "main"},
	}
	now := time.Unix(1_800_000_000, 0)
	m.tasks[1] = store.Task{ID: 1, WorkspaceID: 1, Title: "t1", Status: "idle", Branch: str("b1"), WorktreePath: str("/Users/x/.smind/worktrees/t"),
		PRURL: "https://github.com/o/r/pull/1?token=abc", PRNumber: 1, SetupLog: "sk-setuplogsecret", CreatedAt: now, UpdatedAt: now}
	m.tasks[2] = store.Task{ID: 2, WorkspaceID: 2, Title: "t2", Status: "idle", CreatedAt: now, UpdatedAt: now}
	m.origins[1] = [2]string{"host-a", "trace-1"}
	cost := 1.5
	m.runs["r1"] = store.Run{ID: "r1", TaskID: 1, Provider: "claude", Status: "done", StartedAt: now, Model: "m"}
	m.costs["r1"] = &cost
	m.runs["r2"] = store.Run{ID: "r2", TaskID: 2, Provider: "claude", Status: "done", StartedAt: now}
	m.events["r2"] = []store.RunEvent{{RunID: "r2", Seq: 1, EventData: `{"type":0,"text":"hi"}`}}
	m.addEvent("r1", map[string]any{"type": int(taskrunner.EventTypeText), "text": "hello /Users/x/proj " + canary})
	return m
}

func TestFleetReadReturnsNoPathsNoCredentialURLs(t *testing.T) {
	m := fixture()
	m.addEvent("r1", map[string]any{"type": int(taskrunner.EventTypeToolCall), "toolName": "Bash", "toolStatus": "completed", "toolInput": map[string]any{"cmd": "cat /Users/x/.ssh/id"}, "toolResult": "sk-toolresult"})
	rd := NewReader(m, ReaderOptions{HostID: "h", Version: "1", HomeDirs: []string{"/Users/x"}})
	g := allGrant()
	var all []any
	add := func(v any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, v)
	}
	add(rd.Info(g))
	add(rd.Workspaces(g))
	add(rd.TaskList(g, wire.TaskListParams{}))
	add(rd.TaskGet(g, wire.TaskGetParams{ID: 1}))
	add(rd.RunStatus(g, wire.RunStatusParams{TaskID: 1}))
	add(rd.Activity(g, wire.TaskActivityParams{TaskID: 1}))
	for _, v := range all {
		b, _ := json.Marshal(v)
		for _, bad := range []string{"/Users/", "/Users/x/.smind/worktrees/t", "ghp_", "token=", "sk-"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%T JSON contains %q: %s", v, bad, b)
			}
		}
	}
	ws, _ := rd.Workspaces(g)
	if ws.Workspaces[0].RepoURL != "https://github.com/o/r.git" || ws.Workspaces[1].RepoURL != "" {
		t.Errorf("repo urls: %+v", ws.Workspaces)
	}
}

func TestFleetActivityCappedAndPaged(t *testing.T) {
	m := newMemSource()
	m.workspaces = []WorkspaceInfo{{ID: 1}}
	m.tasks[1] = store.Task{ID: 1, WorkspaceID: 1}
	m.runs["r"] = store.Run{ID: "r", TaskID: 1}
	for i := 1; i <= 130; i++ {
		text := fmt.Sprintf("line %d", i)
		if i == 3 {
			text = canary + " " + strings.Repeat("word ", 1000)
		}
		m.addEvent("r", map[string]any{"type": int(taskrunner.EventTypeText), "text": text})
	}
	m.addEvent("r", map[string]any{"type": int(taskrunner.EventTypeThinking), "text": "secret thoughts"})
	m.addEvent("r", map[string]any{"type": int(taskrunner.EventTypeToolCall), "toolName": "Bash", "toolStatus": "completed", "toolInput": map[string]any{"c": "x"}, "toolResult": "res"})
	m.addEvent("r", map[string]any{"type": int(taskrunner.EventTypePermissionRequest), "permissionSummary": "run rm -rf"})
	rd := NewReader(m, ReaderOptions{})
	g := allGrant()

	var cursor, lastSeq int64
	var pages, hasMore []int
	var got []wire.Event
	for i := 0; i < 10; i++ {
		a, err := rd.Activity(g, wire.TaskActivityParams{TaskID: 1, Cursor: cursor, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, len(a.Events))
		hm := 0
		if a.HasMore {
			hm = 1
		}
		hasMore = append(hasMore, hm)
		for _, e := range a.Events {
			if e.Seq <= lastSeq {
				t.Fatalf("seq %d not after %d", e.Seq, lastSeq)
			}
			lastSeq = e.Seq
		}
		got = append(got, a.Events...)
		cursor = a.NextCursor
		if !a.HasMore {
			break
		}
	}
	// 133 events read: 50, 50, 33 read; thinking is omitted from the last page.
	if fmt.Sprint(pages) != "[50 50 32]" || fmt.Sprint(hasMore) != "[1 1 0]" {
		t.Fatalf("pages %v hasMore %v", pages, hasMore)
	}
	if cursor != 133 {
		t.Errorf("final cursor %d", cursor)
	}
	var seenTool, seenPerm bool
	for _, e := range got {
		b, _ := json.Marshal(e)
		switch e.Type {
		case "text":
			if e.Seq == 3 && (len(e.Text) > 1024 || !e.Truncated || strings.Contains(e.Text, "sk-")) {
				t.Errorf("event 3 not capped/redacted: len %d trunc %v", len(e.Text), e.Truncated)
			}
		case "tool_call":
			seenTool = true
			if strings.Contains(string(b), "input") || strings.Contains(string(b), "result") || e.ToolName != "Bash" {
				t.Errorf("tool_call leaks: %s", b)
			}
		case "permission_request":
			seenPerm = true
			if e.Text != "" || strings.Contains(string(b), "rm -rf") {
				t.Errorf("permission_request has text: %s", b)
			}
		case "thinking":
			t.Errorf("thinking present")
		}
	}
	if !seenTool || !seenPerm {
		t.Error("tool_call or permission_request missing")
	}
}

func TestFleetReadGrantFiltersWorkspaces(t *testing.T) {
	m := fixture()
	rd := NewReader(m, ReaderOptions{})
	g := ws1Grant()
	wl, _ := rd.Workspaces(g)
	if len(wl.Workspaces) != 1 || wl.Workspaces[0].ID != 1 {
		t.Errorf("workspaces: %+v", wl)
	}
	tl, err := rd.TaskList(g, wire.TaskListParams{})
	if err != nil || len(tl.Tasks) != 1 || tl.Tasks[0].WorkspaceID != 1 {
		t.Errorf("tasks: %+v %v", tl, err)
	}
	_, err = rd.TaskList(g, wire.TaskListParams{WorkspaceID: 2})
	var rf *Refusal
	if !errors.As(err, &rf) || rf.Code != wire.CodeNotGranted {
		t.Errorf("task list ws2: %v", err)
	}
	_, e1 := rd.TaskGet(g, wire.TaskGetParams{ID: 2})
	_, e2 := rd.RunStatus(g, wire.RunStatusParams{TaskID: 2})
	_, e3 := rd.RunStatus(g, wire.RunStatusParams{RunID: "r2"})
	_, e4 := rd.Activity(g, wire.TaskActivityParams{TaskID: 2})
	_, e5 := rd.TaskGet(g, wire.TaskGetParams{ID: 99})
	for i, e := range []error{e1, e2, e3, e4, e5} {
		if !errors.Is(e, ErrNotFound) {
			t.Errorf("case %d: %v", i, e)
		}
	}
	if e1.Error() != e5.Error() {
		t.Errorf("unreadable and absent task differ: %q vs %q", e1, e5)
	}
}

func TestFleetTaskListPaging(t *testing.T) {
	m := newMemSource()
	m.workspaces = []WorkspaceInfo{{ID: 1}, {ID: 2}}
	for i := int64(1); i <= 120; i++ {
		m.tasks[i] = store.Task{ID: i, WorkspaceID: 1 + i%2}
	}
	rd := NewReader(m, ReaderOptions{})
	g := allGrant()
	first, err := rd.TaskList(g, wire.TaskListParams{})
	if err != nil || len(first.Tasks) != 50 || first.NextCursor != 50 {
		t.Fatalf("first page: %d next %d err %v", len(first.Tasks), first.NextCursor, err)
	}
	seen := map[int64]int{}
	var cursor int64
	for i := 0; i < 10; i++ {
		l, err := rd.TaskList(g, wire.TaskListParams{Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, tk := range l.Tasks {
			seen[tk.ID]++
		}
		if l.NextCursor == 0 {
			break
		}
		cursor = l.NextCursor
	}
	if len(seen) != 120 {
		t.Errorf("saw %d tasks", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("task %d seen %d times", id, n)
		}
	}
	big, _ := rd.TaskList(g, wire.TaskListParams{Limit: 500})
	if len(big.Tasks) != 120 {
		t.Errorf("limit 500: %d", len(big.Tasks))
	}
	for i := int64(121); i <= 300; i++ {
		m.tasks[i] = store.Task{ID: i, WorkspaceID: 1}
	}
	big, _ = rd.TaskList(g, wire.TaskListParams{Limit: 500})
	if len(big.Tasks) != 200 || big.NextCursor != 200 {
		t.Errorf("clamp: %d next %d", len(big.Tasks), big.NextCursor)
	}
	_, err = rd.TaskList(g, wire.TaskListParams{Limit: -1})
	var rf *Refusal
	if !errors.As(err, &rf) || rf.Code != wire.CodeInvalid {
		t.Errorf("limit -1: %v", err)
	}
}

func TestFleetRunStatusNilCostStaysNil(t *testing.T) {
	m := fixture()
	m.costs["r1"] = nil
	rd := NewReader(m, ReaderOptions{})
	rs, err := rd.RunStatus(allGrant(), wire.RunStatusParams{RunID: "r1"})
	if err != nil || rs.CostUSD != nil {
		t.Fatalf("%+v %v", rs, err)
	}
	b, _ := json.Marshal(rs)
	if !strings.Contains(string(b), `"costUsd":null`) {
		t.Errorf("json: %s", b)
	}
	cost := 2.5
	m.costs["r1"] = &cost
	rs, _ = rd.RunStatus(allGrant(), wire.RunStatusParams{RunID: "r1"})
	if rs.CostUSD == nil || *rs.CostUSD != 2.5 {
		t.Errorf("cost: %v", rs.CostUSD)
	}
}

func TestFleetRunStatusNeedsExactlyOneSelector(t *testing.T) {
	rd := NewReader(fixture(), ReaderOptions{})
	for _, p := range []wire.RunStatusParams{{}, {RunID: "r1", TaskID: 1}} {
		_, err := rd.RunStatus(allGrant(), p)
		var rf *Refusal
		if !errors.As(err, &rf) || rf.Code != wire.CodeInvalid {
			t.Errorf("%+v: %v", p, err)
		}
	}
	if _, err := rd.RunStatus(allGrant(), wire.RunStatusParams{TaskID: 1}); err != nil {
		t.Error(err)
	}
}
