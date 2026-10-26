package wsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/store"
)

const settingsSecret = "SENTINEL-setting-secret-4242"

const settingsDecl = `[{"key":"token","type":"secret","label":"Token","required":true},
	{"key":"region","type":"enum","label":"Region","options":["eu","us"],"default":"eu"},
	{"key":"name","type":"string","label":"Name"}]`

func setSettings(t *testing.T, e *procEnv, rc *requestContext, id string, values map[string]any) error {
	t.Helper()
	_, err := e.call("plugin.settings.set", rc, map[string]any{"id": id, "values": values})
	return err
}

// TestPluginSettings_SecretsNeverLeak is the sentinel audit: a plugin that
// even prints its own initialize params to stderr, with a secret set, leaks
// the secret through none of plugin.list/get/settings.get/logs, events, the
// daemon log, the request log or any error string.
func TestPluginSettings_SecretsNeverLeak(t *testing.T) {
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { log.SetOutput(os.Stderr); slog.SetDefault(prev) })

	e := newProcEnv(t, procPlugin{id: "leaky", caps: allCaps, mode: "stderr-init", settings: settingsDecl})
	all := subscribeTopics(e.api, PluginEventTopicsAll()...)

	if err := setSettings(t, e, localRC(), "leaky", map[string]any{"token": settingsSecret, "name": "n"}); err != nil {
		t.Fatal(err)
	}
	// The process restarted with the secret and logged its own init params.
	deadline := time.Now().Add(10 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		logs = e.json2(t, "plugin.logs", localRC(), map[string]any{"id": "leaky"})
		if strings.Count(logs, "init params:") >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(logs, "init params:") {
		t.Fatalf("the plugin never logged: %s", logs)
	}
	// What the plugin was really handed (proves the audit is measured against a live secret).
	raw, err := e.api.plugins.CallProcess(context.Background(), "leaky", "getinit", map[string]any{})
	if err != nil || !strings.Contains(string(raw), settingsSecret) {
		t.Fatalf("the plugin must receive the decrypted secret: %s %v", raw, err)
	}

	var reads []string
	for _, rc := range []*requestContext{localRC(), relayRC()} {
		reads = append(reads,
			e.json2(t, "plugin.list", rc, nil),
			e.json2(t, "plugin.get", rc, map[string]any{"id": "leaky"}),
			e.json2(t, "plugin.settings.get", rc, map[string]any{"id": "leaky"}),
			e.json2(t, "plugin.logs", rc, map[string]any{"id": "leaky", "limit": 1000}),
			e.json2(t, "mcp.list", rc, nil),
		)
	}
	// Errors: bad values for the secret and others, none echoes a value.
	for _, bad := range []map[string]any{
		{"token": 12345},
		{"region": settingsSecret},
		{"name": json.Number("1e999")},
		{"token": nil},
	} {
		if err := setSettings(t, e, localRC(), "leaky", bad); err != nil {
			reads = append(reads, err.Error())
		} else if bad["token"] == nil {
			t.Fatal("clearing a required secret must fail")
		}
	}
	if _, err := e.call("plugin.settings.get", localRC(), map[string]any{"id": "nope"}); err != nil {
		reads = append(reads, err.Error())
	}
	time.Sleep(100 * time.Millisecond)
	all.mu.Lock()
	for _, ev := range all.queue {
		b, _ := json.Marshal(ev)
		reads = append(reads, string(b))
	}
	nEvents := len(all.queue)
	all.mu.Unlock()
	rl, _ := e.db.ListRequestLogs(store.RequestLogFilter{})
	rlb, _ := json.Marshal(rl)
	reads = append(reads, string(rlb), logBuf.String())

	for i, r := range reads {
		if strings.Contains(r, settingsSecret) {
			t.Fatalf("read path #%d leaks the secret: %.400s", i, r)
		}
	}
	if nEvents == 0 {
		t.Fatal("the audit saw no events (plugin.updated expected)")
	}
	// The one place it IS stored is the settings table (like provider credentials).
	rows, _ := e.db.ListPluginSettings("leaky")
	var stored bool
	for _, r := range rows {
		stored = stored || strings.Contains(r.Value, settingsSecret)
	}
	if !stored {
		t.Fatal("the secret must be stored for the plugin")
	}
}

// PluginEventTopicsAll is every topic the daemon publishes, for audits.
func PluginEventTopicsAll() []string {
	out := make([]string, 0, len(knownTopics))
	for t := range knownTopics {
		out = append(out, t)
	}
	return out
}

func (e *procEnv) json2(t *testing.T, method string, rc *requestContext, params any) string {
	t.Helper()
	res, err := e.call(method, rc, params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	b, _ := json.Marshal(res)
	return string(b)
}

func TestPluginSettings_RelayAndAgentRefused(t *testing.T) {
	setAllowRemote(t, true) // even with remote management on
	e := newProcEnv(t, procPlugin{id: "p", caps: `[]`, mode: "report", settings: settingsDecl})
	if err := setSettings(t, e, localRC(), "p", map[string]any{"token": settingsSecret}); err != nil {
		t.Fatal(err)
	}
	for name, rc := range map[string]*requestContext{
		"agent":        agentRC(),
		"relay":        relayRC(),
		"legacy relay": {local: false, channel: ChannelLoopback},
		"nil":          nil,
	} {
		err := setSettings(t, e, rc, "p", map[string]any{"token": "SENTINEL-overwrite-by-" + strings.ReplaceAll(name, " ", "-")})
		wantPluginForbidden(t, name+" plugin.settings.set", err)
	}
	// Nothing was overwritten.
	rows, _ := e.db.ListPluginSettings("p")
	if len(rows) != 1 || !strings.Contains(rows[0].Value, settingsSecret) {
		t.Fatalf("a refused set changed the store: %v", rows)
	}
	// A relay session may READ, and sees the secret redacted: set true, no value.
	got := e.json2(t, "plugin.settings.get", relayRC(), map[string]any{"id": "p"})
	var v struct {
		Settings []struct {
			Key    string `json:"key"`
			Set    bool   `json:"set"`
			Secret bool   `json:"secret"`
			Value  any    `json:"value"`
		} `json:"settings"`
	}
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, s := range v.Settings {
		if s.Key == "token" {
			seen = true
			if !s.Set || !s.Secret || s.Value != nil {
				t.Fatalf("relay read of a secret: %+v", s)
			}
		}
	}
	if !seen || strings.Contains(got, settingsSecret) {
		t.Fatalf("relay read: %s", got)
	}
	// An agent connection cannot read either (not on RunScopedMethods).
	if _, err := e.call("plugin.settings.get", agentRC(), map[string]any{"id": "p"}); err == nil {
		t.Fatal("an agent read plugin.settings.get")
	}
	for _, m := range RunScopedMethods {
		if strings.HasPrefix(m, "plugin.") {
			t.Fatalf("%s is on RunScopedMethods", m)
		}
	}
}

func TestPluginSettings_AllowRemoteManageHonoured(t *testing.T) {
	e := newProcEnv(t, procPlugin{id: "p", caps: `[]`, mode: "report", settings: settingsDecl})
	remote := &requestContext{local: true, channel: ChannelTLS}
	loop := &requestContext{local: true, channel: ChannelLoopback}

	setAllowRemote(t, false)
	err := setSettings(t, e, remote, "p", map[string]any{"name": "a"})
	wantPluginForbidden(t, "remote, off", err)
	if !strings.Contains(err.Error(), "plugins.allowRemoteManage") {
		t.Fatalf("the refusal must name the setting: %v", err)
	}
	if err := setSettings(t, e, loop, "p", map[string]any{"name": "loop"}); err != nil {
		t.Fatalf("loopback is allowed with the setting off: %v", err)
	}
	setAllowRemote(t, true)
	if err := setSettings(t, e, remote, "p", map[string]any{"name": "remote"}); err != nil {
		t.Fatalf("remote with the setting on: %v", err)
	}
	rows, _ := e.db.ListPluginSettings("p")
	if len(rows) != 1 || rows[0].Value != `"remote"` {
		t.Fatalf("stored: %v", rows)
	}
	// The relay and agents stay refused with the setting on (and reads are unaffected).
	wantPluginForbidden(t, "relay", setSettings(t, e, relayRC(), "p", map[string]any{"name": "x"}))
	if _, err := e.call("plugin.settings.get", remote, map[string]any{"id": "p"}); err != nil {
		t.Fatal(err)
	}
	var _ = host.APIVersion
}

// TestPluginMCP_SecretSubstitutionInRealDaemon: with NO injected provider (the
// real settings store), a placeholder MCP server is left out of runs until the
// Human sets the secret, then reaches the agent with it substituted, while
// every read path keeps it redacted.
func TestPluginMCP_SecretSubstitutionInRealDaemon(t *testing.T) {
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { log.SetOutput(os.Stderr); slog.SetDefault(prev) })

	e := newContribEnvOpt(t, false)
	id := e.ownedMcpID(t)

	// Before the Human sets it: listed, not injected, and plugin.get says why.
	pg := e.json(t, "plugin.get", localRC(), map[string]any{"id": "acme"})
	if !strings.Contains(pg, "not injected") || !strings.Contains(pg, "token") {
		t.Fatalf("an unset setting must show as a conflict: %s", pg)
	}
	env := &wsenv{srv: e.srv, db: e.db, ws: dialWS(t, e.srv, "tok")}
	t.Cleanup(func() { _ = env.ws.Close() })
	runID, _ := env.runPromptCollectFrames(t, e.task.ID)
	if b, err := os.ReadFile(e.dump); err == nil && strings.Contains(string(b), "acme__files") {
		t.Fatalf("a server with an unset secret must not be injected: %s", b)
	}
	_ = os.Remove(e.dump)

	// The Human sets it (local), through the real RPC.
	if _, err := e.call("plugin.settings.set", localRC(), map[string]any{"id": "acme", "values": map[string]any{"token": wsSecret}}); err != nil {
		t.Fatal(err)
	}
	var reads []string
	reads = append(reads,
		e.json(t, "mcp.list", localRC(), nil), e.json(t, "mcp.get", localRC(), map[string]any{"id": id}),
		e.json(t, "mcp.list", relayRC(), nil),
		e.json(t, "plugin.list", localRC(), nil), e.json(t, "plugin.get", localRC(), map[string]any{"id": "acme"}),
		e.json(t, "plugin.settings.get", relayRC(), map[string]any{"id": "acme"}),
	)
	runID, frames := env.runPromptCollectFrames(t, e.task.ID)
	dumped, err := os.ReadFile(e.dump)
	if err != nil || !strings.Contains(string(dumped), wsSecret) || !strings.Contains(string(dumped), "acme__files") {
		t.Fatalf("the agent must receive the owned server with the real secret: %s %v", dumped, err)
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
