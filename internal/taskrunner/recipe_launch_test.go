package taskrunner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
)

// recipeRunner wires the fake claude CLI (echo-args scenario) at a set of
// claude-code instance specs, with SMIND_HOME pointed at a fresh dir (the
// private CLAUDE_CONFIG_DIR lives under it) and env as the launch env.
// Not usable with t.Parallel (t.Setenv).
func recipeRunner(t *testing.T, specs []providers.Spec, env map[string]string) (*Runner, store.Task, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	_, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("echo-args"), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}
	r := claudeNativeRunner(t, wm)
	reg := providers.New()
	if err := reg.SetCustom(specs); err != nil {
		t.Fatal(err)
	}
	WithProviderRegistry(reg)(r)
	for _, s := range specs {
		WithProviderEnv(Provider(s.ID), env)(r)
	}
	return r, task, home
}

// runRecipeTurn drives one turn of provider (chatID keeps runs from
// resuming each other's session) and returns the recorded argv.
func runRecipeTurn(t *testing.T, r *Runner, task store.Task, chatID int64, provider Provider, thinking ThinkingLevel, model string) []string {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, chatID, provider, "hi", denyAllDecider{}, PermissionSettings{}, thinking, model, events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt(%s) error = %v", provider, err)
	}
	return readLaunchArgv(t, task)
}

// A compatible-endpoint instance reads its login from a private
// CLAUDE_CONFIG_DIR under SMIND_HOME, not ~/.claude.
func TestRecipe_PrivateConfigDirSet(t *testing.T) {
	r, task, home := recipeRunner(t,
		[]providers.Spec{{ID: "zai-t", Extends: providers.IDClaudeNative,
			Env: map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:1"}}}, nil)

	runRecipeTurn(t, r, task, task.ID, "zai-t", ThinkingLevelUnspecified, testClaudeModel)

	want := filepath.Join(home, "providers", "zai-t", "claude")
	if got := readEnvDump(t, *task.WorktreePath); got != want {
		t.Fatalf("CLI CLAUDE_CONFIG_DIR = %q, want the private dir %q", got, want)
	}
	fi, err := os.Stat(want)
	if err != nil || !fi.IsDir() {
		t.Fatalf("stat %s: %v (isDir %v)", want, err, fi.IsDir())
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Fatalf("private dir mode = %o, want 700", fi.Mode().Perm())
	}
}

// InheritClaudeConfig opts the instance back into the default (~/.claude)
// config dir: no private dir is created at all.
func TestRecipe_InheritToggleUsesDefaultConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	r, task, home := recipeRunner(t,
		[]providers.Spec{{ID: "zai-t", Extends: providers.IDClaudeNative, InheritClaudeConfig: true,
			Env: map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:1"}}}, nil)

	runRecipeTurn(t, r, task, task.ID, "zai-t", ThinkingLevelUnspecified, testClaudeModel)

	if got := readEnvDump(t, *task.WorktreePath); got != "" {
		t.Fatalf("CLI CLAUDE_CONFIG_DIR = %q, want the daemon's own (empty)", got)
	}
	if _, err := os.Stat(filepath.Join(home, "providers", "zai-t")); !os.IsNotExist(err) {
		t.Fatalf("providers/zai-t exists (%v), want nothing created", err)
	}
}

// A CLAUDE_CONFIG_DIR pinned in the launch env wins over the private dir.
func TestRecipe_PinnedConfigDirWins(t *testing.T) {
	pinned := t.TempDir()
	r, task, home := recipeRunner(t,
		[]providers.Spec{{ID: "zai-t", Extends: providers.IDClaudeNative,
			Env: map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:1"}}},
		map[string]string{"CLAUDE_CONFIG_DIR": pinned})

	runRecipeTurn(t, r, task, task.ID, "zai-t", ThinkingLevelUnspecified, testClaudeModel)

	if got := readEnvDump(t, *task.WorktreePath); got != pinned {
		t.Fatalf("CLI CLAUDE_CONFIG_DIR = %q, want the pinned %q", got, pinned)
	}
	if _, err := os.Stat(filepath.Join(home, "providers", "zai-t")); !os.IsNotExist(err) {
		t.Fatalf("providers/zai-t exists (%v), want nothing created", err)
	}
}

// An instance without an endpoint (no ANTHROPIC_BASE_URL) is launched
// unchanged: no private dir, no env entry.
func TestRecipe_NonCompatibleInstanceUnchanged(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	r, task, home := recipeRunner(t,
		[]providers.Spec{{ID: "zai-t", Extends: providers.IDClaudeNative}}, nil)

	runRecipeTurn(t, r, task, task.ID, "zai-t", ThinkingLevelUnspecified, testClaudeModel)

	if got := readEnvDump(t, *task.WorktreePath); got != "" {
		t.Fatalf("CLI CLAUDE_CONFIG_DIR = %q, want none set", got)
	}
	if _, err := os.Stat(filepath.Join(home, "providers", "zai-t")); !os.IsNotExist(err) {
		t.Fatalf("providers/zai-t exists (%v), want nothing created", err)
	}
}

// Thinking "hidden" (the endpoint cannot turn thinking off) drops every
// thinking flag a level would add: the hidden instance's Extended argv
// equals its Unspecified argv, while a plain instance's differ.
func TestRecipe_ThinkingHiddenHidesOption(t *testing.T) {
	r, task, _ := recipeRunner(t, []providers.Spec{
		{ID: "zai-hidden", Extends: providers.IDClaudeNative, Thinking: "hidden"},
		{ID: "zai-plain", Extends: providers.IDClaudeNative},
	}, nil)

	hiddenExt := runRecipeTurn(t, r, task, task.ID, "zai-hidden", ThinkingLevelExtended, testClaudeModel)
	hiddenUnspec := runRecipeTurn(t, r, task, task.ID+1, "zai-hidden", ThinkingLevelUnspecified, testClaudeModel)
	plainExt := runRecipeTurn(t, r, task, task.ID+2, "zai-plain", ThinkingLevelExtended, testClaudeModel)
	plainUnspec := runRecipeTurn(t, r, task, task.ID+3, "zai-plain", ThinkingLevelUnspecified, testClaudeModel)

	if !slices.Equal(hiddenExt, hiddenUnspec) {
		t.Fatalf("hidden instance: Extended argv %q, want the Unspecified argv %q", hiddenExt, hiddenUnspec)
	}
	if slices.Equal(plainExt, plainUnspec) {
		t.Fatalf("plain instance: Extended argv equals the Unspecified argv %q, want the thinking flags", plainExt)
	}
	// The flags Extended adds on a plain instance are exactly what hidden
	// must not pass.
	var extOnly []string
	for _, a := range plainExt {
		if !slices.Contains(plainUnspec, a) {
			extOnly = append(extOnly, a)
		}
	}
	if len(extOnly) == 0 {
		t.Fatalf("no Extended-only flags found to compare: %q vs %q", plainExt, plainUnspec)
	}
	for _, a := range extOnly {
		if slices.Contains(hiddenExt, a) {
			t.Fatalf("hidden instance passed the thinking flag %q: %q", a, hiddenExt)
		}
	}
}

// A zai recipe instance runs against its endpoint: the CLI POSTs
// /v1/messages with the stored key as a Bearer header (never in argv), the
// tools/model flags come from the spec, and the config dir is private.
func TestRecipe_ZAI_RunAgainstFakeAnthropicServer(t *testing.T) {
	var mu sync.Mutex
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r, task, home := recipeRunner(t,
		[]providers.Spec{{ID: "zai-t", Extends: providers.IDClaudeNative, Vendor: "zai",
			DisallowedTools: []string{"WebSearch"},
			Models:          []providers.Model{{ID: "glm-5.3", Default: true}},
			SecretEnv:       map[string]string{"ANTHROPIC_AUTH_TOKEN": "api-key"}}},
		map[string]string{"ANTHROPIC_BASE_URL": srv.URL, "API_TIMEOUT_MS": "3000000"})
	WithProviderSecrets(&fakeSecretSource{secrets: map[string]string{"zai-t\x1fapi-key": zaiSentinel}})(r)

	args := runRecipeTurn(t, r, task, task.ID, "zai-t", ThinkingLevelUnspecified, "glm-5.3")

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/v1/messages" {
		t.Fatalf("endpoint saw path %q, want /v1/messages", gotPath)
	}
	if gotAuth != "Bearer "+zaiSentinel {
		t.Fatalf("endpoint Authorization = %q, want Bearer %q", gotAuth, zaiSentinel)
	}
	if v, ok := flagValue(args, "--disallowedTools"); !ok || v != "WebSearch" {
		t.Fatalf("--disallowedTools = %q (present %v), want WebSearch; args %q", v, ok, args)
	}
	if v, ok := flagValue(args, "--model"); !ok || v != "glm-5.3" {
		t.Fatalf("--model = %q (present %v), want glm-5.3; args %q", v, ok, args)
	}
	if slices.Contains(args, zaiSentinel) {
		t.Fatalf("the secret leaked into argv: %q", args)
	}
	if got := readEnvDump(t, *task.WorktreePath); got != filepath.Join(home, "providers", "zai-t", "claude") {
		t.Fatalf("CLI CLAUDE_CONFIG_DIR = %q, want the private dir", got)
	}
	if envAll := readLaunchEnv(t, task); !strings.Contains(envAll, "ANTHROPIC_BASE_URL="+srv.URL) {
		t.Fatalf("CLI env has no ANTHROPIC_BASE_URL=%s", srv.URL)
	}
	if hs, err := os.ReadFile(filepath.Join(*task.WorktreePath, "http-status")); err != nil || string(hs) != "200" {
		t.Fatalf("http-status = %q (%v), want 200", hs, err)
	}
}

// provider.list renders the recipe an instance was made from.
func TestProviderList_RecipeFields(t *testing.T) {
	t.Parallel()
	r, _ := instanceRunner(t,
		providers.Spec{ID: "zai-t", Extends: providers.IDClaudeNative, Vendor: "zai", Recipe: "zai-glm",
			Env: map[string]string{"ANTHROPIC_BASE_URL": "https://api.z.ai/api/anthropic"}},
		providers.Spec{ID: "mm-t", Extends: providers.IDClaudeNative, Recipe: "minimax", Thinking: "hidden",
			Env: map[string]string{"ANTHROPIC_BASE_URL": "https://api.minimax.io/anthropic"}})

	zai, ok := r.ProviderCatalogFor("zai-t")
	if !ok {
		t.Fatal("no zai-t instance")
	}
	if zai.Recipe != "zai-glm" || zai.Vendor != "zai" || zai.Thinking != "supported" ||
		zai.EndpointKind != "anthropic" || len(zai.Notes) == 0 || !zai.Unverified {
		t.Fatalf("zai-t recipe fields = %+v", zai)
	}
	mm, _ := r.ProviderCatalogFor("mm-t")
	if mm.Thinking != "hidden" {
		t.Fatalf("mm-t Thinking = %q, want hidden", mm.Thinking)
	}
	base, _ := r.ProviderCatalogFor(ProviderClaudeNative)
	if base.Recipe != "" || base.Vendor != "" || base.EndpointKind != "" || base.Notes != nil || base.Unverified {
		t.Fatalf("claude-native recipe fields = %+v, want none set", base)
	}
	if base.Thinking != "supported" {
		t.Fatalf("claude-native Thinking = %q, want supported", base.Thinking)
	}
}
