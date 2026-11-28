package taskrunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

// clearProxyEnv empties every proxy environment name for the running test,
// so an ambient proxy setup (or its absence) cannot leak into the launch
// entries the test asserts on.
func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(name, "")
	}
}

// isProxyEnvName reports whether name is one of the proxy or no-proxy
// environment names.
func isProxyEnvName(name string) bool {
	switch name {
	case "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy":
		return true
	}
	return false
}

func Test_launch_env_entries_carry_the_proxy_defaults(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTPS_PROXY", "proxy.corp:3128")
	r, _ := secretInstanceRunner(t, providers.Spec{ID: "zai", Extends: providers.IDClaudeNative}, &fakeSecretSource{})

	entries, err := r.launchEnvEntries("zai")
	if err != nil {
		t.Fatalf("launchEnvEntries() error = %v", err)
	}
	env := envMapOf(entries)
	if env["HTTPS_PROXY"] != "http://proxy.corp:3128" {
		t.Fatalf("HTTPS_PROXY = %q, want http://proxy.corp:3128; entries %q", env["HTTPS_PROXY"], entries)
	}
	if env["NO_PROXY"] != "127.0.0.1,::1,localhost" {
		t.Fatalf("NO_PROXY = %q, want the loopback names; entries %q", env["NO_PROXY"], entries)
	}
}

func Test_launch_env_entries_are_unchanged_without_a_proxy(t *testing.T) {
	clearProxyEnv(t)
	r, _ := secretInstanceRunner(t, providers.Spec{ID: "zai", Extends: providers.IDClaudeNative}, &fakeSecretSource{})

	entries, err := r.launchEnvEntries("zai")
	if err != nil {
		t.Fatalf("launchEnvEntries() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("launchEnvEntries() = %q, want the same no-env entries as before the proxy defaults (none)", entries)
	}
	for _, e := range entries {
		if name, _, _ := strings.Cut(e, "="); isProxyEnvName(name) {
			t.Fatalf("launchEnvEntries() = %q, want no proxy or no-proxy entry without a proxy set", entries)
		}
	}
}

func Test_isolated_claude_env_carries_the_proxy_defaults(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTPS_PROXY", "proxy.corp:3128")
	wm, task := newIsolatedTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)

	if _, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"}); err != nil {
		t.Fatalf("RunPromptIsolated() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "env-all"))
	if err != nil {
		t.Fatalf("read env-all: %v", err)
	}
	got := string(data)
	for _, want := range []string{"HTTPS_PROXY=http://proxy.corp:3128", "NO_PROXY=127.0.0.1,::1,localhost"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the isolated claude env lacks %q; env:\n%s", want, got)
		}
	}
}
