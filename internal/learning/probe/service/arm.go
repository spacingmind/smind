package probesvc

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/learning/probe/arm"
	"github.com/spacingmind/smind/internal/learning/probe/contrast"
	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
	"github.com/spacingmind/smind/internal/learning/probe/signature"
	"github.com/spacingmind/smind/internal/learning/window"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Arm roles in execution order.
const (
	roleA  = "A"
	roleA2 = "A2"
	roleB  = "B"
)

// kindSurvivors is the finding for arm processes that outlived the kill.
const kindSurvivors = "survivors"

// truncatedMarker ends a stored artifact that was clipped.
const truncatedMarker = "\n[truncated]\n"

const (
	maxDiffBytes  = 64 << 10
	maxFinalBytes = 8 << 10
	stopWait      = 10 * time.Second
)

// armJob is everything one arm run needs; the real runner and the test seam
// both receive it.
type armJob struct {
	ProbeID       int64
	WorkspaceID   int64
	Role          string
	ArmDir        string
	PrivateDir    string // launch dir, 0700, outside the arm
	ProjectionDir string // "" = no skills
	Prompt, Model string
	MaxTurns      int
	CostCapUSD    float64
	OutputCap     int64
	Timeout       time.Duration
	// Attestation is the canary's proof for this probe; RunPromptProbe
	// requires it before it spawns anything.
	Attestation sandbox.Attestation
	setRun      func(runID string)
}

// armOutcome is what a runner reports. RunID is "" when no run was started.
type armOutcome struct {
	RunID  string
	Report taskrunner.ProbeReport
}

// armMetrics is the per-arm JSON stored in learning_probe_arms.metrics.
type armMetrics struct {
	Turns             int     `json:"turns"`
	ToolCalls         int     `json:"toolCalls"`
	FailedToolCalls   int     `json:"failedToolCalls"`
	PermissionDenials int     `json:"permissionDenials"`
	CostUSD           float64 `json:"costUSD"`
	DurationMs        int64   `json:"durationMs"`
	DiffBytes         int64   `json:"diffBytes"`
	LoadedSkill       *bool   `json:"loadedSkill,omitempty"`
	FailureRecurred   *bool   `json:"failureRecurred,omitempty"`
}

// armRecord is one collected arm.
type armRecord struct {
	role     string
	status   string
	end      arm.End
	runID    string
	cost     float64
	input    contrast.ArmInput
	findings []sandbox.Finding
	started  time.Time
}

func (s *Service) armsRoot() string   { return filepath.Join(s.d.Home, "probe", "arms") }
func (s *Service) launchRoot() string { return filepath.Join(s.d.Home, "probe", "launch") }

// armParams carries the per-probe constants into every arm.
type armParams struct {
	rn        *running
	pl        plan
	proj      probeProjections
	sig       *signature.Signature
	prompt    string
	model     string
	maxTurns  int
	capUSD    float64
	seq       int
	ws        store.Workspace
	baseSHA   string
	skillName string
	humanDirs []string
	att       sandbox.Attestation
	baseDir   string // pristine export of the base commit, outside every arm
}

// runOneArm materialises, runs, collects and cleans up one arm. It never
// returns an error: every failure is an end state on the record.
func (s *Service) runOneArm(ctx context.Context, ap armParams, role string) *armRecord {
	cfg := s.d.Config().Probe
	rec := &armRecord{role: role, started: s.d.Now()}
	armDir := filepath.Join(s.armsRoot(), fmt.Sprintf("%s%d-%s", arm.DirPrefix, ap.rn.probeID, role))
	launchDir := filepath.Join(s.launchRoot(), fmt.Sprintf("%d-%s", ap.rn.probeID, role))
	defer func() {
		if err := arm.Remove(s.armsRoot(), armDir); err != nil {
			slog.Warn("learning probe: remove arm dir", "probe", ap.rn.probeID, "role", role, "err", err)
		}
		_ = os.RemoveAll(launchDir)
	}()

	row := store.LearningProbeArm{ProbeID: ap.rn.probeID, Role: role, Seq: ap.seq, Status: "running", ProjectionHash: ap.proj.hashFor(role), Metrics: "{}"}
	startedAt := rec.started.UTC()
	row.StartedAt = &startedAt
	s.saveArm(ap.rn, row)
	s.notifyArm(ap.rn, role, "running")

	fail := func(why string, err error) *armRecord {
		slog.Warn("learning probe: arm could not run", "probe", ap.rn.probeID, "role", role, "why", why, "err", s.short(err))
		rec.end, rec.status = arm.EndError, string(arm.EndError)
		s.finishArm(ap, rec, row, "", "", armMetrics{})
		return rec
	}

	for _, d := range []string{s.armsRoot(), s.launchRoot()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fail("create dir", err)
		}
	}
	if _, err := arm.Materialise(ctx, arm.Spec{Repo: ap.ws.Path, BaseSHA: ap.baseSHA, Dir: armDir}); err != nil {
		return fail("materialise", err)
	}
	if err := os.Mkdir(launchDir, 0o700); err != nil {
		return fail("launch dir", err)
	}
	trip, err := sandbox.Arm(sandbox.TripwireConfig{RepoDir: ap.ws.Path, HumanSkillDirs: ap.humanDirs})
	if err != nil {
		return fail("tripwire baseline", err)
	}

	job := armJob{ProbeID: ap.rn.probeID, WorkspaceID: ap.ws.ID, Role: role, ArmDir: armDir, PrivateDir: launchDir,
		ProjectionDir: ap.proj.of(role).Dir, Prompt: ap.prompt, Model: ap.model, MaxTurns: ap.maxTurns,
		CostCapUSD: ap.capUSD, OutputCap: int64(cfg.OutputCapKB) * 1024, Timeout: time.Duration(cfg.ArmTimeoutMinutes) * time.Minute,
		Attestation: ap.att, setRun: func(id string) {
			s.setRunning(ap.rn, id)
			if id != "" { // persist at once: a crash must not orphan the run from PurgeDecidedProbes
				row.RunID = id
				s.saveArm(ap.rn, row)
			}
		}}
	run := s.realArm
	if s.armRun != nil {
		run = s.armRun
	}
	out, runErr := run(ctx, job)
	s.setRunning(ap.rn, "")
	rec.runID = out.RunID
	rep := out.Report
	switch {
	case ctx.Err() != nil:
		rec.end = arm.EndKilled
	case runErr != nil && rep.End == "":
		rec.end = arm.EndError
	case rep.End == "":
		rec.end = arm.EndError
	default:
		rec.end = rep.End
	}
	if runErr != nil {
		slog.Warn("learning probe: arm run", "probe", ap.rn.probeID, "role", role, "err", s.short(runErr))
	}

	// Collect before the directory goes.
	// Up to twice the stored cap, so a secret straddling the stored cut is
	// redacted whole before the text is clipped.
	diff, diffBytes, derr := arm.Diff(context.WithoutCancel(ctx), ap.baseDir, armDir, 2*maxDiffBytes)
	if derr != nil {
		slog.Warn("learning probe: arm diff", "probe", ap.rn.probeID, "role", role, "err", s.short(derr))
	}
	rec.findings = trip.Check()
	if rep.End == arm.EndDone || len(rep.InitTools) > 0 || rep.MCPServers > 0 || rep.Plugins > 0 {
		rec.findings = append(rec.findings, sandbox.CheckInit(sandbox.Observation{InitTools: rep.InitTools, MCPServers: rep.MCPServers, Plugins: rep.Plugins})...)
	}

	if rep.Survivors > 0 {
		rec.findings = append(rec.findings, sandbox.Finding{Kind: kindSurvivors, Detail: fmt.Sprintf("%d process(es) outlived the kill pass", rep.Survivors)})
	}
	for _, f := range rec.findings {
		slog.Warn("learning probe: tripwire fired", "probe", ap.rn.probeID, "role", role, "kind", f.Kind, "detail", clipText(s.d.Redact(f.Detail), 200))
	}

	events, final := s.collectEvents(out.RunID)
	cost, reported := s.runCost(out.RunID, rep)
	if !reported && out.RunID != "" {
		cost = ap.capUSD // unreported usage is charged at the arm's cap
	}
	rec.cost = cost
	rec.input = contrast.ArmInput{Role: contrast.Role(role), Events: events, Turns: rep.Turns, ReportedDenials: rep.PermissionDenials,
		CostUSD: cost, DurationMs: s.d.Now().Sub(rec.started).Milliseconds(), DiffBytes: int64(diffBytes)}
	m := contrast.ComputeMetrics(rec.input)
	am := armMetrics{Turns: m.Turns, ToolCalls: m.ToolCalls, FailedToolCalls: m.FailedToolCalls, PermissionDenials: m.PermissionDenials,
		CostUSD: m.CostUSD, DurationMs: m.DurationMs, DiffBytes: m.DiffBytes}
	if role == roleB {
		loaded := contrast.Loaded(events, ap.skillName, ap.proj.b.Dir)
		am.LoadedSkill = &loaded
	}
	am.FailureRecurred = contrast.FailureRecurred(events, ap.sig)

	rec.status = string(rec.end)
	s.finishArm(ap, rec, row, s.storable(diff, maxDiffBytes), s.storable(final, maxFinalBytes), am)
	return rec
}

// storable redacts text whole and only then clips it to limit bytes: the
// other order would leave a prefix of a secret that straddles the cut.
func (s *Service) storable(text string, limit int) string {
	red := s.d.Redact(text)
	if len(red) <= limit {
		return red
	}
	return clipText(red, limit) + truncatedMarker
}

// finishArm persists the arm's end and tells the notifier.
func (s *Service) finishArm(ap armParams, rec *armRecord, row store.LearningProbeArm, diff, final string, am armMetrics) {
	now := s.d.Now().UTC()
	row.Status, row.RunID, row.Diff, row.FinalMessage, row.CostUSD, row.FinishedAt = rec.status, rec.runID, diff, final, rec.cost, &now
	row.Metrics = mustJSON(am)
	s.saveArm(ap.rn, row)
	s.notifyArm(ap.rn, rec.role, rec.status)
}

func (s *Service) saveArm(rn *running, row store.LearningProbeArm) {
	if err := s.d.Store.UpsertProbeArm(row); err != nil {
		slog.Warn("learning probe: save arm", "probe", rn.probeID, "role", row.Role, "err", err)
	}
}

// collectEvents decodes the arm run's persisted events and its final text.
func (s *Service) collectEvents(runID string) ([]window.Event, string) {
	if runID == "" {
		return nil, ""
	}
	rows, err := s.d.Store.ListRunEvents(runID)
	if err != nil {
		slog.Warn("learning probe: list arm events", "run", runID, "err", err)
		return nil, ""
	}
	evs := make([]window.Event, 0, len(rows))
	raw := make([]taskrunner.Event, 0, len(rows))
	for _, r := range rows {
		ev, err := runs.DecodeStoredEvent(r.EventData)
		if err != nil {
			continue
		}
		evs = append(evs, window.Event{ID: r.ID, Seq: r.Seq, E: ev})
		raw = append(raw, ev)
	}
	return evs, taskrunner.FinalText(raw)
}

// runCost is the arm's reported cost; reported=false when neither the usage
// row nor the CLI report gave one.
func (s *Service) runCost(runID string, rep taskrunner.ProbeReport) (cost float64, reported bool) {
	if runID == "" {
		return 0, true
	}
	if u, err := s.d.Store.GetRunUsage(runID); err == nil && u.CostUSD != nil {
		return *u.CostUSD, true
	}
	if rep.CostUSD > 0 {
		return rep.CostUSD, true
	}
	return 0, false
}

// realArm starts the arm as a hidden probe run through the registry and
// waits for it. It is the only production caller of WithProbe.
func (s *Service) realArm(ctx context.Context, job armJob) (armOutcome, error) {
	cli, err := s.d.RealCLI()
	if err != nil {
		return armOutcome{}, fmt.Errorf("resolve claude: %w", err)
	}
	l, err := sandbox.Prepare(sandbox.Config{ArmDir: job.ArmDir, ProjectionDir: job.ProjectionDir, PrivateDir: job.PrivateDir, RealCLI: cli})
	if err != nil {
		return armOutcome{}, err
	}
	task, err := s.d.Exec.SystemTask(job.WorkspaceID)
	if err != nil {
		return armOutcome{}, err
	}
	var mu sync.Mutex
	var rep taskrunner.ProbeReport
	po := taskrunner.ProbeOptions{Launch: l, Attestation: job.Attestation, MaxTurns: job.MaxTurns, CostCapUSD: job.CostCapUSD, OutputCapBytes: job.OutputCap,
		Timeout: job.Timeout, OnReport: func(r taskrunner.ProbeReport) { mu.Lock(); rep = r; mu.Unlock() }}
	runID, err := s.d.Reg.Start(context.Background(), s.d.WM, s.d.Runner, task.ID, 0, taskrunner.ProviderClaudeNative, job.Prompt,
		taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified,
		runs.WithPurpose(store.RunPurposeProbe), runs.WithModel(job.Model), runs.WithProbe(po))
	if err != nil {
		return armOutcome{}, fmt.Errorf("start probe run: %w", err)
	}
	job.setRun(runID)
	werr := s.waitRun(ctx, runID)
	mu.Lock()
	defer mu.Unlock()
	return armOutcome{RunID: runID, Report: rep}, werr
}

// waitRun blocks until runID is terminal; on ctx cancel it stops the run and
// waits (bounded) for the stop to land.
func (s *Service) waitRun(ctx context.Context, runID string) error {
	ch, unsub, err := s.d.Reg.Subscribe(runID)
	if err != nil {
		return fmt.Errorf("subscribe run: %w", err)
	}
	defer unsub()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range ch {
		}
	}()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		_ = s.d.Reg.Stop(runID)
		select {
		case <-drained:
		case <-time.After(stopWait):
		}
		return ctx.Err()
	}
}
