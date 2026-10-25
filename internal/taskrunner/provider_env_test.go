package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func readEnvDump(t *testing.T, worktree string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktree, "env-config-dir"))
	if err != nil {
		t.Fatalf("read env-config-dir: %v", err)
	}
	return string(data)
}

func TestClaudeLaunchEnvFromProviderEnv(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	r.SetProviderEnv(ProviderClaudeNative, "CLAUDE_CONFIG_DIR", "/from/smind/settings")
	runClaudeMcp(t, r, task)
	if got := readEnvDump(t, *task.WorktreePath); got != "/from/smind/settings" {
		t.Fatalf("CLI CLAUDE_CONFIG_DIR = %q, want the setting", got)
	}
}

func TestClaudeLaunchEnvSettingBeatsProcessEnv(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/from/daemon/environment")
	r, _, task := newClaudeMcpEnv(t)
	r.SetProviderEnv(ProviderClaudeNative, "CLAUDE_CONFIG_DIR", "/from/smind/settings")
	runClaudeMcp(t, r, task)
	if got := readEnvDump(t, *task.WorktreePath); got != "/from/smind/settings" {
		t.Fatalf("CLI CLAUDE_CONFIG_DIR = %q, want the setting to win over the daemon's environment", got)
	}
}

func TestClaudeLaunchEnvAbsentSettingUnchanged(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/from/daemon/environment")
	r, _, task := newClaudeMcpEnv(t)
	runClaudeMcp(t, r, task)
	if got := readEnvDump(t, *task.WorktreePath); got != "/from/daemon/environment" {
		t.Fatalf("CLI CLAUDE_CONFIG_DIR = %q, want the daemon's own value untouched", got)
	}
	// Setting then clearing returns to today's behaviour on the next launch.
	r.SetProviderEnv(ProviderClaudeNative, "CLAUDE_CONFIG_DIR", "/x")
	r.SetProviderEnv(ProviderClaudeNative, "CLAUDE_CONFIG_DIR", "")
	runClaudeMcp(t, r, task)
	if got := readEnvDump(t, *task.WorktreePath); got != "/from/daemon/environment" {
		t.Fatalf("after clear CLAUDE_CONFIG_DIR = %q", got)
	}
}

// The next launch reads the setting live; no Runner rebuild.
func TestProviderEnvAppliesToNextLaunch(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	runClaudeMcp(t, r, task)
	first := readEnvDump(t, *task.WorktreePath)
	r.SetProviderEnv(ProviderClaudeNative, "CLAUDE_CONFIG_DIR", "/live/change")
	runClaudeMcp(t, r, task)
	if got := readEnvDump(t, *task.WorktreePath); got != "/live/change" || first == got {
		t.Fatalf("first launch %q, second %q", first, got)
	}
}

func TestIsolatedLaunchEnvFromProviderEnv(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/from/daemon/environment")
	wm, task := newIsolatedTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	r.SetProviderEnv(ProviderClaudeNative, "CLAUDE_CONFIG_DIR", "/from/smind/settings")
	if _, err := runIsolated(t, r, task, ProviderClaudeNative, IsolationOptions{SystemPrompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if got := readEnvDump(t, *task.WorktreePath); got != "/from/smind/settings" {
		t.Fatalf("isolated CLAUDE_CONFIG_DIR = %q", got)
	}
}

func TestCodexLaunchEnvFromProviderEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	out := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("CODEX_HOME", "/from/daemon/environment")
	r := New(nil)
	r.codexCommand = []string{"/bin/sh", "-c", `printf %s "$CODEX_HOME" > ` + out + `; exit 1`}
	r.SetProviderEnv(ProviderCodexNative, "CODEX_HOME", "/from/smind/settings")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = r.probeCodexModels(ctx, ProviderCodexNative) // the stand-in is no app-server; only its env matters
	var got []byte
	for i := 0; i < 100; i++ {
		var err error
		if got, err = os.ReadFile(out); err == nil && len(got) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if strings.TrimSpace(string(got)) != "/from/smind/settings" {
		t.Fatalf("codex CODEX_HOME = %q, want the setting to win over the daemon's environment", got)
	}
}

func TestProviderEnvCopyAndClear(t *testing.T) {
	r := New(nil)
	r.SetProviderEnv(ProviderClaudeNative, "CLAUDE_CONFIG_DIR", "/a")
	m := r.ProviderEnv(ProviderClaudeNative)
	m["CLAUDE_CONFIG_DIR"] = "/mutated"
	if got := r.ProviderEnv(ProviderClaudeNative)["CLAUDE_CONFIG_DIR"]; got != "/a" {
		t.Fatalf("ProviderEnv leaked its map: %q", got)
	}
	r.SetProviderEnv(ProviderClaudeNative, "CLAUDE_CONFIG_DIR", "")
	if r.ProviderEnv(ProviderClaudeNative) != nil {
		t.Fatal("cleared env still listed")
	}
	if e := r.providerEnvEntries(ProviderCodexNative); len(e) != 0 {
		t.Fatalf("unset provider has entries %v", e)
	}
}
