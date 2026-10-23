package runs

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// withClaude rebuilds e's runner so it can run both the fake GLM agent and
// the fake claude CLI (the test binary re-executed; scenario "reply"), and
// re-wires the registry's starter to it.
func withClaude(t *testing.T, e *queuedTestEnv) *queuedTestEnv {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e.runner = taskrunner.New(e.wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(e.st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}),
		taskrunner.WithClaudeCLIPath(self, "FAKE_CLAUDE_SCENARIO=reply"))
	e.reg.SetStarter(e.wm, e.runner)
	return e
}

func newFallbackEnv(t *testing.T, scenario string) *queuedTestEnv {
	t.Helper()
	return withClaude(t, newQueuedTestEnv(t, scenario))
}

// fallbackProfiles creates the claude-native fallback profile and a glm
// profile that falls back to it; it returns (glm profile id, fallback id).
func fallbackProfiles(t *testing.T, st *store.Store) (int64, int64) {
	t.Helper()
	fb, err := st.CreateAgentProfile(store.AgentProfile{Name: "Claude backup", Provider: string(taskrunner.ProviderClaudeNative), PermissionMode: "plan", Model: "haiku"})
	if err != nil {
		t.Fatalf("CreateAgentProfile(fallback) error = %v", err)
	}
	p, err := st.CreateAgentProfile(store.AgentProfile{Name: "GLM main", Provider: "glm", OnRateLimit: "fallback", FallbackProfileID: &fb.ID})
	if err != nil {
		t.Fatalf("CreateAgentProfile(main) error = %v", err)
	}
	return p.ID, fb.ID
}

// extraChats returns the task's chats other than its default one.
func extraChats(t *testing.T, e *queuedTestEnv, taskID int64) []store.Chat {
	t.Helper()
	def, err := e.wm.DefaultChat(taskID)
	if err != nil {
		t.Fatal(err)
	}
	all, err := e.wm.ListChats(taskID, true)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Chat
	for _, c := range all {
		if c.ID != def.ID {
			out = append(out, c)
		}
	}
	return out
}

// successorRows polls until a run row with FailoverOf == failedID exists.
func successorRow(t *testing.T, e *queuedTestEnv, failedID string) store.Run {
	t.Helper()
	var got store.Run
	waitUntil(t, "successor run of "+failedID, func() bool {
		for _, s := range e.reg.List(0) {
			if s.FailoverOf == failedID {
				row, err := e.st.GetRun(s.ID)
				if err != nil {
					return false
				}
				got = row
				return true
			}
		}
		return false
	})
	return got
}

func startWithOpts(t *testing.T, e *queuedTestEnv, prompt string, opts ...StartOption) string {
	t.Helper()
	id, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, prompt, taskrunner.PermissionSettings{}, "", opts...)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return id
}

func queuedPrompt(t *testing.T, e *queuedTestEnv, chatID int64) string {
	t.Helper()
	items, err := e.st.ListChatQueue(chatID)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListChatQueue(%d) = %d items, %v; want 1", chatID, len(items), err)
	}
	return items[0].Prompt
}

func TestOnRateLimitFallback_NewChatBoundToFallbackProvider(t *testing.T) {
	t.Parallel()
	e := newFallbackEnv(t, "quota-error")
	pid, fbID := fallbackProfiles(t, e.st)
	failedID := startWithOpts(t, e, "build the thing", WithProfile(pid))
	waitForStatus(t, e.reg, failedID, StatusError, 10*time.Second)

	row := successorRow(t, e, failedID)
	waitForStatus(t, e.reg, row.ID, StatusDone, 10*time.Second)
	chats := extraChats(t, e, e.task.ID)
	if len(chats) != 1 || chats[0].Title != "Continued on Claude backup" {
		t.Fatalf("new chats = %+v, want one titled \"Continued on Claude backup\"", chats)
	}
	chat, err := e.wm.GetChat(chats[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Provider == nil || *chat.Provider != string(taskrunner.ProviderClaudeNative) {
		t.Fatalf("new chat provider = %v, want claude-native", chat.Provider)
	}
	if row.ChatID != chat.ID || row.TaskID != e.task.ID || row.Provider != string(taskrunner.ProviderClaudeNative) {
		t.Fatalf("successor = chat %d task %d provider %s", row.ChatID, row.TaskID, row.Provider)
	}
	if row.ProfileID != fbID || row.FailoverOf != failedID || row.Model != "haiku" || row.PermissionMode != "plan" {
		t.Fatalf("successor row = profile %d failoverOf %q model %q mode %q", row.ProfileID, row.FailoverOf, row.Model, row.PermissionMode)
	}
	// The failed chat stays bound to glm.
	orig, _ := e.wm.DefaultChat(e.task.ID)
	if orig.Provider == nil || *orig.Provider != "glm" {
		t.Fatalf("original chat provider = %v, want glm", orig.Provider)
	}
}

func TestOnRateLimitFallback_PrefaceAndTranscriptTailBounded(t *testing.T) {
	t.Parallel()
	e := newFallbackEnv(t, "")
	pid, _ := fallbackProfiles(t, e.st)
	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid, prompt: "[message from task #3, chat #4]\nbuild it"})
	long := strings.Repeat("日本語", 3000) // 9000 bytes: 4096 cuts mid-rune
	e.reg.record(r, Event{Type: taskrunner.EventTypeText, Text: long})

	e.reg.applyRateLimitPolicy(r, futureFail(time.Hour))

	chats := extraChats(t, e, e.task.ID)
	if len(chats) != 1 {
		t.Fatalf("new chats = %d, want 1", len(chats))
	}
	prompt := queuedPrompt(t, e, chats[0].ID)
	pre := failoverPreface("glm")
	if !strings.HasPrefix(prompt, pre) || !strings.Contains(pre, "stopped at a rate limit") {
		t.Fatalf("prompt = %.200q", prompt)
	}
	body := strings.TrimPrefix(prompt, pre)
	const marker = "\n\n--- tail of the previous attempt's output (read-only context, may be incomplete) ---\n"
	orig, tail, ok := strings.Cut(body, marker)
	if !ok {
		t.Fatalf("no tail marker in prompt")
	}
	if orig != "build it" || strings.Contains(prompt, "[message from") {
		t.Fatalf("original prompt part = %q", orig)
	}
	if len(tail) > 4096 || len(tail) < 4090 || !utf8.ValidString(tail) || !strings.HasSuffix(long, tail) {
		t.Fatalf("tail len %d valid=%v suffix=%v", len(tail), utf8.ValidString(tail), strings.HasSuffix(long, tail))
	}
}

func TestOnRateLimitFallback_OneHopOnly(t *testing.T) {
	t.Parallel()
	e := newFallbackEnv(t, "")
	pid, _ := fallbackProfiles(t, e.st)
	// The failed run is itself a failover successor: nothing more happens.
	r := e.failedRun(t, "successor-1", failedOpts{profileID: pid, failoverOf: "first-run"})
	before := len(e.reg.List(0))
	e.reg.applyRateLimitPolicy(r, futureFail(time.Hour))
	if n := len(extraChats(t, e, e.task.ID)); n != 0 {
		t.Fatalf("new chats = %d, want 0", n)
	}
	if n := len(e.reg.List(0)); n != before {
		t.Fatalf("runs = %d, want %d", n, before)
	}
}

func TestOnRateLimitFallback_NeverForStopOrOtherErrors(t *testing.T) {
	t.Parallel()
	e := newFallbackEnv(t, "plain-error")
	pid, _ := fallbackProfiles(t, e.st)
	plain := startWithOpts(t, e, "work", WithProfile(pid))
	if got := waitForStatus(t, e.reg, plain, StatusError, 10*time.Second); got.FailureKind != "" {
		t.Fatalf("plain error classified as %q", got.FailureKind)
	}

	// A stopped run (a distinct chat so it does not queue behind the error).
	writeScenario(t, e.task, "hang")
	chat, err := e.wm.CreateChat(e.task.ID, "stopme")
	if err != nil {
		t.Fatal(err)
	}
	stopID, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, chat.ID, taskrunner.ProviderGLM, "work", taskrunner.PermissionSettings{}, "", WithProfile(pid))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.reg.Stop(stopID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, stopID, StatusStopped, 10*time.Second)
	e.reg.DeliverQueued()

	if n := len(extraChats(t, e, e.task.ID)); n != 1 { // only "stopme"
		t.Fatalf("chats besides default = %d, want 1 (the one the test made)", n)
	}
	for _, s := range e.reg.List(0) {
		if s.FailoverOf != "" {
			t.Fatalf("unexpected successor run %+v", s)
		}
	}
}

func TestOnRateLimitFallback_FallbackProviderCoolingDownSkips(t *testing.T) {
	t.Parallel()
	e := newFallbackEnv(t, "")
	pid, _ := fallbackProfiles(t, e.st)
	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid})
	if err := e.st.SetProviderCooldown(string(taskrunner.ProviderClaudeNative), time.Now().Add(time.Hour), "failed-1", time.Now()); err != nil {
		t.Fatalf("SetProviderCooldown() error = %v", err)
	}
	e.reg.applyRateLimitPolicy(r, futureFail(time.Hour))
	if n := len(extraChats(t, e, e.task.ID)); n != 0 {
		t.Fatalf("new chats = %d, want 0 while the fallback provider cools down", n)
	}
}

func TestOnRateLimitFallback_FallbackProfileDeletedGivesUp(t *testing.T) {
	t.Parallel()
	e := newFallbackEnv(t, "")
	pid, fbID := fallbackProfiles(t, e.st)
	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid})
	if err := e.st.DeleteAgentProfile(fbID); err != nil {
		t.Fatal(err)
	}
	e.reg.applyRateLimitPolicy(r, futureFail(time.Hour))
	if n := len(extraChats(t, e, e.task.ID)); n != 0 {
		t.Fatalf("new chats = %d, want 0 after the fallback profile was deleted", n)
	}
}

func TestOnRateLimitFallback_TreeFullKeepsItQueuedThenDelivers(t *testing.T) {
	t.Parallel()
	e := newFallbackEnv(t, "")
	pid, _ := fallbackProfiles(t, e.st)
	e.reg.SetMaxConcurrentRunsPerTree(1)
	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid})
	r.treeRoot = e.task.ID // the hand-built run occupies the tree's only slot

	e.reg.applyRateLimitPolicy(r, futureFail(time.Hour))
	chats := extraChats(t, e, e.task.ID)
	if len(chats) != 1 {
		t.Fatalf("new chats = %d, want 1", len(chats))
	}
	items, err := e.st.ListChatQueue(chats[0].ID)
	if err != nil || len(items) != 1 || items[0].Status != store.ChatQueueStatusQueued {
		t.Fatalf("items = %+v, %v; want one queued item", items, err)
	}
	for _, s := range e.reg.List(0) {
		if s.FailoverOf != "" {
			t.Fatalf("successor started while the tree was full: %+v", s)
		}
	}

	// The slot frees when the failed run ends; finish's DeliverQueued retry
	// then starts it.
	r.mu.Lock()
	r.status = StatusError
	r.mu.Unlock()
	e.reg.DeliverQueued()
	row := successorRow(t, e, "failed-1")
	waitForStatus(t, e.reg, row.ID, StatusDone, 10*time.Second)
	pollItem(t, e.st, items[0].ID, store.ChatQueueStatusDelivered, 10*time.Second)
}

func TestFailover_ParentHearsSuccessorOnce(t *testing.T) {
	t.Parallel()
	e := &notifyEnv{newReplyEnv(t, "")}
	withClaude(t, e.queuedTestEnv)
	e.reg.SetNotifyDebounce(0)
	e.waitDone(t, e.startRun(t, "init"))
	pid, _ := fallbackProfiles(t, e.st)
	childChat := e.chatOf(t, e.child.ID)
	r := e.failedRun(t, "child-failed", failedOpts{taskID: e.child.ID, chatID: childChat, profileID: pid, replyChatID: e.parentChat.ID})
	e.finishRateLimited(r, time.Now().Add(time.Hour))

	e.reg.evaluateChildNotifications(e.child.ID)
	if n := len(e.events(t, e.parentChat.ID)); n != 0 {
		t.Fatalf("parent has %d child_event items after the failure, want 0", n)
	}
	row := successorRow(t, e.queuedTestEnv, "child-failed")
	e.waitDone(t, row.ID)
	waitUntil(t, "one child_event after the successor", func() bool { return len(e.events(t, e.parentChat.ID)) == 1 })
	e.reg.evaluateChildNotifications(e.child.ID)
	evs := e.events(t, e.parentChat.ID)
	if len(evs) != 1 || !strings.Contains(evs[0].Prompt, row.ID) || strings.Contains(evs[0].Prompt, "child-failed") {
		t.Fatalf("child events = %+v, want exactly one naming %s", evs, row.ID)
	}
}

func TestFailover_CopiesReplyChatAndCountsInTreeGuard(t *testing.T) {
	t.Parallel()
	e := newFallbackEnv(t, "")
	pid, _ := fallbackProfiles(t, e.st)
	// Hold the successor's run open: the claude fake "hang" scenario.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e.runner = taskrunner.New(e.wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(e.st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}),
		taskrunner.WithClaudeCLIPath(self, "FAKE_CLAUDE_SCENARIO=hang"))
	e.reg.SetStarter(e.wm, e.runner)

	r := e.failedRun(t, "failed-1", failedOpts{profileID: pid, replyChatID: 77})
	e.reg.applyRateLimitPolicy(r, futureFail(time.Hour))
	row := successorRow(t, e, "failed-1")
	t.Cleanup(func() { _ = e.reg.Stop(row.ID) })
	if row.ReplyChatID != 77 {
		t.Fatalf("successor ReplyChatID = %d, want 77", row.ReplyChatID)
	}
	e.reg.mu.Lock()
	root := e.reg.runs[row.ID].treeRoot
	n := e.reg.runningInTreeLocked(root)
	e.reg.mu.Unlock()
	if n != 1 {
		t.Fatalf("runningInTreeLocked(%d) = %d while the successor runs, want 1", root, n)
	}
}
