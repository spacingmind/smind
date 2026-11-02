package taskrunner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
)

const (
	pluginSecretValue = "SENTINEL-plugin-secret-7741"
	otherSecretValue  = "SENTINEL-other-provider-secret-1903"
	daemonLeakValue   = "SENTINEL-daemon-env-leak-5520"
)

type pluginFixture struct {
	r      *Runner
	task   store.Task
	dir    string // PluginRoot
	spec   providers.Spec
	reg    *providers.Registry
	secret *fakeSecretSource
}

// newPluginFixture is a plugin "acme" whose provider acme/agent runs the
// scripted fake ACP agent copied into the plugin directory (a real child
// process: nothing is mocked on the launch path). A second plugin's provider
// other/agent and a core provider both have stored secrets that must never
// reach acme's agent.
func newPluginFixture(t *testing.T, scenario string, mut func(*providers.Spec)) *pluginFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent launch is exercised on unix")
	}
	wm, task := newTestTask(t, scenario)
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(fakeACPAgentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "agent"), raw, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := providers.Spec{
		ID: "acme/agent", Label: "Acme agent", Plugin: "acme", Runner: providers.RunnerACP,
		Command: []string{"bin/agent"}, CommandForm: "path", PluginRoot: dir, PluginData: filepath.Join(dir, "data"),
		NoMCPServers: true, Env: map[string]string{"DEMO_MODE": "acp", "PATH": "/evil", "HOME": "/evil"},
		SecretEnv:   map[string]string{"DEMO_API_KEY": "DEMO_API_KEY"},
		SecretSlots: []providers.SecretSlot{{Name: "DEMO_API_KEY", Label: "key", Required: true}},
	}
	if mut != nil {
		mut(&spec)
	}
	other := providers.Spec{ID: "other/agent", Plugin: "other", Runner: providers.RunnerACP, Command: []string{"bin/agent"}, CommandForm: "path",
		PluginRoot: dir, PluginData: filepath.Join(dir, "data2"), SecretEnv: map[string]string{"OTHER_KEY": "OTHER_KEY"},
		SecretSlots: []providers.SecretSlot{{Name: "OTHER_KEY"}}}
	reg := providers.New()
	if p := reg.SetPlugins(map[string][]providers.Spec{"acme": {spec}, "other": {other}}); len(p) != 0 {
		t.Fatal(p)
	}
	sec := &fakeSecretSource{secrets: map[string]string{
		"acme/agent\x1fDEMO_API_KEY": pluginSecretValue,
		"other/agent\x1fOTHER_KEY":   otherSecretValue,
		"glm\x1fkey":                 otherSecretValue,
	}}
	r := New(wm, WithProviderRegistry(reg), WithProviderSecrets(sec))
	return &pluginFixture{r: r, task: task, dir: dir, spec: spec, reg: reg, secret: sec}
}

func (f *pluginFixture) run(t *testing.T) (events, notes []string, err error) {
	t.Helper()
	return collectRedactedRun(t, f.r, f.task, "acme/agent", "")
}

func (f *pluginFixture) envDump(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(*f.task.WorktreePath, "envdump.txt"))
	if err != nil {
		t.Fatalf("the agent never dumped its env: %v", err)
	}
	out := map[string]string{}
	for _, kv := range strings.Split(string(raw), "\n") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}

func envNames(env map[string]string) string {
	var n []string
	for k := range env {
		n = append(n, k)
	}
	sort.Strings(n)
	return strings.Join(n, ",")
}

// leakEnv plants things in the daemon's own environment that must never
// reach a plugin's agent.
func leakEnv(t *testing.T) {
	t.Setenv("SMIND_TOKEN", daemonLeakValue)
	t.Setenv("SMIND_HOME_SENTINEL", daemonLeakValue)
	t.Setenv("ANTHROPIC_API_KEY", daemonLeakValue)
	t.Setenv("GITHUB_TOKEN", daemonLeakValue)
	t.Setenv("SMIND_PD2_PASSTHRU", daemonLeakValue)
}

// TestPluginProvider_AgentEnv_ScrubbedAndBuilt: a real run of the plugin's
// agent (a script-like child that dumps its environment) sees ONLY the built
// environment: the fixed PATH, a private HOME under the plugin's data dir,
// the manifest literals and ITS OWN secret; nothing of the daemon's, no
// SMIND_*, no tokens, no other provider's secret; a plugin cannot override
// PATH or HOME.
func TestPluginProvider_AgentEnv_ScrubbedAndBuilt(t *testing.T) {
	leakEnv(t)
	f := newPluginFixture(t, "env-dump", nil)
	events, notes, err := f.run(t)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	env := f.envDump(t)

	allowed := map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "DEMO_MODE": true, "DEMO_API_KEY": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "LC_MESSAGES": true, "TZ": true}
	for k := range env {
		if !allowed[k] {
			t.Errorf("unexpected variable %s in the agent's environment (have %s)", k, envNames(env))
		}
	}
	if env["PATH"] != "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin" {
		t.Errorf("PATH = %q, want the fixed system list (the plugin's PATH=/evil must not win)", env["PATH"])
	}
	wantHome := filepath.Join(f.dir, "data", "providers", "agent", "home")
	if env["HOME"] != wantHome {
		t.Errorf("HOME = %q, want the private %q", env["HOME"], wantHome)
	}
	if env["DEMO_MODE"] != "acp" || env["DEMO_API_KEY"] != pluginSecretValue {
		t.Errorf("the plugin's own literal and secret must be present: %v", env)
	}
	dump := strings.Join(mapValues(env), "\n")
	for _, bad := range []string{daemonLeakValue, otherSecretValue, "SMIND_", "ANTHROPIC", "GITHUB_TOKEN"} {
		if strings.Contains(dump, bad) || strings.Contains(envNames(env), bad) {
			t.Errorf("%q reached the plugin's agent", bad)
		}
	}
	// The plugin's own secret never reaches the run's observable output.
	assertNoSentinel(t, pluginSecretValue, append(events, notes...)...)
}

func mapValues(m map[string]string) []string {
	var out []string
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func TestPluginProvider_InheritHome_OnlyHome(t *testing.T) {
	leakEnv(t)
	f := newPluginFixture(t, "env-dump", func(s *providers.Spec) { s.InheritHome = true })
	if _, _, err := f.run(t); err != nil {
		t.Fatal(err)
	}
	env := f.envDump(t)
	real, _ := os.UserHomeDir()
	if env["HOME"] != real {
		t.Errorf("HOME = %q, want the daemon user's %q with inheritHome", env["HOME"], real)
	}
	if strings.Contains(strings.Join(mapValues(env), "\n"), daemonLeakValue) || strings.Contains(envNames(env), "SMIND_") {
		t.Errorf("inheritHome leaked more than HOME: %s", envNames(env))
	}
	if env["TMPDIR"] != filepath.Join(f.dir, "data", "providers", "agent", "tmp") {
		t.Errorf("TMPDIR = %q, want the private one", env["TMPDIR"])
	}
}

// A plugin has no passthrough: a spec naming daemon variables to pass through
// (the third ADR-0046 value kind) still never gets them.
func TestPluginProvider_AgentEnv_NoPassthrough(t *testing.T) {
	leakEnv(t)
	f := newPluginFixture(t, "env-dump", func(s *providers.Spec) { s.PassthroughEnv = []string{"SMIND_PD2_PASSTHRU", "GITHUB_TOKEN"} })
	if _, _, err := f.run(t); err != nil {
		t.Fatal(err)
	}
	env := f.envDump(t)
	if _, ok := env["SMIND_PD2_PASSTHRU"]; ok {
		t.Fatal("a passthrough name reached a plugin's agent")
	}
	if _, ok := env["GITHUB_TOKEN"]; ok {
		t.Fatal("GITHUB_TOKEN reached a plugin's agent")
	}
}

func TestPluginProvider_Secret_ResolvedIntoChildEnvOnly(t *testing.T) {
	f := newPluginFixture(t, "env-dump", nil)
	if _, _, err := f.run(t); err != nil {
		t.Fatal(err)
	}
	env := f.envDump(t)
	if env["DEMO_API_KEY"] != pluginSecretValue {
		t.Fatal("the plugin's own secret did not reach its agent")
	}
	if _, ok := env["OTHER_KEY"]; ok {
		t.Fatal("another plugin provider's secret slot reached this agent")
	}
	// The launch pieces: the secret is in the env list only, never in argv.
	cmd, opts, err := f.r.pluginLaunch(f.spec, t.TempDir())
	if err != nil || len(opts) == 0 {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(cmd, " "), pluginSecretValue) {
		t.Fatal("a secret is in argv")
	}
}

func TestPluginProvider_RequiredSecretUnset_LaunchRefusedNamingSlot(t *testing.T) {
	f := newPluginFixture(t, "env-dump", nil)
	delete(f.secret.secrets, "acme/agent\x1fDEMO_API_KEY")
	_, _, err := f.run(t)
	if err == nil || !strings.Contains(err.Error(), "DEMO_API_KEY") || !strings.Contains(err.Error(), "plugin acme") {
		t.Fatalf("err = %v, want a refusal naming the slot and the plugin", err)
	}
	if _, statErr := os.Stat(filepath.Join(*f.task.WorktreePath, "envdump.txt")); statErr == nil {
		t.Fatal("the agent was started although a required secret was unset")
	}
}

func TestPluginProvider_RunsViaACP_FakeAgent_UsageAndEvents(t *testing.T) {
	f := newPluginFixture(t, "", nil)
	events, _, err := f.run(t)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(events, "\n")
	if !strings.Contains(all, "Hello, ") || !strings.Contains(all, "world!") {
		t.Fatalf("the plugin agent's streamed text did not arrive as run events: %s", all)
	}
	// What the session advertised is recorded like any ACP provider's.
	if f.r.ProviderCatalog() == nil {
		t.Fatal("no catalog")
	}
}

func TestPluginProvider_ModelSelectionAndModes(t *testing.T) {
	f := newPluginFixture(t, "", func(s *providers.Spec) {
		s.Models = []providers.Model{{ID: "big", Label: "Big", Default: true}}
		s.Modes = []providers.Mode{{ID: "ask", Label: "Ask"}}
		s.DefaultMode = "ask"
	})
	var info *ProviderInfo
	for _, p := range f.r.ProviderCatalog() {
		if p.ID == "acme/agent" {
			p := p
			info = &p
		}
	}
	if info == nil || info.Plugin != "acme" || len(info.Models) != 1 || info.Models[0].ID != "big" || len(info.Modes) != 1 || info.DefaultMode != "ask" {
		t.Fatalf("catalog entry = %+v", info)
	}
	// The probe path for an explicit call is the same scrubbed launch, and
	// records what the agent's session advertises.
	f2 := newPluginFixtureCommand(t, "modes:session")
	if err := f2.r.probeACPModes(context.Background(), "acme/agent"); err != nil {
		t.Fatal(err)
	}
	if c, ok := f2.r.acpCatalog("acme/agent"); !ok || len(c.modes) == 0 {
		t.Fatalf("probe recorded no modes: %+v", c)
	}
}

func newPluginFixtureCommand(t *testing.T, extraArg string) *pluginFixture {
	return newPluginFixture(t, "", func(s *providers.Spec) {
		s.Command = []string{"bin/agent", extraArg}
		s.SecretSlots, s.SecretEnv = nil, nil
	})
}

// TestPluginProvider_FailureClasses_SameClassifier: the same scripted failure
// reaches the caller as the same error/events whether the ACP agent is a
// plugin's or a providers.custom entry (the classification itself keys on the
// spec's vendor, generic for both).
func TestPluginProvider_FailureClasses_SameClassifier(t *testing.T) {
	for _, scenario := range []string{"quota-error", "auth-error", "transient-error", "residual-429", "model-error", "plain-error"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPluginFixture(t, scenario, nil)
			_, _, perr := f.run(t)
			if perr == nil {
				t.Fatal("the scripted failure did not fail the plugin run")
			}
			wm, task := newTestTask(t, scenario)
			reg := providers.New()
			if err := reg.SetCustom([]providers.Spec{{ID: "custom-acp", Command: []string{fakeACPAgentPath}}}); err != nil {
				t.Fatal(err)
			}
			r := New(wm, WithProviderRegistry(reg))
			_, _, cerr := collectRedactedRun(t, r, task, "custom-acp", "")
			if cerr == nil {
				t.Fatal("the custom provider did not fail")
			}
			norm := func(e error, id string) string {
				return strings.ReplaceAll(strings.ReplaceAll(e.Error(), id, "<id>"), "(plugin acme)", "")
			}
			if norm(perr, "acme/agent") != norm(cerr, "custom-acp") {
				t.Errorf("plugin error %q != custom error %q", perr, cerr)
			}
		})
	}
}

func TestPluginProvider_Launch_RechecksConfinedCommand(t *testing.T) {
	outside := t.TempDir()
	marker := filepath.Join(outside, "executed")
	evil := filepath.Join(outside, "evil")
	if err := os.WriteFile(evil, []byte("#!/bin/sh\necho ran > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, tamper := range map[string]func(agent string){
		"symlink to a file outside the plugin": func(a string) { os.Remove(a); os.Symlink(evil, a) },
		"no longer executable":                 func(a string) { os.Chmod(a, 0o644) },
		"deleted":                              func(a string) { os.Remove(a) },
		"replaced by a directory":              func(a string) { os.Remove(a); os.Mkdir(a, 0o755) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newPluginFixture(t, "env-dump", nil)
			if _, _, err := f.run(t); err != nil { // registered and fine
				t.Fatal(err)
			}
			os.Remove(filepath.Join(*f.task.WorktreePath, "envdump.txt"))
			tamper(filepath.Join(f.dir, "bin", "agent"))
			_, _, err := f.run(t)
			if err == nil {
				t.Fatal("a tampered command launched")
			}
			if _, e := os.Stat(marker); e == nil {
				t.Fatal("the file outside the plugin was EXECUTED")
			}
			if _, e := os.Stat(filepath.Join(*f.task.WorktreePath, "envdump.txt")); e == nil {
				t.Fatal("an agent started after the tamper")
			}
		})
	}
}

func TestPluginProvider_BareName_LooksOnFixedPathOnly(t *testing.T) {
	hijack := t.TempDir()
	marker := filepath.Join(t.TempDir(), "executed")
	if err := os.WriteFile(filepath.Join(hijack, "pd2-hijack-tool"), []byte("#!/bin/sh\necho ran > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", hijack+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := newPluginFixture(t, "env-dump", func(s *providers.Spec) {
		s.CommandForm, s.Command = "bare", []string{"pd2-hijack-tool"}
	})
	if _, _, err := f.run(t); err == nil {
		t.Fatal("a bare name found on the DAEMON's PATH launched")
	}
	if _, e := os.Stat(marker); e == nil {
		t.Fatal("a program on the daemon's PATH was executed")
	}
}

// TestPluginProvider_SlashID_NeverUsedAsPathOrRef: the id "acme/agent" is not
// a path: a run creates no "acme" directory anywhere, only the one validated
// provider part under the plugin's data dir; and the launch sources never
// join the id into a path.
func TestPluginProvider_SlashID_NeverUsedAsPathOrRef(t *testing.T) {
	f := newPluginFixture(t, "", nil)
	if _, _, err := f.run(t); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{f.dir, *f.task.WorktreePath} {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			rel, _ := filepath.Rel(root, p)
			for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
				if seg == "acme" || seg == "acme/agent" {
					t.Errorf("a path component named after the plugin id: %s", p)
				}
			}
			return nil
		})
	}
	if _, err := os.Stat(filepath.Join(f.dir, "data", "providers", "agent", "home")); err != nil {
		t.Errorf("the private home is not under the validated provider part: %v", err)
	}
	for _, file := range []string{"plugin_provider_launch.go", "../pluginagent/launch.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "filepath.Join") && (strings.Contains(line, "spec.ID") || strings.Contains(line, "provider)") || strings.Contains(line, "string(provider)")) {
				t.Errorf("%s joins a provider id into a path: %s", file, line)
			}
		}
	}
}

// Core providers are untouched: a built-in/custom ACP provider still gets the
// DAEMON's environment (everything in os.Environ plus its own entries) and
// the same argv, exactly as before PD2.
func TestPluginProvider_CoreProviderLaunchUnchanged(t *testing.T) {
	leakEnv(t)
	wm, task := newTestTask(t, "env-dump")
	r := New(wm, WithACPCommand(ProviderGLM, []string{fakeACPAgentPath}))
	if _, _, err := collectRedactedRun(t, r, task, ProviderGLM, ""); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(*task.WorktreePath, "envdump.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, kv := range strings.Split(string(raw), "\n") {
		got[kv] = true
	}
	for _, kv := range os.Environ() {
		if !got[kv] {
			t.Errorf("the core provider's child lost the inherited variable %q", strings.SplitN(kv, "=", 2)[0])
		}
	}
	if !got["SMIND_TOKEN="+daemonLeakValue] {
		t.Error("core ACP providers must still inherit the daemon environment (unchanged behaviour)")
	}
}
