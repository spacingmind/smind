package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/plugins/host/hosttest"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

const allCaps = `["events.observe","tasks.read","mcp.read","profiles.read"]`

// procEnv is a real API with plugins on and process plugins (the test binary
// re-executed by hosttest) installed and running.
type procEnv struct {
	api    *API
	db     *store.Store
	task   store.Task
	home   string
	wm     *workspace.Manager
	runner *taskrunner.Runner
}

type procPlugin struct {
	id, caps, mode, settings string
	// args are extra arguments of the helper mode; contributes is the
	// manifest's `contributes` object (JSON); noWait skips waiting for the
	// process to run (a plugin that crashes at once).
	args        []string
	contributes string
	noWait      bool
}

func newProcEnv(t *testing.T, ps ...procPlugin) *procEnv {
	t.Helper()
	return newProcEnvOpts(t, nil, ps...)
}

// newProcEnvOpts is newProcEnv with extra runner options (a fake agent).
func newProcEnvOpts(t *testing.T, ropts []taskrunner.Option, ps ...procPlugin) *procEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("plugins:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := taskrunner.New(wm, append([]taskrunner.Option{taskrunner.WithMcpServers(mcpservers.New(db))}, ropts...)...)
	api, err := New(wm, newTestAccountsRegistry(t), runner, db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	e := &procEnv{api: api, db: db, task: task, home: home, wm: wm, runner: runner}
	for _, p := range ps {
		e.install(t, p)
	}
	return e
}

func (e *procEnv) install(t *testing.T, p procPlugin) {
	t.Helper()
	cmd, _ := json.Marshal(hosttest.Command(p.mode, p.args...))
	extra := ""
	if p.settings != "" {
		extra = `,"settings":` + p.settings
	}
	if p.contributes != "" {
		extra += `,"contributes":` + p.contributes
	}
	man := `{"id":"` + p.id + `","name":"P","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"},"process":{"command":` + string(cmd) + `},"capabilities":` + p.caps + extra + `}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "smind-plugin.json"), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := e.call("plugin.install", localRC(), map[string]any{"source": dir})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := e.call("plugin.enable", localRC(), map[string]any{"id": p.id, "confirm": res.(plugins.Info).Consent.Fingerprint}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !p.noWait {
		e.waitRunning(t, p.id)
	}
}

func (e *procEnv) waitRunning(t *testing.T, id string) {
	t.Helper()
	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		if st, ok := e.api.plugins.ProcessStatus(id); ok && st.State == host.StateRunning {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("plugin %s never ran", id)
}

func (e *procEnv) call(method string, rc *requestContext, params any) (any, error) {
	raw, _ := json.Marshal(params)
	h := e.api.hs[method]
	if h == nil {
		return nil, os.ErrNotExist
	}
	return h(context.Background(), rc, raw)
}

// hostCall has the plugin process send one request to the host and returns
// what the host answered (a JSON-RPC result or error).
func (e *procEnv) hostCall(t *testing.T, id, method string, params any) (json.RawMessage, *host.RPCError) {
	t.Helper()
	raw, err := e.api.plugins.CallProcess(context.Background(), id, "callhost", map[string]any{"method": method, "params": params})
	if err != nil {
		t.Fatalf("callhost %s: %v", method, err)
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *host.RPCError  `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if string(r.Result) == "null" {
		r.Result = nil
	}
	return r.Result, r.Error
}

type pushed struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (e *procEnv) drain(t *testing.T, id string) []pushed {
	t.Helper()
	raw, err := e.api.plugins.CallProcess(context.Background(), id, "drain", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var out []pushed
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// waitPushed drains until n notifications of method arrived.
func (e *procEnv) waitPushed(t *testing.T, id, method string, n int) []pushed {
	t.Helper()
	var got []pushed
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		for _, p := range e.drain(t, id) {
			if p.Method == method {
				got = append(got, p)
			}
		}
		if len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("plugin %s got %d %q notifications, want %d", id, len(got), method, n)
	return nil
}

func isTable(method string) bool { _, ok := pluginMethodCap[method]; return ok }

func newSpyRegistry(t *testing.T, e *procEnv) (*pluginRegistry, map[string]*int64) {
	t.Helper()
	calls := map[string]*int64{}
	spies := map[string]handlerFunc{}
	for name := range e.api.hs {
		var n int64
		calls[name] = &n
		spies[name] = func(context.Context, *requestContext, json.RawMessage) (any, error) {
			atomic.AddInt64(&n, 1)
			return map[string]any{"ok": true}, nil
		}
	}
	reg := newPluginRegistry()
	reg.bind(spies, e.api.bus)
	return reg, calls
}

func openAll(reg *pluginRegistry) host.Session {
	return reg.Open(host.Plugin{ID: "spy", Capabilities: []string{CapTasksRead, CapEventsObserve, CapMcpRead, CapProfilesRead, "tasks.write", "files.read", "ui.notify", "hooks.beforeRun"}},
		func(string, any) error { return nil })
}

var readVerbs = map[string]bool{"list": true, "get": true, "tree": true}

func TestPluginSession_CapabilityTable_EveryMethodExists(t *testing.T) {
	e := newProcEnv(t)
	seen := map[string]bool{}
	for capName, ms := range PluginCapabilityTable {
		if !contains(plugins.KnownCapabilities, capName) {
			t.Errorf("table group %q is not a manifest capability", capName)
		}
		for _, m := range ms {
			if seen[m.Name] {
				t.Errorf("%s listed twice", m.Name)
			}
			seen[m.Name] = true
			if !m.Native {
				if _, ok := e.api.hs[m.Name]; !ok {
					t.Errorf("%s (%s) is not a wsapi method", m.Name, capName)
				}
			} else if _, ok := e.api.hs[m.Name]; ok {
				t.Errorf("%s is marked native but is also a wsapi method", m.Name)
			}
			if pluginNever(m.Name) {
				t.Errorf("%s is on the never-for-plugins list but is in the table (%s)", m.Name, capName)
			}
			if _, verb, _ := strings.Cut(m.Name, "."); !readVerbs[verb] && m.Why == "" {
				t.Errorf("%s is not a read verb and has no justification", m.Name)
			}
			if strings.Contains(strings.ToLower(m.Name), "permission") || strings.Contains(m.Name, "respond") {
				t.Errorf("%s looks like permission answering", m.Name)
			}
		}
	}
	// The never-list is itself well-formed: every exact entry is a real
	// wsapi method (or a documented absent name), none is in the table.
	for _, m := range pluginNeverMethods {
		if seen[m] {
			t.Errorf("never-listed %s is in the table", m)
		}
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestPluginSession_DefaultDeny_EveryOtherWsapiMethod(t *testing.T) {
	e := newProcEnv(t)
	reg, calls := newSpyRegistry(t, e)
	sess := openAll(reg) // even a plugin declaring EVERY group
	defer sess.Close()
	names := make([]string, 0, len(e.api.hs))
	for n := range e.api.hs {
		names = append(names, n)
	}
	sort.Strings(names)
	// A method added later is denied by default: it is just another name here.
	names = append(names, "future.method", "task.futureWrite", "")
	for _, name := range names {
		if isTable(name) {
			continue
		}
		res, rerr := sess.Request(context.Background(), name, json.RawMessage(`{}`))
		if rerr == nil || res != nil {
			t.Errorf("%s resolved for a plugin session: %v", name, res)
			continue
		}
		if rerr.Code != host.ErrCodeForbidden && rerr.Code != host.ErrCodeMethodNotFound {
			t.Errorf("%s: code %d", name, rerr.Code)
		}
		if n, ok := calls[name]; ok && atomic.LoadInt64(n) != 0 {
			t.Errorf("%s: its handler ran", name)
		}
	}
	// Sanity: the table methods DO reach their handler for a declared group.
	for capName, ms := range PluginCapabilityTable {
		for _, m := range ms {
			if m.Native {
				continue
			}
			if _, rerr := sess.Request(context.Background(), m.Name, json.RawMessage(`{}`)); rerr != nil {
				t.Errorf("%s (%s) did not resolve: %v", m.Name, capName, rerr)
			}
			if atomic.LoadInt64(calls[m.Name]) != 1 {
				t.Errorf("%s: handler ran %d times", m.Name, atomic.LoadInt64(calls[m.Name]))
			}
		}
	}
}

func TestPluginSession_UndeclaredCapability_Forbidden(t *testing.T) {
	e := newProcEnv(t, procPlugin{id: "obs", caps: `["events.observe"]`, mode: "report"},
		procPlugin{id: "rdr", caps: `["tasks.read"]`, mode: "report"})
	for _, m := range []string{"task.list", "mcp.list", "profile.list"} {
		_, rerr := e.hostCall(t, "obs", m, map[string]any{})
		if rerr == nil || rerr.Code != host.ErrCodeForbidden {
			t.Fatalf("obs %s: %+v", m, rerr)
		}
		need := pluginMethodCap[m]
		if !strings.Contains(rerr.Message, need) || !strings.Contains(rerr.Message, "did not declare") {
			t.Errorf("%s: the error must name the missing capability %q: %s", m, need, rerr.Message)
		}
	}
	if _, rerr := e.hostCall(t, "rdr", "events.subscribe", map[string]any{"topics": []string{TopicTaskCreated}}); rerr == nil || rerr.Code != host.ErrCodeForbidden {
		t.Fatalf("rdr subscribe: %+v", rerr)
	}
	// A declared group works end to end: the real task.list through the real handler.
	res, rerr := e.hostCall(t, "rdr", "task.list", map[string]any{"workspaceId": e.task.WorkspaceID})
	if rerr != nil || !strings.Contains(string(res), `"ID"`) {
		t.Fatalf("rdr task.list: %v %s", rerr, res)
	}
	// What a read returns is a redacted copy: the setup output tail is gone.
	if strings.Contains(string(res), "SetupLog") {
		t.Fatalf("SetupLog must be dropped: %s", res)
	}
}

// humanOnly is a sample of everything that must stay out of a plugin's reach.
var humanOnly = []string{
	"plugin.install", "plugin.link", "plugin.enable", "plugin.disable", "plugin.remove", "plugin.update", "plugin.reload",
	"plugin.settings.get", "plugin.settings.set", "plugin.list", "plugin.logs",
	"provider.setEnv", "provider.addInstance", "provider.setDefaultModel", "provider.reload", "provider.list",
	"account.add", "account.updateCredential", "account.list", "account.remove",
	"automation.create", "automation.list", "automation.run",
	"mcp.create", "mcp.update", "mcp.delete", "mcp.setEnabled",
	"profile.create", "profile.update", "profile.delete",
	"relay.device.approve", "relay.device.revoke", "push.register", "forge.setToken", "forge.logout",
	"settings.set", "auth.setPassword", "daemon.setPassword", "set-password", "session.identify",
	"run.respondPermission", "run.setPermissionMode", "run.start", "run.stop", "run.setConfigOption",
	"task.create", "task.archive", "task.prompt", "task.setBudget", "task.stopTree", "task.commit", "task.push", "task.createPr",
	"workspace.create", "workspace.delete", "workspace.setScripts", "workspace.get", "workspace.readiness",
	"terminal.create", "terminal.write", "file.write", "file.read", "fs.listDir", "learning.setMode", "learning.curate",
	"run.logs", "task.diff", "usage.summary", "daemon.info",
}

func TestPluginSession_CannotReachHumanOnlyMethods(t *testing.T) {
	e := newProcEnv(t, procPlugin{id: "evil", caps: allCaps, mode: "report"})
	before := e.json(t, "plugin.list")
	for _, m := range humanOnly {
		res, rerr := e.hostCall(t, "evil", m, map[string]any{"id": "evil"})
		if rerr == nil || res != nil {
			t.Errorf("%s answered a plugin: %s", m, res)
			continue
		}
		if rerr.Code != host.ErrCodeForbidden && rerr.Code != host.ErrCodeMethodNotFound {
			t.Errorf("%s: code %d %s", m, rerr.Code, rerr.Message)
		}
	}
	if after := e.json(t, "plugin.list"); after != before {
		t.Fatalf("plugin state changed under a plugin's attack:\n%s\n%s", before, after)
	}
	if st, ok := e.api.plugins.ProcessStatus("evil"); !ok || st.State != host.StateRunning {
		t.Fatal("the plugin must still be enabled and running")
	}
}

func (e *procEnv) json(t *testing.T, method string) string {
	t.Helper()
	res, err := e.call(method, localRC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	// process pids/timestamps move; compare only the identity and state
	var l struct {
		Plugins []struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
			Version string `json:"version"`
			Hash    string `json:"hash"`
		} `json:"plugins"`
	}
	_ = json.Unmarshal(b, &l)
	out, _ := json.Marshal(l)
	return string(out)
}

func TestPluginSession_CannotAnswerPermissions(t *testing.T) {
	e := newProcEnv(t, procPlugin{id: "evil", caps: allCaps, mode: "report"})
	for _, m := range []string{
		"run.respondPermission", "permission.respond", "permission.approve", "permission.deny", "permission.answer",
		"run.setPermissionMode", "task.setPermissionMode", "run.approve", "run.deny", "task.permissions", "permission.list",
	} {
		res, rerr := e.hostCall(t, "evil", m, map[string]any{"runId": "r1", "requestId": "q1", "optionId": "allow"})
		if rerr == nil || res != nil {
			t.Errorf("%s answered a plugin: %s", m, res)
		}
	}
	// The spy registry proves no handler is reached for any permission-ish name.
	reg, calls := newSpyRegistry(t, e)
	sess := openAll(reg)
	defer sess.Close()
	for name, n := range calls {
		if strings.Contains(strings.ToLower(name), "permission") {
			if _, rerr := sess.Request(context.Background(), name, json.RawMessage(`{}`)); rerr == nil || atomic.LoadInt64(n) != 0 {
				t.Errorf("%s reached its handler for a plugin", name)
			}
		}
	}
	for _, ms := range PluginCapabilityTable {
		for _, m := range ms {
			if strings.Contains(strings.ToLower(m.Name), "permission") {
				t.Errorf("table lists %s", m.Name)
			}
		}
	}
}

func TestEventsObserve_OnlyAllowlistedTopics(t *testing.T) {
	e := newProcEnv(t, procPlugin{id: "obs", caps: `["events.observe"]`, mode: "report"})
	// Every topic the daemon knows that is not allowlisted is refused.
	for topic := range knownTopics {
		_, rerr := e.hostCall(t, "obs", "events.subscribe", map[string]any{"topics": []string{topic}})
		if pluginTopicSet[topic] {
			if rerr != nil {
				t.Errorf("allowlisted %s refused: %v", topic, rerr)
			}
			continue
		}
		if rerr == nil || rerr.Code != host.ErrCodeInvalidParams || !strings.Contains(rerr.Message, topic) {
			t.Errorf("%s is not allowlisted and must be refused naming it: %+v", topic, rerr)
		}
	}
	if _, rerr := e.hostCall(t, "obs", "events.subscribe", map[string]any{"topics": []string{"made.up"}}); rerr == nil {
		t.Error("an unknown topic must be refused")
	}
	// A mixed request is refused whole: nothing partial.
	_, rerr := e.hostCall(t, "obs", "events.subscribe", map[string]any{"topics": []string{TopicTaskArchived, TopicAccountUpdated}})
	if rerr == nil {
		t.Error("a list with one forbidden topic must fail")
	}

	e.api.bus.Publish(Event{Topic: TopicAccountUpdated, Payload: map[string]any{"x": 1}})
	e.api.bus.Publish(Event{Topic: TopicChatQueueUpdated, Payload: map[string]any{"items": []string{"prompt text"}}})
	e.api.bus.Publish(Event{Topic: TopicTaskDeleted, Payload: taskDeletedPayload{ID: 7}})
	got := e.waitPushed(t, "obs", "event", 1)
	time.Sleep(50 * time.Millisecond)
	got = append(got, e.drainMethod(t, "obs", "event")...)
	if len(got) != 1 {
		t.Fatalf("only the subscribed allowlisted topic is delivered: %d events", len(got))
	}
	var n eventNotification
	if err := json.Unmarshal(got[0].Params, &n); err != nil || n.Topic != TopicTaskDeleted || n.Seq != 1 {
		t.Fatalf("event: %+v %v", n, err)
	}
}

func (e *procEnv) drainMethod(t *testing.T, id, method string) []pushed {
	t.Helper()
	var out []pushed
	for _, p := range e.drain(t, id) {
		if p.Method == method {
			out = append(out, p)
		}
	}
	return out
}

func TestEventsObserve_RedactedCopies(t *testing.T) {
	e := newProcEnv(t, procPlugin{id: "obs", caps: `["events.observe"]`, mode: "report"})
	if _, rerr := e.hostCall(t, "obs", "events.subscribe", map[string]any{"topics": []string{TopicPermissionPending, TopicTaskCreated, TopicRunStatus}}); rerr != nil {
		t.Fatal(rerr)
	}
	const body = "SENTINEL-permission-body-rm-rf"
	ghToken := "ghp_" + strings.Repeat("Ab1", 12)
	home, _ := os.UserHomeDir()

	perm := permissionPendingPayload{RunID: "r1", TaskID: 4, ChatID: 5, RequestID: "q9", Summary: "run: " + body + " " + ghToken,
		Options: []permissionOptionParams{{ID: "allow", Label: "Allow", Kind: "allow_once"}}}
	const sha = "9fceb02d0ae598e95dc970b74767f19372d61af8" // high entropy: the redactor alone would blank it
	task := store.Task{ID: 4, BaseSha: sha, Title: "fix " + ghToken, SetupLog: "setup said " + body, SetupError: body}
	if home != "" {
		task.Title += " in " + filepath.Join(home, "proj")
	}
	e.api.bus.Publish(Event{Topic: TopicPermissionPending, Payload: perm})
	e.api.bus.Publish(Event{Topic: TopicTaskCreated, Payload: taskCreatedPayload{Task: task}})
	e.api.bus.Publish(Event{Topic: TopicRunStatus, Payload: runStatusPayload{RunID: "r1", TaskID: 4, Status: "error", Err: "boom " + ghToken}})
	got := e.waitPushed(t, "obs", "event", 3)
	all := ""
	for _, g := range got {
		all += string(g.Params)
	}
	for _, leak := range []string{body, ghToken, "SetupLog", "SetupError"} {
		if strings.Contains(all, leak) {
			t.Fatalf("a plugin saw %q: %s", leak, all)
		}
	}
	if home != "" && strings.Contains(all, home) {
		t.Fatalf("home dir not rewritten: %s", all)
	}
	if !strings.Contains(all, sha) {
		t.Fatalf("a commit id must survive redaction: %s", all)
	}
	var first eventNotification
	_ = json.Unmarshal(got[0].Params, &first)
	pl, _ := json.Marshal(first.Payload)
	for _, want := range []string{`"runId":"r1"`, `"requestId":"q9"`, `"id":"allow"`} {
		if !strings.Contains(string(pl), want) {
			t.Fatalf("permission.pending keeps its identifying fields, missing %s: %s", want, pl)
		}
	}
	if strings.Contains(string(pl), "summary") {
		t.Fatalf("summary must be dropped: %s", pl)
	}
	// A COPY: the daemon's own objects were not touched.
	if !strings.Contains(perm.Summary, body) || !strings.Contains(task.SetupLog, body) {
		t.Fatal("redaction mutated the published payload")
	}
}

func TestEventsObserve_PermissionPendingObservableNotAnswerable(t *testing.T) {
	e := newProcEnv(t, procPlugin{id: "obs", caps: allCaps, mode: "report"})
	if _, rerr := e.hostCall(t, "obs", "events.subscribe", map[string]any{"topics": []string{TopicPermissionPending, TopicPermissionResolved}}); rerr != nil {
		t.Fatal(rerr)
	}
	e.api.bus.Publish(Event{Topic: TopicPermissionPending, Payload: permissionPendingPayload{RunID: "r1", TaskID: 1, RequestID: "q1", Summary: "x"}})
	got := e.waitPushed(t, "obs", "event", 1)
	var n eventNotification
	_ = json.Unmarshal(got[0].Params, &n)
	if n.Topic != TopicPermissionPending {
		t.Fatalf("observing permission.pending must work: %+v", n)
	}
	// ...and nothing lets this plugin answer it, by any name.
	for _, m := range []string{"run.respondPermission", "permission.respond", "run.setPermissionMode"} {
		if res, rerr := e.hostCall(t, "obs", m, map[string]any{"runId": "r1", "requestId": "q1", "optionId": "allow"}); rerr == nil || res != nil {
			t.Errorf("%s answered", m)
		}
	}
}

func TestEventsObserve_BoundedQueue_EventDropped(t *testing.T) {
	e := newProcEnv(t)
	gate := make(chan struct{})
	var mu sync.Mutex
	var events, dropped int
	first := make(chan struct{})
	var once sync.Once
	push := func(method string, params any) error {
		once.Do(func() { close(first) })
		<-gate
		mu.Lock()
		defer mu.Unlock()
		b, _ := json.Marshal(params)
		switch method {
		case "event":
			events++
		case "event.dropped":
			var d eventDroppedPayload
			_ = json.Unmarshal(b, &d)
			dropped += d.Count
		}
		return nil
	}
	reg := newPluginRegistry()
	reg.bind(map[string]handlerFunc{}, e.api.bus)
	sess := reg.Open(host.Plugin{ID: "slow", Capabilities: []string{CapEventsObserve}}, push)
	defer sess.Close()
	if _, rerr := sess.Request(context.Background(), "events.subscribe", json.RawMessage(`{"topics":["task.deleted"]}`)); rerr != nil {
		t.Fatal(rerr)
	}
	const total = 2000
	start := time.Now()
	for i := 0; i < total; i++ {
		e.api.bus.Publish(Event{Topic: TopicTaskDeleted, Payload: taskDeletedPayload{ID: int64(i)}})
		if i == 0 {
			<-first // the pump is now stuck in push with event 0
		}
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("publishing %d events took %s with a stuck plugin: the bus waited for it", total, d)
	}
	close(gate) // the plugin reads again
	end := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		sum := events + dropped
		ev, dr := events, dropped
		mu.Unlock()
		if sum == total {
			if ev > subscriberQueueCap+2 {
				t.Fatalf("delivered %d events, more than the queue bound %d (+ the one in flight)", ev, subscriberQueueCap)
			}
			if dr == 0 {
				t.Fatal("overflow must be reported with event.dropped")
			}
			return
		}
		if time.Now().After(end) {
			t.Fatalf("delivered %d + dropped %d != %d published", ev, dr, total)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEventsObserve_StalledPluginDoesNotDelayOthers(t *testing.T) {
	e := newProcEnv(t,
		procPlugin{id: "stuck", caps: `["events.observe"]`, mode: "report"},
		procPlugin{id: "fast", caps: `["events.observe"]`, mode: "report"})
	for _, id := range []string{"stuck", "fast"} {
		if _, rerr := e.hostCall(t, id, "events.subscribe", map[string]any{"topics": []string{TopicTaskCreated}}); rerr != nil {
			t.Fatal(rerr)
		}
	}
	// A plain subscriber (what a browser tab is).
	tab := subscribeTopics(e.api, TopicTaskCreated)
	// "stuck" stops reading stdin for a while; its pipe fills up.
	go func() {
		_, _ = e.api.plugins.CallProcess(context.Background(), "stuck", "stall", map[string]any{"ms": 3000})
	}()
	time.Sleep(100 * time.Millisecond)

	// 600 KB in all: far more than a pipe buffer (64 KiB), while a drain of the
	// 256 queued events stays well under the 1 MiB frame cap.
	big := strings.Repeat("x", 1500)
	const n = 400
	start := time.Now()
	for i := 0; i < n; i++ {
		e.api.bus.Publish(Event{Topic: TopicTaskCreated, Payload: taskCreatedPayload{Task: store.Task{ID: int64(i), Title: big}}})
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("publishing took %s: a stalled plugin delayed the daemon", d)
	}
	got := e.waitPushed(t, "fast", "event", 1)
	if len(got) == 0 {
		t.Fatal("the fast plugin saw nothing")
	}
	// The fast plugin ends up with the newest event, in order.
	var last int64 = -1
	end := time.Now().Add(10 * time.Second)
	for last != n-1 && time.Now().Before(end) {
		for _, p := range append(got, e.drainMethod(t, "fast", "event")...) {
			var ev struct {
				Payload struct{ Task struct{ ID int64 } }
			}
			_ = json.Unmarshal(p.Params, &ev)
			if ev.Payload.Task.ID < last {
				t.Fatalf("events out of order: %d after %d", ev.Payload.Task.ID, last)
			}
			last = ev.Payload.Task.ID
		}
		got = nil
		time.Sleep(10 * time.Millisecond)
	}
	if last != n-1 {
		t.Fatalf("the fast plugin's newest event is %d, want %d", last, n-1)
	}
	tab.mu.Lock()
	q := len(tab.queue)
	tab.mu.Unlock()
	if q == 0 {
		t.Fatal("the plain subscriber got nothing")
	}
}
