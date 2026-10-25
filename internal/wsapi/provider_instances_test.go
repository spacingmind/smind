package wsapi

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/learning"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// These tests mutate the process-wide providers.Default (the table learning,
// runs and profiles read), so none is parallel and each restores it.

func resetProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { _ = providers.Default().SetCustom(nil) })
}

func (h *pdmHarness) call(t *testing.T, method string, rc *requestContext, params any) (any, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return h.api.hs[method](context.Background(), rc, raw)
}

func (h *pdmHarness) listIDs(t *testing.T) []string {
	t.Helper()
	res, err := h.call(t, "provider.list", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range res.(providerListResult).Providers {
		ids = append(ids, string(p.ID))
	}
	return ids
}

func TestProviderList_SingleSource(t *testing.T) {
	resetProviders(t)
	h := newPDMHarness(t)
	if err := providers.Default().Register(providers.Spec{ID: "src-acp", Label: "Src", Command: []string{fakeACPAgentPath}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { providers.Default().Unregister("src-acp") })

	// provider.list is the registry, in order, and nothing else.
	if got, want := h.listIDs(t), providers.Default().IDs(); len(got) != len(want) {
		t.Fatalf("list ids %v vs registry %v", got, want)
	}
	res, _ := h.call(t, "provider.list", &requestContext{}, nil)
	raw, _ := json.Marshal(res)
	var wire struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	by := map[string]map[string]any{}
	for _, p := range wire.Providers {
		by[p["id"].(string)] = p
	}
	if by["src-acp"]["custom"] != true || by["src-acp"]["runner"] != "acp" || by["src-acp"]["label"] != "Src" {
		t.Fatalf("custom entry on the wire = %v", by["src-acp"])
	}
	if by["claude-native"]["runner"] != "claude-native" || by["claude-native"]["custom"] != nil || by["claude-native"]["extends"] != nil {
		t.Fatalf("base entry on the wire = %v", by["claude-native"])
	}
	if by["glm"]["kind"] != "cli" || by["glm"]["supportsAutoAccept"] != true {
		t.Fatalf("existing fields must be unchanged: %v", by["glm"])
	}
}

// Adding an ACP provider is one registry entry: every consumer that used to
// switch on provider ids works for it with no other edit.
func TestProviderRegistry_AddACPProvider_NoSwitchEdits(t *testing.T) {
	resetProviders(t)
	h := newPDMHarness(t)
	const id = "nse-agent"
	if err := providers.Default().Register(providers.Spec{
		ID: id, Label: "NSE", Command: []string{fakeACPAgentPath},
		Modes: []providers.Mode{{ID: "plan", Label: "Plan"}}, DefaultMode: "plan",
		Models: []providers.Model{{ID: "m1", Label: "M1"}},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { providers.Default().Unregister(id) })

	steps := []struct {
		name  string
		check func(t *testing.T)
	}{
		{"catalog", func(t *testing.T) {
			e := h.listedEntry(t, id)
			if e.Label != "NSE" || !e.Custom || e.DefaultMode != "plan" || len(e.Models) != 1 {
				t.Fatalf("entry = %+v", e)
			}
		}},
		{"modes", func(t *testing.T) {
			cat, ok := h.runner.ProviderCatalogFor(id)
			if !ok {
				t.Fatal("not in the runner catalog")
			}
			if err := taskrunner.ValidatePermissionSettings(cat, taskrunner.PermissionSettings{Mode: "plan"}); err != nil {
				t.Fatal(err)
			}
			if _, ok := taskrunner.ProviderInfoFor(id); !ok {
				t.Fatal("ProviderInfoFor (profiles, run permission edits) does not know it")
			}
		}},
		{"run", func(t *testing.T) {
			task := newTestTask(t, h.wm, "")
			events := make(chan taskrunner.Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- h.runner.RunPrompt(context.Background(), task.ID, task.ID, id, "hi", nil, taskrunner.PermissionSettings{}, "", "", events)
			}()
			var done bool
			for e := range events {
				done = done || e.Type == taskrunner.EventTypeDone
			}
			if err := <-errCh; err != nil || !done {
				t.Fatalf("run err=%v done=%v", err, done)
			}
		}},
		{"diag", func(t *testing.T) {
			res, err := h.call(t, "provider.test", &requestContext{}, map[string]string{"provider": id})
			if err != nil {
				t.Fatal(err)
			}
			if r := res.(providerTestResult); !r.OK {
				t.Fatalf("provider.test = %+v, want the command found", r)
			}
		}},
		{"delivery", func(t *testing.T) {
			if !learning.EmitsToolEvents(id) || providers.Delivery(id) != providers.DeliveryPreface || learning.CanReflect(id) {
				t.Fatalf("learning flags: tool=%v delivery=%q reflect=%v", learning.EmitsToolEvents(id), providers.Delivery(id), learning.CanReflect(id))
			}
		}},
		{"profile", func(t *testing.T) {
			if _, err := h.call(t, "profile.create", &requestContext{}, map[string]any{"name": "nse", "provider": id, "permissionMode": "plan"}); err != nil {
				t.Fatalf("a profile naming the new provider: %v", err)
			}
		}},
	}
	for _, s := range steps {
		t.Run(s.name, s.check)
	}
}

type recordingLister struct{ calls []string }

func (r *recordingLister) ReadDir(name string) ([]fs.DirEntry, error) {
	r.calls = append(r.calls, name)
	return os.ReadDir(name)
}

func newSuggestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	for _, d := range []string{".claude", ".claude-work", ".codex", ".codex2", "projects"} {
		if err := os.Mkdir(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Content that would fail the test if anything opened it.
	if err := os.WriteFile(filepath.Join(home, ".claude-work", ".credentials.json"), []byte("secret"), 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, ".claude-work", "nested"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(home, ".claude-work", "nested"), 0o700) })
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/", filepath.Join(home, ".claude-link")); err != nil && !os.IsExist(err) {
		t.Logf("symlink unavailable: %v", err)
	}
	makeFIFO(t, filepath.Join(home, ".claude-work", "fifo")) // opening it would block forever
	return home
}

func TestProviderDirSuggestions_NamesOnly(t *testing.T) {
	resetProviders(t)
	h := newPDMHarness(t)
	home := newSuggestHome(t)
	rec := &recordingLister{}
	handler := handleProviderDirSuggestions(h.runner, rec)

	res, err := handler(context.Background(), &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Only the home directory's own listing is read: nothing under a
	// suggested directory is listed, opened or stat'ed through the seam.
	if len(rec.calls) != 1 || rec.calls[0] != home {
		t.Fatalf("filesystem reads = %v, want exactly [%s]", rec.calls, home)
	}
	var names []string
	for _, s := range res.(dirSuggestionsResult).Suggestions {
		names = append(names, s.Name)
		if s.Dir != filepath.Join(home, s.Name) || (s.Extends != "claude-native" && s.Extends != "codex-native") {
			t.Errorf("suggestion = %+v", s)
		}
	}
	// ~/.claude and ~/.codex are the base providers' own directories; files,
	// symlinks and unrelated directories are never suggested.
	if got := strings.Join(names, ","); got != ".claude-work,.codex2" {
		t.Fatalf("suggested = %s", got)
	}
	raw, _ := json.Marshal(res)
	for _, leak := range []string{"credentials", "secret", "nested", "fifo", ".claude.json", "size"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("result leaks %q: %s", leak, raw)
		}
	}
}

func TestProviderDirSuggestions_SkipsConfiguredAndFollowsBaseOverride(t *testing.T) {
	resetProviders(t)
	h := newPDMHarness(t)
	home := newSuggestHome(t)
	work := filepath.Join(home, ".claude-work")

	if _, err := h.call(t, "provider.addInstance", &requestContext{}, map[string]string{"extends": "claude-native", "id": "claude-work", "label": "Claude Work", "dir": work}); err != nil {
		t.Fatal(err)
	}
	if got := suggested(t, h); got != ".codex2" {
		t.Fatalf("after adding an instance on %s, suggested = %s", work, got)
	}
	// The base moved to ~/.claude-work's sibling: ~/.claude becomes free to suggest.
	h.runner.SetProviderEnv(taskrunner.ProviderClaudeNative, "CLAUDE_CONFIG_DIR", filepath.Join(home, "elsewhere"))
	if got := suggested(t, h); got != ".claude,.codex2" {
		t.Fatalf("with the base on another dir, suggested = %s", got)
	}
}

func suggested(t *testing.T, h *pdmHarness) string {
	t.Helper()
	res, err := h.call(t, "provider.dirSuggestions", &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range res.(dirSuggestionsResult).Suggestions {
		names = append(names, s.Name)
	}
	return strings.Join(names, ",")
}

func TestProviderInstanceRPCs_RefuseAgentCallers(t *testing.T) {
	resetProviders(t)
	h := newPDMHarness(t)
	home := newSuggestHome(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	agent := &requestContext{runScoped: true}
	for method, params := range map[string]any{
		"provider.dirSuggestions": nil,
		"provider.reload":         nil,
		"provider.addInstance":    map[string]string{"extends": "claude-native", "id": "evil", "dir": filepath.Join(home, ".claude-work")},
	} {
		_, err := h.call(t, method, agent, params)
		if err == nil || codedError(err).Code != codeForbidden {
			t.Errorf("%s: err = %v, want forbidden", method, err)
		}
	}
	if h.file(t) != "" || len(h.listIDs(t)) != 4 || len(queued(sub)) != 0 {
		t.Fatalf("an agent caller changed state: %q %v", h.file(t), h.listIDs(t))
	}
	for _, m := range RunScopedMethods {
		if strings.HasPrefix(m, "provider.") && m != "provider.list" && m != "provider.test" {
			t.Fatalf("%s is on the run-scoped allowlist", m)
		}
	}
}

func TestProviderReload_AddsAndRemovesInstanceLive(t *testing.T) {
	resetProviders(t)
	h := newPDMHarness(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(h.path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("providers:\n  custom:\n    - id: claude-a\n      label: Claude A\n      extends: claude-native\n      env: {CLAUDE_CONFIG_DIR: /d/a}\n    - id: my-agent\n      command: " + fakeACPAgentPath + "\n")
	if _, err := h.call(t, "provider.reload", &requestContext{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.listIDs(t), ","); got != "claude-native,glm,kimi,codex-native,claude-a,my-agent" {
		t.Fatalf("after reload: %s", got)
	}
	e := h.listedEntry(t, "claude-a")
	if e.Extends != "claude-native" || e.Label != "Claude A" || e.EnvEffective["CLAUDE_CONFIG_DIR"].Path != "/d/a" || e.EnvEffective["CLAUDE_CONFIG_DIR"].Source != envSourceSettings {
		t.Fatalf("instance entry = %+v", e)
	}
	if len(queued(sub)) == 0 {
		t.Fatal("no provider.updated event after a reload")
	}

	// A broken file changes nothing and names the field.
	write("providers:\n  custom:\n    - id: claude-a\n      extends: claude-native\n    - id: bad\n")
	_, err := h.call(t, "provider.reload", &requestContext{}, nil)
	if err == nil || !strings.Contains(err.Error(), "providers.custom[1].command: required") {
		t.Fatalf("err = %v", err)
	}
	if got := strings.Join(h.listIDs(t), ","); got != "claude-native,glm,kimi,codex-native,claude-a,my-agent" {
		t.Fatalf("a rejected reload changed the registry: %s", got)
	}

	// Removing the entries removes the providers.
	write("providers:\n  custom: []\n")
	if _, err := h.call(t, "provider.reload", &requestContext{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.listIDs(t), ","); got != "claude-native,glm,kimi,codex-native" {
		t.Fatalf("after removal: %s", got)
	}
}

func TestProviderAddInstance_WritesConfigAppliesLiveAndProfileCanNameIt(t *testing.T) {
	resetProviders(t)
	h := newPDMHarness(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	dir := filepath.Join(t.TempDir(), ".claude-s")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	res, err := h.call(t, "provider.addInstance", &requestContext{}, map[string]string{"extends": "claude-native", "id": "claude-s", "label": "Claude S", "dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(providerUpdatedPayload); got.ID != "claude-s" || got.EnvDirExists["CLAUDE_CONFIG_DIR"] != true {
		t.Fatalf("result = %+v", got)
	}
	if body := h.file(t); !strings.Contains(body, "id: claude-s") || !strings.Contains(body, dir) {
		t.Fatalf("config.yaml:\n%s", body)
	}
	if h.runner.ProviderEnv("claude-s")["CLAUDE_CONFIG_DIR"] != dir || len(queued(sub)) != 1 {
		t.Fatalf("not live: env=%v", h.runner.ProviderEnv("claude-s"))
	}
	// The health check runs against the instance's own directory.
	tr, err := h.call(t, "provider.test", &requestContext{}, map[string]string{"provider": "claude-s"})
	if err != nil {
		t.Fatal(err)
	}
	if d := tr.(providerTestResult).Detail; !strings.Contains(d, "config dir: "+dir) || strings.Contains(d, "does not exist") {
		t.Fatalf("health detail = %q", d)
	}
	// A profile may name the instance.
	if _, err := h.call(t, "profile.create", &requestContext{}, map[string]any{"name": "s-seat", "provider": "claude-s", "permissionMode": "plan"}); err != nil {
		t.Fatalf("profile naming an instance: %v", err)
	}
	// Rejections leave config.yaml alone.
	before := h.file(t)
	for _, p := range []map[string]string{
		{"extends": "claude-native", "id": "claude-s", "dir": dir},
		{"extends": "claude-native", "id": "glm", "dir": dir},
		{"extends": "kimi", "id": "k2", "dir": dir},
		{"extends": "claude-native", "id": "rel", "dir": "relative/path"},
	} {
		if _, err := h.call(t, "provider.addInstance", &requestContext{}, p); err == nil {
			t.Errorf("accepted %v", p)
		}
	}
	if h.file(t) != before {
		t.Fatal("a rejected add changed config.yaml")
	}
}
