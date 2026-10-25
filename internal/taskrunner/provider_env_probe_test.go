//go:build !windows

package taskrunner

import (
	"context"
	"testing"
)

// The sandboxed probe never takes the smind setting: its launcher file is
// built from the daemon's own environment allowlist (sandbox.AllowlistEnv).
func TestProbeLaunchIgnoresProviderEnvSetting(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/daemon/own/login")
	f := newProbeFixture(t, "probe-env")
	f.r.SetProviderEnv(ProviderClaudeNative, "CLAUDE_CONFIG_DIR", "/from/smind/settings")
	if _, err := f.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := readEnvDump(t, f.launch.ArmDir)
	if got == "/from/smind/settings" {
		t.Fatal("the sandboxed probe took the smind setting")
	}
	if got != "/daemon/own/login" {
		t.Fatalf("probe CLAUDE_CONFIG_DIR = %q, want the launcher's own allowlisted value", got)
	}
}
