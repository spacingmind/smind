//go:build !windows

package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// probeEnv is a registry whose probe arm runs the fake CLI (the test binary)
// behind the real sandbox launcher.
type probeEnv struct {
	*isolatedEnv
	po taskrunner.ProbeOptions
}

func newProbeEnv(t *testing.T, scenario string) *probeEnv {
	t.Helper()
	e := &probeEnv{isolatedEnv: newIsolatedEnv(t, "")}
	e.runner = taskrunner.New(e.wm) // the launcher is the CLI path: no WithClaudeCLIPath
	if !sandbox.Supported() {
		// Enough for the validation-only tests; the launch is never run.
		e.po = taskrunner.ProbeOptions{Launch: &sandbox.Launch{ArmDir: t.TempDir()}, MaxTurns: 3, CostCapUSD: 0.05, Timeout: 10 * time.Second}
		return e
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
	script := "#!/bin/sh\n" + fakeClaudeEnv + "=1 FAKE_CLAUDE_SCENARIO='" + scenario + "' exec '" + self + "' \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := sandbox.Prepare(sandbox.Config{ArmDir: armDir, PrivateDir: priv, RealCLI: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	e.po = taskrunner.ProbeOptions{Launch: l, MaxTurns: 3, CostCapUSD: 0.05, Timeout: 20 * time.Second}
	return e
}

func (e *probeEnv) startProbe(opts ...StartOption) (string, error) {
	return e.start(opts...)
}

func TestWithProbe_RequiresSystemPurposeAndClaude(t *testing.T) {
	t.Parallel()
	e := newProbeEnv(t, "probe-ok")
	if id, err := e.startProbe(WithProbe(e.po)); err == nil {
		t.Fatalf("Start(WithProbe) without a purpose = %q, nil", id)
	}
	if _, err := e.startProbe(WithPurpose("reflect"), WithProbe(e.po)); err == nil {
		t.Error("a non-probe purpose was accepted")
	}
	noLaunch := e.po
	noLaunch.Launch = nil
	if _, err := e.startProbe(WithPurpose(store.RunPurposeProbe), WithProbe(noLaunch)); err == nil {
		t.Error("a probe without a launch was accepted")
	}
	for _, p := range []taskrunner.Provider{taskrunner.ProviderGLM, taskrunner.ProviderKimi, taskrunner.ProviderCodexNative} {
		_, err := e.reg.Start(context.Background(), e.wm, e.runner, e.task.ID, 0, p, "hi", taskrunner.PermissionSettings{}, "", WithPurpose(store.RunPurposeProbe), WithProbe(e.po))
		if !errors.Is(err, taskrunner.ErrIsolationUnsupported) {
			t.Errorf("Start(%s) = %v, want ErrIsolationUnsupported", p, err)
		}
	}
	if rows, err := e.st.ListRunsByPurpose(store.RunPurposeProbe, 10); err != nil || len(rows) != 0 {
		t.Errorf("persisted probe runs = %+v, %v; want none", rows, err)
	}
	if got := e.reg.ListWith(RunFilter{IncludeSystem: true}); len(got) != 0 {
		t.Errorf("registry runs = %+v, want none", got)
	}
}

func TestWithProbe_RejectedByStartWhenBusy(t *testing.T) {
	t.Parallel()
	e := newProbeEnv(t, "probe-ok")
	_, err := e.reg.StartWhenBusy(context.Background(), e.wm, e.runner, e.task.ID, 0, taskrunner.ProviderClaudeNative, "hi", taskrunner.PermissionSettings{}, "", WhenBusyReject, store.ChatQueueSourceHuman, 0, 0, WithPurpose(store.RunPurposeProbe), WithProbe(e.po))
	if err == nil || !strings.Contains(err.Error(), "WithProbe") {
		t.Fatalf("StartWhenBusy(WithProbe) = %v, want a WithProbe error", err)
	}
}

func TestWithProbe_ExclusiveWithIsolation(t *testing.T) {
	t.Parallel()
	e := newProbeEnv(t, "probe-ok")
	_, err := e.startProbe(WithPurpose(store.RunPurposeProbe), WithProbe(e.po), WithIsolation(testIso))
	if err == nil || !strings.Contains(err.Error(), "exclusive") {
		t.Fatalf("both options = %v, want a mutual-exclusion error", err)
	}
}

func TestProbeRuns_HiddenFromListAndTaskList(t *testing.T) {
	t.Parallel()
	if !sandbox.Supported() {
		t.Skip("macOS only")
	}
	e := newProbeEnv(t, "probe-ok")
	id, err := e.startProbe(WithPurpose(store.RunPurposeProbe), WithProbe(e.po))
	if err != nil {
		t.Fatal(err)
	}
	st := waitForStatus(t, e.reg, id, StatusDone, 20*time.Second)
	if st.Purpose != store.RunPurposeProbe {
		t.Fatalf("Purpose = %q", st.Purpose)
	}
	for _, got := range [][]RunSummary{e.reg.List(0), e.reg.ListFiltered(0, 0, ""), e.reg.ListWith(RunFilter{})} {
		for _, r := range got {
			if r.ID == id {
				t.Fatal("probe run visible in a default listing")
			}
		}
	}
	var found bool
	for _, r := range e.reg.ListWith(RunFilter{IncludeSystem: true}) {
		found = found || r.ID == id
	}
	if !found {
		t.Error("probe run missing from the include-system listing")
	}
	// Recents (the user-run list) never include it, nor does the store's
	// collector scope.
	recent, err := e.st.ListRecentRuns(50)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recent {
		if r.ID == id {
			t.Error("probe run in ListRecentRuns")
		}
	}
	if rows, _ := e.st.ListRunsByPurpose(store.RunPurposeProbe, 10); len(rows) != 1 {
		t.Errorf("ListRunsByPurpose(probe) = %d rows, want 1", len(rows))
	}
}

func TestProbeRun_StatusAndUsagePersisted(t *testing.T) {
	t.Parallel()
	if !sandbox.Supported() {
		t.Skip("macOS only")
	}
	e := newProbeEnv(t, "probe-max-budget")
	id, err := e.startProbe(WithPurpose(store.RunPurposeProbe), WithProbe(e.po))
	if err != nil {
		t.Fatal(err)
	}
	st := waitForStatus(t, e.reg, id, StatusError, 20*time.Second)
	if !strings.Contains(st.Err, "capped") {
		t.Errorf("Err = %q, want the capped end", st.Err)
	}
	row, err := e.st.GetRun(id)
	if err != nil || row.Status != string(StatusError) || row.Purpose != store.RunPurposeProbe {
		t.Fatalf("row = %+v, %v", row, err)
	}
	u, err := e.st.GetRunUsage(id)
	if err != nil {
		t.Fatalf("no usage row: %v", err)
	}
	if u.CostUSD == nil || *u.CostUSD != 0.02 {
		t.Errorf("usage cost = %v, want 0.02", u.CostUSD)
	}
}
