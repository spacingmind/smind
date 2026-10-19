package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/reflector"
	"github.com/spacingmind/smind/internal/store"
)

// ErrNotEnoughSkills: an explicit curation needs at least two live learned
// skills to fold anything together.
var ErrNotEnoughSkills = errors.New("not enough learned skills to curate")

const (
	jobKindCurate   = "curate"
	triggerSchedule = "schedule"

	curateTimeout   = 5 * time.Minute
	snapshotsKept   = 5
	minCurateSkills = 2
	// ledgerScanRows bounds how much of the ledger the scheduler reads per
	// workspace per tick; five landings are always found among the newest
	// rows unless a long run of non-landing rows (proposals, pins) buries
	// them, in which case the next landing surfaces them.
	ledgerScanRows = 500

	findingBundle = "bundle_rejected"
	findingStale  = "stale_skill"
)

// landingActions are the ledger actions that count as a landed change for
// the curator cadence (ADR-0026 section 7).
var landingActions = []string{"approve", "merge", "patch", "undo", "restore-snapshot"}

// Curate is the explicit trigger ("Curate now"). It bypasses the cadence
// thresholds but not the kill switch, the workspace mode, single-flight or
// the budget (decided by the job and recorded on its row). It returns once
// the job is queued.
func (s *Service) Curate(ctx context.Context, workspaceID int64) error {
	_, err := s.CurateJob(ctx, workspaceID)
	return err
}

// CurateJob is Curate returning the queued job's id.
func (s *Service) CurateJob(ctx context.Context, workspaceID int64) (int64, error) {
	if !s.d.Config().Enabled {
		return 0, ErrDisabled
	}
	mode, err := s.d.Store.GetWorkspaceLearningMode(workspaceID)
	if err != nil {
		return 0, err
	}
	if mode != store.LearningModePropose {
		return 0, ErrModeOff
	}
	skills, err := s.d.Store.ListLearnedSkills(workspaceID, store.SkillActive, store.SkillDisabled)
	if err != nil {
		return 0, err
	}
	if len(skills) < minCurateSkills {
		return 0, ErrNotEnoughSkills
	}
	job, err := s.enqueueCurate(workspaceID, triggerExplicit, nil)
	if err != nil {
		return 0, err
	}
	return job.ID, nil
}

// enqueueCurate is the single place a curate job is created. Like enqueue it
// checks single-flight under the lock; due, when set, is re-evaluated under
// the lock so a scheduled job is only created if it is still due.
func (s *Service) enqueueCurate(workspaceID int64, trigger string, due func() (bool, error)) (store.LearningJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return store.LearningJob{}, ErrClosed
	}
	if s.active[workspaceID] {
		return store.LearningJob{}, ErrBusy
	}
	busy, err := s.d.Store.HasActiveLearningJob(workspaceID)
	if err != nil {
		return store.LearningJob{}, err
	}
	if busy {
		return store.LearningJob{}, ErrBusy
	}
	if due != nil {
		if ok, err := due(); err != nil || !ok {
			return store.LearningJob{}, err
		}
	}
	job, err := s.d.Store.CreateLearningJob(store.LearningJob{
		WorkspaceID: workspaceID, Kind: jobKindCurate, TriggerKind: trigger, Status: statusQueued, CreatedAt: s.now(),
	})
	if err != nil {
		return store.LearningJob{}, fmt.Errorf("create curate job: %w", err)
	}
	s.active[workspaceID] = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.active, workspaceID)
			s.mu.Unlock()
		}()
		s.runJob(job.ID)
	}()
	s.d.Notify.NotifyJob(workspaceID, job.ID, statusQueued)
	return job, nil
}

// scheduleCurators evaluates every propose-mode workspace and enqueues a
// scheduled curate job where the cadence says so.
func (s *Service) scheduleCurators() {
	cfg := s.d.Config()
	if !cfg.Enabled {
		return
	}
	wss, err := s.d.Store.ListWorkspaces()
	if err != nil {
		slog.Warn("learning: list workspaces for curator", "err", err)
		return
	}
	for _, w := range wss {
		if w.LearningMode != store.LearningModePropose {
			continue
		}
		due := func() (bool, error) { return s.curatorDue(w.ID, cfg) }
		if ok, err := due(); err != nil {
			slog.Warn("learning: curator cadence", "workspace", w.ID, "err", err)
			continue
		} else if !ok {
			continue
		}
		if _, err := s.enqueueCurate(w.ID, triggerSchedule, due); err != nil && !errors.Is(err, ErrBusy) {
			slog.Warn("learning: enqueue curate job", "workspace", w.ID, "err", err)
		}
	}
}

// curatorDue applies the ADR-0026 section 7 thresholds: the library is large
// enough, the last curation (failed or skipped ones aside) is older than the
// minimum interval, and enough changes landed since it. A failed or skipped
// attempt does not start the weekly limit but backs off for the reflection
// cool-down so a persistent failure cannot retry every tick.
func (s *Service) curatorDue(workspaceID int64, cfg config.LearningConfig) (bool, error) {
	now := s.now()
	jobs, err := s.d.Store.ListLearningJobs(workspaceID, 50)
	if err != nil {
		return false, err
	}
	var last time.Time
	for _, j := range jobs { // newest first
		if j.Kind != jobKindCurate {
			continue
		}
		if j.Status == statusFailed || j.Status == statusSkipped {
			if now.Sub(j.CreatedAt) < time.Duration(cfg.CooldownMinutes)*time.Minute {
				return false, nil
			}
			continue
		}
		last = j.CreatedAt
		break
	}
	if !last.IsZero() && now.Sub(last) < time.Duration(cfg.CuratorMinIntervalDays)*24*time.Hour {
		return false, nil
	}
	skills, err := s.d.Store.ListLearnedSkills(workspaceID, store.SkillActive, store.SkillDisabled)
	if err != nil {
		return false, err
	}
	if len(skills) < cfg.CuratorMinSkills || len(skills) < minCurateSkills {
		return false, nil
	}
	entries, err := s.d.Store.ListLedger(workspaceID, 0, ledgerScanRows)
	if err != nil {
		return false, err
	}
	landed := 0
	for _, e := range entries {
		if e.CreatedAt.After(last) && slices.Contains(landingActions, e.Action) {
			landed++
		}
	}
	return landed >= cfg.CuratorEveryLandings, nil
}

// curate runs one queued curate job: budget, library snapshot, the isolated
// curator run, gate, and one pending proposal per bundle or standalone patch.
// It never changes a skill itself.
func (s *Service) curate(job *store.LearningJob, cfg config.LearningConfig) {
	mode, err := s.d.Store.GetWorkspaceLearningMode(job.WorkspaceID)
	if err != nil {
		s.fail(job, "load workspace: "+err.Error())
		return
	}
	if mode != store.LearningModePropose {
		s.skip(job, skipModeOff)
		return
	}
	s.setStatus(job, statusRunning, nil)

	learned, err := s.d.Store.ListLearnedSkills(job.WorkspaceID, store.SkillActive, store.SkillDisabled)
	if err != nil {
		s.fail(job, "load learned skills: "+err.Error())
		return
	}
	if len(learned) < minCurateSkills {
		s.setStatus(job, statusEmpty, nil)
		return
	}
	bodies := make([]reflector.SkillBody, 0, len(learned))
	for _, k := range learned {
		bodies = append(bodies, reflector.SkillBody{Name: k.Name, Category: k.Category, Description: k.Description, Body: k.Body})
	}
	prompt := reflector.ComposeCuratorPrompt(bodies)
	var shown []string
	for _, k := range learned {
		if strings.Contains(prompt, "### "+k.Name+"\n") {
			shown = append(shown, k.Name)
		}
	}

	// Budget: decided before anything is written or run.
	if over, err := s.overBudget(job.WorkspaceID, cfg, len(prompt)); err != nil {
		s.fail(job, "budget check: "+err.Error())
		return
	} else if over {
		s.skip(job, skipBudget)
		return
	}

	// The snapshot comes first; without it the job does not run.
	if err := s.takeSnapshot(job.WorkspaceID, fmt.Sprintf("before curation job %d", job.ID)); err != nil {
		s.fail(job, "snapshot: "+err.Error())
		return
	}

	res, err := s.d.Exec.Run(s.ctx, executor.Request{
		WorkspaceID: job.WorkspaceID, Purpose: learning.PurposeCurate, Model: cfg.CuratorModel,
		SystemPrompt: reflector.CuratorPrompt(), UserPrompt: prompt, MaxTurns: 1, Timeout: curateTimeout,
	})
	usage := func(j *store.LearningJob) {
		j.RunID = res.RunID
		j.CostUSD = res.CostUSD
		if res.InputTokens != nil || res.OutputTokens != nil {
			var n int64
			if res.InputTokens != nil {
				n += *res.InputTokens
			}
			if res.OutputTokens != nil {
				n += *res.OutputTokens
			}
			j.Tokens = &n
		}
	}
	failed := func(msg string) {
		s.setStatus(job, statusFailed, func(j *store.LearningJob) { usage(j); j.Error = clipError(msg) })
	}
	if err != nil {
		failed(err.Error())
		return
	}

	intents, err := reflector.ParseOutput(res.FinalText, gate.ModeCurate)
	if err == nil {
		err = reflector.ScopeCheck(intents, gate.ModeCurate, shown)
	}
	if err != nil {
		failed(err.Error())
		return
	}

	verdicts, created, err := s.gateAndStoreCurated(job, intents, learned)
	vj, _ := json.Marshal(verdicts)
	if err != nil {
		s.setStatus(job, statusFailed, func(j *store.LearningJob) {
			usage(j)
			j.Verdicts = string(vj)
			j.Error = clipError(err.Error())
		})
		return
	}
	status := statusEmpty
	if created > 0 {
		status = statusLanded
	}
	s.setStatus(job, status, func(j *store.LearningJob) { usage(j); j.Verdicts = string(vj) })
}

func (s *Service) takeSnapshot(workspaceID int64, reason string) error {
	if s.snapshotFn != nil {
		return s.snapshotFn(workspaceID, reason)
	}
	_, err := s.d.Store.CreateLearningSnapshot(workspaceID, reason, snapshotsKept)
	return err
}

// gateAndStoreCurated gates every intent against the library as it is NOW
// (the run took a while) and stores bundles and standalone patches as
// pending proposals. shown are the skills (id and version) the model saw: a
// skill changed since is a stale target and fails its intent.
func (s *Service) gateAndStoreCurated(job *store.LearningJob, intents []gate.Intent, shown []store.LearnedSkill) ([]verdict, int, error) {
	ws, err := s.d.Store.GetWorkspace(job.WorkspaceID)
	if err != nil {
		return nil, 0, fmt.Errorf("load workspace: %w", err)
	}
	rd := s.redactor(store.ChatWorkspace{WorkspaceID: ws.ID, WorkspacePath: ws.Path})
	now, err := s.d.Store.ListLearnedSkills(job.WorkspaceID, store.SkillActive, store.SkillDisabled)
	if err != nil {
		return nil, 0, fmt.Errorf("reload learned skills: %w", err)
	}
	byName := make(map[string]store.LearnedSkill, len(now))
	live := make([]gate.LiveSkill, 0, len(now))
	for _, k := range now {
		byName[k.Name] = k
		live = append(live, gate.LiveSkill{Name: k.Name, Category: k.Category, Description: k.Description, Body: k.Body, Status: k.Status})
	}
	pending, err := s.d.Store.ListProposals(job.WorkspaceID, "pending")
	if err != nil {
		return nil, 0, fmt.Errorf("load pending proposals: %w", err)
	}
	for _, p := range pending {
		if p.Kind != "create" {
			continue
		}
		if items, err := approval.DecodeItems(p.Intents); err == nil && len(items) > 0 {
			r := items[0].Result
			live = append(live, gate.LiveSkill{Name: r.Name, Category: r.Category, Description: r.Description, Body: r.Body, Status: "proposed"})
		}
	}
	if s.d.Human != nil {
		live = append(live, s.d.Human(job.WorkspaceID)...)
	}
	shownByName := make(map[string]store.LearnedSkill, len(shown))
	for _, k := range shown {
		shownByName[k.Name] = k
	}

	cfg := s.d.Config()
	gcfg := gate.DefaultConfig()
	gcfg.Mode = gate.ModeCurate
	gcfg.Redact = rd.String
	if cfg.Similarity.Duplicate > 0 {
		gcfg.Duplicate = cfg.Similarity.Duplicate
	}
	if cfg.Similarity.Possible > 0 {
		gcfg.Possible = cfg.Similarity.Possible
	}

	// deletes[umbrella] are the intents absorbed into that umbrella.
	deletes := map[string][]int{}
	for i, in := range intents {
		if in.Action == gate.ActionDelete {
			deletes[in.AbsorbedInto] = append(deletes[in.AbsorbedInto], i)
		}
	}
	absorbedBy := func(umbrella string) []string {
		var names []string
		for _, i := range deletes[umbrella] {
			names = append(names, intents[i].Name)
		}
		return names
	}

	type checked struct {
		verdict gate.Verdict
		entry   verdict
	}
	results := make([]checked, len(intents))
	for i, in := range intents {
		l := live
		if in.Action == gate.ActionPatch {
			// The merged text is compared against the library without the
			// skills it absorbs, as approval does when the bundle lands.
			absorbed := absorbedBy(in.Name)
			l = slices.DeleteFunc(slices.Clone(live), func(k gate.LiveSkill) bool {
				return !k.Human && slices.Contains(absorbed, k.Name)
			})
		}
		v := gate.Check(in, gate.Window{}, l, gcfg)
		if v.OK {
			if cur, ok := shownByName[in.Name]; !ok || byName[in.Name].ID != cur.ID || byName[in.Name].Version != cur.Version {
				v = gate.Verdict{Findings: []gate.Finding{{Reason: findingStale, Detail: "skill changed while the curator ran"}}}
			}
		}
		if v.OK && secretIn(rd, in) {
			v = gate.Verdict{Findings: []gate.Finding{{Reason: gate.ReasonSecret, Detail: "reason changed under redaction"}}}
		}
		entry := verdict{Name: clipField(in.Name), Action: clipField(string(in.Action))}
		for _, f := range v.Findings {
			entry.Findings = append(entry.Findings, verdictFinder{Reason: clipField(string(f.Reason)), Detail: clipField(f.Detail)})
		}
		results[i] = checked{verdict: v, entry: entry}
	}

	created := 0
	store1 := func(kind, name string, members []int, items []approval.Item, v gate.Verdict) error {
		p, err := s.d.Approval.CreateProposal(approval.NewProposal{
			WorkspaceID: job.WorkspaceID, JobID: job.ID, Source: "curator", Kind: kind, Name: name,
			Items: items, Evidence: "[]", PossibleDuplicateOf: v.PossibleDuplicateOf, Similarity: v.Similarity,
		})
		if err != nil {
			return fmt.Errorf("create proposal: %w", err)
		}
		created++
		for _, i := range members {
			results[i].entry.Accepted, results[i].entry.ProposalID = true, p.ID
		}
		s.d.Notify.NotifyProposed(job.WorkspaceID, p.ID, name, kind)
		return nil
	}
	collect := func() []verdict {
		out := make([]verdict, len(results))
		for i, r := range results {
			out[i] = r.entry
		}
		return out
	}
	patchItem := func(i int) approval.Item {
		target := byName[intents[i].Name]
		return approval.Item{Intent: intents[i], Result: results[i].verdict.Result, TargetSkillID: target.ID, BaseVersion: target.Version}
	}

	for i, in := range intents {
		if in.Action != gate.ActionPatch {
			continue
		}
		members := append([]int{i}, deletes[in.Name]...)
		ok := true
		for _, m := range members {
			ok = ok && results[m].verdict.OK
		}
		if !ok {
			// One bad member rejects the whole bundle.
			if len(members) > 1 {
				for _, m := range members {
					if results[m].verdict.OK {
						results[m].entry.Findings = append(results[m].entry.Findings,
							verdictFinder{Reason: findingBundle, Detail: "another member of the bundle failed the gate"})
					}
				}
			}
			continue
		}
		if len(members) == 1 {
			if err := store1("patch", in.Name, members, []approval.Item{patchItem(i)}, results[i].verdict); err != nil {
				return collect(), created, err
			}
			continue
		}
		items := []approval.Item{patchItem(i)}
		for _, m := range members[1:] {
			items = append(items, approval.Item{Intent: intents[m], TargetSkillID: byName[intents[m].Name].ID})
		}
		if err := store1("bundle", in.Name, members, items, results[i].verdict); err != nil {
			return collect(), created, err
		}
	}
	return collect(), created, nil
}
