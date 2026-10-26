package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

func (h *pdmHarness) setEnv(rc *requestContext, provider, key string, value *string) (any, error) {
	raw, _ := json.Marshal(map[string]any{"provider": provider, "key": key, "value": value})
	return h.api.hs["provider.setEnv"](context.Background(), rc, raw)
}

func str(s string) *string { return &s }

func (h *pdmHarness) listedEntry(t *testing.T, provider string) providerListEntry {
	t.Helper()
	res, err := h.api.hs["provider.list"](context.Background(), &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.(providerListResult).Providers {
		if string(p.ID) == provider {
			return p
		}
	}
	t.Fatalf("provider %q not listed", provider)
	return providerListEntry{}
}

func TestProviderSetEnvSetAndNullClears(t *testing.T) {
	h := newPDMHarness(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	dir := filepath.Join(t.TempDir(), "scopedocs")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	res, err := h.setEnv(&requestContext{}, "claude-native", "CLAUDE_CONFIG_DIR", str(dir))
	if err != nil {
		t.Fatal(err)
	}
	got := res.(providerUpdatedPayload)
	if got.ID != "claude-native" || got.Env["CLAUDE_CONFIG_DIR"] != dir || !got.EnvDirExists["CLAUDE_CONFIG_DIR"] {
		t.Fatalf("result = %+v", got)
	}
	if eff := got.EnvEffective["CLAUDE_CONFIG_DIR"]; eff.Path != dir || eff.Source != envSourceSettings || !eff.Exists {
		t.Fatalf("effective = %+v", eff)
	}
	if body := h.file(t); !strings.Contains(body, dir) {
		t.Fatalf("config.yaml lacks the path:\n%s", body)
	}
	// Live: the Runner a launch reads has it, and provider.list shows it.
	if v := h.runner.ProviderEnv(taskrunner.ProviderClaudeNative)["CLAUDE_CONFIG_DIR"]; v != dir {
		t.Fatalf("runner env = %q", v)
	}
	if e := h.listedEntry(t, "claude-native"); e.Env["CLAUDE_CONFIG_DIR"] != dir {
		t.Fatalf("provider.list env = %v", e.Env)
	}
	// The refresh event carries the same payload.
	ev := queued(sub)
	if len(ev) != 1 || ev[0].Topic != TopicProviderUpdated || ev[0].Payload.(providerUpdatedPayload).Env["CLAUDE_CONFIG_DIR"] != dir {
		t.Fatalf("events = %+v", ev)
	}
	// null clears (file, runner, list).
	res, err = h.setEnv(&requestContext{}, "claude-native", "CLAUDE_CONFIG_DIR", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(providerUpdatedPayload); got.Env != nil || got.EnvEffective["CLAUDE_CONFIG_DIR"].Source != envSourceDefault {
		t.Fatalf("after clear = %+v", got)
	}
	if h.runner.ProviderEnv(taskrunner.ProviderClaudeNative) != nil || strings.Contains(h.file(t), dir) {
		t.Fatal("clear left the setting behind")
	}
	if len(queued(sub)) != 2 {
		t.Fatal("clear published no event")
	}
}

func TestProviderSetEnvValidatesKey(t *testing.T) {
	h := newPDMHarness(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	for _, tc := range []struct{ provider, key, value, want string }{
		{"claude-native", "ANTHROPIC_API_KEY", "/x", "CLAUDE_CONFIG_DIR"},
		{"claude-native", "PATH", "/usr/bin", "not allowed"},
		{"claude-native", "CODEX_HOME", "/x", "not allowed"},
		{"glm", "CLAUDE_CONFIG_DIR", "/x", "no settable launch env"},
		{"claude-native", "CLAUDE_CONFIG_DIR", "relative/dir", "absolute"},
		{"claude-native", "CLAUDE_CONFIG_DIR", "/a\x00b", "control"},
		{"nope", "CLAUDE_CONFIG_DIR", "/x", "unknown provider"},
	} {
		_, err := h.setEnv(&requestContext{}, tc.provider, tc.key, str(tc.value))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: err = %v, want %q", tc, err, tc.want)
		}
	}
	if h.file(t) != "" || h.runner.ProviderEnv(taskrunner.ProviderClaudeNative) != nil {
		t.Fatal("a rejected call changed state")
	}
	if len(queued(sub)) != 0 {
		t.Fatal("events on failure")
	}
}

func TestProviderSetEnvRefusesAgentCaller(t *testing.T) {
	h := newPDMHarness(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	_, err := h.setEnv(&requestContext{runScoped: true}, "claude-native", "CLAUDE_CONFIG_DIR", str("/x"))
	if err == nil || codedError(err).Code != codeForbidden {
		t.Fatalf("err = %v, want forbidden", err)
	}
	if h.file(t) != "" || h.runner.ProviderEnv(taskrunner.ProviderClaudeNative) != nil || len(queued(sub)) != 0 {
		t.Fatal("an agent caller changed state")
	}
	for _, m := range RunScopedMethods {
		if m == "provider.setEnv" {
			t.Fatal("provider.setEnv is on the run-scoped allowlist")
		}
	}
}

func TestProviderListEnvEffectiveSources(t *testing.T) {
	h := newPDMHarness(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	eff := func() providerEnvEffective {
		return h.listedEntry(t, "claude-native").EnvEffective["CLAUDE_CONFIG_DIR"]
	}
	if e := eff(); e.Source != envSourceDefault || e.Path != filepath.Join(home, ".claude") || e.Exists {
		t.Fatalf("default = %+v", e)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/from/daemon/environment")
	if e := eff(); e.Source != envSourceEnvironment || e.Path != "/from/daemon/environment" {
		t.Fatalf("environment = %+v", e)
	}
	// A filepath.Join under a temp dir, not "/from/settings": the setEnv
	// validator wants an absolute path, and a bare "/x" is not one on Windows.
	fromSettings := filepath.Join(t.TempDir(), "from-settings")
	if _, err := h.setEnv(&requestContext{}, "claude-native", "CLAUDE_CONFIG_DIR", str(fromSettings)); err != nil {
		t.Fatal(err)
	}
	if e := eff(); e.Source != envSourceSettings || e.Path != fromSettings {
		t.Fatalf("settings = %+v", e)
	}
	if e := h.listedEntry(t, "glm"); e.EnvEffective != nil || e.Env != nil {
		t.Fatalf("glm has env fields: %+v", e)
	}
	if e := h.listedEntry(t, "codex-native"); e.EnvEffective["CODEX_HOME"].Source != envSourceDefault {
		t.Fatalf("codex = %+v", e.EnvEffective)
	}
}

// providerTestDetail runs provider.test for claude-native with a stand-in
// `claude` on PATH.
func (h *pdmHarness) providerTestDetail(t *testing.T) providerTestResult {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	raw, _ := json.Marshal(map[string]string{"provider": "claude-native"})
	res, err := h.api.hs["provider.test"](context.Background(), &requestContext{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	return res.(providerTestResult)
}

func TestProviderTestNamesConfigDirAndSource(t *testing.T) {
	h := newPDMHarness(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if d := h.providerTestDetail(t).Detail; !strings.Contains(d, "config dir: "+filepath.Join(home, ".claude")+" (default ~/.claude)") {
		t.Fatalf("default detail = %q", d)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/from/daemon/environment")
	if d := h.providerTestDetail(t).Detail; !strings.Contains(d, "config dir: /from/daemon/environment (from the daemon's environment)") {
		t.Fatalf("environment detail = %q", d)
	}
	dir := filepath.Join(t.TempDir(), "scopedocs")
	if _, err := h.setEnv(&requestContext{}, "claude-native", "CLAUDE_CONFIG_DIR", str(dir)); err != nil {
		t.Fatal(err)
	}
	d := h.providerTestDetail(t).Detail
	if !strings.Contains(d, "config dir: "+dir+" (from smind settings)") || !strings.Contains(d, "[directory does not exist]") {
		t.Fatalf("settings detail = %q", d)
	}
}

func TestProviderTestNeverReadsDirContents(t *testing.T) {
	h := newPDMHarness(t)
	dir := t.TempDir()
	const marker = "PLANTED-SECRET-MARKER-7f3a"
	for _, name := range []string{".credentials.json", "settings.json", ".claude.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.setEnv(&requestContext{}, "claude-native", "CLAUDE_CONFIG_DIR", str(dir)); err != nil {
		t.Fatal(err)
	}
	res := h.providerTestDetail(t)
	listed, _ := json.Marshal(h.listedEntry(t, "claude-native"))
	for name, text := range map[string]string{"provider.test": res.Detail, "provider.list": string(listed), "config.yaml": h.file(t)} {
		if strings.Contains(text, marker) {
			t.Errorf("%s output contains file contents: %s", name, text)
		}
	}
	if strings.Contains(res.Detail, "does not exist") {
		t.Fatalf("an existing dir reported missing: %q", res.Detail)
	}
}
