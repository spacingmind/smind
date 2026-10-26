package runs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/taskrunner/fakeclaude"
)

// unpinnedDefault is the configured default of the TestUnpinnedClaude_* env:
// the haiku the 2026-10-25 live check configured and the run ignored.
const unpinnedDefault = "claude-haiku-5-5"

// unpinnedEnv is a reply env whose claude-native runs go through the fake CLI
// with argv recording (fakeclaude.ArgvLogEnv). defaultModel "" configures no
// providers.claude-native.defaultModel.
type unpinnedEnv struct {
	*replyEnv
	argvLog string
}

func newUnpinnedEnv(t *testing.T, scenario, defaultModel string, extra ...taskrunner.Option) *unpinnedEnv {
	t.Helper()
	e := &unpinnedEnv{replyEnv: newReplyEnv(t, ""), argvLog: filepath.Join(t.TempDir(), "argv.log")}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	opts := []taskrunner.Option{
		taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(e.st)),
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}),
		taskrunner.WithClaudeCLIPath(self, "FAKE_CLAUDE_SCENARIO="+scenario, fakeclaude.ArgvLogEnv+"="+e.argvLog),
	}
	if defaultModel != "" {
		opts = append(opts, taskrunner.WithDefaultModel(taskrunner.ProviderClaudeNative, defaultModel))
	}
	e.runner = taskrunner.New(e.wm, append(opts, extra...)...)
	e.reg.SetStarter(e.wm, e.runner)
	return e
}

// launches is the --model value of every CLI spawn so far ("<none>" for a
// spawn with no --model flag at all: the unpinned launch this suite forbids).
func (e *unpinnedEnv) launches(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(e.argvLog)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		args := strings.Split(line, fakeclaude.ArgvSep)
		if len(args) < 2 || args[0] != "--output-format" {
			continue // the SDK's `claude -v` version probe, not a run
		}
		model := "<none>"
		for i, a := range args {
			if a == "--model" && i+1 < len(args) {
				model = args[i+1]
			}
		}
		out = append(out, model)
	}
	return out
}

// wantLaunches asserts the full list of spawns' --model values.
func (e *unpinnedEnv) wantLaunches(t *testing.T, want ...string) {
	t.Helper()
	got := e.launches(t)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("CLI --model per launch = %v, want %v", got, want)
	}
}

// send is a StartWhenBusy of the claude-native provider on taskID.
func (e *unpinnedEnv) send(t *testing.T, taskID int64, source string, wait bool, opts ...StartOption) (StartOutcome, error) {
	t.Helper()
	from := int64(0)
	fromChat := int64(0)
	if source == store.ChatQueueSourceAgent {
		from, fromChat = e.task.ID, e.parentChat.ID
	}
	out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, taskID, 0, taskrunner.ProviderClaudeNative, "x", taskrunner.PermissionSettings{}, "", WhenBusyReject, source, from, fromChat, opts...)
	if err == nil && wait && !out.Queued {
		waitForStatus(t, e.reg, out.RunID, StatusDone, 10*time.Second)
	}
	return out, err
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_CLISend(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "reply", unpinnedDefault)
	// `smind task send <t> claude-native "x"`: run.start with no model key.
	if _, err := e.send(t, e.task.ID, store.ChatQueueSourceHuman, true); err != nil {
		t.Fatal(err)
	}
	e.wantLaunches(t, unpinnedDefault)
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_WebRunStart(t *testing.T) {
	t.Parallel()
	// The web client sends no model key, "" or "default" (run-start.ts). The
	// first two are the configured default; "default" is the human's explicit
	// "the CLI's own" and the only unpinned launch left.
	for _, tc := range []struct{ name, model, want string }{
		{"no-key", "", unpinnedDefault},
		{"empty", "", unpinnedDefault},
		{"default-keyword", taskrunner.ClaudeModelDefault, "<none>"},
		{"explicit", "sonnet", "sonnet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newUnpinnedEnv(t, "reply", unpinnedDefault)
			if _, err := e.send(t, e.task.ID, store.ChatQueueSourceHuman, true, WithModel(tc.model)); err != nil {
				t.Fatal(err)
			}
			e.wantLaunches(t, tc.want)
		})
	}
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_MCPSend(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "reply", unpinnedDefault)
	// task_send from a seat: source agent with its own task/chat ids.
	if _, err := e.send(t, e.child.ID, store.ChatQueueSourceAgent, true); err != nil {
		t.Fatal(err)
	}
	e.wantLaunches(t, unpinnedDefault)
	// task_send from an orchestrator (no seat identity).
	if _, err := e.send(t, e.sibling.ID, store.ChatQueueSourceOrchestrator, true); err != nil {
		t.Fatal(err)
	}
	e.wantLaunches(t, unpinnedDefault, unpinnedDefault)
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_StickySeatWake(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "reply", unpinnedDefault)
	e.reg.SetNotifyDebounce(0)
	prof, err := e.st.CreateAgentProfile(store.AgentProfile{Name: "Seat", Provider: string(taskrunner.ProviderClaudeNative)})
	if err != nil {
		t.Fatal(err)
	}
	// The parent seat's last run, as the buggy daemon recorded an unpinned one:
	// claude-native, profile Seat, model "".
	fin := time.Now()
	if _, err := e.st.CreateRun(store.Run{ID: "seat-1", TaskID: e.task.ID, ChatID: e.parentChat.ID, Provider: string(taskrunner.ProviderClaudeNative), Prompt: "p",
		Status: string(StatusDone), StartedAt: fin.Add(-time.Second), FinishedAt: &fin, ProfileID: prof.ID}); err != nil {
		t.Fatal(err)
	}
	// A child finishes: the daemon wakes the parent seat with a child event
	// copying the parent's last run's settings (profile included).
	out, err := e.send(t, e.child.ID, store.ChatQueueSourceAgent, false, WithModel("haiku"))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 10*time.Second)
	e.reg.evaluateChildNotifications(e.child.ID)
	waitUntil(t, "the woken seat's run", func() bool { return len(e.launches(t)) == 2 })
	e.wantLaunches(t, "haiku", unpinnedDefault)

	// A sticky re-send on the seat's chat (no profile key: inherited) takes
	// the profile's own model before the default.
	if _, err := e.st.UpdateAgentProfile(store.AgentProfile{ID: prof.ID, Name: "Seat", Provider: string(taskrunner.ProviderClaudeNative), Model: "sonnet"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "idle parent chat", func() bool { _, busy := e.reg.runningRunForChat(e.parentChat.ID); return !busy })
	// the woken run is recorded under the seat's profile (inherited)
	if _, err := e.send(t, e.task.ID, store.ChatQueueSourceHuman, true); err != nil {
		t.Fatal(err)
	}
	got := e.launches(t)
	if got[len(got)-1] != "sonnet" {
		t.Fatalf("sticky re-send --model = %q, want the profile's sonnet (launches %v)", got[len(got)-1], got)
	}
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_QueueItem(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "reply", unpinnedDefault)
	// A queued prompt stores no model (an old item, or a client that named
	// none); the queue worker's delivery resolves the default.
	cfg, _ := json.Marshal(chatQueueRunConfig{Provider: string(taskrunner.ProviderClaudeNative)})
	chat, _ := e.wm.DefaultChat(e.task.ID)
	item, err := e.st.EnqueueChatPrompt(chat.ID, "later", string(cfg), store.ChatQueueSourceHuman, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	e.reg.deliverOne(chat.ID)
	e.waitForQueuedStatus(t, item.ID, store.ChatQueueStatusDelivered)
	waitUntil(t, "delivered run", func() bool { return len(e.launches(t)) == 1 })
	e.wantLaunches(t, unpinnedDefault)
}

// A prompt queued onto a busy chat stores the resolved model, so what the
// queue shows is what will launch.
func TestUnpinnedClaude_QueuedPromptStoresResolvedModel(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "hang", unpinnedDefault)
	if _, err := e.send(t, e.task.ID, store.ChatQueueSourceHuman, false, WithModel("haiku")); err != nil {
		t.Fatal(err)
	}
	q, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderClaudeNative, "queued", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0)
	if err != nil || !q.Queued {
		t.Fatalf("queue send = %+v, %v", q, err)
	}
	it, err := e.st.GetChatQueueItem(q.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	var stored chatQueueRunConfig
	_ = json.Unmarshal([]byte(it.RunConfig), &stored)
	if stored.Model != unpinnedDefault {
		t.Fatalf("queued item model = %q, want %q", stored.Model, unpinnedDefault)
	}
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_Continue(t *testing.T) {
	t.Parallel()
	// Continue / resubmit / a CLI follow-up are plain sends on the same chat.
	e := newUnpinnedEnv(t, "reply", unpinnedDefault)
	for i := 0; i < 2; i++ {
		if _, err := e.send(t, e.task.ID, store.ChatQueueSourceHuman, true); err != nil {
			t.Fatal(err)
		}
	}
	e.wantLaunches(t, unpinnedDefault, unpinnedDefault)
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_ResumedSession(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "reply", unpinnedDefault)
	if _, err := e.send(t, e.task.ID, store.ChatQueueSourceHuman, true, WithModel("sonnet")); err != nil {
		t.Fatal(err)
	}
	// The follow-up names no model: the resumed CLI still gets --model.
	if _, err := e.send(t, e.task.ID, store.ChatQueueSourceHuman, true); err != nil {
		t.Fatal(err)
	}
	e.wantLaunches(t, "sonnet", unpinnedDefault)
	data, _ := os.ReadFile(e.argvLog)
	if !strings.Contains(string(data), "--resume") {
		t.Fatalf("no launch resumed the session:\n%s", strings.ReplaceAll(string(data), fakeclaude.ArgvSep, " "))
	}
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_Failover(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "reply", unpinnedDefault)
	fb, err := e.st.CreateAgentProfile(store.AgentProfile{Name: "Claude backup", Provider: string(taskrunner.ProviderClaudeNative), PermissionMode: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	main, err := e.st.CreateAgentProfile(store.AgentProfile{Name: "GLM main", Provider: "glm", OnRateLimit: "fallback", FallbackProfileID: &fb.ID})
	if err != nil {
		t.Fatal(err)
	}
	// glm hits its quota; the fallback profile names no model.
	writeScenario(t, e.task, "quota-error")
	failed, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderGLM, "build", taskrunner.PermissionSettings{}, "", WithProfile(main.ID))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, failed, StatusError, 10*time.Second)
	// The registry lists the successor before its row is guaranteed to be in
	// the store (a slow disk: Windows CI), so take the id from the registry
	// rather than reading the row back and ignoring the error.
	var succID string
	waitUntil(t, "successor", func() bool {
		for _, s := range e.reg.List(0) {
			if s.FailoverOf == failed {
				succID = s.ID
				return true
			}
		}
		return false
	})
	waitForStatus(t, e.reg, succID, StatusDone, 10*time.Second)
	e.wantLaunches(t, unpinnedDefault)
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_WaitRequeue(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "session-limit", unpinnedDefault)
	prof, err := e.st.CreateAgentProfile(store.AgentProfile{Name: "waiter", Provider: string(taskrunner.ProviderClaudeNative), OnRateLimit: "wait"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.send(t, e.task.ID, store.ChatQueueSourceHuman, false, WithProfile(prof.ID))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusError, 10*time.Second)
	chat, _ := e.wm.DefaultChat(e.task.ID)
	var stored chatQueueRunConfig
	waitUntil(t, "the held retry", func() bool {
		items, _ := e.st.ListChatQueue(chat.ID)
		if len(items) != 1 {
			return false
		}
		_ = json.Unmarshal([]byte(items[0].RunConfig), &stored)
		return true
	})
	if stored.Model != unpinnedDefault {
		t.Fatalf("held retry model = %q, want %q (it launches with exactly this)", stored.Model, unpinnedDefault)
	}
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_Instance(t *testing.T) {
	t.Parallel()
	reg := providers.New()
	for _, id := range []string{"claude-own", "claude-inherit"} {
		if err := reg.Register(providers.Spec{ID: id, Label: id, Extends: providers.IDClaudeNative}); err != nil {
			t.Fatal(err)
		}
	}
	e := newUnpinnedEnv(t, "reply", unpinnedDefault, taskrunner.WithProviderRegistry(reg), taskrunner.WithDefaultModel("claude-own", "claude-sonnet-5-5"))
	for _, id := range []string{"claude-own", "claude-inherit"} {
		chat, err := e.wm.CreateChat(e.task.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		out, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, chat.ID, taskrunner.Provider(id), "x", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		waitForStatus(t, e.reg, out.RunID, StatusDone, 10*time.Second)
	}
	// its own default, else the base provider's
	e.wantLaunches(t, "claude-sonnet-5-5", unpinnedDefault)
}

func TestUnpinnedClaude_AlwaysGetsExplicitModel_SystemRuns(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "reply", "")
	if err := os.RemoveAll(filepath.Join(*e.task.WorktreePath, ".git")); err != nil {
		t.Fatal(err)
	}
	// Learning / reflector runs pin their own model.
	id, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderClaudeNative, "reflect", taskrunner.PermissionSettings{}, "",
		WithPurpose("reflect"), WithIsolation(testIso), WithModel(unpinnedDefault), WithOrigin(""))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, e.reg, id, StatusDone, 10*time.Second)
	e.wantLaunches(t, unpinnedDefault)
	// One that names none and has no default is refused, never launched.
	_, err = e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderClaudeNative, "reflect", taskrunner.PermissionSettings{}, "",
		WithPurpose("reflect"), WithIsolation(testIso), WithOrigin(""))
	var nm *taskrunner.NoModelError
	if !errors.As(err, &nm) {
		t.Fatalf("unpinned system run error = %v, want NoModelError", err)
	}
	e.wantLaunches(t, unpinnedDefault)
}

func TestUnpinnedClaude_NoModelResolved_RefusedWithTypedError(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "hang", "")
	wantRefusal := func(t *testing.T, where string, err error) {
		t.Helper()
		var nm *taskrunner.NoModelError
		if !errors.As(err, &nm) || nm.Provider != taskrunner.ProviderClaudeNative {
			t.Fatalf("%s: error = %v, want *NoModelError for claude-native", where, err)
		}
		if !strings.Contains(err.Error(), "no_model") || !strings.Contains(err.Error(), "providers.claude-native.defaultModel") {
			t.Fatalf("%s: message %q lacks the code or the remedy", where, err.Error())
		}
	}
	// CLI / web (human), MCP (agent, orchestrator), queue (busy), wake.
	_, err := e.send(t, e.task.ID, store.ChatQueueSourceHuman, false)
	wantRefusal(t, "human send", err)
	_, err = e.send(t, e.child.ID, store.ChatQueueSourceAgent, false)
	wantRefusal(t, "agent send", err)
	_, err = e.send(t, e.sibling.ID, store.ChatQueueSourceOrchestrator, false)
	wantRefusal(t, "orchestrator send", err)
	_, err = e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderClaudeNative, "x", taskrunner.PermissionSettings{}, "", WhenBusyQueue, store.ChatQueueSourceHuman, 0, 0)
	wantRefusal(t, "queue send", err)
	_, err = e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderClaudeNative, "x", taskrunner.PermissionSettings{}, "")
	wantRefusal(t, "Start", err)
	if got := e.launches(t); len(got) != 0 {
		t.Fatalf("a refused send still launched the CLI: %v", got)
	}
	if rows, _ := e.st.ListRecentRuns(10); len(rows) != 0 {
		t.Fatalf("refused sends left run rows: %+v", rows)
	}

	// A queue item (delivered later) or a wake that resolves nothing is
	// cancelled with the reason, not launched unpinned.
	chat, _ := e.wm.DefaultChat(e.task.ID)
	cfg, _ := json.Marshal(chatQueueRunConfig{Provider: string(taskrunner.ProviderClaudeNative)})
	item, err := e.st.EnqueueChatPrompt(chat.ID, "later", string(cfg), store.ChatQueueSourceHuman, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	e.reg.deliverOne(chat.ID)
	got := e.waitForQueuedStatus(t, item.ID, store.ChatQueueStatusCancelled)
	if !strings.Contains(got.CancelReason, "no_model") {
		t.Fatalf("cancel reason = %v, want the no_model refusal", got.CancelReason)
	}
	if got := e.launches(t); len(got) != 0 {
		t.Fatalf("an undeliverable item launched the CLI: %v", got)
	}
}

func TestDefaultKeyword_HumanOnly(t *testing.T) {
	t.Parallel()
	e := newUnpinnedEnv(t, "reply", unpinnedDefault)
	// A human may knowingly keep the CLI's own default.
	if _, err := e.send(t, e.task.ID, store.ChatQueueSourceHuman, true, WithModel(taskrunner.ClaudeModelDefault)); err != nil {
		t.Fatalf("human default: %v", err)
	}
	e.wantLaunches(t, "<none>")
	// An agent or an orchestrator gets a refusal and nothing launches.
	for _, src := range []string{store.ChatQueueSourceAgent, store.ChatQueueSourceOrchestrator} {
		task := e.child
		if src == store.ChatQueueSourceOrchestrator {
			task = e.sibling
		}
		_, err := e.send(t, task.ID, src, false, WithModel(taskrunner.ClaudeModelDefault))
		if err == nil || !strings.Contains(err.Error(), `"default"`) {
			t.Fatalf("%s send of model=default: error = %v, want a refusal naming it", src, err)
		}
	}
	e.wantLaunches(t, "<none>")
}
