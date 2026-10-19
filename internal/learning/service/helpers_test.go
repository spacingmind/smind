package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

var t0 = time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

// fakeExec is the Executor: it counts calls and answers from fn.
type fakeExec struct {
	mu    sync.Mutex
	calls []executor.Request
	fn    func(executor.Request) (executor.Result, error)
}

func (f *fakeExec) Run(ctx context.Context, r executor.Request) (executor.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, r)
	fn := f.fn
	f.mu.Unlock()
	if fn == nil {
		return executor.Result{FinalText: `{"intents":[]}`}, nil
	}
	return fn(r)
}

func (f *fakeExec) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeExec) last() executor.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

type recNotifier struct {
	mu       sync.Mutex
	proposed []string
	jobs     []string
	changed  []string
}

func (n *recNotifier) NotifyProposed(ws, id int64, name, kind string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.proposed = append(n.proposed, name+"/"+kind)
}

func (n *recNotifier) NotifyJob(ws, id int64, status string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.jobs = append(n.jobs, status)
}

func (n *recNotifier) NotifyChanged(ws int64, reason string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.changed = append(n.changed, reason)
}

func (n *recNotifier) proposedCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.proposed)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type fx struct {
	t      *testing.T
	st     *store.Store
	clk    *clock
	exec   *fakeExec
	notify *recNotifier
	svc    *Service
	ws     store.Workspace
	task   store.Task
	chat   store.Chat

	cfgMu sync.Mutex
	cfg   config.LearningConfig
	n     int
}

func (f *fx) config() config.LearningConfig {
	f.cfgMu.Lock()
	defer f.cfgMu.Unlock()
	return f.cfg
}

func (f *fx) setConfig(mut func(*config.LearningConfig)) {
	f.cfgMu.Lock()
	mut(&f.cfg)
	f.cfgMu.Unlock()
}

// newFx builds a store with one workspace (mode as given), one task and one
// claude-native chat, and a Service wired to fakes (not started).
func newFx(t *testing.T, mode string) *fx {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fx{t: t, st: st, clk: &clock{t: t0}, exec: &fakeExec{}, notify: &recNotifier{}, cfg: config.DefaultLearning()}
	f.ws, err = st.CreateWorkspace(store.Workspace{Path: "/repo/" + t.Name(), Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	if mode != "" {
		if err := st.SetWorkspaceLearningMode(f.ws.ID, mode); err != nil {
			t.Fatal(err)
		}
	}
	f.task, err = st.CreateTask(store.Task{WorkspaceID: f.ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	f.chat = f.newChat(f.task, "claude-native")
	f.svc = New(Deps{
		Store: st, Exec: f.exec, Approval: approval.New(approval.Deps{Store: st}),
		Config: f.config, Notify: f.notify, Now: f.clk.Now, Home: t.TempDir(),
	})
	t.Cleanup(f.svc.Close)
	return f
}

func (f *fx) newChat(task store.Task, provider string) store.Chat {
	f.t.Helper()
	c, err := f.st.CreateChat(task.ID, "chat")
	if err != nil {
		f.t.Fatal(err)
	}
	if provider != "" {
		if c, err = f.st.BindChatProvider(c.ID, provider); err != nil {
			f.t.Fatal(err)
		}
	}
	return c
}

// finished feeds the observer synchronously (no goroutines) and waits for any
// job it started.
func (f *fx) finished(chat store.Chat, provider string, toolCalls int) {
	f.t.Helper()
	f.svc.handleFinished(runs.RunFinished{
		RunID: "r" + strconv.Itoa(f.nextN()), TaskID: chat.TaskID, ChatID: chat.ID, Provider: provider,
		Purpose: "user", Status: "done", ToolCalls: toolCalls,
	})
	f.svc.wg.Wait()
}

func (f *fx) nextN() int {
	f.n++
	return f.n
}

func (f *fx) jobs() []store.LearningJob {
	f.t.Helper()
	js, err := f.st.ListLearningJobs(f.ws.ID, 100)
	if err != nil {
		f.t.Fatal(err)
	}
	return js
}

func (f *fx) pending(chat store.Chat) int64 {
	f.t.Helper()
	c, err := f.st.GetChat(chat.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return c.LearnPending
}

// ev is one fabricated stored event.
type ev map[string]any

func textEv(s string) ev { return ev{"type": taskrunner.EventTypeText, "text": s} }

func toolEv(id, name, status string, input any) ev {
	e := ev{"type": taskrunner.EventTypeToolCall, "toolCallId": id, "toolName": name, "toolStatus": status}
	if input != nil {
		b, _ := json.Marshal(input)
		e["toolInput"] = json.RawMessage(b)
	}
	return e
}

// addRun stores a finished claude-native user run on chat with the given
// prompt and events, returning the stored event ids.
func (f *fx) addRun(chat store.Chat, prompt string, events ...ev) (string, []int64) {
	f.t.Helper()
	id := "run-" + strconv.Itoa(f.nextN())
	started := f.clk.Now()
	fin := started.Add(time.Minute)
	if _, err := f.st.CreateRun(store.Run{ID: id, TaskID: chat.TaskID, ChatID: chat.ID, Provider: "claude-native", Prompt: prompt,
		Status: "done", StartedAt: started, FinishedAt: &fin, StopReason: "end_turn", Purpose: "user", Origin: "human"}); err != nil {
		f.t.Fatal(err)
	}
	var ids []int64
	for i, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			f.t.Fatal(err)
		}
		row, err := f.st.AppendRunEvent(id, int64(i), string(b))
		if err != nil {
			f.t.Fatal(err)
		}
		ids = append(ids, row.ID)
	}
	return id, ids
}
