package wsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
)

const configSetYAML = "# my config, hand written\nserver:\n  port: 4648 # keep\nplugins:\n  enabled: false\nunrelated:\n  keep: me\n"

// configSetEnv is a real API with a temp SMIND_HOME holding a commented
// config.yaml; set calls the registered config.set handler.
type configSetEnv struct {
	api  *API
	home string
}

func newConfigSetEnv(t *testing.T) *configSetEnv {
	t.Helper()
	e := newProcEnv(t)
	if err := os.WriteFile(filepath.Join(e.home, "config.yaml"), []byte(configSetYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return &configSetEnv{api: e.api, home: e.home}
}

func (e *configSetEnv) set(rc *requestContext, key string, value any) (any, error) {
	raw, _ := json.Marshal(map[string]any{"key": key, "value": value})
	return e.api.hs["config.set"](context.Background(), rc, raw)
}

func (e *configSetEnv) file(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e *configSetEnv) audit(t *testing.T) []config.AuditEntry {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.home, "config-audit.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []config.AuditEntry
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		var a config.AuditEntry
		if err := json.Unmarshal([]byte(l), &a); err != nil {
			t.Fatalf("bad audit line %q: %v", l, err)
		}
		out = append(out, a)
	}
	return out
}

// refused asserts the call failed and config.yaml is byte-identical.
func (e *configSetEnv) refused(t *testing.T, rc *requestContext, key string, value any, wantMsg string) {
	t.Helper()
	before := e.file(t)
	res, err := e.set(rc, key, value)
	if err == nil || res != nil {
		t.Fatalf("config.set %s = %v: got (%v, %v), want a refusal", key, value, res, err)
	}
	if wantMsg != "" && !strings.Contains(err.Error(), wantMsg) {
		t.Errorf("error %q does not contain %q", err, wantMsg)
	}
	if !bytes.Equal(before, e.file(t)) {
		t.Errorf("config.yaml changed by a refused config.set %s", key)
	}
}

func Test_config_set_refuses_run_scoped_caller(t *testing.T) {
	e := newConfigSetEnv(t)
	// An agent run reaches the daemon over loopback, so the loopback guard alone
	// must not be what refuses it: this isolates the run-scoped guard.
	e.refused(t, &requestContext{local: true, runScoped: true, channel: ChannelLoopback}, "plugins.enabled", true, "not available to agents")
}

func Test_config_set_refuses_relay_caller(t *testing.T) {
	e := newConfigSetEnv(t)
	// local=false with a loopback channel isolates the relay guard.
	e.refused(t, &requestContext{local: false, deviceID: "dev-1", channel: ChannelLoopback}, "plugins.enabled", true, "not over the relay")
	e.refused(t, relayRC(), "plugins.enabled", true, "not over the relay")
}

func Test_config_set_refuses_non_loopback_login(t *testing.T) {
	e := newConfigSetEnv(t)
	for _, ch := range []Channel{ChannelPlain, ChannelTLS} {
		e.refused(t, &requestContext{local: true, channel: ch}, "plugins.enabled", true, "loopback")
	}
}

func Test_config_set_refused_for_plugin_session(t *testing.T) {
	e := newProcEnv(t)
	reg, calls := newSpyRegistry(t, e)
	sess := openAll(reg)
	defer sess.Close()
	for _, m := range []string{"config.set", "config.get"} {
		res, rerr := sess.Request(context.Background(), m, json.RawMessage(`{"key":"plugins.enabled","value":true}`))
		if rerr == nil || res != nil {
			t.Errorf("%s resolved for a plugin session: %v", m, res)
		} else if !strings.Contains(rerr.Message, "never available to plugins") {
			// The never-prefix must be what refuses it, not "unknown method".
			t.Errorf("%s refused for the wrong reason: %q", m, rerr.Message)
		}
		if n := calls[m]; n != nil && *n != 0 {
			t.Errorf("%s: its handler ran", m)
		}
	}
}

func Test_config_methods_not_in_run_scoped_or_mcp(t *testing.T) {
	for _, m := range []string{"config.get", "config.set"} {
		if slices.Contains(RunScopedMethods, m) {
			t.Errorf("%s is on RunScopedMethods", m)
		}
	}
	files, err := filepath.Glob("../../cmd/smind/mcp*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no MCP sources found: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range []string{`"config.get"`, `"config.set"`, `"config_get"`, `"config_set"`} {
			if strings.Contains(string(b), m) {
				t.Errorf("%s names %s", f, m)
			}
		}
	}
}

func Test_config_set_refuses_key_outside_allowlist(t *testing.T) {
	e := newConfigSetEnv(t)
	for _, k := range []string{"providers.glm.defaultModel", "server.passwordHash", "nope.nope"} {
		e.refused(t, localRC(), k, "x", "unknown key")
	}
}

func Test_config_set_refuses_read_only_key(t *testing.T) {
	e := newConfigSetEnv(t)
	n := 0
	for _, ck := range config.ConfigKeys() {
		if ck.Writable {
			continue
		}
		n++
		e.refused(t, localRC(), ck.Key, "true", "read-only over the API")
	}
	if n == 0 {
		t.Fatal("no read-only rows in the table")
	}
}

func Test_config_set_refuses_secret_keys(t *testing.T) {
	e := newConfigSetEnv(t)
	for _, k := range []string{"server.passwordHash", "providers.glm.secret", "relay.token", "account.token"} {
		e.refused(t, localRC(), k, "hunter2", "unknown key")
	}
	for _, a := range e.audit(t) {
		if a.Old == "hunter2" || a.New == "hunter2" {
			t.Errorf("audit leaked a submitted secret: %+v", a)
		}
	}
}

func Test_config_set_refuses_invalid_value(t *testing.T) {
	e := newConfigSetEnv(t)
	e.refused(t, localRC(), "plugins.enabled", "maybe", "wants true or false")
	e.refused(t, localRC(), "plugins.enabled", 7, "wants true or false")
}

func Test_config_set_refuses_env_overridden_key(t *testing.T) {
	e := newConfigSetEnv(t)
	t.Setenv("SMIND_LEARNING", "0")
	e.refused(t, localRC(), "learning.enabled", true, "overridden by SMIND_LEARNING, unset it first")
}

func Test_config_set_preserves_comments_and_unrelated_keys(t *testing.T) {
	e := newConfigSetEnv(t)
	if _, err := e.set(localRC(), "plugins.enabled", true); err != nil {
		t.Fatal(err)
	}
	got := string(e.file(t))
	for _, want := range []string{"# my config, hand written", "port: 4648 # keep", "unrelated:", "keep: me", "enabled: true"} {
		if !strings.Contains(got, want) {
			t.Errorf("config.yaml lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "enabled: false") {
		t.Errorf("old value still present:\n%s", got)
	}
}

func Test_config_set_concurrent_calls_do_not_lose_updates(t *testing.T) {
	e := newConfigSetEnv(t)
	var keys []string
	for _, ck := range config.ConfigKeys() {
		if ck.Writable {
			keys = append(keys, ck.Key)
		}
	}
	var wg sync.WaitGroup
	for _, k := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.set(localRC(), k, "true"); err != nil {
				t.Errorf("%s: %v", k, err)
			}
		}()
	}
	wg.Wait()
	for _, k := range keys {
		if v, err := config.Get(k); err != nil || v != "true" {
			t.Errorf("%s = %q (%v), want true: an update was lost", k, v, err)
		}
	}
}

func Test_config_set_result_is_reread_after_write(t *testing.T) {
	e := newConfigSetEnv(t)
	path := filepath.Join(e.home, "config.yaml")
	configAfterWrite = func() {
		if err := os.WriteFile(path, []byte("plugins:\n  enabled: false\n"), 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { configAfterWrite = nil })
	res, err := e.set(localRC(), "plugins.enabled", true)
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["value"] != "false" {
		t.Fatalf("response value = %v, want the re-read file value false (not the submitted true)", m["value"])
	}
	if a := e.audit(t); len(a) != 1 || a[0].New != "false" {
		t.Errorf("audit new = %+v, want the re-read value", a)
	}
}

func Test_config_set_writes_audit_line_with_old_and_new(t *testing.T) {
	e := newConfigSetEnv(t)
	if _, err := e.set(localRC(), "fleet.enabled", true); err != nil {
		t.Fatal(err)
	}
	a := e.audit(t)
	if len(a) != 1 {
		t.Fatalf("audit lines = %d, want 1", len(a))
	}
	got := a[0]
	if got.Key != "fleet.enabled" || got.Old != "false" || got.New != "true" || got.Outcome != "ok" || got.Caller != "human" || got.Channel != "loopback" || got.Time == "" {
		t.Errorf("audit line = %+v", got)
	}
	if st, err := os.Stat(filepath.Join(e.home, "config-audit.jsonl")); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("audit file: %v %v", st, err)
	}
}

func Test_config_refusal_is_audited_with_caller_class(t *testing.T) {
	e := newConfigSetEnv(t)
	cases := []struct {
		rc      *requestContext
		caller  string
		outcome string
	}{
		{agentRC(), "run", "refused: run-scoped"},
		{relayRC(), "relay", "refused: relay"},
		{&requestContext{local: true, channel: ChannelTLS}, "remote", "refused: non-loopback"},
	}
	for _, c := range cases {
		if _, err := e.set(c.rc, "plugins.enabled", "SENTINEL-new-value"); err == nil {
			t.Fatalf("%s: not refused", c.caller)
		}
	}
	// The plugin session is a fourth caller class.
	pe := newProcEnv(t)
	reg, _ := newSpyRegistry(t, pe)
	sess := openAll(reg)
	defer sess.Close()
	if _, rerr := sess.Request(context.Background(), "config.set", json.RawMessage(`{}`)); rerr == nil {
		t.Fatal("plugin session was not refused")
	}
	pluginAudit := pe.home

	got := e.audit(t)
	if len(got) != len(cases) {
		t.Fatalf("audit lines = %d, want %d: %+v", len(got), len(cases), got)
	}
	for i, c := range cases {
		if got[i].Caller != c.caller || got[i].Outcome != c.outcome {
			t.Errorf("line %d = %+v, want caller %s outcome %s", i, got[i], c.caller, c.outcome)
		}
		if strings.Contains(got[i].New+got[i].Old, "SENTINEL") {
			t.Errorf("line %d leaked the attempted value", i)
		}
	}
	pl, err := os.ReadFile(filepath.Join(pluginAudit, "config-audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pl), `"caller":"plugin"`) || !strings.Contains(string(pl), "refused: plugin session") {
		t.Errorf("plugin refusal not audited: %s", pl)
	}
}

func Test_config_updated_event_emitted_on_success_only(t *testing.T) {
	e := newConfigSetEnv(t)
	sub := newSubscriber()
	sub.subscribe([]string{TopicConfigUpdated})
	e.api.bus.register(sub)
	t.Cleanup(func() { e.api.bus.unregister(sub); sub.close() })

	e.set(agentRC(), "plugins.enabled", true)
	e.set(relayRC(), "plugins.enabled", true)
	e.set(localRC(), "plugins.enabled", "maybe")
	e.set(localRC(), "server.port", "1")
	e.set(localRC(), "nope.nope", "1")
	if ev, _, ok := sub.next(); ok {
		t.Fatalf("a refusal published %+v", ev)
	}
	if _, err := e.set(localRC(), "plugins.enabled", true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !sub.wait(ctx) {
		t.Fatal("no config.updated event after a successful config.set")
	}
	ev, _, ok := sub.next()
	if !ok || ev.Topic != TopicConfigUpdated {
		t.Fatalf("event = %+v ok=%v", ev, ok)
	}
	if b, _ := json.Marshal(ev.Payload); string(b) != `{"key":"plugins.enabled"}` {
		t.Errorf("payload = %s", b)
	}
	if _, _, ok := sub.next(); ok {
		t.Error("more than one event for one config.set")
	}
}

// Test_human_only_rpcs_drift: config.set has a handler and is not run-scoped.
// (Its HumanOnlyRPCs entry is reconciled where the permpolicy list is edited;
// this pin covers the wsapi side only.)
func Test_human_only_rpcs_drift(t *testing.T) {
	e := newProcEnv(t)
	if _, ok := e.api.hs["config.set"]; !ok {
		t.Error("config.set has no handler")
	}
	if slices.Contains(RunScopedMethods, "config.set") {
		t.Error("config.set is on RunScopedMethods")
	}
}

// Test_config_set_audit_chain_is_consistent_under_concurrency hammers ONE key
// from many goroutines: the handler's mutex makes read-old, write and audit one
// step, so each audit line's old value is the previous line's new value. With
// the handler mutex gone the old value is read before a rival's write lands and
// the chain breaks.
func Test_config_set_audit_chain_is_consistent_under_concurrency(t *testing.T) {
	e := newConfigSetEnv(t)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 6; j++ {
				v := (i+j)%2 == 0
				if _, err := e.set(localRC(), "plugins.enabled", v); err != nil {
					t.Errorf("set: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	lines := e.audit(t)
	if len(lines) != 24*6 {
		t.Fatalf("audit lines = %d, want %d", len(lines), 24*6)
	}
	for i := 1; i < len(lines); i++ {
		if lines[i].Old != lines[i-1].New {
			t.Fatalf("audit line %d old = %q but line %d new = %q: a rival write slipped between read and write", i, lines[i].Old, i-1, lines[i-1].New)
		}
	}
}
