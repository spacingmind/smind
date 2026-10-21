package probesvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/probe/arm"
	"github.com/spacingmind/smind/internal/learning/probe/contrast"
	"github.com/spacingmind/smind/internal/learning/probe/prevalence"
	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
	"github.com/spacingmind/smind/internal/learning/probe/signature"
	"github.com/spacingmind/smind/internal/learning/probe/verdict"
	"github.com/spacingmind/smind/internal/learning/projection"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Probe statuses.
const (
	statusQueued    = "queued"
	statusRunning   = "running"
	statusDone      = "done"
	statusFailed    = "failed"
	statusCancelled = "cancelled"
	statusUnsafe    = "unsafe"
)

// cancelWait bounds how long Cancel waits for the probe goroutine to clean up.
const cancelWait = 30 * time.Second

// workspaceCancelWait bounds how long CancelWorkspace waits before its
// caller deletes the workspace's rows.
const workspaceCancelWait = 10 * time.Second

var (
	errStartCancelled = errors.New("the probe was cancelled before it started")
	errClosing        = errors.New("the probe service is shutting down")
)

// IneligibleError is returned by Start when the probe cannot run now. Its
// message names the first reason code; Reasons has all of them.
type IneligibleError struct{ Reasons []Reason }

func (e *IneligibleError) Error() string {
	if len(e.Reasons) == 0 {
		return "probe not available"
	}
	return fmt.Sprintf("probe not available (%s): %s", e.Reasons[0].Code, e.Reasons[0].Message)
}

// Code is the first reason's code.
func (e *IneligibleError) Code() string {
	if len(e.Reasons) == 0 {
		return ""
	}
	return e.Reasons[0].Code
}

// running is the in-flight probe.
type running struct {
	probeID   int64
	proposal  int64
	workspace int64
	cancel    context.CancelFunc
	done      chan struct{}
	release   func() // the executor slot, nil when none was taken
	// guarded by Service.mu
	cancelled bool
	runID     string // the arm run executing now, "" between arms
}

// canaryFunc runs one sandbox self-check and returns the attestation and the
// cost it incurred (also on failure).
type canaryFunc func(ctx context.Context, probeID int64) (sandbox.Attestation, float64, error)

// armFunc runs one arm (see arm.go).
type armFunc func(ctx context.Context, job armJob) (armOutcome, error)

func (s *Service) initCtx() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
}

// Close cancels the running probe and waits for its goroutine.
func (s *Service) Close() {
	s.cancel()
	s.wg.Wait()
}

func plannedRoles(quick bool) []string {
	if quick {
		return []string{roleA, roleB}
	}
	return []string{roleA, roleA2, roleB}
}

// Start re-checks eligibility (failing closed), records the probe and its
// planned arms, and runs it in a goroutine the Service owns. It returns the
// queued probe row at once.
func (s *Service) Start(ctx context.Context, proposalID int64, quick bool) (store.LearningProbe, error) {
	if err := ctx.Err(); err != nil {
		return store.LearningProbe{}, err
	}
	s.mu.Lock()
	if s.starting || s.active != nil || s.ctx.Err() != nil {
		s.mu.Unlock()
		return store.LearningProbe{}, &IneligibleError{Reasons: []Reason{reason(ReasonBusy, "Another probe is already running.")}}
	}
	s.starting, s.startingProp, s.startingWS, s.startCancelled = true, proposalID, 0, false
	s.mu.Unlock()
	started := false
	defer func() {
		if !started {
			s.mu.Lock()
			s.starting = false
			s.mu.Unlock()
		}
	}()

	pl, err := s.assessInner(proposalID, quick, true)
	if err != nil {
		return store.LearningProbe{}, err
	}
	s.mu.Lock()
	s.startingWS = pl.ws.ID
	s.mu.Unlock()
	if !pl.est.Eligible {
		return store.LearningProbe{}, &IneligibleError{Reasons: pl.est.Reasons}
	}
	if s.startAborted() {
		return store.LearningProbe{}, errStartCancelled
	}
	// The probe's arms run in the workspace's system-task chat, which holds
	// one run at a time: keep reflect and curate out for the whole probe.
	release := func() {}
	if s.d.Exec != nil {
		rel, ok := s.d.Exec.Acquire(pl.ws.ID)
		if !ok {
			return store.LearningProbe{}, &IneligibleError{Reasons: []Reason{reason(ReasonBusy, "A learning job is running for this workspace.")}}
		}
		release = rel
	}
	handedOver := false
	defer func() {
		if !handedOver {
			release()
		}
	}()
	probe, err := s.d.Store.CreateProbe(store.LearningProbe{
		WorkspaceID: pl.ws.ID, ProposalID: proposalID, Quick: quick, Status: statusQueued, EstUSD: pl.est.EstUSD,
		SourceRunID: pl.run.ID, BaseSHA: *pl.run.BaseSHA, Contrast: "{}", StartedAt: s.d.Now().UTC(),
	})
	if err != nil {
		return store.LearningProbe{}, err
	}
	for i, role := range plannedRoles(quick) {
		if err := s.d.Store.UpsertProbeArm(store.LearningProbeArm{ProbeID: probe.ID, Role: role, Seq: i, Status: "pending", Metrics: "{}"}); err != nil {
			s.markFailed(probe.ID, "could not record the planned arms")
			return store.LearningProbe{}, err
		}
	}

	runCtx, cancel := context.WithCancel(s.ctx)
	rn := &running{probeID: probe.ID, proposal: proposalID, workspace: pl.ws.ID, cancel: cancel, done: make(chan struct{}), release: release}
	s.mu.Lock()
	if s.ctx.Err() != nil || s.startCancelled { // Close or Cancel arrived while the row was written
		closing := s.ctx.Err() != nil
		s.mu.Unlock()
		cancel()
		s.markCancelled(probe.ID)
		if closing {
			return store.LearningProbe{}, errClosing
		}
		return store.LearningProbe{}, errStartCancelled
	}
	s.active, s.starting = rn, false
	started = true
	s.wg.Add(1)
	handedOver = true
	s.mu.Unlock()
	if s.d.Notify != nil {
		s.d.Notify.ProbeStarted(pl.ws.ID, probe.ID, proposalID)
	}
	go s.run(runCtx, rn, pl)
	return probe, nil
}

// startAborted reports a Cancel or Close that arrived while Start was still
// checking eligibility.
func (s *Service) startAborted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startCancelled || s.ctx.Err() != nil
}

// markCancelled settles a probe row that was cancelled before it ran.
func (s *Service) markCancelled(probeID int64) {
	p, err := s.d.Store.GetProbe(probeID)
	if err != nil {
		return
	}
	now := s.d.Now().UTC()
	p.Status, p.VerdictReason, p.FinishedAt = statusCancelled, "the probe was cancelled before it finished.", &now
	if err := s.d.Store.UpdateProbe(p); err != nil {
		slog.Warn("learning probe: settle cancelled start", "probe", probeID, "err", err)
	}
}

// markFailed settles a probe row that never got to run.
func (s *Service) markFailed(probeID int64, why string) {
	p, err := s.d.Store.GetProbe(probeID)
	if err != nil {
		return
	}
	now := s.d.Now().UTC()
	p.Status, p.Verdict, p.VerdictReason, p.Error, p.FinishedAt = statusFailed, string(verdict.ProbeFailed), why, why, &now
	_ = s.d.Store.UpdateProbe(p)
}

// Cancel stops the proposal's running probe: it cancels the probe's context,
// stops the running arm and waits (bounded) for the arm and launch
// directories to be removed. A probe still being started is cancelled too.
// It is a no-op when nothing runs.
func (s *Service) Cancel(proposalID int64) error {
	s.stop(func(rn *running) bool { return rn.proposal == proposalID },
		func() bool { return s.startingProp == proposalID }, cancelWait)
	return nil
}

// CancelWorkspace stops the workspace's running or starting probe and waits
// (bounded) for it to clean up, so the caller can delete the workspace's
// rows without a probe goroutine writing to them afterwards.
func (s *Service) CancelWorkspace(workspaceID int64) {
	s.stop(func(rn *running) bool { return rn.workspace == workspaceID },
		func() bool { return s.startingWS == 0 || s.startingWS == workspaceID }, workspaceCancelWait)
}

// stop cancels the active probe when mineActive matches and a start in
// progress when mineStarting matches (called under s.mu), then waits up to
// wait for both to settle. Marking the start cancelled happens in the same
// critical section that reads s.active, so a Start cannot slip between.
func (s *Service) stop(mineActive func(*running) bool, mineStarting func() bool, wait time.Duration) {
	deadline := time.Now().Add(wait)
	s.mu.Lock()
	rn := s.active
	if rn != nil && !mineActive(rn) {
		rn = nil
	}
	starting := s.starting && mineStarting()
	if starting {
		s.startCancelled = true
	}
	var runID string
	if rn != nil {
		rn.cancelled = true
		runID = rn.runID
	}
	s.mu.Unlock()
	if rn != nil {
		rn.cancel()
		if runID != "" && s.d.Reg != nil {
			_ = s.d.Reg.Stop(runID)
		}
		select {
		case <-rn.done:
		case <-time.After(time.Until(deadline)):
		}
	}
	for starting && time.Now().Before(deadline) {
		s.mu.Lock()
		still := s.starting
		s.mu.Unlock()
		if !still {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *Service) setRunning(rn *running, runID string) {
	s.mu.Lock()
	rn.runID = runID
	s.mu.Unlock()
}

func (s *Service) wasCancelled(rn *running) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return rn.cancelled
}

func (s *Service) notifyArm(rn *running, role, status string) {
	if s.d.Notify == nil {
		return
	}
	p, err := s.d.Store.GetProbe(rn.probeID)
	if err != nil {
		return
	}
	s.d.Notify.ProbeArm(p.WorkspaceID, rn.probeID, rn.proposal, role, status)
}

// run is the probe goroutine. Whatever happens it removes the probe's
// directories and projections and clears the in-flight marker.
func (s *Service) run(ctx context.Context, rn *running, pl plan) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		s.active = nil
		s.mu.Unlock()
		if rn.release != nil {
			rn.release()
		}
		close(rn.done)
		rn.cancel()
	}()
	defer s.cleanupProbe(rn.probeID, pl.ws.ID)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("learning probe: panic", "probe", rn.probeID, "panic", fmt.Sprint(r))
			s.failProbe(rn, pl, "the probe stopped on an internal error")
		}
	}()
	s.execute(ctx, rn, pl)
}

// cleanupProbe removes every directory the probe created.
func (s *Service) cleanupProbe(probeID, workspaceID int64) {
	_ = arm.Remove(s.armsRoot(), filepath.Join(s.armsRoot(), fmt.Sprintf("%s%d-base", arm.DirPrefix, probeID)))
	for _, role := range []string{roleA, roleA2, roleB} {
		_ = arm.Remove(s.armsRoot(), filepath.Join(s.armsRoot(), fmt.Sprintf("%s%d-%s", arm.DirPrefix, probeID, role)))
		removeAll(filepath.Join(s.launchRoot(), fmt.Sprintf("%d-%s", probeID, role)))
	}
	removeAll(filepath.Join(s.launchRoot(), fmt.Sprintf("%d-canary", probeID)))
	if err := projection.Remove(filepath.Join(s.d.Home, "learning", "probe-projections"), workspaceID); err != nil {
		slog.Warn("learning probe: remove projections", "probe", probeID, "err", err)
	}
}

// failProbe settles a probe that could not produce a verdict from its arms.
func (s *Service) failProbe(rn *running, pl plan, why string) {
	st := s.d.Store
	p, err := st.GetProbe(rn.probeID)
	if err != nil {
		return
	}
	if arms, err := st.ListProbeArms(rn.probeID); err == nil {
		now := s.d.Now().UTC()
		for _, a := range arms {
			if a.Status == "running" {
				a.Status, a.FinishedAt = string(arm.EndKilled), &now
				_ = st.UpsertProbeArm(a)
			}
		}
	}
	now := s.d.Now().UTC()
	p.Status, p.Verdict, p.VerdictReason, p.Error, p.FinishedAt = statusFailed, string(verdict.ProbeFailed), why, why, &now
	if err := st.UpdateProbe(p); err != nil {
		slog.Warn("learning probe: settle failed probe", "probe", rn.probeID, "err", err)
	}
	s.appendLedger(pl, p, "", "")
	if s.d.Notify != nil {
		s.d.Notify.ProbeFailed(pl.ws.ID, rn.probeID, rn.proposal, why)
	}
}

// short is an error as a short redacted sentence fragment: no token, no path
// beyond what the redactor allows.
func (s *Service) short(err error) string {
	if err == nil {
		return ""
	}
	return clipText(s.d.Redact(err.Error()), 200)
}

func (s *Service) model() string {
	if m := s.d.Config().ReflectorModel; m != "" {
		return m
	}
	return config.DefaultHaikuModel
}

// attest runs the canary (once) and charges its cost to the probe.
func (s *Service) attest(ctx context.Context, probeID int64) (sandbox.Attestation, error) {
	canary := s.canaryRun
	if canary == nil {
		canary = s.realCanary
	}
	att, cost, err := canary(ctx, probeID)
	if cost > 0 {
		if p, gerr := s.d.Store.GetProbe(probeID); gerr == nil {
			p.CanaryCostUSD += cost
			if uerr := s.d.Store.UpdateProbe(p); uerr != nil {
				slog.Warn("learning probe: record canary cost", "probe", probeID, "err", uerr)
			}
		}
	}
	return att, err
}

func (s *Service) execute(ctx context.Context, rn *running, pl plan) {
	st := s.d.Store
	cfg := s.d.Config().Probe
	probe, err := st.GetProbe(rn.probeID)
	if err != nil {
		s.failProbe(rn, pl, "the probe row could not be read")
		return
	}
	probe.Status = statusRunning
	if err := st.UpdateProbe(probe); err != nil {
		s.failProbe(rn, pl, "the probe row could not be updated")
		return
	}
	if ctx.Err() != nil {
		s.settleCancelled(rn, pl)
		return
	}

	att, err := s.attest(ctx, rn.probeID)
	if ctx.Err() != nil {
		s.settleCancelled(rn, pl)
		return
	}
	if err != nil {
		s.failProbe(rn, pl, "sandbox could not be attested: "+s.short(err))
		return
	}
	proj, err := s.projections(pl.proposal, att)
	if err != nil {
		s.failProbe(rn, pl, "the skill projections could not be built: "+s.short(err))
		return
	}
	var sig *signature.Signature
	if sigs, err := prevalence.New(prevalence.Deps{Store: st, Home: s.d.Home}).Derive(pl.proposal, s.d.Redact); err == nil && len(sigs) > 0 {
		sig = &sigs[0]
	}
	baseDir := filepath.Join(s.armsRoot(), fmt.Sprintf("%s%d-base", arm.DirPrefix, rn.probeID))
	if err := os.MkdirAll(s.armsRoot(), 0o700); err != nil {
		s.failProbe(rn, pl, "the arm directory could not be created")
		return
	}
	if _, err := arm.Export(ctx, arm.Spec{Repo: pl.ws.Path, BaseSHA: *pl.run.BaseSHA, Dir: baseDir}); err != nil {
		if ctx.Err() != nil {
			s.settleCancelled(rn, pl)
			return
		}
		s.failProbe(rn, pl, "the base commit could not be exported: "+s.short(err))
		return
	}
	var humanDirs []string
	if s.d.HumanSkillDirs != nil {
		humanDirs = s.d.HumanSkillDirs(pl.ws.ID)
	}
	ap := armParams{rn: rn, pl: pl, proj: proj, sig: sig, prompt: pl.run.Prompt, model: pl.run.Model, ws: pl.ws,
		baseSHA: *pl.run.BaseSHA, baseDir: baseDir, att: att, skillName: proj.name, humanDirs: humanDirs, maxTurns: s.armTurns(pl.run.ID)}

	roles := plannedRoles(probe.Quick)
	total := probe.CanaryCostUSD
	if p, err := st.GetProbe(rn.probeID); err == nil {
		total = p.CanaryCostUSD
	}
	var recs []*armRecord
	budgetStop, unsafe := false, false
	for i, role := range roles {
		if ctx.Err() != nil {
			break
		}
		if att.Require(s.d.Now()) != nil { // stale: one more canary, charged to the probe
			if att, err = s.attest(ctx, rn.probeID); err != nil {
				if ctx.Err() != nil {
					break
				}
				s.failProbe(rn, pl, "sandbox could not be attested: "+s.short(err))
				return
			}
			ap.att = att
			if p, err := st.GetProbe(rn.probeID); err == nil {
				total = p.CanaryCostUSD + sumCost(recs)
			}
		}
		capUSD := min(2*pl.estArm, cfg.PerProbeBudgetUSD-total)
		if capUSD <= 0 {
			budgetStop = true
			break
		}
		ap.capUSD, ap.seq = capUSD, i
		rec := s.runOneArm(ctx, ap, role)
		recs = append(recs, rec)
		total += rec.cost
		if len(rec.findings) > 0 {
			unsafe = true
			break
		}
		if rec.end == arm.EndError || rec.end == arm.EndKilled {
			break
		}
		if i < len(roles)-1 && total >= cfg.PerProbeBudgetUSD-1e-9 {
			budgetStop = true
			break
		}
	}

	if ctx.Err() != nil {
		s.settleCancelled(rn, pl)
		return
	}
	s.settle(rn, pl, proj, sig, roles, recs, budgetStop, unsafe)
}

func sumCost(recs []*armRecord) float64 {
	var t float64
	for _, r := range recs {
		t += r.cost
	}
	return t
}

// armTurns is the source run's tool-call count doubled, within [10, cap].
func (s *Service) armTurns(runID string) int {
	evs, _ := s.collectEvents(runID)
	ids := map[string]bool{}
	n := 0
	for _, e := range evs {
		if e.E.Type != taskrunner.EventTypeToolCall {
			continue
		}
		if e.E.ToolCallID == "" {
			n++
		} else if !ids[e.E.ToolCallID] {
			ids[e.E.ToolCallID] = true
			n++
		}
	}
	return min(s.d.Config().Probe.MaxTurnsCap, max(10, 2*n))
}

// settleCancelled records a cancelled probe.
func (s *Service) settleCancelled(rn *running, pl plan) {
	st := s.d.Store
	p, err := st.GetProbe(rn.probeID)
	if err != nil {
		return
	}
	now := s.d.Now().UTC()
	p.Status, p.VerdictReason, p.FinishedAt = statusCancelled, "the probe was cancelled before it finished.", &now
	if err := st.UpdateProbe(p); err != nil {
		slog.Warn("learning probe: settle cancelled", "probe", rn.probeID, "err", err)
	}
	if s.d.Notify != nil {
		s.d.Notify.ProbeFinished(pl.ws.ID, rn.probeID, rn.proposal, statusCancelled, "")
	}
}

// settle contrasts the arms that ran, decides the advisory verdict and
// stores it. The verdict never touches the proposal.
func (s *Service) settle(rn *running, pl plan, proj probeProjections, sig *signature.Signature, roles []string, recs []*armRecord,
	budgetStop, unsafe bool) {
	st := s.d.Store
	p, err := st.GetProbe(rn.probeID)
	if err != nil {
		return
	}
	byRole := map[string]*armRecord{}
	ends := map[contrast.Role]verdict.ArmEnd{}
	var planned []contrast.Role
	for _, r := range roles {
		planned = append(planned, contrast.Role(r))
	}
	for _, r := range recs {
		byRole[r.role] = r
		ends[contrast.Role(r.role)] = verdict.ArmEnd(r.end)
	}
	failedArm := false
	for _, r := range recs {
		if r.end == arm.EndError || r.end == arm.EndKilled {
			failedArm = true
		}
	}

	var con contrast.Contrast
	var res verdict.Result
	switch {
	case budgetStop && !unsafe && !failedArm:
		res = verdict.Result{Code: verdict.Inconclusive, Reason: "the per-probe budget cap was reached before every arm ran."}
	default:
		if a, b := byRole[roleA], byRole[roleB]; a != nil && b != nil && !unsafe {
			var a2 *contrast.ArmInput
			if r := byRole[roleA2]; r != nil {
				a2 = &r.input
			}
			con = contrast.Build(a.input, b.input, a2, proj.name, proj.b.Dir, sig)
		}
		res = verdict.Decide(verdict.Input{Quick: p.Quick, HasSignature: sig != nil, Contrast: con, Ends: ends, Planned: planned, Unsafe: unsafe})
	}

	if unsafe {
		for _, r := range recs {
			if len(r.findings) > 0 {
				res.Reason = fmt.Sprintf("a tripwire fired: %s; the probe was quarantined.", r.findings[0].Kind)
				break
			}
		}
	}
	now := s.d.Now().UTC()
	p.Verdict, p.VerdictReason, p.FinishedAt = string(res.Code), res.Reason, &now
	switch {
	case unsafe:
		p.Status = statusUnsafe
	case res.Code == verdict.ProbeFailed:
		p.Status = statusFailed
		p.Error = res.Reason
	default:
		p.Status = statusDone
	}
	if len(con.Metrics) > 0 {
		p.Contrast = mustJSON(toContrastView(con))
	}
	if err := st.UpdateProbe(p); err != nil {
		slog.Warn("learning probe: store verdict", "probe", rn.probeID, "err", err)
	}
	bRun := ""
	if r := byRole[roleB]; r != nil {
		bRun = r.runID
	}
	s.appendLedger(pl, p, bRun, s.armSummary(recs, proj))
	if s.d.Notify != nil {
		s.d.Notify.ProbeFinished(pl.ws.ID, rn.probeID, rn.proposal, p.Status, p.Verdict)
	}
}

// armSummary is a value-free one-liner of run ids and projection hashes.
func (s *Service) armSummary(recs []*armRecord, proj probeProjections) string {
	var parts []string
	var cost float64
	for _, r := range recs {
		parts = append(parts, fmt.Sprintf("%s=%s@%s", r.role, r.runID, proj.hashFor(r.role)))
		cost += r.cost
	}
	return fmt.Sprintf("arms %s; arm cost $%.4f", strings.Join(parts, " "), cost)
}

// appendLedger writes the probe's one `probed` ledger row.
func (s *Service) appendLedger(pl plan, p store.LearningProbe, bRunID, summary string) {
	ev, _ := json.Marshal([]map[string]any{{"kind": "probe", "run_id": bRunID, "seq": 0, "quote": clipText(summary, 500)}})
	err := s.d.Store.AppendLedger(store.LearnedLedgerEntry{
		WorkspaceID: pl.ws.ID, SkillName: pl.proposal.Name, Action: "probed", Reason: p.Verdict,
		Evidence: string(ev), RunID: bRunID, Actor: "system", CreatedAt: s.d.Now().UTC(),
	})
	if err != nil {
		slog.Warn("learning probe: ledger", "probe", p.ID, "err", err)
	}
}
