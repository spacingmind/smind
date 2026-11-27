package config

import (
	"reflect"
	"strings"
	"testing"
)

// wantWritable is the writable set of ADR-0062 section 3, hardcoded here so
// the drift test fails when the table and the decision disagree.
var wantWritable = map[string]bool{
	"automations.enabled": true,
	"automations.paused":  true,
	"plugins.enabled":     true,
	"fleet.enabled":       true,
	"learning.enabled":    true,
}

// wantLoopbackOnly is the loopback-only set of ADR-0062 section 3.
var wantLoopbackOnly = map[string]bool{
	"server.port":      true,
	"server.listen":    true,
	"server.hostnames": true,
}

func Test_config_allowlist_drift(t *testing.T) {
	rows := ConfigKeys()
	if len(rows) != len(configKeys) {
		t.Fatalf("ConfigKeys() returned %d rows, the table has %d", len(rows), len(configKeys))
	}
	seen := make(map[string]bool, len(rows))
	gotWritable := map[string]bool{}
	gotLoopbackOnly := map[string]bool{}
	for _, ck := range rows {
		if seen[ck.Key] {
			t.Errorf("duplicate row %q", ck.Key)
		}
		seen[ck.Key] = true
		if ck.Kind == kindSecret {
			t.Errorf("%q is kindSecret: no secret may be in the table", ck.Key)
		}
		lower := strings.ToLower(ck.Key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
			t.Errorf("%q looks like a secret key", ck.Key)
		}
		ki, known := knownKeys[ck.Key]
		if !known && ck.Writable {
			t.Errorf("%q is neither in knownKeys nor read-only", ck.Key)
		}
		if ck.Writable {
			gotWritable[ck.Key] = true
			if !known {
				t.Errorf("writable %q is missing from knownKeys: config.Set would refuse it", ck.Key)
				continue
			}
			if ki.kind != kindBool {
				t.Errorf("writable %q has kind %v, want kindBool", ck.Key, ki.kind)
			}
		}
		if ck.LoopbackOnly {
			gotLoopbackOnly[ck.Key] = true
		}
		if ck.Writable && ck.Confirm == "" {
			t.Errorf("writable %q has no confirmation sentence", ck.Key)
		}
		if !ck.Writable && ck.Confirm != "" {
			t.Errorf("read-only %q has a confirmation sentence", ck.Key)
		}
	}
	if !reflect.DeepEqual(gotWritable, wantWritable) {
		t.Errorf("writable set = %v, want exactly %v", gotWritable, wantWritable)
	}
	if !reflect.DeepEqual(gotLoopbackOnly, wantLoopbackOnly) {
		t.Errorf("loopback-only set = %v, want exactly %v", gotLoopbackOnly, wantLoopbackOnly)
	}
	if _, ok := LookupConfigKey("no.such.key"); ok {
		t.Error("LookupConfigKey answered for a key outside the table")
	}
}

func Test_config_get_secret_keys_absent(t *testing.T) {
	useHome(t, "")
	for _, ck := range ConfigKeys() {
		lower := strings.ToLower(ck.Key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
			t.Errorf("%q must not be in the table", ck.Key)
		}
		if ck.Kind == kindSecret {
			t.Errorf("%q has kindSecret", ck.Key)
		}
	}
	for _, key := range []string{"server.passwordHash", "push.expoAccessToken"} {
		if _, ok := LookupConfigKey(key); ok {
			t.Errorf("%q is in the table", key)
		}
		if _, err := Effective(key); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("Effective(%q) = %v, want an unknown-key error naming it", key, err)
		}
	}
}

func Test_config_effective_source(t *testing.T) {
	for _, k := range []string{EnvLearning, EnvBrowserDisabled, EnvListen, EnvHostnames, EnvPassword} {
		t.Setenv(k, "")
	}

	// No file, no environment: the default.
	useHome(t, "")
	for key, want := range map[string]string{
		"automations.enabled": "false",
		"learning.enabled":    "true",
		"relay.maxDevices":    "16",
		"server.port":         "4648",
	} {
		ev, err := Effective(key)
		if err != nil {
			t.Fatalf("Effective(%q): %v", key, err)
		}
		if ev.Value != want || ev.Source != "default" || ev.Env != "" {
			t.Errorf("Effective(%q) = %+v, want value %q source default", key, ev, want)
		}
	}

	// A value in config.yaml: the file.
	useHome(t, "automations:\n  enabled: true\nfleet:\n  enabled: true\nlearning:\n  enabled: true\n")
	for _, key := range []string{"automations.enabled", "fleet.enabled", "learning.enabled"} {
		ev, err := Effective(key)
		if err != nil {
			t.Fatalf("Effective(%q): %v", key, err)
		}
		if ev.Value != "true" || ev.Source != "file" || ev.Env != "" {
			t.Errorf("Effective(%q) = %+v, want value \"true\" source file", key, ev)
		}
	}

	// The environment wins over the file, and only its NAME is reported.
	t.Setenv(EnvLearning, "0")
	ev, err := Effective("learning.enabled")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Value != "false" || ev.Source != "env" || ev.Env != EnvLearning {
		t.Errorf("Effective(learning.enabled) = %+v, want value false source env env %q", ev, EnvLearning)
	}
	if strings.Contains(ev.Value, "SMIND") {
		t.Errorf("the environment variable's value leaked: %+v", ev)
	}
}
