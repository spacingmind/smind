package runs

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// withCodex rebuilds e's runner with a discovering fake Codex (gpt-fake-a:
// low/medium/high, gpt-fake-b: low/medium) next to the fake claude CLI.
func withCodex(t *testing.T, e *queuedTestEnv) *queuedTestEnv {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e.runner = taskrunner.New(e.wm,
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(e.st)),
		taskrunner.WithACPModeProbe(),
		taskrunner.WithCodexCommand([]string{fakeCodexAgentPath}),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}),
		taskrunner.WithDefaultModel(taskrunner.ProviderClaudeNative, "claude-haiku-5-5"),
		taskrunner.WithClaudeCLIPath(self, "FAKE_CLAUDE_SCENARIO=reply"))
	e.reg.SetStarter(e.wm, e.runner)
	waitUntil(t, "codex models discovered", func() bool {
		p, _ := e.runner.ProviderCatalogFor(taskrunner.ProviderCodexNative)
		return p.ModelsDiscovered
	})
	return e
}

func hasLevelNote(t *testing.T, e *queuedTestEnv, runID string) bool {
	t.Helper()
	hist, _, err := e.reg.History(runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range hist {
		if ev.Type == taskrunner.EventTypeSessionNote && strings.Contains(ev.Text, "reasoning level") && strings.Contains(ev.Text, "using the provider default") {
			return true
		}
	}
	return false
}

// deliverQueued enqueues cfg on a fresh chat bound to provider, delivers it
// and returns the started run's id once it is done.
func deliverQueued(t *testing.T, e *queuedTestEnv, provider, cfg string) string {
	t.Helper()
	chat, err := e.wm.CreateChat(e.task.ID, "queued")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.wm.BindChatProvider(chat.ID, provider); err != nil {
		t.Fatal(err)
	}
	item, err := e.st.EnqueueChatPrompt(chat.ID, "go", cfg, store.ChatQueueSourceHuman, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	e.reg.deliverOne(chat.ID)
	e.waitForQueuedStatus(t, item.ID, store.ChatQueueStatusDelivered)
	for _, s := range e.reg.List(0) {
		if s.ChatID == chat.ID {
			waitForStatus(t, e.reg, s.ID, StatusDone, 15*time.Second)
			return s.ID
		}
	}
	t.Fatal("no run for the delivered item")
	return ""
}

// A queued item keeps the level it was enqueued with; if the task's model
// changed meanwhile, the level is dropped with a note and the run still runs.
func TestQueuedEffortDroppedWhenModelChanges(t *testing.T) {
	t.Parallel()
	e := withCodex(t, newQueuedTestEnv(t, "reply"))
	// "medium" is listed by gpt-fake-a only through the default model; gpt-fake-b lists it too,
	// so use "high", which gpt-fake-b does not.
	id := deliverQueued(t, e, "codex-native", `{"provider":"codex-native","model":"gpt-fake-b","thinkingLevel":"high"}`)
	if !hasLevelNote(t, e, id) {
		t.Fatal("no drop note for a level the model does not list")
	}
	ok := deliverQueued(t, e, "codex-native", `{"provider":"codex-native","model":"gpt-fake-a","thinkingLevel":"high"}`)
	if hasLevelNote(t, e, ok) {
		t.Fatal("a supported level was dropped")
	}
}

func TestExplicitEffortRejectedWhenModelChanges(t *testing.T) {
	t.Parallel()
	e := withCodex(t, newQueuedTestEnv(t, "reply"))
	_, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderCodexNative, "x",
		taskrunner.PermissionSettings{}, "high", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0, WithModel("gpt-fake-b"))
	if err == nil || !strings.Contains(err.Error(), "supported: low, medium") {
		t.Fatalf("explicit unsupported level err = %v, want a rejection naming the supported levels", err)
	}
	// "off" is not translated to Codex's none.
	_, err = e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderCodexNative, "x",
		taskrunner.PermissionSettings{}, "off", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0, WithModel("gpt-fake-a"))
	if err == nil || !strings.Contains(err.Error(), "supported: low, medium, high") {
		t.Fatalf("explicit off err = %v, want a rejection naming the supported levels", err)
	}
}

// Old rows carrying Claude's words on a codex task can never brick a run.
func TestOldStandardLevelOnCodexRowDroppedWithNote(t *testing.T) {
	t.Parallel()
	e := withCodex(t, newQueuedTestEnv(t, "reply"))
	for _, lvl := range []string{"standard", "extended", "off"} {
		id := deliverQueued(t, e, "codex-native", `{"provider":"codex-native","model":"gpt-fake-a","thinkingLevel":"`+lvl+`"}`)
		if !hasLevelNote(t, e, id) {
			t.Fatalf("%s: no drop note", lvl)
		}
	}
}

// A failed codex run's effort id must not ride into the Claude fallback run.
func TestFailoverCodexToClaudeDropsEffort(t *testing.T) {
	t.Parallel()
	e := withCodex(t, newQueuedTestEnv(t, "reply"))
	id := deliverQueued(t, e, "claude-native", `{"provider":"claude-native","model":"haiku","thinkingLevel":"xhigh"}`)
	if !hasLevelNote(t, e, id) {
		t.Fatal("codex effort on a claude run was not dropped with a note")
	}
}

// A Claude level must not reach Codex.
func TestFailoverClaudeToCodexDropsThinking(t *testing.T) {
	t.Parallel()
	e := withCodex(t, newQueuedTestEnv(t, "reply"))
	id := deliverQueued(t, e, "codex-native", `{"provider":"codex-native","thinkingLevel":"extended"}`)
	if !hasLevelNote(t, e, id) {
		t.Fatal("claude level on a codex run was not dropped with a note")
	}
}

// The successor profile applies its own pinned level; the failed run's level
// is never copied across providers.
func TestFailoverSuccessorProfileUsesOwnPinnedLevel(t *testing.T) {
	t.Parallel()
	e := withCodex(t, newFallbackEnv(t, "quota-error"))
	fb, err := e.st.CreateAgentProfile(store.AgentProfile{Name: "Claude pinned", Provider: string(taskrunner.ProviderClaudeNative), PermissionMode: "plan", Model: "haiku", ThinkingLevel: "extended"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.st.CreateAgentProfile(store.AgentProfile{Name: "GLM main", Provider: "glm", OnRateLimit: "fallback", FallbackProfileID: &fb.ID, ThinkingLevel: "high"})
	if err != nil {
		t.Fatal(err)
	}
	failedID, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "build", taskrunner.PermissionSettings{}, "high", WithProfile(p.ID), WithInheritedLevel())
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, failedID, StatusError, 10*time.Second)
	row := successorRow(t, e, failedID)
	waitForStatus(t, e.reg, row.ID, StatusDone, 10*time.Second)
	if hasLevelNote(t, e, row.ID) {
		t.Fatal("the successor's own valid pin was dropped")
	}
}
