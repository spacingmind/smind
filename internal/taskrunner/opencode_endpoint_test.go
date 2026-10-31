package taskrunner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/securefile"
)

// chatKeySentinel is the only "key" these tests use: not valid for anything.
const chatKeySentinel = "sentinel-CHAT-NOT-A-REAL-KEY-98765"

const openCodeChatID Provider = "vendor-x"

// openCodeChatRunner registers one opencode chat-completions instance through
// the real config path (CustomProvider -> ProviderSpecs) in a throwaway
// SMIND_HOME, with the vendor key in a fake secret store when withKey. The
// returned dir is the instance's private OpenCode home.
func openCodeChatRunner(t *testing.T, withKey bool) (r *Runner, home string) {
	t.Helper()
	smindHome := t.TempDir()
	t.Setenv("SMIND_HOME", smindHome)
	entry := config.CustomProvider{
		ID: string(openCodeChatID), Label: "Vendor X", Extends: providers.IDOpenCode,
		Endpoint: &config.CustomEndpoint{Kind: "chat-completions", BaseURL: "https://api.vendor.test/v1"},
		Models:   []config.CustomModel{{ID: "chat-1", Default: true}, {ID: "qwen/qwen3-coder"}},
	}
	src := &fakeSecretSource{secrets: map[string]string{}}
	if withKey {
		entry.Env = map[string]config.EnvValue{providers.OpenCodeKeyEnv: {Secret: "api-key"}}
		src.secrets[string(openCodeChatID)+"\x1f"+"api-key"] = chatKeySentinel
	}
	list := []config.CustomProvider{entry}
	if err := config.ValidateCustomProviders(list); err != nil {
		t.Fatal(err)
	}
	reg := providers.New()
	if err := reg.SetCustom(config.ProviderSpecs(list)); err != nil {
		t.Fatal(err)
	}
	r = New(nil, WithProviderRegistry(reg), WithProviderSecrets(src))
	return r, filepath.Join(smindHome, "providers", string(openCodeChatID), "opencode")
}

func envMapOf(entries []string) map[string]string {
	m := map[string]string{}
	for _, e := range entries {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

// The whole translation of the instance into OpenCode's configuration, as the
// child sees it.
func TestOpenCodeEndpoint_LaunchEntries(t *testing.T) {
	r, home := openCodeChatRunner(t, true)
	entries, err := r.launchEnvEntries(openCodeChatID)
	if err != nil {
		t.Fatal(err)
	}
	env := envMapOf(entries)

	if env[providers.OpenCodeKeyEnv] != chatKeySentinel {
		t.Fatalf("%s = %q; want the stored key in the child's environment", providers.OpenCodeKeyEnv, env[providers.OpenCodeKeyEnv])
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("OPENCODE_CONFIG_CONTENT is not JSON: %v", err)
	}
	if cfg["model"] != "smind-vendor-x/chat-1" {
		t.Errorf("model = %v", cfg["model"])
	}
	for name, want := range map[string]string{
		"OPENCODE_DISABLE_PROJECT_CONFIG": "1", "OPENCODE_DISABLE_AUTOUPDATE": "1", "OPENCODE_DISABLE_SHARE": "1",
		"OPENCODE_DISABLE_LSP_DOWNLOAD": "1", "OPENCODE_DISABLE_MODELS_FETCH": "1",
		"XDG_CONFIG_HOME": filepath.Join(home, "config"), "XDG_DATA_HOME": filepath.Join(home, "data"),
		"XDG_STATE_HOME": filepath.Join(home, "state"), "XDG_CACHE_HOME": filepath.Join(home, "cache"),
	} {
		if env[name] != want {
			t.Errorf("%s = %q, want %q", name, env[name], want)
		}
	}
	// The built-in ask policy for bash and edit survives the instance.
	if !strings.Contains(env["OPENCODE_PERMISSION"], `"bash":"ask"`) {
		t.Errorf("OPENCODE_PERMISSION = %q; the shipped ask policy was lost", env["OPENCODE_PERMISSION"])
	}
	// The managed entries come last, so nothing earlier can override them.
	last := map[string]int{}
	for i, e := range entries {
		k, _, _ := strings.Cut(e, "=")
		last[k] = i
	}
	for _, name := range []string{"OPENCODE_CONFIG_CONTENT", "OPENCODE_DISABLE_PROJECT_CONFIG", "XDG_DATA_HOME"} {
		if last[name] < last[providers.OpenCodeKeyEnv] {
			t.Errorf("%s (entry %d) precedes the key entry (%d): it could be overridden", name, last[name], last[providers.OpenCodeKeyEnv])
		}
	}
}

// The key is in the child's environment and nowhere else: not in the
// generated config, not in the command line, not in any other entry.
func TestOpenCodeEndpoint_KeyOnlyInItsOwnEnvEntry(t *testing.T) {
	r, _ := openCodeChatRunner(t, true)
	entries, err := r.launchEnvEntries(openCodeChatID)
	if err != nil {
		t.Fatal(err)
	}
	holders := 0
	for _, e := range entries {
		if strings.Contains(e, chatKeySentinel) {
			holders++
			if !strings.HasPrefix(e, providers.OpenCodeKeyEnv+"=") {
				t.Errorf("the key appears in another entry: %.60s...", e)
			}
		}
	}
	if holders != 1 {
		t.Errorf("%d entries hold the key, want exactly 1", holders)
	}
	if !strings.Contains(envMapOf(entries)["OPENCODE_CONFIG_CONTENT"], "{env:"+providers.OpenCodeKeyEnv+"}") {
		t.Error("the config does not reference the key by name")
	}
	cmd, ok := r.acpCommandFor(openCodeChatID)
	if !ok {
		t.Fatal("no ACP command for the instance")
	}
	if got := strings.Join(cmd, " "); strings.Contains(got, chatKeySentinel) || got != "opencode acp" {
		t.Errorf("argv = %q; want %q and no key", got, "opencode acp")
	}
	for _, v := range r.providerSecretValues(openCodeChatID) {
		if v != chatKeySentinel {
			t.Errorf("an unexpected secret is scrubbed: %q", v)
		}
	}
	if secrets := r.providerSecretValues(openCodeChatID); len(secrets) != 1 {
		t.Errorf("secret values to scrub = %v; want the one key (so errors are scrubbed)", secrets)
	}
}

// Without a key the config has no apiKey (a local server that needs none).
func TestOpenCodeEndpoint_KeylessInstanceHasNoApiKey(t *testing.T) {
	r, _ := openCodeChatRunner(t, false)
	entries, err := r.launchEnvEntries(openCodeChatID)
	if err != nil {
		t.Fatal(err)
	}
	env := envMapOf(entries)
	if _, ok := env[providers.OpenCodeKeyEnv]; ok {
		t.Error("a keyless instance still passes a key entry")
	}
	if strings.Contains(env["OPENCODE_CONFIG_CONTENT"], "apiKey") {
		t.Errorf("a keyless config carries an apiKey: %s", env["OPENCODE_CONFIG_CONTENT"])
	}
}

// A missing secret is an error that names the secret, never a value.
func TestOpenCodeEndpoint_MissingSecretErrorNamesNotValue(t *testing.T) {
	r, _ := openCodeChatRunner(t, true)
	WithProviderSecrets(&fakeSecretSource{secrets: map[string]string{}})(r)
	_, err := r.launchEnvEntries(openCodeChatID)
	if err == nil || !strings.Contains(err.Error(), `secret "api-key"`) || strings.Contains(err.Error(), chatKeySentinel) {
		t.Fatalf("err = %v", err)
	}
}

// The instance's own OpenCode home is private: nobody but the current user
// (and SYSTEM) can read what OpenCode writes there (conversations, logs), on
// Windows by an inheritable ACL, checked on files created after the fact.
func TestOpenCodeEndpoint_PrivateHomeIsPrivateIncludingWhatOpenCodeWrites(t *testing.T) {
	r, home := openCodeChatRunner(t, true)
	if _, err := r.launchEnvEntries(openCodeChatID); err != nil {
		t.Fatal(err)
	}
	// What OpenCode would write: a db under data, logs under state, a cache.
	for _, rel := range []string{"data/opencode/opencode.db", "state/opencode/log/run.log", "cache/opencode/models.json", "config/opencode/node_modules/x/index.js"} {
		p := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("conversation text"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ok, err := securefile.IsPrivateTree(home)
	if err != nil || !ok {
		t.Fatalf("IsPrivateTree(%s) = %v, %v; want true", home, ok, err)
	}
	// Launching again (every run does) keeps it that way and keeps the files.
	if _, err := r.launchEnvEntries(openCodeChatID); err != nil {
		t.Fatal(err)
	}
	if ok, err := securefile.IsPrivateTree(home); err != nil || !ok {
		t.Fatalf("after a second launch IsPrivateTree = %v, %v", ok, err)
	}
	if b, err := os.ReadFile(filepath.Join(home, "data", "opencode", "opencode.db")); err != nil || string(b) != "conversation text" {
		t.Errorf("the owner lost access to OpenCode's own file: %q, %v", b, err)
	}
}

// An OpenCode instance without an endpoint is launched exactly as before: no
// private home, no generated config, nothing disabled.
func TestOpenCodeEndpoint_PlainInstanceUntouched(t *testing.T) {
	smindHome := t.TempDir()
	t.Setenv("SMIND_HOME", smindHome)
	list := []config.CustomProvider{{ID: "oc-plain", Extends: providers.IDOpenCode}}
	if err := config.ValidateCustomProviders(list); err != nil {
		t.Fatal(err)
	}
	reg := providers.New()
	if err := reg.SetCustom(config.ProviderSpecs(list)); err != nil {
		t.Fatal(err)
	}
	r := New(nil, WithProviderRegistry(reg))
	entries, err := r.launchEnvEntries("oc-plain")
	if err != nil {
		t.Fatal(err)
	}
	builtin, err := r.launchEnvEntries(providers.IDOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	got, base := envMapOf(entries), envMapOf(builtin)
	for _, managed := range providers.OpenCodeManagedEnv {
		// Only what the base itself sets (OPENCODE_DISABLE_AUTOUPDATE) may appear.
		if got[managed] != base[managed] {
			t.Errorf("a plain instance's %s = %q, the built-in provider's is %q", managed, got[managed], base[managed])
		}
	}
	if got["OPENCODE_PERMISSION"] != base["OPENCODE_PERMISSION"] || got["OPENCODE_PERMISSION"] == "" {
		t.Errorf("OPENCODE_PERMISSION = %q, want the base's %q", got["OPENCODE_PERMISSION"], base["OPENCODE_PERMISSION"])
	}
	if _, err := os.Stat(filepath.Join(smindHome, "providers", "oc-plain")); !os.IsNotExist(err) {
		t.Errorf("a plain instance created a private home: %v", err)
	}
	// And so does the built-in opencode provider.
	if got, err := r.launchEnvEntries(providers.IDOpenCode); err != nil || len(envMapOf(got)["OPENCODE_CONFIG_CONTENT"]) != 0 {
		t.Errorf("the built-in provider got a generated config: %v %v", got, err)
	}
}

// The model a run asks OpenCode for is mapped into the instance's provider key.
func TestOpenCodeEndpoint_ModelIsMappedToTheInstanceProviderKey(t *testing.T) {
	r, _ := openCodeChatRunner(t, true)
	for in, want := range map[string]string{
		"chat-1":           "smind-vendor-x/chat-1",
		"qwen/qwen3-coder": "smind-vendor-x/qwen/qwen3-coder",
		"":                 "",
	} {
		if got := r.acpModelFor(openCodeChatID, in); got != want {
			t.Errorf("acpModelFor(%q) = %q, want %q", in, got, want)
		}
	}
	if got := r.acpModelFor(providers.IDOpenCode, "anthropic/claude"); got != "anthropic/claude" {
		t.Errorf("the built-in opencode provider's model was rewritten: %q", got)
	}
	if got := r.acpModelFor("no-such-provider", "m"); got != "m" {
		t.Errorf("an unknown provider's model was rewritten: %q", got)
	}
}
