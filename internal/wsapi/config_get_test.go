package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configGetHome(t *testing.T, yaml string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	for _, k := range []string{"SMIND_LEARNING", "SMIND_BROWSER_DISABLED", "SMIND_LISTEN", "SMIND_HOSTNAMES", "SMIND_PASSWORD"} {
		t.Setenv(k, "")
	}
	if yaml != "" {
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func callConfigGet(t *testing.T, rc *requestContext, keys ...string) ([]configKeyView, error) {
	t.Helper()
	raw := json.RawMessage(nil)
	if len(keys) > 0 {
		raw, _ = json.Marshal(map[string]any{"keys": keys})
	}
	res, err := handleConfigGet()(context.Background(), rc, raw)
	if err != nil {
		return nil, err
	}
	return res.(map[string]any)["keys"].([]configKeyView), nil
}

func byKey(rows []configKeyView) map[string]configKeyView {
	m := map[string]configKeyView{}
	for _, r := range rows {
		m[r.Key] = r
	}
	return m
}

func Test_config_get_returns_source_and_restart_flags(t *testing.T) {
	configGetHome(t, "fleet:\n  enabled: true\n")
	t.Setenv("SMIND_LEARNING", "0")
	rows, err := callConfigGet(t, localRC())
	if err != nil {
		t.Fatal(err)
	}
	m := byKey(rows)
	if f := m["fleet.enabled"]; f.Source != "file" || f.Value != "true" || !f.Restart || !f.Writable {
		t.Errorf("fleet.enabled = %+v", f)
	}
	if l := m["learning.enabled"]; l.Source != "env" || l.Env != "SMIND_LEARNING" || l.Value != "false" || !l.Restart || l.Writable {
		t.Errorf("learning.enabled = %+v", l)
	}
	if a := m["automations.enabled"]; a.Source != "default" || a.Value != "false" || a.Env != "" {
		t.Errorf("automations.enabled = %+v", a)
	}
	if m["relay.maxDevices"].Writable {
		t.Error("a read-only key reported writable")
	}
	if _, err := callConfigGet(t, localRC(), "no.such.key"); err == nil || !strings.Contains(err.Error(), `unknown key "no.such.key"`) {
		t.Errorf("unknown key error = %v", err)
	}
}

func Test_config_get_hides_server_keys_over_relay(t *testing.T) {
	configGetHome(t, "")
	server := []string{"server.port", "server.listen", "server.hostnames"}
	for name, rc := range map[string]*requestContext{
		"relay": relayRC(),
		"plain": {local: true, channel: ChannelPlain},
	} {
		rows, err := callConfigGet(t, rc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		m := byKey(rows)
		for _, k := range server {
			if _, ok := m[k]; ok {
				t.Errorf("%s: default list leaked %s", name, k)
			}
			if _, err := callConfigGet(t, rc, k); err == nil || !strings.Contains(err.Error(), "only available on a local connection") {
				t.Errorf("%s: naming %s = %v", name, k, err)
			}
		}
		if len(m) == 0 {
			t.Errorf("%s: no keys at all", name)
		}
		if m["fleet.enabled"].Writable {
			t.Errorf("%s: writable reported off loopback", name)
		}
	}
	rows, err := callConfigGet(t, localRC())
	if err != nil {
		t.Fatal(err)
	}
	m := byKey(rows)
	for _, k := range server {
		if _, ok := m[k]; !ok {
			t.Errorf("loopback missing %s", k)
		}
	}
}

func Test_config_get_refuses_run_scoped_caller(t *testing.T) {
	configGetHome(t, "")
	if _, err := callConfigGet(t, agentRC()); err == nil || !strings.Contains(err.Error(), "not available to agents") {
		t.Errorf("agent err = %v", err)
	}
	if _, err := handleConfigGet()(context.Background(), nil, nil); err == nil {
		t.Error("nil request context accepted")
	}
}

func Test_config_get_secret_keys_absent_over_rpc(t *testing.T) {
	configGetHome(t, "server:\n  passwordHash: abc\n")
	rows, err := callConfigGet(t, localRC())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		l := strings.ToLower(r.Key)
		if strings.Contains(l, "password") || strings.Contains(l, "token") || strings.Contains(l, "secret") {
			t.Errorf("secret-looking key listed: %s", r.Key)
		}
		if strings.Contains(r.Value, "abc") {
			t.Errorf("secret value leaked in %s", r.Key)
		}
	}
	if _, err := callConfigGet(t, localRC(), "server.passwordHash"); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("passwordHash err = %v", err)
	}
}
