package wsapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

type forkEnv struct {
	wm     *workspace.Manager
	db     *store.Store
	reg    *runs.Registry
	task   store.Task
	parent store.Chat
	other  store.Chat
	fork   handlerFunc
	list   handlerFunc
}

// newForkEnv stores the Claude fixture turn as finished run "r1" of a bound
// claude-native chat, with a native_ref to the recorded session, a second chat
// with its own run "r2", and the handlers under test.
func newForkEnv(t *testing.T) *forkEnv {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	reg, err := runs.New(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.CloseAll)
	root := t.TempDir()
	file := filepath.Join(root, "projects", native.ClaudeProjectDir("/work/repo"), hcSession+".jsonl")
	data, err := os.ReadFile("../history/native/testdata/claude/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	reg.SetRebuilder(native.New(native.Config{Roots: func(string, string) []string { return []string{root} }}))
	ws, err := db.CreateWorkspace(store.Workspace{Path: t.TempDir(), Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 5, 9, 10, 0, 0, time.UTC)
	fin := started.Add(10 * time.Second)
	sum := sha256.Sum256([]byte(hcPrompt))
	ref, _ := json.Marshal(native.Ref{Provider: "claude-native", Harness: native.HarnessClaudeCode, SessionID: hcSession, ConfigDir: root, Cwd: "/work/repo",
		PromptSha256: hex.EncodeToString(sum[:]), TurnWindow: native.TurnWindow{StartedAt: started, FinishedAt: fin}})
	mk := func(title, runID string) store.Chat {
		c, err := db.CreateChat(task.ID, title)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.BindChatProvider(c.ID, "claude-native"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateRun(store.Run{ID: runID, TaskID: task.ID, ChatID: c.ID, Provider: "claude-native", Prompt: hcPrompt, Status: "done", StartedAt: started, FinishedAt: &fin}); err != nil {
			t.Fatal(err)
		}
		for i, ev := range hcLiveTurn() {
			d, err := runs.EncodeStoredEvent(ev)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.AppendRunEvent(runID, int64(i), d); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.SetRunNativeRef(runID, string(ref)); err != nil {
			t.Fatal(err)
		}
		return c
	}
	parent, other := mk("main", "r1"), mk("side", "r2")
	return &forkEnv{wm: wm, db: db, reg: reg, task: task, parent: parent, other: other,
		fork: handleChatFork(wm, reg, db), list: handleChatList(wm, db)}
}

func (e *forkEnv) call(t *testing.T, h handlerFunc, rc *requestContext, params any) (any, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return h(context.Background(), rc, raw)
}

var humanRC = &requestContext{local: true}

func TestChatFork_NativeCreatesChildWithPendingForkHandle(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	got, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1"})
	if err != nil {
		t.Fatalf("chat.fork error = %v", err)
	}
	view := got.(chatView)
	if view.Fork == nil || view.Fork.ParentChatID != e.parent.ID || view.Fork.ForkedAtRunID != "r1" || view.Fork.Mode != store.ChatForkModeNative || view.Fork.Lossy {
		t.Fatalf("fork = %+v", view.Fork)
	}
	if view.TaskID != e.task.ID || view.Provider == nil || *view.Provider != "claude-native" || !strings.HasPrefix(view.Title, "Fork of main") {
		t.Fatalf("child = %+v", view.Chat)
	}
	var h taskrunner.SessionHandle
	if view.AgentSession == nil || json.Unmarshal([]byte(*view.AgentSession), &h) != nil {
		t.Fatalf("agent session = %v", view.AgentSession)
	}
	if h.SessionID != hcSession || h.Fork == nil || h.Fork.EntryUUID == "" || string(h.Provider) != "claude-native" {
		t.Fatalf("child handle = %+v, want the parent's session with a pending fork marker", h)
	}
}

func TestChatFork_ParentUntouched(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	before, _ := e.db.GetChat(e.parent.ID)
	if _, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1"}); err != nil {
		t.Fatal(err)
	}
	after, err := e.db.GetChat(e.parent.ID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("parent chat changed: %+v -> %+v (%v)", before, after, err)
	}
	if _, err := e.db.GetChatFork(e.parent.ID); err == nil {
		t.Fatal("the parent chat gained a fork record")
	}
}

func TestChatFork_BaselineTotalsAtAnchorBecomeTheChildsUsageBaseline(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	fin := time.Date(2026, 11, 5, 9, 10, 10, 0, time.UTC)
	if err := e.db.AppendChatClaudeTotals(e.parent.ID, fin.Add(-time.Minute), `{"costUsd":1.5}`); err != nil {
		t.Fatal(err)
	}
	if err := e.db.AppendChatClaudeTotals(e.parent.ID, fin.Add(time.Hour), `{"costUsd":9}`); err != nil { // a later turn
		t.Fatal(err)
	}
	got, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1"})
	if err != nil {
		t.Fatal(err)
	}
	var h taskrunner.SessionHandle
	if err := json.Unmarshal([]byte(*got.(chatView).AgentSession), &h); err != nil || h.UsageTotals == nil || h.UsageTotals.CostUSD != 1.5 {
		t.Fatalf("handle totals = %+v, %v; want the totals logged at or before the anchor turn's end (1.5)", h.UsageTotals, err)
	}
}

func TestChatFork_SeededCreatesLossyChildFromStoredTurns(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	got, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1", "mode": "seeded"})
	if err != nil {
		t.Fatalf("chat.fork seeded error = %v", err)
	}
	v := got.(chatView)
	if v.Fork == nil || !v.Fork.Lossy || v.Fork.Mode != store.ChatForkModeSeeded || v.AgentSession != nil {
		t.Fatalf("seeded child = %+v fork %+v, want lossy, no provider session", v.Chat, v.Fork)
	}
	f, err := e.db.GetChatFork(v.ID)
	if err != nil || !strings.Contains(f.Seed, hcPrompt) || !strings.Contains(f.Seed, "Done: fixed.") {
		t.Fatalf("seed = %q, %v; want the stored prompt and the final text", f.Seed, err)
	}
}

func TestChatFork_RefusedForOtherHarnessesWithReasonAndSeededOpen(t *testing.T) {
	t.Parallel()
	for _, harness := range []string{native.HarnessCodex, native.HarnessACP} {
		t.Run(harness, func(t *testing.T) {
			t.Parallel()
			e := newForkEnv(t)
			sk, _ := e.db.GetRunSkeleton("r1")
			var ref native.Ref
			_ = json.Unmarshal([]byte(sk.NativeRef), &ref)
			ref.Harness = harness
			b, _ := json.Marshal(ref)
			if err := e.db.SetRunNativeRef("r1", string(b)); err != nil {
				t.Fatal(err)
			}
			_, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1"})
			ce, ok := err.(*rpcCodeError)
			if !ok || ce.code != codeForkUnavailable {
				t.Fatalf("error = %v, want %s", err, codeForkUnavailable)
			}
			d := ce.data.(map[string]any)
			if d["reason"] != string(native.ReasonForkUnsupported) || d["seededAvailable"] != true {
				t.Fatalf("data = %+v", d)
			}
			if forks, _ := e.db.ListChatForksByTask(e.task.ID); len(forks) != 0 {
				t.Fatalf("a refused fork left records: %+v", forks)
			}
			chats, _ := e.db.ListChatsByTask(e.task.ID, true)
			if len(chats) != 2 {
				t.Fatalf("a refused fork created a chat: %d chats", len(chats))
			}
		})
	}
}

func TestChatFork_RefusesBadTargets(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	for name, params := range map[string]map[string]any{
		"a run of another chat": {"chatId": e.parent.ID, "atRunId": "r2"},
		"no such run":           {"chatId": e.parent.ID, "atRunId": "nope"},
		"no run id":             {"chatId": e.parent.ID},
		"bad mode":              {"chatId": e.parent.ID, "atRunId": "r1", "mode": "rewrite"},
	} {
		if _, err := e.call(t, e.fork, humanRC, params); err == nil {
			t.Errorf("%s: chat.fork succeeded, want a refusal", name)
		}
	}
	if _, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": 9999, "atRunId": "r1"}); err == nil {
		t.Error("an unknown chat was forked")
	}
}

func TestChatFork_RPCHumanOnlyAndNotRunScoped(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	if slices.Contains(RunScopedMethods, "chat.fork") {
		t.Fatal("chat.fork is run-scoped: an agent connection could call it")
	}
	_, err := e.call(t, e.fork, &requestContext{runScoped: true}, map[string]any{"chatId": e.parent.ID, "atRunId": "r1"})
	if ce, ok := err.(*rpcCodeError); !ok || ce.code != codeForbidden {
		t.Fatalf("run-scoped chat.fork error = %v, want forbidden", err)
	}
	if chats, _ := e.db.ListChatsByTask(e.task.ID, true); len(chats) != 2 {
		t.Fatalf("a refused call created a chat: %d", len(chats))
	}
	if _, ok := newIdentEnv(t, "hang").api.hs["chat.fork"]; !ok {
		t.Fatal("chat.fork is not registered")
	}
}

// chat.list answers an unforked chat with exactly the bytes it always had, and a
// forked one with an additive `fork` field.
func TestChatList_ForkFieldAdditive(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	plainJSON := func() string {
		chats, _ := e.db.ListChatsByTask(e.task.ID, false)
		b, _ := json.Marshal(chats)
		return string(b)
	}
	listJSON := func() string {
		got, err := e.call(t, e.list, humanRC, map[string]any{"taskId": e.task.ID})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(got)
		return string(b)
	}
	if listJSON() != plainJSON() {
		t.Fatalf("chat.list of unforked chats changed:\n%s\nvs\n%s", listJSON(), plainJSON())
	}
	forked, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1"})
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(listJSON()), &rows); err != nil || len(rows) != 3 {
		t.Fatalf("list = %v, %v", rows, err)
	}
	for _, r := range rows {
		_, has := r["fork"]
		if has != (int64(r["ID"].(float64)) == forked.(chatView).ID) {
			t.Fatalf("fork field on chat %v = %v", r["ID"], has)
		}
	}
}

func TestForkTitle(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("Chào", 40) // multi-byte, well past the cap
	for name, tc := range map[string]struct{ in, want string }{
		"plain":       {"main", "Fork of main"},
		"empty":       {"  ", "Fork of Chat"},
		"trimmed":     {"  side  ", "Fork of side"},
		"cut by rune": {long, "Fork of " + string([]rune(long)[:forkTitleMax])},
	} {
		got := forkTitle(tc.in)
		if got != tc.want || !utf8.ValidString(got) {
			t.Errorf("%s: forkTitle = %q, want %q (valid UTF-8)", name, got, tc.want)
		}
	}
}

// chat.created and chat.updated carry the fork field of a forked chat, so a client
// that patches its list from the event keeps the banner.
func TestChatEventsCarryTheForkField(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	got, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1"})
	if err != nil {
		t.Fatal(err)
	}
	child := got.(chatView)
	b, _ := json.Marshal(chatCreatedPayload{Chat: chatViewOf(e.db, child.Chat)})
	if !strings.Contains(string(b), `"fork":{"parentChatId":`) {
		t.Fatalf("created payload = %s, want a fork field", b)
	}
	b, _ = json.Marshal(chatCreatedPayload{Chat: chatViewOf(e.db, e.parent)})
	if strings.Contains(string(b), `"fork"`) {
		t.Fatalf("an unforked chat's payload = %s, want no fork field", b)
	}
}

func TestChatFork_RefusesARunThatIsNotAUserRun(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	started := time.Now().UTC()
	if _, err := e.db.CreateRun(store.Run{ID: "sys", TaskID: e.task.ID, ChatID: e.parent.ID, Provider: "claude-native", Prompt: "p", Status: "done", Purpose: "reflect", StartedAt: started, FinishedAt: &started}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "sys", "mode": "seeded"}); err == nil {
		t.Fatal("a seeded fork at a non-user run succeeded: its seed would carry every turn of the chat")
	}
}

// Through the real notifier: chat.created for a chat made with a fork record and
// chat.updated for a renamed fork carry the fork field; an unforked chat's events
// do not.
func TestChatLifecycleEventsCarryTheForkField(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicChatCreated, TopicChatUpdated)

	parent, err := db.CreateChat(task.ID, "parent")
	if err != nil {
		t.Fatal(err)
	}
	child, err := wm.CreateChatThen(task.ID, "child", func(c store.Chat) error {
		return db.InsertChatFork(store.ChatFork{ChatID: c.ID, ParentChatID: parent.ID, ForkedAtRunID: "r", Mode: store.ChatForkModeSeeded, Seed: "s"})
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := ec.nextEvent(5 * time.Second)
	var created chatCreatedPayload
	decodePayload(t, ev, &created)
	if ev.Topic != TopicChatCreated || created.Chat.ID != child.ID || created.Chat.Fork == nil || !created.Chat.Fork.Lossy {
		t.Fatalf("chat.created = %s %+v, want the fork field", ev.Topic, created.Chat)
	}
	sendRequest(t, ec.ws, "rename", "chat.rename", map[string]any{"id": child.ID, "title": "Renamed"})
	if resp := ec.nextResponse("rename", 5*time.Second); resp.Error != nil {
		t.Fatalf("chat.rename error = %v", resp.Error.Message)
	}
	ev = ec.nextEvent(5 * time.Second)
	var updated chatUpdatedPayload
	decodePayload(t, ev, &updated)
	if ev.Topic != TopicChatUpdated || updated.Chat.Fork == nil {
		t.Fatalf("chat.updated = %s %+v, want the fork field", ev.Topic, updated.Chat)
	}
}

func TestChatFork_RetryKindIsRecordedAndTitled(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	for _, mode := range []string{"native", "seeded"} {
		got, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1", "kind": "retry", "mode": mode})
		if err != nil {
			t.Fatalf("chat.fork retry (%s) error = %v", mode, err)
		}
		v := got.(chatView)
		if v.Fork == nil || v.Fork.Kind != store.ChatForkKindRetry || !strings.HasPrefix(v.Title, "Retry of main") {
			t.Fatalf("%s retry child = %+v fork %+v, want kind retry and a Retry title", mode, v.Chat, v.Fork)
		}
		f, err := e.db.GetChatFork(v.ID)
		if err != nil || f.Kind != store.ChatForkKindRetry {
			t.Fatalf("stored fork = %+v, %v; want kind retry", f, err)
		}
	}
	if _, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1", "kind": "rewrite"}); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
}

func TestChatForkSeed_ReturnsTheSeedOnlyForASeededFork(t *testing.T) {
	t.Parallel()
	e := newForkEnv(t)
	seedOf := func(rc *requestContext, chatID int64) (string, error) {
		got, err := e.call(t, handleChatForkSeed(e.db), rc, map[string]any{"chatId": chatID})
		if err != nil {
			return "", err
		}
		return got.(map[string]any)["seed"].(string), nil
	}
	seeded, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1", "mode": "seeded"})
	if err != nil {
		t.Fatal(err)
	}
	native, err := e.call(t, e.fork, humanRC, map[string]any{"chatId": e.parent.ID, "atRunId": "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if s, err := seedOf(humanRC, seeded.(chatView).ID); err != nil || !strings.Contains(s, hcPrompt) {
		t.Fatalf("seeded fork seed = %q, %v; want the stored turns", s, err)
	}
	for name, id := range map[string]int64{"native fork": native.(chatView).ID, "unforked chat": e.parent.ID, "unknown chat": 9999} {
		if s, err := seedOf(humanRC, id); err != nil || s != "" {
			t.Fatalf("%s seed = %q, %v; want empty", name, s, err)
		}
	}
	if _, err := seedOf(&requestContext{runScoped: true}, seeded.(chatView).ID); err == nil {
		t.Fatal("a run-scoped caller read a fork's seed")
	}
	if slices.Contains(RunScopedMethods, "chat.forkSeed") {
		t.Fatal("chat.forkSeed is run-scoped")
	}
}
