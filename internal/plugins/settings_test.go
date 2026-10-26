package plugins

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/plugins/host/hosttest"
	"github.com/spacingmind/smind/internal/store"
)

const settingsDecl = `"settings":[
	{"key":"token","type":"secret","label":"Token","required":true,"maxLength":40},
	{"key":"region","type":"enum","label":"Region","options":["eu","us"],"default":"eu"},
	{"key":"name","type":"string","label":"Name","maxLength":8},
	{"key":"retries","type":"number","label":"Retries","default":3},
	{"key":"verbose","type":"boolean","label":"Verbose"}]`

func settingsPlugin(t *testing.T, e *env) Info {
	t.Helper()
	src := writePluginDir(t, manifestWith("demo", "1.0.0", settingsDecl), nil)
	in, err := e.m.Install(bg, src)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func put(t *testing.T, m *Manager, patch string) (SettingsView, error) {
	t.Helper()
	var p map[string]json.RawMessage
	if err := json.Unmarshal([]byte(patch), &p); err != nil {
		t.Fatal(err)
	}
	return m.PutSettings("demo", p)
}

func TestPluginSettings_TypedValidation(t *testing.T) {
	e := newEnv(t)
	settingsPlugin(t, e)
	const secret = "SENTINEL-typed-secret-91"

	view, err := e.m.Settings("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Missing) != 1 || view.Missing[0] != "token" {
		t.Fatalf("a required setting with no value or default is missing: %+v", view)
	}

	bad := map[string]string{
		"string for number":     `{"retries":"3"}`,
		"number for string":     `{"name":5}`,
		"enum outside options":  `{"region":"asia"}`,
		"string over its cap":   `{"name":"123456789"}`,
		"secret over its cap":   `{"token":"` + strings.Repeat("x", 41) + `"}`,
		"boolean from string":   `{"verbose":"true"}`,
		"undeclared key":        `{"nope":1}`,
		"clear a required":      `{"token":null}`,
		"nul in a string":       `{"name":"a\u0000b"}`,
		"huge number":           `{"retries":1e300}`,
		"one bad key voids all": `{"name":"ok","retries":"no"}`,
	}
	for name, patch := range bad {
		if _, err := put(t, e.m, patch); err == nil {
			t.Errorf("%s: accepted %s", name, patch)
		} else if CodeOf(err) != CodeBadSetting {
			t.Errorf("%s: code %q, want bad_setting (%v)", name, CodeOf(err), err)
		}
	}
	if v, _ := e.m.Settings("demo"); v.Settings[2].Set {
		t.Fatal("a rejected patch must change nothing (name was set by the all-or-nothing case)")
	}
	// An error never echoes the offending value (it may be a secret).
	_, err = put(t, e.m, `{"token":`+strings.Repeat("9", 5)+`}`)
	if err == nil || strings.Contains(err.Error(), "99999") {
		t.Fatalf("error must name the key, not the value: %v", err)
	}

	view, err = put(t, e.m, `{"token":"`+secret+`","region":"us","name":"abc","retries":7.5,"verbose":true}`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]SettingView{}
	for _, s := range view.Settings {
		got[s.Key] = s
	}
	if !got["token"].Set || got["token"].Value != nil || !got["token"].Secret {
		t.Fatalf("a secret is set:true with no value: %+v", got["token"])
	}
	if got["region"].Value != "us" || got["retries"].Value != 7.5 || got["verbose"].Value != true || got["name"].Value != "abc" {
		t.Fatalf("view: %+v", view)
	}
	if b, _ := json.Marshal(view); strings.Contains(string(b), secret) {
		t.Fatalf("view leaks the secret: %s", b)
	}
	if len(view.Missing) != 0 {
		t.Fatalf("missing after set: %v", view.Missing)
	}

	// null clears back to the default.
	view, err = put(t, e.m, `{"region":null,"retries":null}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range view.Settings {
		if (s.Key == "region" || s.Key == "retries") && (s.Set || s.Value != nil) {
			t.Fatalf("%s not cleared: %+v", s.Key, s)
		}
	}
	// The resolved object (what initialize carries) has defaults for them.
	man, _, err := e.m.loadInstalled(mustRow(t, e.m, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	r := e.m.resolved("demo", man)
	if r["region"] != "eu" || r["retries"] != 3.0 || r["token"] != secret {
		t.Fatalf("resolved = %v", r)
	}

	// Removing the plugin deletes its settings, keepData or not.
	if err := e.m.Remove("demo", true); err != nil {
		t.Fatal(err)
	}
	if rows, _ := e.st.ListPluginSettings("demo"); len(rows) != 0 {
		t.Fatalf("remove left settings behind: %v", rows)
	}
}

func TestManifest_SettingsDeclarations(t *testing.T) {
	for name, decl := range map[string]string{
		"secret with a default": `"settings":[{"key":"k","type":"secret","label":"K","default":"hunter2hunter2"}]`,
		"enum without options":  `"settings":[{"key":"k","type":"enum","label":"K"}]`,
		"options on a string":   `"settings":[{"key":"k","type":"string","label":"K","options":["a"]}]`,
		"default of wrong type": `"settings":[{"key":"k","type":"number","label":"K","default":"x"}]`,
		"default outside enum":  `"settings":[{"key":"k","type":"enum","label":"K","options":["a"],"default":"b"}]`,
		"maxLength on boolean":  `"settings":[{"key":"k","type":"boolean","label":"K","maxLength":4}]`,
		"unknown type":          `"settings":[{"key":"k","type":"password","label":"K"}]`,
	} {
		if _, err := ParseManifest([]byte(manifestWith("demo", "1.0.0", decl)), "0.15.0"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func mustRow(t *testing.T, m *Manager, id string) store.Plugin {
	t.Helper()
	p, err := m.row(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestInitialize_CarriesDecryptedSettings: the process's plugin.initialize
// carries the secret in the clear (it is the plugin's own), defaults for what
// is unset, and a settings change restarts it with the new values.
func TestInitialize_CarriesDecryptedSettings(t *testing.T) {
	e := newHostEnv(t, host.DenyAll)
	cmd, _ := json.Marshal(hosttest.Command("report"))
	src := writePluginDir(t, manifestWith("demo", "1.0.0", `"process":{"command":`+string(cmd)+`},`+settingsDecl), nil)
	in, err := e.m.Install(bg, src)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "SENTINEL-init-secret-55"
	if _, err := put(t, e.m, `{"token":"`+secret+`","name":"abc"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Enable("demo", in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	st := waitProc(t, e.m, "demo", host.StateRunning)

	getInit := func() map[string]any {
		raw, err := e.m.CallProcess(bg, "demo", "getinit", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var init struct{ Settings map[string]any }
		if err := json.Unmarshal(raw, &init); err != nil {
			t.Fatal(err)
		}
		return init.Settings
	}
	s := getInit()
	if s["token"] != secret || s["name"] != "abc" || s["region"] != "eu" || s["retries"] != 3.0 {
		t.Fatalf("initialize settings = %v", s)
	}
	if _, set := s["verbose"]; set {
		t.Fatalf("an unset setting without a default is absent: %v", s)
	}

	// A change restarts the process, which initializes with the new values.
	if _, err := put(t, e.m, `{"region":"us"}`); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "restart with new settings", func() bool {
		st2, ok := e.m.ProcessStatus("demo")
		return ok && st2.State == host.StateRunning && st2.PID != st.PID
	})
	time.Sleep(20 * time.Millisecond)
	if s := getInit(); s["region"] != "us" || s["token"] != secret {
		t.Fatalf("after the change: %v", s)
	}
	// None of it is in plugin.get / the logs.
	info, _ := e.m.Get("demo")
	b, _ := json.Marshal(info)
	logs, _ := e.m.Logs("demo", 1000)
	lb, _ := json.Marshal(logs)
	if strings.Contains(string(b)+string(lb), secret) {
		t.Fatalf("secret leaked into plugin.get or the logs: %s %s", b, lb)
	}
}
