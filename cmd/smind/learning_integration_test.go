package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/learning/projection"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

const (
	wireToken  = "wiretoken"
	wirePrompt = "why does this test keep failing on CI only, the shared `t.TempDir` is reused across parallel tests"
	wireQuote  = "why does this test keep failing on CI only"
)

// wireExec is the isolated executor: it counts calls and answers one reply.
type wireExec struct {
	mu    sync.Mutex
	calls int
	reply string
}

func (e *wireExec) Run(context.Context, executor.Request) (executor.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return executor.Result{RunID: "refl-1", FinalText: e.reply}, nil
}

func (e *wireExec) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// wireMsg is one server message: a response (ID set) or an event.
type wireMsg struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Event *struct {
		Topic   string          `json:"topic"`
		Payload json.RawMessage `json:"payload"`
	} `json:"event"`
}

type wiringFx struct {
	t      *testing.T
	db     *store.Store
	api    *wsapi.API
	home   string
	exec   *wireExec
	ws     *websocket.Conn
	seq    int
	events []wireMsg
	stop   func()
}

// newWiringFx builds an in-process daemon slice: store, wsapi (real handler on
// an ephemeral port), and wireLearning with a fake executor, all under
// temp homes.
func newWiringFx(t *testing.T, enabled bool) *wiringFx {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	api, err := wsapi.New(workspace.New(db), accounts.New(db), nil, db, wireToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	cfg := config.Config{Learning: config.DefaultLearning()}
	cfg.Learning.Enabled = enabled
	f := &wiringFx{t: t, db: db, api: api, home: t.TempDir(), exec: &wireExec{}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f.stop = wireLearning(ctx, learningWiring{
		Cfg: cfg, DB: db, API: api, Reg: api.Runs, Home: f.home, Secrets: []string{wireToken}, Exec: f.exec,
	})
	t.Cleanup(f.stop)
	f.ws, _, err = websocket.DefaultDialer.Dial("ws"+srv.URL[len("http"):]+"/ws?token="+wireToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.ws.Close() })
	return f
}

func (f *wiringFx) read(deadline time.Time) (wireMsg, bool) {
	_ = f.ws.SetReadDeadline(deadline)
	_, data, err := f.ws.ReadMessage()
	if err != nil {
		return wireMsg{}, false
	}
	var m wireMsg
	if err := json.Unmarshal(data, &m); err != nil {
		f.t.Fatalf("decode %s: %v", data, err)
	}
	return m, true
}

// call sends one request and returns its response; events seen meanwhile are
// kept for waitEvent.
func (f *wiringFx) call(method string, params any) wireMsg {
	f.t.Helper()
	f.seq++
	id := "c" + strconv.Itoa(f.seq)
	raw, _ := json.Marshal(params)
	data, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": json.RawMessage(raw)})
	if err := f.ws.WriteMessage(websocket.TextMessage, data); err != nil {
		f.t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		m, ok := f.read(deadline)
		if !ok {
			f.t.Fatalf("no response to %s", method)
		}
		if m.ID == id {
			return m
		}
		if m.Event != nil {
			f.events = append(f.events, m)
		}
	}
}

func (f *wiringFx) ok(method string, params any, out any) {
	f.t.Helper()
	m := f.call(method, params)
	if m.Error != nil {
		f.t.Fatalf("%s: %+v", method, m.Error)
	}
	if out != nil {
		if err := json.Unmarshal(m.Result, out); err != nil {
			f.t.Fatalf("decode %s: %v", method, err)
		}
	}
}

func (f *wiringFx) failCode(method string, params any) (code, msg string) {
	f.t.Helper()
	m := f.call(method, params)
	if m.Error == nil {
		f.t.Fatalf("%s: want an error", method)
	}
	return m.Error.Code, m.Error.Message
}

func (f *wiringFx) subscribe(topics ...string) {
	f.t.Helper()
	f.ok("events.subscribe", map[string]any{"topics": topics}, nil)
}

// waitEvent returns the first event on topic (buffered or new).
func (f *wiringFx) waitEvent(topic string) json.RawMessage {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for i, e := range f.events {
			if e.Event.Topic == topic {
				f.events = append(f.events[:i], f.events[i+1:]...)
				return e.Event.Payload
			}
		}
		m, ok := f.read(deadline)
		if !ok {
			f.t.Fatalf("timed out waiting for %s", topic)
		}
		if m.Event != nil {
			f.events = append(f.events, m)
		}
	}
}

// workspaceWithChat creates a propose-mode workspace with one finished run on
// a claude-native chat whose human prompt can ground a skill.
func (f *wiringFx) workspaceWithChat(mode string) (store.Workspace, store.Chat, string) {
	f.t.Helper()
	w, err := f.db.CreateWorkspace(store.Workspace{Path: f.t.TempDir(), Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		f.t.Fatal(err)
	}
	if mode != "" {
		if err := f.db.SetWorkspaceLearningMode(w.ID, mode); err != nil {
			f.t.Fatal(err)
		}
	}
	task, err := f.db.CreateTask(store.Task{WorkspaceID: w.ID, Title: "t", Status: "created"})
	if err != nil {
		f.t.Fatal(err)
	}
	chat, err := f.db.CreateChat(task.ID, "chat")
	if err != nil {
		f.t.Fatal(err)
	}
	if chat, err = f.db.BindChatProvider(chat.ID, "claude-native"); err != nil {
		f.t.Fatal(err)
	}
	runID := "run-1"
	started := time.Now().Add(-time.Hour)
	fin := started.Add(time.Minute)
	if _, err := f.db.CreateRun(store.Run{ID: runID, TaskID: task.ID, ChatID: chat.ID, Provider: "claude-native", Prompt: wirePrompt,
		Status: "done", StartedAt: started, FinishedAt: &fin, StopReason: "end_turn", Purpose: "user", Origin: "human"}); err != nil {
		f.t.Fatal(err)
	}
	for i, text := range []string{"the fix is separate dirs", "done"} {
		b, _ := json.Marshal(map[string]any{"type": taskrunner.EventTypeText, "text": text})
		if _, err := f.db.AppendRunEvent(runID, int64(i), string(b)); err != nil {
			f.t.Fatal(err)
		}
	}
	return w, chat, runID
}

func groundedReply(runID string) string {
	b, _ := json.Marshal(map[string]any{"intents": []map[string]any{{
		"action": "create", "name": "learned-flaky-tempdir", "category": "go",
		"description": "Use when a Go test fails on a shared TempDir.",
		"body":        "Run the failing test alone with -race.\nCheck for a shared `t.TempDir` across parallel tests.\nAdd t.Parallel guards or separate dirs.",
		"reason":      "the user hit this on CI",
		"evidence":    []map[string]any{{"kind": "user_message", "run_id": runID, "seq": -1, "quote": wireQuote}},
	}}})
	return string(b)
}

func TestLearningWiring_SharedApprovalAndNotifier(t *testing.T) {
	f := newWiringFx(t, true)
	w, chat, runID := f.workspaceWithChat("propose")
	f.exec.reply = groundedReply(runID)
	f.subscribe(wsapi.TopicLearnedSkillProposed, wsapi.TopicLearningJob, wsapi.TopicLearnedSkillChanged)

	f.ok("learning.reflect", map[string]any{"chatId": chat.ID}, nil)

	var proposed struct {
		WorkspaceID int64  `json:"workspaceId"`
		ProposalID  int64  `json:"proposalId"`
		Name        string `json:"name"`
		Kind        string `json:"kind"`
	}
	if err := json.Unmarshal(f.waitEvent(wsapi.TopicLearnedSkillProposed), &proposed); err != nil {
		t.Fatal(err)
	}
	if proposed.WorkspaceID != w.ID || proposed.Name != "learned-flaky-tempdir" || proposed.Kind != "create" || proposed.ProposalID == 0 {
		t.Fatalf("proposed = %+v", proposed)
	}
	var job struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(f.waitEvent(wsapi.TopicLearningJob), &job); err != nil || job.Status == "" {
		t.Fatalf("job event = %+v, %v", job, err)
	}
	var changed struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(f.waitEvent(wsapi.TopicLearnedSkillChanged), &changed); err != nil || changed.Reason != "job" {
		t.Fatalf("changed event = %+v, %v", changed, err)
	}

	// The proposal the collector stored is approvable through the RPC.
	var skill wsapi.LearnedSkillView
	f.ok("learning.proposals.approve", map[string]any{"id": proposed.ProposalID}, &skill)
	if skill.Name != "learned-flaky-tempdir" || skill.Status != "active" {
		t.Fatalf("approved skill = %+v", skill)
	}
}

func TestLearningWiring_TriggerReflectGoesToCollector(t *testing.T) {
	f := newWiringFx(t, true)
	w, chat, _ := f.workspaceWithChat("propose")
	f.exec.reply = `{"intents":[]}`
	f.ok("learning.reflect", map[string]any{"chatId": chat.ID}, nil)
	deadline := time.Now().Add(10 * time.Second)
	for f.exec.count() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("reflect never reached the collector's executor")
		}
		time.Sleep(10 * time.Millisecond)
	}
	jobs, err := f.db.ListLearningJobs(w.ID, 10)
	if err != nil || len(jobs) != 1 || jobs[0].TriggerKind != "explicit" {
		t.Fatalf("jobs = %+v, %v", jobs, err)
	}

	// learning.curate reaches the real collector either way: before the
	// curator job exists its ErrNotImplemented is the wire's "unavailable";
	// once it does, a workspace with no learned skills is refused by the
	// curator itself ("not enough learned skills"). Both prove the routing.
	code, msg := f.failCode("learning.curate", map[string]any{"workspaceId": w.ID})
	notShipped := code == "unavailable" && strings.Contains(msg, "not available yet")
	refusedByCurator := strings.Contains(msg, "not enough learned skills")
	if !notShipped && !refusedByCurator {
		t.Fatalf("curate = %q %q, want the collector's refusal", code, msg)
	}
}

func TestLearningWiring_DisabledConstructsNothing(t *testing.T) {
	f := newWiringFx(t, false)
	w, chat, _ := f.workspaceWithChat("propose")

	var st struct {
		Enabled    bool `json:"enabled"`
		Workspaces []struct {
			ID int64 `json:"id"`
		} `json:"workspaces"`
	}
	f.ok("learning.status", nil, &st)
	if st.Enabled || len(st.Workspaces) != 1 {
		t.Fatalf("status = %+v", st)
	}
	if code, _ := f.failCode("learning.reflect", map[string]any{"chatId": chat.ID}); code != "unavailable" {
		t.Fatalf("reflect code = %q", code)
	}
	if code, _ := f.failCode("learning.curate", map[string]any{"workspaceId": w.ID}); code != "unavailable" {
		t.Fatalf("curate code = %q", code)
	}
	// Read and mutate RPCs still work, so data stays manageable.
	var ps []wsapi.ProposalView
	f.ok("learning.proposals.list", map[string]any{"workspaceId": w.ID}, &ps)
	f.ok("learning.setMode", map[string]any{"workspaceId": w.ID, "mode": "off"}, nil)

	if f.exec.count() != 0 {
		t.Fatalf("executor ran %d times with learning disabled", f.exec.count())
	}
	if _, err := os.Stat(filepath.Join(f.home, "learning")); !os.IsNotExist(err) {
		t.Fatalf("learning dir exists with learning disabled: %v", err)
	}
	f.stop()
	f.stop() // idempotent
}

func TestLearningWiring_ForgetAndOffRemoveProjection(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run("enabled="+strconv.FormatBool(enabled), func(t *testing.T) {
			f := newWiringFx(t, enabled)
			w, _, _ := f.workspaceWithChat("propose")
			root := filepath.Join(f.home, "learning", "projection")
			// Projections are read-only trees; let the temp dir cleanup delete them.
			t.Cleanup(func() { _ = projection.Remove(root, w.ID) })
			project := func() string {
				p, err := projection.Project(root, w.ID, []projection.Skill{{Name: "learned-x", Category: "go", Description: "d", Body: "b"}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(p.SkillsDir); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(root, strconv.FormatInt(w.ID, 10))
			}

			dir := project()
			f.ok("learning.setMode", map[string]any{"workspaceId": w.ID, "mode": "off"}, nil)
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("projection survived mode off: %v", err)
			}
			dir = project()
			f.ok("learning.forget", map[string]any{"workspaceId": w.ID}, nil)
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("projection survived forget: %v", err)
			}
			// Turning learning ON again leaves a projection alone.
			dir = project()
			f.ok("learning.setMode", map[string]any{"workspaceId": w.ID, "mode": "propose"}, nil)
			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("projection removed on propose: %v", err)
			}
		})
	}
}

func TestLearningWiring_RedactorIncludesMcpSecrets(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	redact := newLearningRedactor(db, []string{"daemon-token-0123456789"}, []string{"/Users/someone"})

	const first, second = "mcp-secret-value-AAAA1111", "mcp-secret-value-BBBB2222"
	line := "see " + first + " and " + second + " and daemon-token-0123456789 in /Users/someone/x"
	got := redact(line)
	if !strings.Contains(got, first) || !strings.Contains(got, second) {
		t.Fatalf("redacted before any MCP server exists: %q", got)
	}
	if strings.Contains(got, "daemon-token-0123456789") || strings.Contains(got, "/Users/someone") {
		t.Fatalf("daemon token or home dir leaked: %q", got)
	}
	// A server added after the redactor was built is covered at once.
	if _, err := db.CreateMcpServer(store.McpServer{Name: "a", Transport: "stdio", Command: "x", Env: `{"API":"` + first + `"}`}); err != nil {
		t.Fatal(err)
	}
	got = redact(line)
	if strings.Contains(got, first) || !strings.Contains(got, second) {
		t.Fatalf("first server not covered (or second covered early): %q", got)
	}
	if _, err := db.CreateMcpServer(store.McpServer{Name: "b", Transport: "http", URL: "http://x", Headers: `{"Authorization":"Bearer ` + second + `"}`}); err != nil {
		t.Fatal(err)
	}
	if got = redact(line); strings.Contains(got, first) || strings.Contains(got, second) {
		t.Fatalf("MCP secrets leaked: %q", got)
	}
}
