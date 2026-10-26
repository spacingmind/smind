package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// Tests for plugin hooks and task.send (ADR-0042 PB.3). Every plugin is a real
// child process (hosttest), the daemon is a real API, and the agent is the fake
// ACP agent, which dumps what the launch gave it.

const hookCaps = `["hooks.beforeRun","tasks.read","tasks.write"]`

// hookEnv is a procEnv whose runner talks to the fake ACP agent and whose
// agent dumps its environment and MCP servers.
type hookEnv struct {
	*procEnv
	envDump, mcpDump string
}

func newHookEnv(t *testing.T, ps ...procPlugin) *hookEnv {
	t.Helper()
	t.Setenv("FAKEAGENT_MCP_CAPS", "stdio")
	dir := t.TempDir()
	h := &hookEnv{envDump: filepath.Join(dir, "env.txt"), mcpDump: filepath.Join(dir, "mcp.json")}
	t.Setenv("FAKEAGENT_ENV_DUMP", h.envDump)
	t.Setenv("FAKEAGENT_MCP_DUMP", h.mcpDump)
	h.procEnv = newProcEnvOpts(t, []taskrunner.Option{taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath})}, ps...)
	if err := os.WriteFile(filepath.Join(*h.task.WorktreePath, "record-prompt-blocks"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return h
}

// hookPlugin is a plugin declaring run.beforeStart that answers result.
func hookPlugin(id, result string, hooks string) procPlugin {
	if hooks == "" {
		hooks = `[{"event":"run.beforeStart"}]`
	}
	return procPlugin{id: id, caps: hookCaps, mode: "hook", args: []string{result},
		contributes: `{"hooks":` + hooks + `,"mcpServers":[{"name":"files","transport":"stdio","command":["go","version"]}]}`}
}

func (e *procEnv) startRun(t *testing.T, taskID int64, prompt string) string {
	t.Helper()
	out, err := e.api.Runs.StartWhenBusy(context.Background(), e.wm, e.runner, taskID, 0, taskrunner.ProviderGLM, prompt,
		taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified, runs.WhenBusyReject, store.ChatQueueSourceHuman, 0, 0)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	return out.RunID
}

func (e *procEnv) waitRun(t *testing.T, runID string) runs.RunStatus {
	t.Helper()
	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		if _, st, err := e.api.Runs.History(runID); err == nil && st.Status != runs.StatusRunning {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s never finished", runID)
	return runs.RunStatus{}
}

func (e *procEnv) notes(t *testing.T, runID string) string {
	t.Helper()
	evs, _, err := e.api.Runs.History(runID)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, ev := range evs {
		if ev.Type == taskrunner.EventTypeSessionNote {
			b.WriteString(ev.Text + "\n")
		}
	}
	return b.String()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func (e *procEnv) promptBlocks(t *testing.T) string {
	return readFile(t, filepath.Join(*e.task.WorktreePath, "prompt-blocks"))
}

func (e *procEnv) beforeStart(t *testing.T) (plugins.BeforeStartResult, error) {
	t.Helper()
	return e.api.plugins.RunBeforeStart(context.Background(), plugins.BeforeStartInput{RunID: "r1", TaskID: e.task.ID, ChatID: 1, Provider: "glm", Origin: "human"})
}

func lastErrorOf(t *testing.T, e *procEnv, id string) string {
	t.Helper()
	in, err := e.api.plugins.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return in.LastError
}

func TestHook_BeforeStart_AppendsInstructions_LabeledByPlugin(t *testing.T) {
	e := newHookEnv(t, hookPlugin("acme", `{"additionalContext":"always use tabs"}`, ""))
	id := e.startRun(t, e.task.ID, "hello there")
	st := e.waitRun(t, id)
	if st.Status != runs.StatusDone {
		t.Fatalf("run = %+v", st)
	}
	blocks := e.promptBlocks(t)
	for _, want := range []string{"hello there", "[plugin acme]", "always use tabs", "[end plugin acme]"} {
		if !strings.Contains(blocks, want) {
			t.Fatalf("the agent's prompt lacks %q: %s", want, blocks)
		}
	}
	if strings.Index(blocks, "hello there") > strings.Index(blocks, "[plugin acme]") {
		t.Fatalf("instructions are APPENDED, after the sender's text: %s", blocks)
	}
	if st.Prompt != "hello there" {
		t.Fatalf("the stored run prompt must stay the sender's text, got %q", st.Prompt)
	}
	note := e.notes(t, id)
	if !strings.Contains(note, "plugin acme run.beforeStart added") || strings.Contains(note, "always use tabs") {
		t.Fatalf("the timeline says what was added, never the text: %q", note)
	}
}

func TestHook_BeforeStart_AddsEnv_RefusesReservedAndOverrides(t *testing.T) {
	t.Setenv("PRESET_VAR", "from-the-daemon")
	e := newHookEnv(t, hookPlugin("acme", `{"env":{"ACME_FLAG":"on","PRESET_VAR":"from-the-plugin"}}`, ""))
	st := e.waitRun(t, e.startRun(t, e.task.ID, "hi"))
	if st.Status != runs.StatusDone {
		t.Fatalf("run = %+v", st)
	}
	env := readFile(t, e.envDump)
	if !strings.Contains(env, "ACME_FLAG=on") {
		t.Fatalf("the hook's env must reach the agent: %s", env)
	}
	if !strings.Contains(env, "PRESET_VAR=from-the-daemon") || strings.Contains(env, "from-the-plugin") {
		t.Fatalf("a key that already exists is never overridden: %s", env)
	}
	if !strings.Contains(e.notes(t, st.ID), "PRESET_VAR") {
		t.Fatalf("the skipped key is named on the timeline: %q", e.notes(t, st.ID))
	}

	// The host refuses reserved names, loader variables, credentials, bad shapes.
	for _, key := range []string{"LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "PATH", "HOME", "SMIND_HOME", "SMIND_PORT", "ANTHROPIC_API_KEY", "CLAUDE_CONFIG_DIR",
		"OPENAI_BASE_URL", "GIT_SSH_COMMAND", "HTTPS_PROXY", "NODE_OPTIONS", "BASH_ENV", "SOME_TOKEN", "my_var", "1BAD", "WITH-DASH", ""} {
		r := newHookEnv(t, hookPlugin("evil", `{"env":{"`+key+`":"x"}}`, ""))
		res, err := r.beforeStart(t)
		var he *plugins.HookError
		if err != nil || len(res.Failures) != 1 {
			t.Fatalf("env %q: err %v failures %+v", key, err, res.Failures)
		}
		he = res.Failures[0]
		if he.Code != plugins.HookErrEnv || he.Plugin != "evil" || !res.Empty() {
			t.Fatalf("env %q must fail the hook with %s and apply nothing, got %+v %+v", key, plugins.HookErrEnv, he, res)
		}
	}
}

func TestHook_BeforeStart_AddsOwnMcp_RefusesOthers(t *testing.T) {
	e := newHookEnv(t, hookPlugin("acme", `{"mcpServers":["files","acme__files"]}`, ""))
	res, err := e.beforeStart(t)
	if err != nil || len(res.Failures) != 0 || len(res.Mcp) != 1 || res.Mcp[0].Name != "acme__files" || res.Mcp[0].OwnerPlugin != "acme" {
		t.Fatalf("its own server, once: %+v err %v", res, err)
	}
	// End to end: the run carries it exactly once.
	st := e.waitRun(t, e.startRun(t, e.task.ID, "hi"))
	if st.Status != runs.StatusDone || strings.Count(readFile(t, e.mcpDump), `"name":"acme__files"`) != 1 {
		t.Fatalf("run %+v mcp %s", st, readFile(t, e.mcpDump))
	}

	for _, name := range []string{"acme__files", "acme:files", "files", "smind", "nope", ""} {
		other := newHookEnv(t, hookPlugin("acme", `{}`, ""))
		other.install(t, procPlugin{id: "zed", caps: hookCaps, mode: "hook", args: []string{`{"mcpServers":["` + name + `"]}`},
			contributes: `{"hooks":[{"event":"run.beforeStart"}],"mcpServers":[{"name":"mine","transport":"stdio","command":["go","version"]}]}`})
		res, err := other.beforeStart(t)
		if err != nil {
			t.Fatal(err)
		}
		var zed *plugins.HookError
		for _, f := range res.Failures {
			if f.Plugin == "zed" {
				zed = f
			}
		}
		if zed == nil || zed.Code != plugins.HookErrMcp {
			t.Fatalf("zed naming %q must be refused (%s), got failures %+v", name, plugins.HookErrMcp, res.Failures)
		}
		if len(res.Mcp) != 0 {
			t.Fatalf("another plugin's server leaked through: %+v", res.Mcp)
		}
	}
}

func TestHook_BeforeStart_ChangeProviderCwdModelProfileModeAutoAccept_TypedError(t *testing.T) {
	forbidden := []string{`"provider":"claude-native"`, `"cwd":"/etc"`, `"model":"claude-opus-5-5"`, `"profile":"boss"`, `"profileId":3`,
		`"permissionMode":"bypassPermissions"`, `"autoAccept":true`, `"mode":"auto"`, `"approvalPolicy":"never"`, `"tools":["Bash"]`, `"prompt":"rewritten"`}
	for _, field := range forbidden {
		e := newHookEnv(t, hookPlugin("acme", `{"additionalContext":"x",`+field+`}`, ""))
		res, err := e.beforeStart(t)
		if err != nil || len(res.Failures) != 1 {
			t.Fatalf("%s: err %v failures %+v", field, err, res.Failures)
		}
		if f := res.Failures[0]; f.Code != plugins.HookErrForbiddenEdit || f.Plugin != "acme" || f.Event != plugins.HookRunBeforeStart || f.Closed {
			t.Fatalf("%s: want a typed %s failure of an open hook, got %+v", field, plugins.HookErrForbiddenEdit, f)
		}
		if !res.Empty() {
			t.Fatalf("%s: nothing of a failed answer is applied: %+v", field, res)
		}
		// open: the run starts, nothing was applied, the Human sees why.
		st := e.waitRun(t, e.startRun(t, e.task.ID, "plain"))
		if st.Status != runs.StatusDone || strings.Contains(e.promptBlocks(t), "[plugin acme]") {
			t.Fatalf("%s: run %+v blocks %s", field, st, e.promptBlocks(t))
		}
		if le := lastErrorOf(t, e.procEnv, "acme"); !strings.Contains(le, plugins.HookErrForbiddenEdit) {
			t.Fatalf("%s: the failure is visible as lastError, got %q", field, le)
		}
		if !strings.Contains(e.notes(t, st.ID), "forbidden_edit") {
			t.Fatalf("%s: the run's timeline carries it: %q", field, e.notes(t, st.ID))
		}
	}
	// A field the host does not know is refused too.
	e := newHookEnv(t, hookPlugin("acme", `{"surprise":1}`, ""))
	if res, _ := e.beforeStart(t); len(res.Failures) != 1 || res.Failures[0].Code != plugins.HookErrUnknownField {
		t.Fatalf("unknown field: %+v", res.Failures)
	}
}

func TestHook_Timeout_DefaultIsTenSeconds(t *testing.T) {
	if plugins.HookTimeout != 10*time.Second {
		t.Fatalf("HookTimeout = %s", plugins.HookTimeout)
	}
}

func TestHook_BeforeStart_Timeout10s_OnErrorOpenAndClosed(t *testing.T) {
	old := plugins.HookTimeout
	plugins.HookTimeout = 300 * time.Millisecond
	t.Cleanup(func() { plugins.HookTimeout = old })

	slow := func(onError string) procPlugin {
		p := hookPlugin("slow", "", `[{"event":"run.beforeStart","onError":"`+onError+`"}]`)
		p.mode, p.args = "hook-sleep", nil
		return p
	}
	// open: the run goes ahead after the timeout and says so.
	e := newHookEnv(t, slow("open"))
	start := time.Now()
	st := e.waitRun(t, e.startRun(t, e.task.ID, "hi"))
	if st.Status != runs.StatusDone {
		t.Fatalf("an open hook that times out must not stop the run: %+v", st)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("the run waited %s for a hook with a %s timeout", d, plugins.HookTimeout)
	}
	if le := lastErrorOf(t, e.procEnv, "slow"); !strings.Contains(le, "timeout") {
		t.Fatalf("lastError = %q", le)
	}
	if !strings.Contains(e.notes(t, st.ID), "timeout") {
		t.Fatalf("notes = %q", e.notes(t, st.ID))
	}

	// closed: the start fails with the plugin id and the reason.
	c := newHookEnv(t, slow("closed"))
	st = c.waitRun(t, c.startRun(t, c.task.ID, "hi"))
	if st.Status != runs.StatusError || !strings.Contains(st.Err, "slow") || !strings.Contains(st.Err, "timeout") {
		t.Fatalf("a closed hook that times out fails the start with the plugin and reason: %+v", st)
	}
	if strings.Contains(c.promptBlocks2(t), "hi") {
		t.Fatalf("nothing may be spawned for a failed closed hook")
	}

	// A plugin that answers with an error: open continues, closed stops.
	for _, onError := range []string{"open", "closed"} {
		p := hookPlugin("grumpy", "", `[{"event":"run.beforeStart","onError":"`+onError+`"}]`)
		p.mode, p.args = "hook-error", nil
		g := newHookEnv(t, p)
		st := g.waitRun(t, g.startRun(t, g.task.ID, "hi"))
		want := runs.StatusDone
		if onError == "closed" {
			want = runs.StatusError
		}
		if st.Status != want {
			t.Fatalf("onError %s with a hook error: %+v", onError, st)
		}
		if strings.Contains(g.notes(t, st.ID)+st.Err, "abcdefghijklmnopqrstuvwxyz0123456789") {
			t.Fatalf("the plugin's error text must be redacted: %q %q", g.notes(t, st.ID), st.Err)
		}
	}
}

// promptBlocks2 is the prompt-blocks file, "" when the agent never ran.
func (e *procEnv) promptBlocks2(t *testing.T) string {
	b, _ := os.ReadFile(filepath.Join(*e.task.WorktreePath, "prompt-blocks"))
	return string(b)
}

func TestHook_Ordering_ByPluginIdThenRegistration_NoDeepMerge(t *testing.T) {
	two := `[{"event":"run.beforeStart"},{"event":"run.beforeStart"}]`
	// bbb is installed FIRST: order is by id, not by installation.
	e := newHookEnv(t,
		hookPlugin("bbb", `{"additionalContext":"from bbb","env":{"K":"bbb","L":"bbb"}}`, ""),
		hookPlugin("aaa", `{"additionalContext":"from aaa","env":{"K":"aaa"}}`, two))
	res, err := e.beforeStart(t)
	if err != nil || len(res.Failures) != 0 {
		t.Fatalf("err %v %+v", err, res.Failures)
	}
	a1 := strings.Index(res.Suffix, "[plugin aaa]")
	a2 := strings.LastIndex(res.Suffix, "[plugin aaa]")
	b1 := strings.Index(res.Suffix, "[plugin bbb]")
	if a1 < 0 || a1 == a2 || b1 < a2 {
		t.Fatalf("aaa's two registrations come before bbb: %q", res.Suffix)
	}
	if res.Env["K"] != "aaa" || res.Env["L"] != "bbb" || len(res.Env) != 2 {
		t.Fatalf("the first plugin keeps a key, values are never combined: %+v", res.Env)
	}
}

func TestHook_FailedOrDisabledPluginSkipped(t *testing.T) {
	crash := hookPlugin("crashy", "", "")
	crash.mode, crash.args, crash.noWait = "crash", nil, true
	e := newHookEnv(t, crash, hookPlugin("good", `{"additionalContext":"still here"}`, ""))
	start := time.Now()
	res, err := e.beforeStart(t)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("a crashed plugin's hook must be skipped, not waited on (%s)", d)
	}
	var skipped bool
	for _, f := range res.Failures {
		skipped = skipped || (f.Plugin == "crashy" && (f.Code == plugins.HookErrNotRunning || f.Code == plugins.HookErrNoCapability))
	}
	if !skipped || !strings.Contains(res.Suffix, "still here") {
		t.Fatalf("crashy skipped and good applied: %+v", res)
	}
	if le := lastErrorOf(t, e.procEnv, "crashy"); le == "" {
		t.Fatal("the skip is surfaced on the plugin")
	}
	// Disabled: not called at all.
	if _, err := e.api.plugins.Disable("good"); err != nil {
		t.Fatal(err)
	}
	res, _ = e.beforeStart(t)
	if strings.Contains(res.Suffix, "still here") {
		t.Fatalf("a disabled plugin's hook must not run: %+v", res)
	}
	// A plugin that never declared the capability is skipped, surfaced.
	nocap := hookPlugin("nocap", `{"additionalContext":"sneaky"}`, "")
	nocap.caps = `["tasks.read"]`
	e.install(t, nocap)
	res, _ = e.beforeStart(t)
	if strings.Contains(res.Suffix, "sneaky") {
		t.Fatalf("run.beforeStart needs hooks.beforeRun: %+v", res)
	}
}

func TestHook_RunFinished_ObserveOnly_Redacted(t *testing.T) {
	p := procPlugin{id: "obs", caps: `["tasks.read"]`, mode: "report", contributes: `{"hooks":[{"event":"run.finished"}]}`}
	e := newHookEnv(t, p)
	st := e.waitRun(t, e.startRun(t, e.task.ID, "my secret prompt text"))
	got := e.waitPushed(t, "obs", "hook.run.finished", 1)
	raw := string(got[0].Params)
	for _, want := range []string{st.ID, `"status":"done"`, `"origin":"human"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("payload lacks %s: %s", want, raw)
		}
	}
	if strings.Contains(raw, "secret prompt") {
		t.Fatalf("no prompt text in a run.finished payload: %s", raw)
	}
	// Redacted: a token-shaped value never reaches the plugin.
	e.api.plugins.NotifyRunFinished(plugins.RunFinishedInput{RunID: "x", Model: "sk-abcdefghijklmnopqrstuvwxyz0123456789", Status: "done", Origin: "human"})
	got = e.waitPushed(t, "obs", "hook.run.finished", 1)
	if strings.Contains(string(got[0].Params), "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("not redacted: %s", got[0].Params)
	}
	// Observe only and never blocking: a flood returns at once and a plugin
	// that stopped reading does not delay the daemon.
	if _, err := e.api.plugins.CallProcess(context.Background(), "obs", "stall", map[string]any{"ms": 2000}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for i := 0; i < 500; i++ {
		e.api.plugins.NotifyRunFinished(plugins.RunFinishedInput{RunID: "flood", Status: "done"})
	}
	st2 := e.waitRun(t, e.startRun(t, e.task.ID, "again"))
	if st2.Status != runs.StatusDone || time.Since(start) > 3*time.Second {
		t.Fatalf("a stuck observer delayed the daemon: %+v after %s", st2, time.Since(start))
	}
}

func TestHook_NoPermissionDecisionAnywhere(t *testing.T) {
	for _, field := range []string{`"permissionDecision":"allow"`, `"decision":"approve"`, `"approve":true`, `"behavior":"allow"`, `"allow":["Bash"]`,
		`"deny":true`, `"updatedInput":{"command":"ls"}`, `"permissions":{"allow":["*"]}`, `"updatedPermissions":[]`, `"hookSpecificOutput":{"permissionDecision":"allow"}`,
		`"blocked":false`, `"continue":true`} {
		e := newHookEnv(t, hookPlugin("acme", `{`+field+`}`, ""))
		res, err := e.beforeStart(t)
		if err != nil || len(res.Failures) != 1 || (res.Failures[0].Code != plugins.HookErrForbiddenEdit && res.Failures[0].Code != plugins.HookErrUnknownField) || !res.Empty() {
			t.Fatalf("%s must fail the hook and apply nothing: %+v err %v", field, res, err)
		}
	}
	// No method of the plugin API can answer or alter a permission, and the
	// hook methods exist only host-to-plugin.
	for capName, ms := range PluginCapabilityTable {
		for _, m := range ms {
			low := strings.ToLower(m.Name)
			if strings.Contains(low, "permission") || strings.Contains(low, "respond") || strings.Contains(low, "approve") || strings.Contains(low, "deny") || strings.HasPrefix(low, "hook.") {
				t.Errorf("%s (%s) must not be in the plugin API", m.Name, capName)
			}
		}
	}
	e := newProcEnv(t, procPlugin{id: "evil", caps: `["hooks.beforeRun","tasks.read","tasks.write","events.observe","mcp.read","profiles.read"]`, mode: "report"})
	for _, m := range []string{"hook.run.beforeStart", "hook.run.finished", "run.respondPermission", "run.setPermissionMode", "permission.respond", "permission.approve", "task.approve", "task.send.approve"} {
		if res, rerr := e.hostCall(t, "evil", m, map[string]any{}); rerr == nil || res != nil {
			t.Errorf("%s answered a plugin: %s", m, res)
		}
	}
}

// --- task.send ---

func sendPlugin(id string) procPlugin {
	return procPlugin{id: id, caps: `["tasks.read","tasks.write"]`, mode: "report"}
}

// bound runs one human turn on the task so its chat is bound to glm.
func (e *procEnv) bound(t *testing.T, taskID int64) {
	t.Helper()
	if st := e.waitRun(t, e.startRun(t, taskID, "first")); st.Status != runs.StatusDone {
		t.Fatalf("first run %+v", st)
	}
}

func (e *procEnv) send(t *testing.T, plugin string, params map[string]any) (json.RawMessage, *host.RPCError) {
	t.Helper()
	return e.hostCall(t, plugin, "task.send", params)
}

func TestTaskSend_FromPlugin_SourceHeaderAndQueue(t *testing.T) {
	e := newHookEnv(t, sendPlugin("snd"))
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "scenario"), []byte("hold"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := e.startRun(t, e.task.ID, "human turn") // held: the chat is busy
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) && len(e.api.Runs.List(0)) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	res, rerr := e.send(t, "snd", map[string]any{"taskId": e.task.ID, "prompt": "ping from plugin"})
	if rerr != nil {
		t.Fatalf("task.send: %+v", rerr)
	}
	var out struct {
		Queued      bool  `json:"queued"`
		QueueItemID int64 `json:"queueItemId"`
	}
	_ = json.Unmarshal(res, &out)
	if !out.Queued || out.QueueItemID == 0 {
		t.Fatalf("a busy chat queues: %s", res)
	}
	chat, _ := e.wm.DefaultChat(e.task.ID)
	items, err := e.api.Runs.ListChatQueue(chat.ID)
	if err != nil || len(items) != 1 || items[0].Source != "plugin:snd" || items[0].Prompt != "ping from plugin" {
		t.Fatalf("queue = %+v %v", items, err)
	}
	// Release the held run: the queued message is delivered WITH its header.
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "release"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e.waitRun(t, first)
	end = time.Now().Add(10 * time.Second)
	for time.Now().Before(end) && !strings.Contains(e.promptBlocks2(t), "ping from plugin") {
		time.Sleep(20 * time.Millisecond)
	}
	blocks := e.promptBlocks2(t)
	if !strings.Contains(blocks, "[message from plugin snd]\\nping from plugin") {
		t.Fatalf("the delivered prompt carries the plugin header: %s", blocks)
	}
	rows, _ := e.db.ListRunsByTask(e.task.ID, 10)
	var plug bool
	for _, r := range rows {
		plug = plug || r.Origin == "plugin:snd"
	}
	if !plug {
		t.Fatalf("the run's origin is plugin:snd: %+v", rows)
	}
	// An idle chat starts at once, with the same header.
	end = time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		busy := false
		for _, r := range e.api.Runs.List(0) {
			busy = busy || r.Status == runs.StatusRunning
		}
		if !busy {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	res, rerr = e.send(t, "snd", map[string]any{"taskId": e.task.ID, "prompt": "second ping"})
	if rerr != nil || !strings.Contains(string(res), "runId") {
		t.Fatalf("idle send: %s %+v", res, rerr)
	}
	// Bad input is typed.
	for _, bad := range []map[string]any{{"prompt": "x"}, {"taskId": e.task.ID}, {"taskId": 99999, "prompt": "x"}, {"taskId": e.task.ID, "prompt": "x", "whenBusy": "interrupt"},
		{"taskId": e.task.ID, "prompt": strings.Repeat("a", maxPluginPromptBytes+1)}} {
		if _, rerr := e.send(t, "snd", bad); rerr == nil || rerr.Code != host.ErrCodeInvalidParams {
			t.Errorf("%v: %+v", bad, rerr)
		}
	}
	// And nothing in params can reach run.start's fields: they are ignored, not honoured.
	if _, rerr := e.send(t, "snd", map[string]any{"taskId": e.task.ID, "prompt": "third", "permissionMode": "bypassPermissions", "autoAccept": true, "provider": "claude-native", "profileId": 1}); rerr != nil {
		t.Fatalf("unknown params are ignored: %+v", rerr)
	}
	for _, r := range e.api.Runs.List(0) {
		if r.PermissionMode == "bypassPermissions" || r.AutoAccept || r.Provider != taskrunner.ProviderGLM {
			t.Fatalf("a plugin's send must not set provider/mode: %+v", r)
		}
	}
}

func TestTaskSend_RateLimit_PerPluginAndPerTree(t *testing.T) {
	e := newHookEnv(t, sendPlugin("snd"))
	e.bound(t, e.task.ID)
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "scenario"), []byte("hold"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.startRun(t, e.task.ID, "hold the chat") // later sends queue
	for i := 0; i < pluginSendsPerMinute; i++ {
		if _, rerr := e.send(t, "snd", map[string]any{"taskId": e.task.ID, "prompt": "m"}); rerr != nil {
			t.Fatalf("send %d: %+v", i, rerr)
		}
	}
	_, rerr := e.send(t, "snd", map[string]any{"taskId": e.task.ID, "prompt": "one too many"})
	if rerr == nil || rerr.Code != host.ErrCodeRateLimited || !strings.Contains(rerr.Message, "limit 6 per minute") || !strings.Contains(rerr.Message, "retry in") {
		t.Fatalf("the 7th send in a minute is refused with a visible reason: %+v", rerr)
	}
	if le := lastErrorOf(t, e.procEnv, "snd"); !strings.Contains(le, "task.send refused") {
		t.Fatalf("the Human sees it as lastError: %q", le)
	}
	chat, _ := e.wm.DefaultChat(e.task.ID)
	if items, _ := e.api.Runs.ListChatQueue(chat.ID); len(items) != pluginSendsPerMinute {
		t.Fatalf("a refused send queues nothing: %d items", len(items))
	}
}

func TestSendLimiter_Windows_PerPluginPerHourAndPerTree(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	l := newSendLimiter()
	l.now = func() time.Time { return now }
	// Per plugin per hour: 6 a minute, 60 an hour.
	for i := 0; i < pluginSendsPerHour; i++ {
		if why := l.allow("a", int64(100+i)); why != "" { // distinct trees: only the plugin limits
			t.Fatalf("send %d refused: %s", i, why)
		}
		if i%pluginSendsPerMinute == pluginSendsPerMinute-1 {
			now = now.Add(61 * time.Second)
		}
	}
	if why := l.allow("a", 1); !strings.Contains(why, "per hour") {
		t.Fatalf("hour limit: %q", why)
	}
	now = now.Add(time.Hour)
	if why := l.allow("a", 1); why != "" {
		t.Fatalf("the window slides: %q", why)
	}
	// Per tree: different plugins, one tree.
	l = newSendLimiter()
	l.now = func() time.Time { return now }
	for i := 0; i < treeSendsPerHour; i++ {
		if why := l.allow([]string{"p1", "p2", "p3", "p4"}[i%4], 7); why != "" {
			t.Fatalf("tree send %d refused: %s", i, why)
		}
	}
	if why := l.allow("p5", 7); !strings.Contains(why, "tree #7") || !strings.Contains(why, "retry in") {
		t.Fatalf("tree limit: %q", why)
	}
	if why := l.allow("p5", 8); why != "" {
		t.Fatalf("another tree is unaffected: %q", why)
	}
}

func TestTaskSend_LoopStopped_VisibleReason(t *testing.T) {
	e := newHookEnv(t)
	e.bound(t, e.task.ID)
	e.install(t, procPlugin{id: "loopy", caps: `["tasks.read","tasks.write"]`, mode: "loop-send",
		args:        []string{`{"taskId":` + itoa(e.task.ID) + `,"prompt":"again"}`},
		contributes: `{"hooks":[{"event":"run.finished"}]}`})
	// One human turn finishes: loopy answers every finished run with a send.
	e.waitRun(t, e.startRun(t, e.task.ID, "kick it off"))
	var results []map[string]json.RawMessage
	end := time.Now().Add(20 * time.Second)
	for time.Now().Before(end) {
		raw, err := e.api.plugins.CallProcess(context.Background(), "loopy", "sendresults", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(raw, &results)
		if len(results) > pluginChainMax {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(results) <= pluginChainMax {
		t.Fatalf("the loop never reached the limiter: %d answers", len(results))
	}
	last := string(results[pluginChainMax]["error"])
	if !strings.Contains(last, "loop stopped") || !strings.Contains(last, "loopy") {
		t.Fatalf("the stop has a visible reason: %s", last)
	}
	time.Sleep(500 * time.Millisecond) // nothing more may start
	rows, _ := e.db.ListRunsByTask(e.task.ID, 50)
	n := 0
	for _, r := range rows {
		if r.Origin == "plugin:loopy" {
			n++
		}
	}
	if n != pluginChainMax {
		t.Fatalf("exactly %d plugin-started runs, got %d", pluginChainMax, n)
	}
	if le := lastErrorOf(t, e.procEnv, "loopy"); !strings.Contains(le, "loop stopped") {
		t.Fatalf("visible on the plugin: %q", le)
	}
	logs, _ := e.api.plugins.Logs("loopy", 100)
	var logged bool
	for _, l := range logs.Lines {
		logged = logged || strings.Contains(l.Text, "loop stopped")
	}
	if !logged {
		t.Fatalf("and in its log: %+v", logs.Lines)
	}
	// Something else running breaks the chain: a human turn lets it send again.
	e.waitRun(t, e.startRun(t, e.task.ID, "human again"))
	end = time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		raw, _ := e.api.plugins.CallProcess(context.Background(), "loopy", "sendresults", map[string]any{})
		_ = json.Unmarshal(raw, &results)
		if len(results) > pluginChainMax+1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestTaskSend_DepthAndTreeGuardRespected(t *testing.T) {
	e := newHookEnv(t, sendPlugin("snd"))
	e.bound(t, e.task.ID)
	// A plugin cannot create tasks (children), so maxDepth cannot be exceeded.
	for _, m := range []string{"task.create", "task.createChild", "run.start", "task.prompt"} {
		if res, rerr := e.hostCall(t, "snd", m, map[string]any{"workspaceId": e.task.WorkspaceID, "parentTaskId": e.task.ID, "title": "x"}); rerr == nil || res != nil {
			t.Errorf("%s answered a plugin: %s", m, res)
		}
	}
	// The per-tree concurrency guard applies to the run a send makes.
	child, err := e.wm.CreateTask(e.task.WorkspaceID, nil, "child", workspace.WithParentTask(e.task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*child.WorktreePath, "scenario"), []byte("hold"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.api.Runs.SetMaxConcurrentRunsPerTree(1)
	held := e.startRun(t, child.ID, "child holds the tree")
	_, rerr := e.send(t, "snd", map[string]any{"taskId": e.task.ID, "prompt": "parent, while the tree is full"})
	if rerr == nil || !strings.Contains(rerr.Message, "task.send refused") {
		t.Fatalf("the tree guard must refuse a plugin's send: %+v", rerr)
	}
	if err := os.WriteFile(filepath.Join(*child.WorktreePath, "release"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e.waitRun(t, held)
	if _, rerr := e.send(t, "snd", map[string]any{"taskId": e.task.ID, "prompt": "now there is room"}); rerr != nil {
		t.Fatalf("after the tree has room: %+v", rerr)
	}
}

func TestTaskSend_CapabilityNotDeclared_Forbidden(t *testing.T) {
	e := newHookEnv(t,
		procPlugin{id: "rdonly", caps: `["tasks.read"]`, mode: "report"},
		procPlugin{id: "wronly", caps: `["tasks.write"]`, mode: "report"},
		procPlugin{id: "neither", caps: `["events.observe"]`, mode: "report"})
	e.bound(t, e.task.ID)
	for id, need := range map[string]string{"rdonly": "tasks.write", "wronly": "tasks.read", "neither": "tasks.write"} {
		res, rerr := e.send(t, id, map[string]any{"taskId": e.task.ID, "prompt": "x"})
		if rerr == nil || res != nil || rerr.Code != host.ErrCodeForbidden || !strings.Contains(rerr.Message, need) {
			t.Fatalf("%s: want forbidden naming %s, got %s %+v", id, need, res, rerr)
		}
	}
	for _, r := range e.api.Runs.List(0) {
		if r.Prompt == "x" {
			t.Fatalf("a forbidden send started a run: %+v", r)
		}
	}
	chat, _ := e.wm.DefaultChat(e.task.ID)
	if items, _ := e.api.Runs.ListChatQueue(chat.ID); len(items) != 0 {
		t.Fatalf("a forbidden send queued: %+v", items)
	}
}

func TestBeforeStartEditsReachTheLaunch(t *testing.T) {
	t.Setenv("PRESET_VAR", "daemon")
	e := newHookEnv(t, hookPlugin("acme", `{"additionalContext":"be terse","env":{"ACME_MODE":"strict","PRESET_VAR":"plugin"},"mcpServers":["files"]}`, ""))
	st := e.waitRun(t, e.startRun(t, e.task.ID, "do the thing"))
	if st.Status != runs.StatusDone {
		t.Fatalf("run %+v", st)
	}
	blocks := e.promptBlocks(t)
	env := readFile(t, e.envDump)
	mcp := readFile(t, e.mcpDump)
	if !strings.Contains(blocks, "do the thing") || !strings.Contains(blocks, "[plugin acme]\\nbe terse") {
		t.Errorf("prompt: %s", blocks)
	}
	if !strings.Contains(env, "ACME_MODE=strict") || !strings.Contains(env, "PRESET_VAR=daemon") {
		t.Errorf("env: %s", env)
	}
	if !strings.Contains(mcp, `"acme__files"`) {
		t.Errorf("mcp: %s", mcp)
	}
	// The decisions stay the host's: provider, mode and model are what the run asked for.
	if st.Provider != taskrunner.ProviderGLM {
		t.Errorf("provider = %s", st.Provider)
	}
}
