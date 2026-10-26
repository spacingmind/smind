package wsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const wsSecret = "SENTINEL-ws-secret-77aa"

type wsSettings map[string]string

func (w wsSettings) Setting(plugin, key string) (string, bool) {
	v, ok := w[plugin+"/"+key]
	return v, ok
}

// contribEnv is a real API server with plugins switched on and one plugin
// ("acme": an MCP server with a secret in env, a skill, a calm and an
// escalating profile) installed and enabled.
type contribEnv struct {
	api  *API
	db   *store.Store
	srv  *httptest.Server
	task store.Task
	dump string
	home string
}

func newContribEnv(t *testing.T) *contribEnv { return newContribEnvOpt(t, true) }

// newContribEnvOpt: injectSettings false leaves the daemon's own settings
// store in charge (PB.2), true replaces it with the fixed sentinel provider.
func newContribEnvOpt(t *testing.T, injectSettings bool) *contribEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("plugins:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKEAGENT_MCP_CAPS", "stdio")
	dump := filepath.Join(t.TempDir(), "mcp-dump.json")
	t.Setenv("FAKEAGENT_MCP_DUMP", dump)

	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := taskrunner.New(wm,
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}),
		taskrunner.WithMcpServers(mcpservers.New(db)),
	)
	api, err := New(wm, newTestAccountsRegistry(t), runner, db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	if injectSettings {
		api.plugins.SetSettings(wsSettings{"acme/token": wsSecret})
	}
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)

	src := t.TempDir()
	files := map[string]string{
		"smind-plugin.json": `{"id":"acme","name":"Acme","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"},
			"settings":[{"key":"token","type":"secret","label":"API token"}],
			"contributes":{"mcpServers":[{"name":"files","transport":"stdio","command":["go","version"],"env":{"TOKEN":"${setting.token}"}}],
			"skills":["skills/review"],"profiles":["profiles/reviewer.json","profiles/boss.json"]}}`,
		"skills/review/SKILL.md": "---\ndescription: Use when reviewing\n---\nReview carefully.\n",
		"profiles/reviewer.json": `{"name":"reviewer","provider":"claude-native","instructions":"Review.","skills":["acme:review"]}`,
		"profiles/boss.json":     `{"name":"boss","provider":"claude-native","permissionMode":"` + taskrunner.ClaudeModeBypass + `","orchestrationTools":true,"childPolicy":"profiles","childProfiles":["reviewer"]}`,
	}
	for name, body := range files {
		full := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := &contribEnv{api: api, db: db, srv: srv, task: task, dump: dump, home: home}
	res, err := e.call("plugin.install", localRC(), map[string]any{"source": src})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	in := res.(plugins.Info)
	if _, err := e.call("plugin.enable", localRC(), map[string]any{"id": "acme", "confirm": in.Consent.Fingerprint}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	return e
}

func localRC() *requestContext { return &requestContext{local: true, channel: ChannelLoopback} }
func agentRC() *requestContext { return &requestContext{local: true, runScoped: true} }
func relayRC() *requestContext {
	return &requestContext{local: false, deviceID: "dev-1", channel: ChannelE2EE}
}

func (e *contribEnv) call(method string, rc *requestContext, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return e.api.hs[method](context.Background(), rc, raw)
}

func (e *contribEnv) json(t *testing.T, method string, rc *requestContext, params any) string {
	t.Helper()
	res, err := e.call(method, rc, params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	b, _ := json.Marshal(res)
	return string(b)
}

func (e *contribEnv) ownedMcpID(t *testing.T) int64 {
	t.Helper()
	var rows []mcpServerResult
	if err := json.Unmarshal([]byte(e.json(t, "mcp.list", localRC(), nil)), &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Owner != nil && *r.Owner == "acme" {
			return r.ID
		}
	}
	t.Fatalf("no owned mcp row in %+v", rows)
	return 0
}

func wantReadOnlyOwned(t *testing.T, what string, err error) {
	t.Helper()
	ce, ok := err.(*rpcCodeError)
	if !ok || ce.RPCCode() != codeReadOnlyOwned || !strings.Contains(err.Error(), `"acme"`) {
		t.Fatalf("%s: want read_only_owned naming the plugin, got %v", what, err)
	}
	if d, _ := ce.RPCData().(map[string]string); d["plugin"] != "acme" {
		t.Fatalf("%s: error data = %v", what, ce.RPCData())
	}
}

func TestPluginContribution_WireCarriesOwner_ReadOnlyForEveryCaller(t *testing.T) {
	e := newContribEnv(t)

	var servers []mcpServerResult
	if err := json.Unmarshal([]byte(e.json(t, "mcp.list", relayRC(), nil)), &servers); err != nil {
		t.Fatal(err)
	}
	var owned *mcpServerResult
	for i := range servers {
		if servers[i].Owner != nil {
			owned = &servers[i]
		}
	}
	if owned == nil || *owned.Owner != "acme" || !owned.ReadOnly || owned.Name != "acme:files" || !owned.Enabled {
		t.Fatalf("a relay session lists owned rows with owner + readOnly: %+v", servers)
	}
	if !strings.Contains(string(owned.Env), "[redacted]") {
		t.Fatalf("owned env must be redacted like any: %s", owned.Env)
	}
	raw := e.json(t, "mcp.list", localRC(), nil)
	if !strings.Contains(raw, `"owner":"acme"`) || !strings.Contains(raw, `"readOnly":true`) {
		t.Fatalf("wire shape: %s", raw)
	}
	user, err := e.call("mcp.create", localRC(), map[string]any{"name": "mine", "transport": "http", "url": "https://u/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(user); !strings.Contains(string(b), `"owner":null`) || !strings.Contains(string(b), `"readOnly":false`) {
		t.Fatalf("a user row has owner null: %s", b)
	}

	// Profiles: owned rows carry owner/readOnly; user rows owner null.
	var profs []store.AgentProfile
	if err := json.Unmarshal([]byte(e.json(t, "profile.list", relayRC(), nil)), &profs); err != nil {
		t.Fatal(err)
	}
	byName := map[string]store.AgentProfile{}
	for _, p := range profs {
		byName[p.Name] = p
	}
	boss, rev := byName["acme:boss"], byName["acme:reviewer"]
	if boss.Owner == nil || *boss.Owner != "acme" || !boss.ReadOnly || boss.ChildPolicy != "profiles" || len(boss.ChildProfileIDs) != 1 || boss.ChildProfileIDs[0] != rev.ID {
		t.Fatalf("owned seat on the wire: %+v", boss)
	}
	if got := e.json(t, "profile.get", localRC(), map[string]any{"id": boss.ID}); !strings.Contains(got, `"owner":"acme"`) {
		t.Fatalf("profile.get: %s", got)
	}

	// Skills.
	sk := e.json(t, "skill.list", relayRC(), nil)
	if !strings.Contains(sk, `"name":"acme:review"`) || !strings.Contains(sk, `"owner":"acme"`) || !strings.Contains(sk, `"source":"plugin"`) || !strings.Contains(sk, `"name":"smind-leader"`) {
		t.Fatalf("skill.list: %s", sk)
	}

	// Writes: refused for a human, an agent AND a relay session, and nothing changed.
	for who, rc := range map[string]*requestContext{"human": localRC(), "agent": agentRC(), "relay": relayRC()} {
		_, err := e.call("mcp.update", rc, map[string]any{"id": owned.ID, "name": "files", "transport": "stdio", "command": "evil"})
		wantReadOnlyOwned(t, who+" mcp.update", err)
		_, err = e.call("mcp.delete", rc, map[string]any{"id": owned.ID})
		wantReadOnlyOwned(t, who+" mcp.delete", err)
		_, err = e.call("mcp.setEnabled", rc, map[string]any{"id": owned.ID, "enabled": false})
		wantReadOnlyOwned(t, who+" mcp.setEnabled", err)
		_, err = e.call("profile.update", rc, map[string]any{"id": boss.ID, "name": "x", "provider": "claude-native"})
		wantReadOnlyOwned(t, who+" profile.update", err)
		_, err = e.call("profile.delete", rc, map[string]any{"id": boss.ID})
		wantReadOnlyOwned(t, who+" profile.delete", err)
	}
	if got := e.json(t, "mcp.list", localRC(), nil); !strings.Contains(got, `"name":"acme:files"`) || strings.Contains(got, "evil") {
		t.Fatalf("a refused write changed something: %s", got)
	}
	// A user row named like an owned one is rejected at create and update.
	if _, err := e.call("profile.create", localRC(), map[string]any{"name": "acme:boss", "provider": "claude-native"}); err == nil {
		t.Fatal("a user profile named like an owned one must be rejected")
	}
	mine, _ := e.call("profile.create", localRC(), map[string]any{"name": "mine", "provider": "claude-native"})
	if _, err := e.call("profile.update", localRC(), map[string]any{"id": mine.(store.AgentProfile).ID, "name": "acme:reviewer", "provider": "claude-native"}); err == nil {
		t.Fatal("renaming onto an owned name must be rejected")
	}
	if _, err := e.call("mcp.create", localRC(), map[string]any{"name": "acme:files", "transport": "http", "url": "https://u"}); err == nil {
		t.Fatal("an mcp name with ':' is reserved for plugins")
	}
}

// TestMcpContribution_SecretSubstitution_RedactedOnRead is the end-to-end
// sentinel audit: a real run delivers the secret to the agent, and it appears
// in none of mcp.list/mcp.get, plugin.list/get, the daemon log, the run
// events, the request log or any error string.
func TestMcpContribution_SecretSubstitution_RedactedOnRead(t *testing.T) {
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { log.SetOutput(os.Stderr); slog.SetDefault(prev) })

	e := newContribEnv(t)
	id := e.ownedMcpID(t)
	var reads []string
	reads = append(reads,
		e.json(t, "mcp.list", localRC(), nil),
		e.json(t, "mcp.get", localRC(), map[string]any{"id": id}),
		e.json(t, "plugin.list", localRC(), nil),
		e.json(t, "plugin.get", localRC(), map[string]any{"id": "acme"}),
		e.json(t, "skill.list", localRC(), nil),
		e.json(t, "profile.list", localRC(), nil),
	)
	for _, op := range []func() error{
		func() error {
			_, err := e.call("mcp.update", localRC(), map[string]any{"id": id, "name": "x", "transport": "stdio", "command": "y", "env": map[string]string{"TOKEN": "[redacted]"}})
			return err
		},
		func() error { _, err := e.call("mcp.delete", localRC(), map[string]any{"id": id}); return err },
	} {
		if err := op(); err != nil {
			reads = append(reads, err.Error())
		}
	}

	// A real run: the agent receives the substituted secret.
	env := &wsenv{srv: e.srv, db: e.db, ws: dialWS(t, e.srv, "tok")}
	t.Cleanup(func() { _ = env.ws.Close() })
	runID, frames := env.runPromptCollectFrames(t, e.task.ID)
	dumped, err := os.ReadFile(e.dump)
	if err != nil || !strings.Contains(string(dumped), wsSecret) || !strings.Contains(string(dumped), "acme__files") {
		t.Fatalf("the agent must receive the owned server with the secret (this is what the audit is measured against): %s %v", dumped, err)
	}
	for _, f := range frames {
		reads = append(reads, string(f))
	}
	for _, f := range env.attachCollectFrames(t, runID) {
		reads = append(reads, string(f))
	}
	evs, err := e.db.ListRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		reads = append(reads, ev.EventData)
	}
	rl, _ := e.db.ListRequestLogs(store.RequestLogFilter{})
	rlb, _ := json.Marshal(rl)
	reads = append(reads, string(rlb), logBuf.String())

	for i, r := range reads {
		if strings.Contains(r, wsSecret) {
			t.Fatalf("read path #%d leaks the secret: %.300s", i, r)
		}
	}
	if len(evs) == 0 {
		t.Fatal("the audit saw no run events")
	}
}

// TestPluginContribution_Propagation_EventsAndNoRestart: enable, disable,
// remove and the global switch change what the lists return at once and
// publish the events clients refetch on; a running run keeps what it was
// started with.
func TestPluginContribution_Propagation_EventsAndNoRestart(t *testing.T) {
	e := newContribEnv(t)
	sub := subscribeTopics(e.api, TopicPluginUpdated, TopicMcpServerDeleted, TopicMcpServerCreated, TopicProfileDeleted, TopicProfileCreated)
	// topics drains what has been published since the last call.
	topics := func() map[string]int {
		sub.mu.Lock()
		evs := sub.queue
		sub.queue = nil
		sub.mu.Unlock()
		out := map[string]int{}
		for _, ev := range evs {
			out[ev.Topic]++
		}
		return out
	}
	has := func(method, needle string) bool { return strings.Contains(e.json(t, method, localRC(), nil), needle) }

	if !has("mcp.list", "acme:files") || !has("profile.list", "acme:boss") || !has("skill.list", "acme:review") {
		t.Fatal("precondition: contributions listed")
	}
	if _, err := e.call("plugin.disable", localRC(), map[string]any{"id": "acme"}); err != nil {
		t.Fatal(err)
	}
	if has("mcp.list", "acme:files") || has("profile.list", "acme:") || has("skill.list", "acme:review") {
		t.Fatal("a disabled plugin's rows must vanish from every list at once")
	}
	got := topics()
	if got[TopicPluginUpdated] != 1 || got[TopicMcpServerDeleted] != 1 || got[TopicProfileDeleted] != 2 {
		t.Fatalf("disable events = %v", got)
	}

	in, _ := e.api.plugins.Get("acme")
	if _, err := e.call("plugin.enable", localRC(), map[string]any{"id": "acme", "confirm": in.Consent.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	if got := topics(); got[TopicPluginUpdated] != 1 || got[TopicMcpServerCreated] != 1 || got[TopicProfileCreated] != 2 {
		t.Fatalf("enable events = %v", got)
	}
	if !has("mcp.list", "acme:files") {
		t.Fatal("enable applies with no restart")
	}

	// The global switch: a config edit, picked up by the next read and by the watcher.
	if err := os.WriteFile(filepath.Join(e.home, "config.yaml"), []byte("plugins:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if has("mcp.list", "acme:files") || has("profile.list", "acme:") || has("skill.list", "acme:review") {
		t.Fatal("global switch off: nothing contributed (AC2)")
	}
	stop := e.api.WatchPlugins(context.Background())
	deadline := time.Now().Add(6 * time.Second)
	seen := false
	for !seen && time.Now().Before(deadline) {
		seen = topics()[TopicPluginUpdated] > 0
		time.Sleep(50 * time.Millisecond)
	}
	stop()
	if !seen {
		t.Fatal("the watcher never announced the switch flip")
	}
	if err := os.WriteFile(filepath.Join(e.home, "config.yaml"), []byte("plugins:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !has("mcp.list", "acme:files") {
		t.Fatal("switch back on applies with no restart")
	}

	if _, err := e.call("plugin.remove", localRC(), map[string]any{"id": "acme"}); err != nil {
		t.Fatal(err)
	}
	if has("mcp.list", "acme:files") || has("profile.list", "acme:") || has("skill.list", "acme:review") {
		t.Fatal("a removed plugin's rows must vanish")
	}
}
