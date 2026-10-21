//go:build liveprobe && !windows

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestLive_CanaryAttests makes ONE real model call (claude-haiku-5-5, at most
// USD 0.05): the canary must be confined by the sandbox and mint an
// attestation. Gated on SMIND_LIVE_PROBE=1; never run by CI.
func TestLive_CanaryAttests(t *testing.T) {
	if os.Getenv("SMIND_LIVE_PROBE") != "1" {
		t.Skip("set SMIND_LIVE_PROBE=1 to make one live model call")
	}
	if !Supported() {
		t.Skip("macOS only")
	}
	cli, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude CLI not on PATH")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCanary(root)
	if err != nil {
		t.Fatal(err)
	}
	priv := filepath.Join(root, "private")
	if err := os.Mkdir(priv, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := Prepare(Config{ArmDir: c.ArmDir, ProjectionDir: c.ProjectionDir, PrivateDir: priv, RealCLI: cli})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	att, obs, err := RunCanary(ctx, l, c, "claude-haiku-5-5", nil)
	t.Logf("canary cost USD %.4f, turns %d, permission denials %d, init tools %v", obs.CostUSD, obs.Turns, obs.PermissionDenials, obs.InitTools)
	if err != nil {
		t.Fatalf("canary failed: %v", err)
	}
	if !att.Valid(time.Now()) {
		t.Fatalf("no valid attestation: %+v", att)
	}
}
