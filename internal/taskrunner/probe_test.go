//go:build !windows

package taskrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/learning/probe/arm"
	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
)

// probeFixture is a prepared arm whose "claude" is this test binary running
// the given fakeclaude scenario behind the real launcher.
type probeFixture struct {
	r      *Runner
	launch *sandbox.Launch
	po     ProbeOptions
	report *ProbeReport
	calls  *atomic.Int32
}

func newProbeFixture(t *testing.T, scenario string) *probeFixture {
	t.Helper()
	return newProbeFixtureEnv(t, scenario, "")
}

// newProbeFixtureEnv is newProbeFixture with extra NAME=VALUE text spliced
// into the stand-in's wrapper environment.
func newProbeFixtureEnv(t *testing.T, scenario, extraEnv string) *probeFixture {
	t.Helper()
	if !sandbox.Supported() {
		t.Skip("the probe sandbox is macOS only")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	armDir, priv := filepath.Join(root, "arm"), filepath.Join(root, "priv")
	for _, d := range []string{armDir, priv} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	wrapper := filepath.Join(root, "fake-claude")
	script := "#!/bin/sh\nTASKRUNNER_FAKE_CLAUDE_CLI=1 FAKE_CLAUDE_SCENARIO='" + scenario + "' " + strings.ReplaceAll(extraEnv, "@PRIV@", priv) + " exec '" + self + "' \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := sandbox.Prepare(sandbox.Config{ArmDir: armDir, PrivateDir: priv, RealCLI: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	f := &probeFixture{r: New(nil), launch: l, report: &ProbeReport{}, calls: &atomic.Int32{}}
	f.po = ProbeOptions{Launch: l, Attestation: sandbox.AttestationForTest(t, time.Now()), MaxTurns: 3, CostCapUSD: 0.05, Timeout: 20 * time.Second, OnReport: func(r ProbeReport) {
		f.calls.Add(1)
		*f.report = r
	}}
	return f
}

func (f *probeFixture) run(ctx context.Context) ([]Event, error) {
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- f.r.RunPromptProbe(ctx, 0, 0, ProviderClaudeNative, "hi", "", f.po, events)
	}()
	got := drainEvents(events)
	return got, <-errCh
}

func TestRunPromptProbe_RequiresLaunchAndCaps(t *testing.T) {
	r := New(nil)
	var spawned atomic.Int32
	r.newClaudeClient = func(string, ...claudecode.Option) (claudeBackend, error) {
		spawned.Add(1)
		return nil, errors.New("must not spawn")
	}
	f := newProbeFixture(t, "probe-ok")
	good := f.po
	mod := func(fn func(po *ProbeOptions)) ProbeOptions { po := good; fn(&po); return po }
	cases := map[string]ProbeOptions{
		"no launch":      mod(func(po *ProbeOptions) { po.Launch = nil }),
		"unprepared":     mod(func(po *ProbeOptions) { po.Launch = &sandbox.Launch{ArmDir: po.Launch.ArmDir} }),
		"no turns":       mod(func(po *ProbeOptions) { po.MaxTurns = 0 }),
		"no cost cap":    mod(func(po *ProbeOptions) { po.CostCapUSD = 0 }),
		"no timeout":     mod(func(po *ProbeOptions) { po.Timeout = 0 }),
		"negative turns": mod(func(po *ProbeOptions) { po.MaxTurns = -1 }),
		"no attestation": mod(func(po *ProbeOptions) { po.Attestation = sandbox.Attestation{} }),
		"stale attestation": mod(func(po *ProbeOptions) {
			po.Attestation = sandbox.AttestationForTest(t, time.Now().Add(-sandbox.AttestationTTL-time.Minute))
		}),
		"future attestation": mod(func(po *ProbeOptions) {
			po.Attestation = sandbox.AttestationForTest(t, time.Now().Add(time.Hour))
		}),
	}
	for name, po := range cases {
		var reports []ProbeReport
		po.OnReport = func(r ProbeReport) { reports = append(reports, r) }
		events := make(chan Event)
		errCh := make(chan error, 1)
		go func() {
			errCh <- r.RunPromptProbe(context.Background(), 0, 0, ProviderClaudeNative, "p", "", po, events)
		}()
		drainEvents(events)
		if err := <-errCh; err == nil {
			t.Errorf("%s: accepted", name)
		}
		if len(reports) != 1 || reports[0].End != arm.EndError {
			t.Errorf("%s: reports = %+v, want exactly one error report", name, reports)
		}
	}
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() { errCh <- r.RunPromptProbe(context.Background(), 0, 0, ProviderGLM, "p", "", good, events) }()
	drainEvents(events)
	if err := <-errCh; !errors.Is(err, ErrIsolationUnsupported) {
		t.Errorf("non-claude provider: %v", err)
	}
	if spawned.Load() != 0 {
		t.Errorf("a client was spawned %d time(s) for an invalid request", spawned.Load())
	}
}

func TestRunPromptProbe_AppliesLaunchOptionsOnly(t *testing.T) {
	f := newProbeFixture(t, "probe-env")
	t.Setenv("FAKE_API_KEY", "must-not-reach-the-cli")
	if _, err := f.run(context.Background()); err != nil {
		t.Fatalf("run: %v (report %+v)", err, *f.report)
	}
	b, err := os.ReadFile(filepath.Join(f.launch.ArmDir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(string(b), "\n")
	has := func(a string) bool {
		for _, x := range argv {
			if x == a || strings.HasPrefix(x, a+"=") {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"--tools", "--permission-mode", "--settings", "--strict-mcp-config", "--setting-sources", "--max-turns", "--max-budget-usd", "--disallowedTools"} {
		if !has(want) {
			t.Errorf("argv lacks %s: %q", want, argv)
		}
	}
	for _, bad := range []string{"--mcp-config", "--resume", "--continue", "--add-dir", "--permission-prompt-tool", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "--session-id", "--model"} {
		if has(bad) {
			t.Errorf("argv must not contain %s: %q", bad, argv)
		}
	}
	envb, _ := os.ReadFile(filepath.Join(f.launch.ArmDir, "env-names"))
	env := string(envb)
	if strings.Count(env, "ENABLE_CLAUDEAI_MCP_SERVERS=false") != 1 {
		t.Errorf("connector opt-out not set exactly once:\n%s", env)
	}
	if strings.Contains(env, "FAKE_API_KEY") {
		t.Error("a daemon secret-looking var reached the CLI")
	}
	if f.calls.Load() != 1 {
		t.Errorf("OnReport called %d times", f.calls.Load())
	}
	rep := *f.report
	if rep.End != arm.EndDone || rep.Turns != 2 || rep.PermissionDenials != 2 || len(rep.InitTools) != 7 || rep.CostUSD != 0.001 {
		t.Errorf("report = %+v", rep)
	}
}

func TestArmCaps_TimeoutTurnsCostOutputEachEndArm(t *testing.T) {
	cases := []struct {
		name, scenario string
		want           arm.End
		tweak          func(f *probeFixture)
		ctx            func() (context.Context, context.CancelFunc)
	}{
		{name: "timeout", scenario: "probe-hang", want: arm.EndTimeout, tweak: func(f *probeFixture) { f.po.Timeout = time.Second }},
		{name: "turns", scenario: "probe-max-turns", want: arm.EndTurns},
		{name: "capped", scenario: "probe-max-budget", want: arm.EndCapped},
		{name: "output", scenario: "probe-flood", want: arm.EndOutput, tweak: func(f *probeFixture) { f.po.OutputCapBytes = 1000 }},
		{name: "killed", scenario: "probe-hang", want: arm.EndKilled, ctx: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(700*time.Millisecond, cancel)
			return ctx, cancel
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newProbeFixture(t, tc.scenario)
			if tc.tweak != nil {
				tc.tweak(f)
			}
			ctx := context.Background()
			if tc.ctx != nil {
				var cancel context.CancelFunc
				ctx, cancel = tc.ctx()
				defer cancel()
			}
			events, err := f.run(ctx)
			if !errors.Is(err, ErrProbeEnded) || !strings.Contains(err.Error(), string(tc.want)) {
				t.Fatalf("err = %v, want ErrProbeEnded naming %s", err, tc.want)
			}
			if f.calls.Load() != 1 || f.report.End != tc.want {
				t.Errorf("reports = %d, End = %q, want one report with %q", f.calls.Load(), f.report.End, tc.want)
			}
			if n := len(events); n == 0 || events[n-1].Type != EventTypeDone || events[n-1].StopReason != "probe_"+string(tc.want) {
				t.Errorf("last event = %+v, want Done probe_%s", events[len(events)-1:], tc.want)
			}
		})
	}
}

func TestArmEnd_NoSurvivingChildProcess(t *testing.T) {
	f := newProbeFixture(t, "probe-child")
	if _, err := f.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(f.launch.ArmDir, "child.pid"))
	if err != nil {
		t.Fatalf("the stand-in recorded no child: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		t.Fatalf("child pid %q", b)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) }) // never leave a sleeper behind
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Errorf("background child %d survived the arm", pid)
	}
}

func TestRunPromptProbe_UnreportedUsageChargedAtCap(t *testing.T) {
	f := newProbeFixture(t, "probe-unreported")
	_, err := f.run(context.Background())
	if !errors.Is(err, ErrProbeEnded) || f.report.End != arm.EndCapped {
		t.Fatalf("err = %v, report = %+v, want capped", err, *f.report)
	}
	if f.report.CostUSD != f.po.CostCapUSD {
		t.Errorf("charged %v, want the whole cap %v", f.report.CostUSD, f.po.CostCapUSD)
	}
}

func TestRunPromptProbe_WrongToolSurfaceFailsClosed(t *testing.T) {
	f := newProbeFixture(t, "probe-bad-tools")
	events, err := f.run(context.Background())
	if !errors.Is(err, ErrIsolationViolated) {
		t.Fatalf("err = %v, want ErrIsolationViolated", err)
	}
	if f.report.End != arm.EndError || f.calls.Load() != 1 {
		t.Errorf("report = %+v (calls %d)", *f.report, f.calls.Load())
	}
	for _, e := range events {
		if e.Type == EventTypeText {
			t.Errorf("an event was forwarded after the bad init: %+v", e)
		}
	}
}

// The real CLI reports a tool result as a tool-call event with the call id and
// no tool name: it belongs to the call seen earlier and must not trip the
// allowlist (found by the live arm run, 2026-10-22).
func TestRunPromptProbe_ResultEventWithoutNameBelongsToItsCall(t *testing.T) {
	f := newProbeFixture(t, "probe-tools")
	events, err := f.run(context.Background())
	if err != nil {
		t.Fatalf("err = %v, want nil (report %+v)", err, *f.report)
	}
	if f.report.End != arm.EndDone {
		t.Errorf("End = %s, want done", f.report.End)
	}
	var calls, results int
	for _, e := range events {
		if e.Type == EventTypeToolCall {
			calls++
			if e.ToolStatus != ToolStatusRunning {
				results++
			}
		}
	}
	if calls < 2 || results < 1 {
		t.Errorf("tool-call events = %d (finished %d), want the call and its result", calls, results)
	}
}

// A result for a call id the arm never started has no allowed tool behind it.
func TestRunPromptProbe_OrphanResultFailsClosed(t *testing.T) {
	f := newProbeFixture(t, "probe-orphan-result")
	_, err := f.run(context.Background())
	if !errors.Is(err, ErrIsolationViolated) {
		t.Fatalf("err = %v, want ErrIsolationViolated", err)
	}
	if f.report.End != arm.EndError {
		t.Errorf("End = %s, want error", f.report.End)
	}
}

func TestRunPromptProbe_AttestationIsAGate(t *testing.T) {
	f := newProbeFixture(t, "probe-ok")
	var spawned atomic.Int32
	f.r.newClaudeClient = func(string, ...claudecode.Option) (claudeBackend, error) {
		spawned.Add(1)
		return nil, errors.New("must not spawn")
	}
	for name, tc := range map[string]struct {
		att  sandbox.Attestation
		want error
	}{
		"zero":  {sandbox.Attestation{}, sandbox.ErrNoAttestation},
		"stale": {sandbox.AttestationForTest(t, time.Now().Add(-sandbox.AttestationTTL-time.Minute)), sandbox.ErrStaleAttestation},
	} {
		f.po.Attestation = tc.att
		_, err := f.run(context.Background())
		if !errors.Is(err, tc.want) || f.report.End != arm.EndError {
			t.Errorf("%s: err = %v, End = %q", name, err, f.report.End)
		}
	}
	if spawned.Load() != 0 {
		t.Errorf("a client was spawned %d time(s) without a valid attestation", spawned.Load())
	}
}

// A failed cleanup is surfaced whatever the end state: here the run itself ends
// `capped`, and the corrupted pidfile (so the arm's processes cannot be proven
// gone) is still reported, wrapped beside ErrProbeEnded.
func TestArmEnd_CleanupFailureSurfacedOnEveryEnd(t *testing.T) {
	for _, tc := range []struct {
		scenario string
		want     arm.End
		wantErr  error
	}{
		{"probe-max-budget", arm.EndCapped, ErrProbeEnded},
		{"probe-ok", arm.EndError, nil}, // a normal end becomes an error
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			f := newProbeFixtureEnv(t, tc.scenario, "FAKE_CLAUDE_CLOBBER_PIDFILE='@PRIV@/claude.pid'")
			_, err := f.run(context.Background())
			if err == nil || !strings.Contains(err.Error(), "processes survived") {
				t.Fatalf("err = %v, want it to mention surviving processes", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want it to still wrap %v", err, tc.wantErr)
			}
			if f.report.End != tc.want {
				t.Errorf("End = %q, want %q", f.report.End, tc.want)
			}
		})
	}
}
