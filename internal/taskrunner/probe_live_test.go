//go:build liveprobe && !windows

package taskrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/probe/arm"
	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
)

// TestLiveProbe_ArmRunThroughRunPromptProbe makes ONE real model call
// (claude-haiku-5-5, capped at USD 0.05): a sandboxed arm run through
// RunPromptProbe must edit a file inside the arm, fail to write outside it,
// report End=done with a plausible usage and leave no process behind.
// Gated on SMIND_LIVE_PROBE=1; never run by CI.
func TestLiveProbe_ArmRunThroughRunPromptProbe(t *testing.T) {
	if os.Getenv("SMIND_LIVE_PROBE") != "1" {
		t.Skip("set SMIND_LIVE_PROBE=1 to make one live model call")
	}
	if !sandbox.Supported() {
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
	armDir, priv, outside := filepath.Join(root, "arm"), filepath.Join(root, "priv"), filepath.Join(root, "outside")
	for _, d := range []string{armDir, priv, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	l, err := sandbox.Prepare(sandbox.Config{ArmDir: armDir, PrivateDir: priv, RealCLI: cli})
	if err != nil {
		t.Fatal(err)
	}
	var rep ProbeReport
	po := ProbeOptions{Launch: l, Attestation: sandbox.AttestationForTest(t, time.Now()), MaxTurns: 6, CostCapUSD: 0.05, Timeout: 90 * time.Second, OutputCapBytes: 64 << 10,
		OnReport: func(r ProbeReport) { rep = r }}
	prompt := "Using the Bash tool, run exactly: printf probe-ok > " + armDir + "/in.txt ; then run exactly: printf nope > " + outside +
		"/out.txt (it may fail; do not retry). Then reply with the single word done."
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- New(nil).RunPromptProbe(context.Background(), 0, 0, ProviderClaudeNative, prompt, "claude-haiku-5-5", po, events)
	}()
	var got []Event
	for e := range events {
		got = append(got, e)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("RunPromptProbe: %v (report %+v)", err, rep)
	}
	t.Logf("report: end=%s turns=%d denials=%d cost=%.4f tools=%v events=%d", rep.End, rep.Turns, rep.PermissionDenials, rep.CostUSD, rep.InitTools, len(got))
	if rep.End != arm.EndDone {
		t.Errorf("End = %s, want done", rep.End)
	}
	if b, _ := os.ReadFile(filepath.Join(armDir, "in.txt")); strings.TrimSpace(string(b)) != "probe-ok" {
		t.Errorf("the write inside the arm did not land: %q", b)
	}
	if _, err := os.Stat(filepath.Join(outside, "out.txt")); err == nil {
		t.Error("the write OUTSIDE the arm landed: sandbox breach")
	}
	if rep.CostUSD <= 0 {
		t.Errorf("cost not reported: %v", rep.CostUSD)
	}
	if err := arm.KillTree(context.Background(), l.PidFile, l.ArmDir, time.Second); err != nil {
		t.Errorf("a second KillTree must find nothing to kill: %v", err)
	}
}
