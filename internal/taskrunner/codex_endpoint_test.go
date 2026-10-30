package taskrunner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/codex"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/workspace"
)

// codexEndpointRunner builds a Runner whose registry holds one codex-native
// instance spec, spawning the fake codex app-server like codexRunner does.
func codexEndpointRunner(t *testing.T, wm *workspace.Manager, spec providers.Spec, env map[string]string) *Runner {
	t.Helper()
	r := New(wm, WithProviderRegistry(endpointRegistry(t, spec)), WithProviderEnv(Provider(spec.ID), env))
	r.newCodexClient = func(_ []string, opts ...codex.Option) (codexBackend, error) {
		return codex.New([]string{fakeCodexAgentPath}, opts...)
	}
	return r
}

func gwSpec() providers.Spec {
	return providers.Spec{ID: "gw", Extends: providers.IDCodexNative,
		Endpoint: providers.Endpoint{Kind: providers.EndpointOpenAIResponses, BaseURL: "https://gw.example"},
		Models:   []providers.Model{{ID: "gpt-x"}}}
}

// readThreadConfig reads the modelProvider/config a thread/start carried,
// as the fake agent recorded them in the worktree.
func readThreadConfig(t *testing.T, worktree string) (modelProvider string, config map[string]any, raw string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktree, "thread-config.thread-start"))
	if err != nil {
		t.Fatalf("read thread-config.thread-start: %v", err)
	}
	var tc struct {
		ModelProvider string         `json:"modelProvider"`
		Config        map[string]any `json:"config"`
	}
	if err := json.Unmarshal(data, &tc); err != nil {
		t.Fatalf("parse thread-config.thread-start: %v", err)
	}
	return tc.ModelProvider, tc.Config, string(data)
}

// threadConfigProvider digs config's model_providers.<id> entry out.
func threadConfigProvider(t *testing.T, config map[string]any, id string) map[string]any {
	t.Helper()
	mps, ok := config["model_providers"].(map[string]any)
	if !ok {
		t.Fatalf("config has no model_providers object: %v", config)
	}
	mp, ok := mps[id].(map[string]any)
	if !ok {
		t.Fatalf("model_providers has no %q entry: %v", id, mps)
	}
	return mp
}

// A run of a codex endpoint instance points Codex at the instance's
// endpoint (its model_providers entry) and never leaks the key's value into
// the wire config -- only the env var name travels in env_key.
func TestCodexEndpoint_RunSendsModelProvider(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	wm, task := newTestTask(t, "reply")
	r := codexEndpointRunner(t, wm, gwSpec(), map[string]string{codex.EndpointKeyEnv: "sentinel-not-a-real-key"})
	if err := runModelPrompt(t, r, task.ID, "gw", ""); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	modelProvider, config, raw := readThreadConfig(t, *task.WorktreePath)
	if modelProvider != "gw" {
		t.Fatalf("thread/start modelProvider = %q, want gw", modelProvider)
	}
	mp := threadConfigProvider(t, config, "gw")
	if mp["base_url"] != "https://gw.example/v1" {
		t.Fatalf("base_url = %v, want the normalized endpoint URL", mp["base_url"])
	}
	if mp["wire_api"] != "responses" {
		t.Fatalf("wire_api = %v, want responses", mp["wire_api"])
	}
	if mp["env_key"] != codex.EndpointKeyEnv {
		t.Fatalf("env_key = %v, want %s", mp["env_key"], codex.EndpointKeyEnv)
	}
	if mp["requires_openai_auth"] != false {
		t.Fatalf("requires_openai_auth = %v, want false", mp["requires_openai_auth"])
	}
	if strings.Contains(raw, "sentinel-not-a-real-key") {
		t.Fatal("thread/start config carries the API key's value")
	}
}

// Without a configured key the entry carries no env_key and no
// requires_openai_auth: resolving OPENAI_API_KEY would fail every request.
func TestCodexEndpoint_RunWithoutKeyOmitsEnvKey(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	wm, task := newTestTask(t, "reply")
	r := codexEndpointRunner(t, wm, gwSpec(), nil)
	if err := runModelPrompt(t, r, task.ID, "gw", ""); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	_, config, _ := readThreadConfig(t, *task.WorktreePath)
	mp := threadConfigProvider(t, config, "gw")
	for _, key := range []string{"env_key", "requires_openai_auth"} {
		if _, ok := mp[key]; ok {
			t.Fatalf("keyless entry carries %s: %v", key, mp)
		}
	}
}

// The instance gets a private CODEX_HOME under the smind home, unless the
// provider's own env already pins one.
func TestCodexEndpoint_PrivateCodexHome(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	r := New(nil, WithProviderRegistry(endpointRegistry(t, gwSpec())))
	_, env, err := r.codexEndpointLaunch("gw")
	if err != nil {
		t.Fatalf("codexEndpointLaunch() error = %v", err)
	}
	dir := filepath.Join(config.Dir(), "providers", "gw", "codex")
	if want := []string{"CODEX_HOME=" + dir}; !reflect.DeepEqual(env, want) {
		t.Fatalf("env = %v, want %v", env, want)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat codex home: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Fatalf("codex home mode = %o, want 700", fi.Mode().Perm())
	}

	// A user-pinned CODEX_HOME wins: no private entry, no directory.
	t.Setenv("SMIND_HOME", t.TempDir())
	WithProviderEnv("gw", map[string]string{"CODEX_HOME": "/x"})(r)
	_, env, err = r.codexEndpointLaunch("gw")
	if err != nil {
		t.Fatalf("codexEndpointLaunch() error = %v", err)
	}
	if len(env) != 0 {
		t.Fatalf("env = %v, want none with a user-pinned CODEX_HOME", env)
	}
	if _, err := os.Stat(filepath.Join(config.Dir(), "providers", "gw", "codex")); !os.IsNotExist(err) {
		t.Fatalf("codex home created despite a user-pinned CODEX_HOME (stat err = %v)", err)
	}
}

// A provider without an endpoint (codex-native itself) launches exactly as
// before: no options, no env, no error.
func TestCodexEndpoint_NonEndpointProviderUntouched(t *testing.T) {
	r := New(nil)
	opts, env, err := r.codexEndpointLaunch(ProviderCodexNative)
	if opts != nil || env != nil || err != nil {
		t.Fatalf("codexEndpointLaunch(codex-native) = %v, %v, %v; want nil, nil, nil", opts, env, err)
	}
}

// Codex only speaks the OpenAI Responses API; any other endpoint kind is a
// configuration error, not a silent fallthrough.
func TestCodexEndpoint_UnsupportedKindErrors(t *testing.T) {
	spec := gwSpec()
	spec.Endpoint.Kind = "openai-chat"
	r := New(nil, WithProviderRegistry(endpointRegistry(t, spec)))
	_, _, err := r.codexEndpointLaunch("gw")
	if err == nil || !strings.Contains(err.Error(), "unsupported by the Codex harness") {
		t.Fatalf("codexEndpointLaunch() error = %v, want the unsupported-kind error", err)
	}
}

// endpointRegistry is a registry holding exactly specs.
func endpointRegistry(t *testing.T, specs ...providers.Spec) *providers.Registry {
	t.Helper()
	reg := providers.New()
	if err := reg.SetCustom(specs); err != nil {
		t.Fatal(err)
	}
	return reg
}

// A key that comes through ADR-0046 typed env (passthrough here; a stored
// secret takes the same branch) still marks the endpoint as keyed, so the
// thread config names env_key.
func TestCodexEndpoint_TypedEnvKeySetsEnvKey(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "sentinel-not-a-key")
	spec := gwSpec()
	spec.PassthroughEnv = []string{"OPENAI_API_KEY"}
	wm, task := newTestTask(t, "reply")
	r := codexEndpointRunner(t, wm, spec, nil)
	if err := runModelPrompt(t, r, task.ID, "gw", ""); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	_, config, raw := readThreadConfig(t, *task.WorktreePath)
	mp := threadConfigProvider(t, config, "gw")
	if mp["env_key"] != "OPENAI_API_KEY" {
		t.Fatalf("env_key = %v, want OPENAI_API_KEY (provider %v)", mp["env_key"], mp)
	}
	if strings.Contains(raw, "sentinel-not-a-key") {
		t.Fatal("the key value reached the thread config")
	}
}
