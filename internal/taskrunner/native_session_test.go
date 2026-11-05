package taskrunner

import (
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/providers"
)

// NativeSession must name the directory the CLI is launched with (the rule of
// claudeConfigDirEntries / codexEndpointLaunch), or a later reader looks in the
// wrong place.
func TestNativeSessionDirs(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	instance := providers.Spec{ID: "claude-x", Extends: "claude-native"}
	base := map[string]string{"ANTHROPIC_BASE_URL": "https://example.invalid"}
	if got, want := claudeConfigDirFor(instance, base), filepath.Join(config.Dir(), "providers", "claude-x", "claude"); got != want {
		t.Errorf("private claude dir = %q, want %q", got, want)
	}
	inherits := instance
	inherits.InheritClaudeConfig = true
	if got, want := claudeConfigDirFor(inherits, base), filepath.Join(home, ".claude"); got != want {
		t.Errorf("an instance that inherits ~/.claude = %q, want %q", got, want)
	}
	if got := claudeConfigDirFor(instance, map[string]string{"CLAUDE_CONFIG_DIR": "/pinned"}); got != "/pinned" {
		t.Errorf("a pinned dir = %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/from/daemon")
	if got := claudeConfigDirFor(providers.Spec{ID: "claude-native"}, nil); got != "/from/daemon" {
		t.Errorf("the base provider follows the daemon's env: %q", got)
	}

	if got, want := codexHomeFor(providers.Spec{ID: "codex-native"}, nil), filepath.Join(home, ".codex"); got != want {
		t.Errorf("default codex home = %q, want %q", got, want)
	}
	t.Setenv("CODEX_HOME", "/codex/daemon")
	if got := codexHomeFor(providers.Spec{ID: "codex-native"}, nil); got != "/codex/daemon" {
		t.Errorf("codex home from env = %q", got)
	}
	if got := codexHomeFor(providers.Spec{ID: "codex-x"}, map[string]string{"CODEX_HOME": "/pinned"}); got != "/pinned" {
		t.Errorf("pinned codex home = %q", got)
	}
}

func TestNativeSessionNeedsMatchingProviderHandle(t *testing.T) {
	store := NewMemorySessionStore()
	r := New(nil, WithSessionStore(store))
	if _, ok := r.NativeSession(1, ProviderClaudeNative); ok {
		t.Fatal("no handle must report nothing")
	}
	store.Set(1, SessionHandle{Provider: ProviderGLM, SessionID: "s"})
	if _, ok := r.NativeSession(1, ProviderClaudeNative); ok {
		t.Fatal("a handle of another provider must not be reported")
	}
	store.Set(2, SessionHandle{Provider: ProviderClaudeNative, SessionID: "s2"})
	ns, ok := r.NativeSession(2, ProviderClaudeNative)
	if !ok || ns.SessionID != "s2" || ns.Harness != "claude-code" {
		t.Fatalf("NativeSession = %+v, %v", ns, ok)
	}
}
