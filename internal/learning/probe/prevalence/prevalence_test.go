package prevalence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

type fx struct {
	t    *testing.T
	s    *store.Store
	ws   store.Workspace
	task store.Task
	seq  map[string]int64
}

func newFx(t *testing.T) *fx {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ws, err := s.CreateWorkspace(store.Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	return &fx{t: t, s: s, ws: ws, task: task, seq: map[string]int64{}}
}

func (f *fx) chat(taskID int64) int64 {
	f.t.Helper()
	c, err := f.s.CreateChat(taskID, "c")
	if err != nil {
		f.t.Fatal(err)
	}
	return c.ID
}

func (f *fx) run(id string, taskID, chatID int64, purpose string) {
	f.t.Helper()
	if _, err := f.s.CreateRun(store.Run{ID: id, TaskID: taskID, ChatID: chatID, Provider: "claude-native", Prompt: "p",
		Status: "completed", StartedAt: time.Now().UTC(), Purpose: purpose}); err != nil {
		f.t.Fatal(err)
	}
}

type jev struct {
	Type       taskrunner.EventType `json:"type"`
	ToolCallID string               `json:"toolCallId,omitempty"`
	ToolName   string               `json:"toolName,omitempty"`
	ToolStatus string               `json:"toolStatus,omitempty"`
	ToolInput  json.RawMessage      `json:"toolInput,omitempty"`
	ToolResult json.RawMessage      `json:"toolResult,omitempty"`
}

// add appends one event and returns its seq.
func (f *fx) add(runID string, e jev) int64 {
	f.t.Helper()
	b, _ := json.Marshal(e)
	seq := f.seq[runID]
	f.seq[runID]++
	if _, err := f.s.AppendRunEvent(runID, seq, string(b)); err != nil {
		f.t.Fatal(err)
	}
	return seq
}

func (f *fx) tool(runID, id, status, result string) int64 {
	e := jev{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolName: "Bash", ToolStatus: status}
	e.ToolInput, _ = json.Marshal(map[string]any{"command": "make"})
	if result != "" {
		e.ToolResult, _ = json.Marshal(result)
	}
	return f.add(runID, e)
}

// failure appends a failing Bash call and, when recover is set, a later
// successful one; it returns the success call's seq.
func (f *fx) failure(runID, id, msg string, recover bool) int64 {
	f.tool(runID, id, taskrunner.ToolStatusFailure, msg)
	if !recover {
		return -1
	}
	s := f.tool(runID, id+"-ok", taskrunner.ToolStatusRunning, "")
	f.tool(runID, id+"-ok", taskrunner.ToolStatusSuccess, "ok")
	return s
}

func (f *fx) proposal(runID string, seq int64) int64 {
	f.t.Helper()
	ev, _ := json.Marshal([]gate.Evidence{{Kind: gate.EvFailureRecovery, RunID: runID, Seq: seq, Quote: "q"}})
	p, err := f.s.CreateProposal(store.LearnedProposal{WorkspaceID: f.ws.ID, Kind: "create", Source: "reflector", Name: "n", Evidence: string(ev)})
	if err != nil {
		f.t.Fatal(err)
	}
	return p.ID
}

func (f *fx) svc(mut func(*Deps)) *Service {
	d := Deps{Store: f.s}
	if mut != nil {
		mut(&d)
	}
	return New(d)
}

const failMsg = "make: *** [build] Error 2"

// seed builds the shared scenario: three user runs over two chats, the first
// run failing twice, and returns the proposal id.
func (f *fx) seed() int64 {
	c1, c2 := f.chat(f.task.ID), f.chat(f.task.ID)
	f.run("r1", f.task.ID, c1, "user")
	seq := f.failure("r1", "a", failMsg, true)
	f.failure("r1", "b", failMsg, false) // same failure twice in one run
	f.run("r2", f.task.ID, c1, "user")
	f.failure("r2", "a", "make: *** [build] Error 7", false)
	f.run("r3", f.task.ID, c2, "user")
	f.failure("r3", "a", failMsg, false)
	return f.proposal("r1", seq)
}

func TestPrevalence_CountsDistinctRunsAndChats(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	id := f.seed()
	res, err := f.svc(nil).Scan(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if res.WindowDays != 30 || len(res.Signatures) != 1 {
		t.Fatalf("res = %+v", res)
	}
	sc := res.Signatures[0]
	if sc.Runs != 3 || sc.Chats != 2 || sc.Truncated || sc.LastSeenAt == nil || sc.Tool != "Bash" || sc.Kind != gate.EvFailureRecovery {
		t.Fatalf("sc = %+v", sc)
	}
	if !strings.HasPrefix(sc.Label, "Bash failed: make:") {
		t.Fatalf("label = %q", sc.Label)
	}
}

func TestPrevalence_ExcludesNonUserPurpose(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	c := f.chat(f.task.ID)
	f.run("r1", f.task.ID, c, "user")
	seq := f.failure("r1", "a", failMsg, true)
	f.run("probe1", f.task.ID, c, "probe")
	f.failure("probe1", "a", failMsg, false)
	f.run("refl1", f.task.ID, c, "reflect")
	f.failure("refl1", "a", failMsg, false)
	res, err := f.svc(nil).Scan(context.Background(), f.proposal("r1", seq))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Signatures[0].Runs; got != 1 {
		t.Fatalf("runs = %d, want 1 (non-user purposes excluded)", got)
	}
}

func TestPrevalence_WorkspaceScoped(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	c := f.chat(f.task.ID)
	f.run("r1", f.task.ID, c, "user")
	seq := f.failure("r1", "a", failMsg, true)
	other, err := f.s.CreateWorkspace(store.Workspace{Path: "/other", Title: "o", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	ot, err := f.s.CreateTask(store.Task{WorkspaceID: other.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	f.run("o1", ot.ID, f.chat(ot.ID), "user")
	f.failure("o1", "a", failMsg, false)
	res, err := f.svc(nil).Scan(context.Background(), f.proposal("r1", seq))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Signatures[0]; got.Runs != 1 || got.Chats != 1 {
		t.Fatalf("sc = %+v, want only this workspace", got)
	}

	// Evidence citing another workspace's run yields no signature.
	ev, _ := json.Marshal([]gate.Evidence{{Kind: gate.EvFailureRecovery, RunID: "o1", Seq: 0}})
	p, err := f.s.CreateProposal(store.LearnedProposal{WorkspaceID: f.ws.ID, Kind: "create", Source: "reflector", Name: "x", Evidence: string(ev)})
	if err != nil {
		t.Fatal(err)
	}
	res, err = f.svc(nil).Scan(context.Background(), p.ID)
	if err != nil || len(res.Signatures) != 0 {
		t.Fatalf("foreign evidence: res=%+v err=%v", res, err)
	}
}

func TestPrevalence_WindowBoundary(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	f.run("r1", f.task.ID, f.chat(f.task.ID), "user")
	seq := f.failure("r1", "a", failMsg, true)
	id := f.proposal("r1", seq)
	evs, err := f.s.ListRunEvents("r1")
	if err != nil {
		t.Fatal(err)
	}
	maxCreated := evs[0].CreatedAt
	minCreated := evs[0].CreatedAt
	for _, e := range evs {
		if e.CreatedAt.After(maxCreated) {
			maxCreated = e.CreatedAt
		}
		if e.CreatedAt.Before(minCreated) {
			minCreated = e.CreatedAt
		}
	}
	days := 30 * 24 * time.Hour
	scan := func(now time.Time) int {
		res, err := f.svc(func(d *Deps) { d.Now = func() time.Time { return now } }).Scan(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Signatures) == 0 {
			return 0
		}
		return res.Signatures[0].Runs
	}
	if scan(minCreated.Add(days-time.Minute)) != 1 {
		t.Fatal("events just inside the window must count")
	}
	if scan(maxCreated.Add(days+time.Minute)) != 0 {
		t.Fatal("events just outside the window must not count")
	}
}

func TestPrevalence_TruncatedFlag(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	id := f.seed()
	res, err := f.svc(func(d *Deps) {
		d.Config = func() config.LearningConfig {
			c := config.DefaultLearning()
			c.Probe.PrevalenceMaxEvents = 3
			return c
		}
	}).Scan(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range res.Signatures {
		if !sc.Truncated {
			t.Fatalf("sc = %+v, want truncated", sc)
		}
	}
}

func TestPrevalence_LabelRedacted(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	f.run("r1", f.task.ID, f.chat(f.task.ID), "user")
	token := "ghp_" + strings.Repeat("aB3dE5gH7j", 4)
	seq := f.failure("r1", "a", "auth failed with token "+token, true)
	res, err := f.svc(nil).Scan(context.Background(), f.proposal("r1", seq))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Signatures) != 1 {
		t.Fatalf("res = %+v", res)
	}
	if l := res.Signatures[0].Label; strings.Contains(strings.ToLower(l), "ghp_") || strings.Contains(l, "aB3dE5") {
		t.Fatalf("label leaks the token: %q", l)
	}
}

func TestPrevalence_NoSignatureAndUnknownProposal(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	ev, _ := json.Marshal([]gate.Evidence{{Kind: gate.EvUserMessage, RunID: "r1", Seq: -1, Quote: "q"}})
	p, err := f.s.CreateProposal(store.LearnedProposal{WorkspaceID: f.ws.ID, Kind: "create", Source: "reflector", Name: "n", Evidence: string(ev)})
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.svc(nil).Scan(context.Background(), p.ID)
	if err != nil || res.Signatures == nil || len(res.Signatures) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := f.svc(nil).Scan(context.Background(), 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}
