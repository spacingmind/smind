package probesvc

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const (
	// armHeadroom is the estimate's multiplier over the source run's cost.
	armHeadroom = 1.2
	// minArmMinutes is the floor of one arm's expected duration.
	minArmMinutes = 2.0
)

// Service runs probes. Safe for concurrent use; at most one probe runs at a
// time per daemon.
type Service struct {
	d Deps

	mu       sync.Mutex // guards starting, active
	starting bool
	active   *running

	ctx    context.Context
	wg     sync.WaitGroup
	cancel func()

	// Test seams; production leaves canaryRun and armRun nil.
	lookPath     func(string) (string, error)
	canaryRun    canaryFunc
	armRun       armFunc
	janitorEvery time.Duration
}

// New builds a Service. Config defaults to config.DefaultLearning.
func New(d Deps) *Service {
	if d.Config == nil {
		d.Config = config.DefaultLearning
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Redact == nil {
		d.Redact = func(s string) string { return s }
	}
	if d.GOOS == "" {
		d.GOOS = runtime.GOOS
	}
	s := &Service{d: d, lookPath: exec.LookPath}
	s.initCtx()
	return s
}

// plan is a resolved eligibility assessment of one proposal.
type plan struct {
	est      Estimate
	proposal store.LearnedProposal
	run      store.Run
	ws       store.Workspace
	estArm   float64
}

// Estimate reports eligibility and the forecast for a probe of proposalID.
// It writes nothing and makes no model call.
func (s *Service) Estimate(_ context.Context, proposalID int64, quick bool) (Estimate, error) {
	p, err := s.assess(proposalID, quick)
	if err != nil {
		return Estimate{}, err
	}
	return p.est, nil
}

func reason(code, msg string) Reason { return Reason{Code: code, Message: msg} }

// assess collects every reason (not only the first) and the estimate.
func (s *Service) assess(proposalID int64, quick bool) (plan, error) {
	return s.assessInner(proposalID, quick, false)
}

// assessInner is assess; skipInProc leaves out this process's in-flight
// guard (Start has just taken it itself) but still honours a stored probe.
func (s *Service) assessInner(proposalID int64, quick, skipInProc bool) (plan, error) {
	st := s.d.Store
	cfg := s.d.Config()
	now := s.d.Now()
	p, err := st.GetProposal(proposalID)
	if err != nil {
		return plan{}, err
	}
	ws, err := st.GetWorkspace(p.WorkspaceID)
	if err != nil {
		return plan{}, err
	}
	arms := 3
	if quick {
		arms = 2
	}
	pl := plan{proposal: p, ws: ws}
	est := Estimate{Reasons: []Reason{}, Quick: quick, Arms: arms,
		PerProbeCapUSD: cfg.Probe.PerProbeBudgetUSD, DailyCapUSD: cfg.Probe.DailyBudgetUSD}
	add := func(code, msg string) { est.Reasons = append(est.Reasons, reason(code, msg)) }

	if !cfg.Enabled || ws.LearningMode != "propose" {
		add(ReasonLearningOff, "Learning is turned off for this workspace.")
	}
	supported := s.d.GOOS == "darwin"
	switch {
	case s.d.GOOS == "windows":
		add(ReasonWindows, "Probes are not available on Windows.")
	case !supported:
		add(ReasonPlatformUnsupported, "Probes run only on macOS, where the sandbox has been measured.")
	}
	if p.Status != "pending" {
		add(ReasonNotPending, "This proposal has already been decided.")
	}

	run, haveRun := s.sourceRun(p)
	if !haveRun {
		add(ReasonNoPrompt, "The proposal has no source run in this workspace to replay.")
	} else {
		pl.run = run
		if run.Provider != string(taskrunner.ProviderClaudeNative) {
			add(ReasonProviderNotClaude, "Only runs of the Claude native provider can be probed.")
		}
		if run.Prompt == "" {
			add(ReasonNoPrompt, "The source run has no recorded prompt to replay.")
		}
		if run.BaseSHA == nil || *run.BaseSHA == "" {
			add(ReasonNoBaseSHA, "The source run did not record the commit it started from.")
		}
		switch {
		case run.Dirty == nil:
			add(ReasonDirtyCheckout, "The source run's checkout state is unknown.")
		case *run.Dirty:
			add(ReasonDirtyCheckout, "The source run started with uncommitted changes, so its start state cannot be rebuilt.")
		}
		if run.Interrupt {
			add(ReasonContinuationRun, "The source prompt continued a conversation instead of starting one.")
		} else if run.ChatID != 0 {
			earlier, err := st.ChatHasEarlierRun(run.ChatID, run.ID, run.StartedAt)
			if err != nil {
				return plan{}, err
			}
			if earlier {
				add(ReasonContinuationRun, "The source prompt continued a conversation instead of starting one.")
			}
		}
		cost, known := s.sourceCost(run.ID)
		if !known {
			add(ReasonSourceCostUnknown, "The source run reported no cost, so a probe cannot be priced.")
		} else {
			pl.estArm = armHeadroom * cost
			est.EstArmUSD = pl.estArm
			est.EstUSD = float64(arms) * pl.estArm
		}
		est.ExpectedMinutes = expectedMinutes(arms, run, cfg.Probe.ArmTimeoutMinutes)
	}

	if supported && !s.sandboxAvailable() {
		add(ReasonSandboxUnavailable, "The sandbox tooling (the claude CLI or perl) is not available.")
	}

	spent, unreported, err := st.SumProbeUsageForWorkspace(ws.ID, now.Add(-24*time.Hour))
	if err != nil {
		return plan{}, err
	}
	spent += float64(unreported) * pl.estArm
	est.SpentTodayUSD = spent
	if est.EstUSD > 0 {
		switch {
		case est.EstUSD > cfg.Probe.PerProbeBudgetUSD:
			add(ReasonBudget, fmt.Sprintf("The estimated cost of $%.2f is over the $%.2f per-probe limit.", est.EstUSD, cfg.Probe.PerProbeBudgetUSD))
		case est.EstUSD+spent > cfg.Probe.DailyBudgetUSD:
			add(ReasonBudget, fmt.Sprintf("The estimate plus $%.2f already spent today is over the $%.2f daily limit.", spent, cfg.Probe.DailyBudgetUSD))
		}
	}

	if s.d.QuotaPercent != nil {
		if pct, ok := s.d.QuotaPercent(); ok {
			est.QuotaPercent = &pct
			if pct >= float64(cfg.Probe.QuotaSkipPercent) {
				add(ReasonQuota, fmt.Sprintf("Account quota is %.0f%% used; probes wait until it drops below %d%%.", pct, cfg.Probe.QuotaSkipPercent))
			}
		}
	}

	busy, err := s.busy(skipInProc)
	if err != nil {
		return plan{}, err
	}
	if busy {
		add(ReasonBusy, "Another probe is already running.")
	}
	if job, err := st.HasActiveLearningJob(ws.ID); err != nil {
		return plan{}, err
	} else if job {
		add(ReasonBusy, "A learning job is running for this workspace.")
	}
	est.Eligible = len(est.Reasons) == 0
	pl.est = est
	return pl, nil
}

// busy reports a probe in flight: this process's guard or a stored one.
func (s *Service) busy(skipInProc bool) (bool, error) {
	s.mu.Lock()
	inProc := s.starting || s.active != nil
	s.mu.Unlock()
	if inProc && !skipInProc {
		return true, nil
	}
	_, ok, err := s.d.Store.ActiveProbe()
	return ok, err
}

func (s *Service) sandboxAvailable() bool {
	if s.d.RealCLI == nil {
		return false
	}
	if path, err := s.d.RealCLI(); err != nil || path == "" {
		return false
	}
	_, err := s.lookPath("perl")
	return err == nil
}

// sourceCost is the source run's reported cost (positive), if any.
func (s *Service) sourceCost(runID string) (float64, bool) {
	u, err := s.d.Store.GetRunUsage(runID)
	if err != nil || u.CostUSD == nil || *u.CostUSD <= 0 {
		return 0, false
	}
	return *u.CostUSD, true
}

func expectedMinutes(arms int, run store.Run, armTimeoutMinutes int) int {
	per := minArmMinutes
	if run.FinishedAt != nil {
		per = math.Max(per, run.FinishedAt.Sub(run.StartedAt).Minutes()*armHeadroom)
	}
	total := math.Ceil(float64(arms) * per)
	if limit := float64(arms * armTimeoutMinutes); armTimeoutMinutes > 0 && total > limit {
		total = limit
	}
	return int(total)
}

// sourceRun finds the run to replay: the first evidence item of a
// signature-bearing kind, else the first evidence item. It must be a user
// run of the proposal's workspace.
func (s *Service) sourceRun(p store.LearnedProposal) (store.Run, bool) {
	var items []gate.Evidence
	if p.Evidence == "" || json.Unmarshal([]byte(p.Evidence), &items) != nil || len(items) == 0 {
		return store.Run{}, false
	}
	pick := items[0]
	for _, ev := range items {
		if ev.Kind == gate.EvFailureRecovery || ev.Kind == gate.EvPermissionDenied {
			pick = ev
			break
		}
	}
	r, err := s.d.Store.GetRun(pick.RunID)
	if err != nil {
		return store.Run{}, false
	}
	t, err := s.d.Store.GetTask(r.TaskID)
	if err != nil || t.WorkspaceID != p.WorkspaceID || r.Purpose != store.RunPurposeUser {
		return store.Run{}, false
	}
	return r, true
}
