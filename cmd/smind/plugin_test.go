package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/plugins"
)

type pluginCall struct {
	method string
	params map[string]any
}

type fakePluginDaemon struct {
	calls    []pluginCall
	handlers map[string]func(p map[string]any) (any, error)
}

func (f *fakePluginDaemon) Close() error { return nil }
func (f *fakePluginDaemon) Call(_ context.Context, method string, params, out any) error {
	raw, _ := json.Marshal(params)
	var p map[string]any
	_ = json.Unmarshal(raw, &p)
	f.calls = append(f.calls, pluginCall{method, p})
	h := f.handlers[method]
	if h == nil {
		return errors.New("unexpected method " + method)
	}
	res, err := h(p)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(res)
	return json.Unmarshal(b, out)
}

func (f *fakePluginDaemon) called(method string) bool {
	for _, c := range f.calls {
		if c.method == method {
			return true
		}
	}
	return false
}

func runPluginCLI(t *testing.T, f *fakePluginDaemon, stdin string, interactive bool, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = runPlugin(args, pluginIO{
		in: strings.NewReader(stdin), out: &o, errOut: &e, interactive: interactive,
		dial: func(context.Context) (daemonCaller, error) { return f, nil },
	})
	return code, o.String(), e.String()
}

func sampleInfo() plugins.Info {
	return plugins.Info{
		ID: "acme", Name: "Acme", Version: "1.2.0", SourceKind: "dir", Source: "/src/acme",
		Consent: plugins.Consent{
			ID: "acme", Name: "Acme", Version: "1.2.0", Source: "/src/acme", SourceKind: "dir", Hash: "abc123",
			Process: []string{"node", "server.js"}, Capabilities: []string{"tasks.read", "tasks.write"},
			HookEvents:  []plugins.HookEvent{{Event: "run.finished", OnError: "open"}},
			McpServers:  []plugins.McpEntry{{Name: "search", Transport: "stdio", Command: []string{"node", "mcp.js"}}},
			Fingerprint: "fp-1",
		},
	}
}

func TestPluginInstall_PrintsConsentSummaryAndAbsolutizesPath(t *testing.T) {
	f := &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
		"plugin.install": func(map[string]any) (any, error) { return sampleInfo(), nil },
	}}
	code, out, errOut := runPluginCLI(t, f, "", false, "install", "rel/dir")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"disabled", "node server.js", "YES", "tasks.write", "run.finished", "node mcp.js", "/src/acme", "abc123", "smind plugin enable acme"} {
		if !strings.Contains(out, want) {
			t.Errorf("consent summary lacks %q:\n%s", want, out)
		}
	}
	src, _ := f.calls[0].params["source"].(string)
	if !filepath.IsAbs(src) || !strings.HasSuffix(filepath.ToSlash(src), "rel/dir") {
		t.Fatalf("a relative path must be sent absolute (the daemon has another cwd): %q", src)
	}
	// Git and https sources pass through untouched.
	f.calls = nil
	runPluginCLI(t, f, "", false, "install", "git+https://example.com/r.git@"+strings.Repeat("a", 40))
	if got := f.calls[0].params["source"]; got != "git+https://example.com/r.git@"+strings.Repeat("a", 40) {
		t.Fatalf("%v", got)
	}
}

func TestEnable_RequiresConfirmation_CLI(t *testing.T) {
	mk := func() *fakePluginDaemon {
		return &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
			"plugin.get":    func(map[string]any) (any, error) { return sampleInfo(), nil },
			"plugin.enable": func(map[string]any) (any, error) { in := sampleInfo(); in.Enabled = true; return in, nil },
		}}
	}
	// Non-interactive without --yes: refused, and the daemon is never asked to enable.
	f := mk()
	code, out, errOut := runPluginCLI(t, f, "y\n", false, "enable", "acme")
	if code != 1 || f.called("plugin.enable") || !strings.Contains(errOut, "--yes") {
		t.Fatalf("exit %d, enable called %v, stderr %q", code, f.called("plugin.enable"), errOut)
	}
	if !strings.Contains(out, "node server.js") {
		t.Fatalf("the summary must be shown before refusing:\n%s", out)
	}
	// --yes confirms the exact summary shown (its fingerprint).
	f = mk()
	if code, _, e := runPluginCLI(t, f, "", false, "enable", "acme", "--yes"); code != 0 {
		t.Fatalf("%d %s", code, e)
	}
	last := f.calls[len(f.calls)-1]
	if last.method != "plugin.enable" || last.params["confirm"] != "fp-1" || last.params["id"] != "acme" {
		t.Fatalf("%+v", last)
	}
	// Interactive: y enables, anything else does not.
	f = mk()
	if code, _, _ := runPluginCLI(t, f, "y\n", true, "enable", "acme"); code != 0 || !f.called("plugin.enable") {
		t.Fatal("interactive y did not enable")
	}
	for _, ans := range []string{"n\n", "\n", "maybe\n", ""} {
		f = mk()
		if code, _, _ := runPluginCLI(t, f, ans, true, "enable", "acme"); code != 1 || f.called("plugin.enable") {
			t.Fatalf("answer %q enabled the plugin", ans)
		}
	}
}

func TestPluginList_ReportsGlobalSwitch(t *testing.T) {
	f := &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
		"plugin.list": func(map[string]any) (any, error) {
			in := sampleInfo()
			in.Enabled = true
			return plugins.Listing{GlobalEnabled: false, Plugins: []plugins.Info{in}}, nil
		},
	}}
	code, out, _ := runPluginCLI(t, f, "", false, "list")
	if code != 0 || !strings.Contains(out, "Plugins are OFF") || !strings.Contains(out, "enabled (switch off)") {
		t.Fatalf("%d\n%s", code, out)
	}
}

func TestPluginUpdate_PendingNeedsConfirmation_CLI(t *testing.T) {
	cur := sampleInfo()
	cur.Enabled = true
	next := sampleInfo().Consent
	next.Version, next.Fingerprint = "2.0.0", "fp-2"
	pend := cur
	pend.Pending = &plugins.Pending{Consent: next}
	mk := func() *fakePluginDaemon {
		return &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
			"plugin.get": func(map[string]any) (any, error) { return cur, nil },
			"plugin.update": func(p map[string]any) (any, error) {
				if p["apply"] == true {
					up := sampleInfo()
					up.Version = "2.0.0"
					return plugins.UpdateResult{Applied: true, Info: up}, nil
				}
				return plugins.UpdateResult{Pending: true, Info: pend}, nil
			},
		}}
	}
	f := mk()
	code, out, _ := runPluginCLI(t, f, "", false, "update", "acme")
	applied := false
	for _, c := range f.calls {
		applied = applied || c.params["apply"] == true
	}
	if code != 1 || applied || !strings.Contains(out, "keeps running") || !strings.Contains(out, "2.0.0") {
		t.Fatalf("a pending update must not apply without confirmation (exit %d):\n%s", code, out)
	}
	f = mk()
	if code, _, e := runPluginCLI(t, f, "", false, "update", "acme", "--yes"); code != 0 {
		t.Fatalf("%d %s", code, e)
	}
	if last := f.calls[len(f.calls)-1]; last.params["apply"] != true {
		t.Fatalf("%+v", last)
	}
	// A staged pending update is applied as staged, not re-fetched.
	cur.Pending = &plugins.Pending{Consent: next}
	f = mk()
	runPluginCLI(t, f, "", false, "update", "acme", "--yes")
	for _, c := range f.calls {
		if c.method == "plugin.update" && c.params["apply"] != true {
			t.Fatalf("re-staged instead of applying: %+v", c)
		}
	}
}

func TestPluginCLI_UsageAndErrors(t *testing.T) {
	f := &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
		"plugin.remove": func(map[string]any) (any, error) { return map[string]any{"id": "acme"}, nil },
	}}
	if code, _, _ := runPluginCLI(t, f, "", false); code != 2 {
		t.Fatal(code)
	}
	if code, _, _ := runPluginCLI(t, f, "", false, "frobnicate"); code != 2 {
		t.Fatal(code)
	}
	if code, _, _ := runPluginCLI(t, f, "", false, "remove"); code != 2 {
		t.Fatal(code)
	}
	if code, _, _ := runPluginCLI(t, f, "", false, "remove", "acme", "--bogus"); code != 2 {
		t.Fatal(code)
	}
	code, out, _ := runPluginCLI(t, f, "", false, "remove", "acme", "--keep-data")
	if code != 0 || f.calls[len(f.calls)-1].params["keepData"] != true || !strings.Contains(out, "kept") {
		t.Fatalf("%d %s %+v", code, out, f.calls)
	}
	f.handlers["plugin.remove"] = func(map[string]any) (any, error) { return nil, errors.New("forbidden: nope") }
	if code, _, e := runPluginCLI(t, f, "", false, "remove", "acme"); code != 1 || !strings.Contains(e, "forbidden") {
		t.Fatalf("%d %s", code, e)
	}
}

func TestPluginEnable_LinkedPrintsWarning_ProfilesFlagged(t *testing.T) {
	in := sampleInfo()
	in.SourceKind = "link"
	in.Consent.SourceKind = "link"
	in.Consent.LinkWarning = plugins.LinkWarningText
	in.Consent.Skills = []string{"review"}
	in.Consent.Profiles = []plugins.ProfileEntry{{Name: "boss", Provider: "claude-native", Escalations: []string{`permission mode "bypassPermissions" skips permission prompts`}}}
	f := &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
		"plugin.get":    func(map[string]any) (any, error) { return in, nil },
		"plugin.enable": func(map[string]any) (any, error) { return in, nil },
	}}
	code, out, errOut := runPluginCLI(t, f, "", false, "enable", "acme", "--yes")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	// Shown before confirming and again once enabled.
	if strings.Count(out, plugins.LinkWarningText) != 2 {
		t.Fatalf("the link warning must be printed with the consent summary and after enabling:\n%s", out)
	}
	for _, want := range []string{"Skill:        acme:review", "Profile:      acme:boss on claude-native", "ATTENTION:  permission mode"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	// A copied plugin prints no warning.
	plain := sampleInfo()
	f2 := &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
		"plugin.get":    func(map[string]any) (any, error) { return plain, nil },
		"plugin.enable": func(map[string]any) (any, error) { return plain, nil },
	}}
	if _, out, _ := runPluginCLI(t, f2, "", false, "enable", "acme", "--yes"); strings.Contains(out, "live directory") {
		t.Fatalf("no warning for a copied plugin:\n%s", out)
	}
}
