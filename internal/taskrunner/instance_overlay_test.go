package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

func instanceRunner(t *testing.T, specs ...providers.Spec) (*Runner, *providers.Registry) {
	t.Helper()
	reg := providers.New()
	if err := reg.SetCustom(specs); err != nil {
		t.Fatal(err)
	}
	return New(nil, WithProviderRegistry(reg)), reg
}

func modelIDs(p ProviderInfo) []string {
	var ids []string
	for _, m := range p.Models {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestInstance_ModelsReplaceClaudeCatalog(t *testing.T) {
	t.Parallel()
	r, _ := instanceRunner(t, providers.Spec{ID: "zai", Extends: providers.IDClaudeNative, ModelsMode: providers.ModelsReplace,
		Models: []providers.Model{{ID: "glm-5.3[1m]", Label: "GLM 5.3"}, {ID: "glm-5.3-flash"}}})
	p, ok := r.ProviderCatalogFor("zai")
	if !ok {
		t.Fatal("no instance")
	}
	if got := strings.Join(modelIDs(p), ","); got != "glm-5.3[1m],glm-5.3-flash" {
		t.Fatalf("instance models = %s, want exactly the configured ones", got)
	}
	if p.Models[0].Source != ModelSourceConfig || p.Harness != "claude-code" || p.Runner != "claude-native" {
		t.Fatalf("instance info = %+v", p)
	}
	// Syntax-valid ids are never rejected, in or out of the list.
	for _, id := range []string{"glm-5.3[1m]", "deepseek-v4-pro", "claude-future-9-9"} {
		if err := ValidateModel(p, id); err != nil {
			t.Errorf("ValidateModel(%q) = %v", id, err)
		}
	}
	// The base is untouched.
	base, _ := r.ProviderCatalogFor(ProviderClaudeNative)
	if len(base.Models) != len(claudeModels()) || base.Models[0].Source != ModelSourceCatalog {
		t.Fatalf("base models changed: %+v", base.Models)
	}
}

func TestInstance_AdditionalModelsMergeByID(t *testing.T) {
	t.Parallel()
	r, _ := instanceRunner(t,
		providers.Spec{ID: "plain", Extends: providers.IDClaudeNative},
		providers.Spec{ID: "extra", Extends: providers.IDClaudeNative, ModelsMode: providers.ModelsAdd,
			Models: []providers.Model{{ID: "glm-5.3"}, {ID: "opus", Label: "My Opus"}}})
	plain, _ := r.ProviderCatalogFor("plain")
	if got, want := len(plain.Models), len(claudeModels()); got != want {
		t.Fatalf("instance without models lists %d, want the %d-entry Claude catalog", got, want)
	}
	extra, _ := r.ProviderCatalogFor("extra")
	if len(extra.Models) != len(claudeModels())+1 {
		t.Fatalf("catalog + extras = %v", modelIDs(extra))
	}
	seen := map[string]int{}
	for _, m := range extra.Models {
		seen[m.ID]++
		if m.ID == "opus" && m.Label != "My Opus" {
			t.Errorf("opus not replaced in place: %+v", m)
		}
	}
	if seen["opus"] != 1 || seen["glm-5.3"] != 1 || seen["sonnet"] != 1 {
		t.Fatalf("merge by id failed: %v", seen)
	}
}

func TestInstance_DefaultModelNotInheritedWhenModelsSet(t *testing.T) {
	t.Parallel()
	r, _ := instanceRunner(t,
		providers.Spec{ID: "zai", Extends: providers.IDClaudeNative, Models: []providers.Model{{ID: "glm-5.3"}, {ID: "glm-5.3-flash", Default: true}}},
		providers.Spec{ID: "first", Extends: providers.IDClaudeNative, Models: []providers.Model{{ID: "m-a"}, {ID: "m-b"}}})
	WithDefaultModel(ProviderClaudeNative, "claude-haiku-5-5")(r)
	got, err := r.ResolveModel("zai", "")
	if err != nil || got != "glm-5.3-flash" {
		t.Fatalf("ResolveModel(zai) = %q, %v; want the instance's flagged default, not the base's", got, err)
	}
	if got, _ := r.ResolveModel("first", ""); got != "m-a" {
		t.Fatalf("ResolveModel(first) = %q, want the first model", got)
	}
	// provider.list shows what a run will use.
	if p, _ := r.ProviderCatalogFor("zai"); p.DefaultModel != "glm-5.3-flash" {
		t.Fatalf("catalog default = %q", p.DefaultModel)
	}
	// An explicit instance default still wins.
	WithDefaultModel("zai", "glm-5.3")(r)
	if got, _ := r.ResolveModel("zai", ""); got != "glm-5.3" {
		t.Fatalf("ResolveModel with own default = %q", got)
	}
}

func TestInstance_DefaultModelInheritedWhenNoModelsNoEndpoint(t *testing.T) {
	t.Parallel()
	r, _ := instanceRunner(t,
		providers.Spec{ID: "inherit", Extends: providers.IDClaudeNative},
		providers.Spec{ID: "adds", Extends: providers.IDClaudeNative, ModelsMode: providers.ModelsAdd, Models: []providers.Model{{ID: "extra"}}})
	WithDefaultModel(ProviderClaudeNative, "claude-haiku-5-5")(r)
	for _, id := range []Provider{"inherit", "adds"} {
		if got, err := r.ResolveModel(id, ""); err != nil || got != "claude-haiku-5-5" {
			t.Errorf("ResolveModel(%s) = %q, %v; want the base default", id, got, err)
		}
	}
}

// The launch argv carries the instance's disallowedTools (fakeclaude records
// its args in the worktree).
func TestClaudeLaunch_DisallowedToolsInArgv(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		tools []string
		want  string
	}{
		{"set", []string{"WebSearch", "Bash(rm:*)"}, "WebSearch,Bash(rm:*)"},
		{"none", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, _, task := newClaudeMcpEnv(t)
			WithProviderRegistry(func() *providers.Registry {
				reg := providers.New()
				if err := reg.SetCustom([]providers.Spec{{ID: "zai", Extends: providers.IDClaudeNative, DisallowedTools: tc.tools}}); err != nil {
					t.Fatal(err)
				}
				return reg
			}())(r)
			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, "zai", "hi", denyAllDecider{}, PermissionSettings{}, "", testClaudeModel, events)
			}()
			drainEvents(events)
			if err := <-errCh; err != nil {
				t.Fatalf("RunPrompt() error = %v", err)
			}
			data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "args"))
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(string(data), "\n")
			v, ok := flagValue(args, "--disallowedTools")
			if tc.want == "" {
				if ok {
					t.Fatalf("--disallowedTools passed with none configured: %q", args)
				}
				return
			}
			if !ok || v != tc.want {
				t.Fatalf("--disallowedTools = %q (present %v), want %q; args %q", v, ok, tc.want, args)
			}
		})
	}
}

func TestProviderList_AdditiveFields(t *testing.T) {
	t.Parallel()
	r, _ := instanceRunner(t,
		providers.Spec{ID: "acp-x", Command: []string{"x"}, Models: []providers.Model{{ID: "m"}}},
		providers.Spec{ID: "inst", Extends: providers.IDClaudeNative})
	want := map[Provider][3]string{ // runner, harness, extends
		ProviderClaudeNative: {"claude-native", "claude-code", ""},
		ProviderCodexNative:  {"codex-native", "codex-app-server", ""},
		ProviderGLM:          {"acp", "acp", ""},
		"acp-x":              {"acp", "acp", ""},
		"inst":               {"claude-native", "claude-code", providers.IDClaudeNative},
	}
	for _, p := range r.ProviderCatalog() {
		w, ok := want[p.ID]
		if !ok {
			continue
		}
		if p.Runner != w[0] || p.Harness != w[1] || p.Extends != w[2] {
			t.Errorf("%s: runner/harness/extends = %q/%q/%q, want %v", p.ID, p.Runner, p.Harness, p.Extends, w)
		}
	}
	// Existing providers keep every wire field they had; new ones are additive.
	k, _ := r.ProviderCatalogFor(ProviderKimi)
	if k.Runner != "acp" || k.CredentialKind != "api-key" || k.AccountProvider != "kimi" || !k.SupportsAutoAccept {
		t.Fatalf("kimi entry changed: %+v", k)
	}
	x, _ := r.ProviderCatalogFor("acp-x")
	if len(x.Models) != 1 || x.Models[0].Source != ModelSourceConfig {
		t.Fatalf("custom ACP models = %+v", x.Models)
	}
}

// An instance of an ACP provider runs its own command, not the base's.
func TestInstance_ACPCommandOverrideRuns(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r, _ := customRunner(t, wm,
		providers.Spec{ID: "base-agent", Command: []string{"/nonexistent/base-agent"}},
		providers.Spec{ID: "fast-agent", Extends: "base-agent", Command: []string{fakeACPAgentPath}})
	got := runPlain(t, r, task, "fast-agent", PermissionSettings{}, "")
	var done int
	for _, e := range got {
		if e.Type == EventTypeDone {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("events = %+v", got)
	}
}
