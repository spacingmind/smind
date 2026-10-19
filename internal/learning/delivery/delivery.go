// Package delivery hands approved learned skills to agent runs and counts
// what happens to them (ADR-0026 sections 5 and 6): it prepares each user
// run's per-run Delivery (a read-only add-dir projection for claude-native,
// an inline preface for GLM/Kimi, nothing for Codex), records offers and
// loads on run completion, and applies the lifecycle (graduation and
// capacity archiving) afterwards.
package delivery

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/lifecycle"
	"github.com/spacingmind/smind/internal/learning/loads"
	"github.com/spacingmind/smind/internal/learning/projection"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const queueSize = 256

// Deps are the Service's collaborators.
type Deps struct {
	Store *store.Store
	// Config returns the live learning config (read on every call, so the
	// kill switch takes effect without a restart of the service).
	Config func() config.LearningConfig
	// Root is <home>/learning/projection, where projections are built.
	Root string
	Now  func() time.Time
	// Notify, when set, is told after a lifecycle archive changed the
	// workspace's skills (reason is the lifecycle reason). Optional.
	Notify func(workspaceID int64, reason string)
}

// Service implements runs.DeliveryProvider and runs.Observer.
type Service struct {
	d Deps

	mu     sync.Mutex
	closed bool
	queue  chan runs.RunFinished
	done   chan struct{}
}

var (
	_ runs.DeliveryProvider = (*Service)(nil)
	_ runs.Observer         = (*Service)(nil)
)

// New builds a Service and starts its counting worker; Close stops it.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Config == nil {
		d.Config = config.DefaultLearning
	}
	s := &Service{d: d, queue: make(chan runs.RunFinished, queueSize), done: make(chan struct{})}
	go s.worker()
	return s
}

// Close drains the queued run completions, then stops the worker. Later
// OnRunFinished calls are dropped.
func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.queue)
	s.mu.Unlock()
	<-s.done
}

// activeSkills is the ONLY place that decides which skills are delivered:
// status active. Proposed (they live in learned_skill_proposals, never in
// learned_skills), disabled and archived skills are never delivered.
func (s *Service) activeSkills(workspaceID int64) ([]store.LearnedSkill, error) {
	return s.d.Store.ListLearnedSkills(workspaceID, store.SkillActive)
}

// Prepare implements runs.DeliveryProvider. It returns the empty Delivery
// unless learning is enabled globally and in "propose" mode for the
// workspace, the provider can take a delivery, and the workspace has active
// skills. Failures degrade to no delivery.
func (s *Service) Prepare(_ context.Context, workspaceID int64, _ string, provider taskrunner.Provider, purpose string) (taskrunner.Delivery, runs.OfferedInfo) {
	cfg := s.d.Config()
	if !cfg.Enabled || purpose != "" {
		return taskrunner.Delivery{}, runs.OfferedInfo{}
	}
	if provider != taskrunner.ProviderClaudeNative && provider != taskrunner.ProviderGLM && provider != taskrunner.ProviderKimi {
		return taskrunner.Delivery{}, runs.OfferedInfo{} // codex and unknown providers: untouched
	}
	mode, err := s.d.Store.GetWorkspaceLearningMode(workspaceID)
	if err != nil || mode != store.LearningModePropose {
		return taskrunner.Delivery{}, runs.OfferedInfo{}
	}
	skills, err := s.activeSkills(workspaceID)
	if err != nil {
		slog.Warn("learning delivery: list skills", "workspace", workspaceID, "err", err)
		return taskrunner.Delivery{}, runs.OfferedInfo{}
	}
	if len(skills) == 0 {
		return taskrunner.Delivery{}, runs.OfferedInfo{}
	}
	ids := make([]int64, len(skills))
	for i, k := range skills {
		ids[i] = k.ID
	}

	if provider == taskrunner.ProviderClaudeNative {
		ps := make([]projection.Skill, len(skills))
		for i, k := range skills {
			ps[i] = projection.Skill{Name: k.Name, Category: k.Category, Description: k.Description, Body: k.Body}
		}
		p, err := projection.Project(s.d.Root, workspaceID, ps)
		if err != nil || p.Dir == "" {
			slog.Warn("learning delivery: project", "workspace", workspaceID, "err", err)
			return taskrunner.Delivery{}, runs.OfferedInfo{}
		}
		// cfg.ClaudeIndexPreface (ADR-0026 D8) is deliberately a no-op in
		// v1: the CLI already lists projected skills natively, so nothing
		// extra is added whichever way the switch is set (default off).
		return taskrunner.Delivery{AddDirs: []string{p.Dir}}, runs.OfferedInfo{SkillIDs: ids, Measured: true}
	}

	// GLM/Kimi: inline preface on the first prompt of a session. Their loads
	// cannot be observed, so the offer is recorded unmeasured.
	pf := make([]projection.PrefaceSkill, len(skills))
	for i, k := range skills {
		rate := 0.0
		if k.OfferedRuns > 0 {
			rate = float64(k.LoadedRuns) / float64(k.OfferedRuns)
		}
		pf[i] = projection.PrefaceSkill{Name: k.Name, Description: k.Description, Body: k.Body, LoadRate: rate}
	}
	preface := projection.Preface(pf, cfg.PrefaceBudgetBytes)
	if preface == "" {
		return taskrunner.Delivery{}, runs.OfferedInfo{}
	}
	return taskrunner.Delivery{Preface: preface}, runs.OfferedInfo{SkillIDs: ids, Measured: false}
}

// Remove deletes the workspace's projection tree (forget / learning off
// hooks). A workspace whose skill set merely becomes empty keeps its stale
// directories: Project of an empty set creates nothing and the next
// projection's GC reclaims the old ones.
func (s *Service) Remove(workspaceID int64) error {
	return projection.Remove(s.d.Root, workspaceID)
}

// OnRunFinished implements runs.Observer. It only enqueues: counting and
// lifecycle work happen on the service's worker, never on the finish
// goroutine. System runs and runs that were offered nothing are ignored.
func (s *Service) OnRunFinished(f runs.RunFinished) {
	if f.Purpose != store.RunPurposeUser || len(f.OfferedSkillIDs) == 0 || !s.d.Config().Enabled {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.queue <- f:
	default:
		slog.Warn("learning delivery: queue full, run not counted", "run", f.RunID)
	}
}

func (s *Service) worker() {
	defer close(s.done)
	for f := range s.queue {
		s.safeProcess(f)
	}
}

func (s *Service) safeProcess(f runs.RunFinished) {
	defer func() {
		if v := recover(); v != nil {
			slog.Error("learning delivery: processing panicked", "run", f.RunID, "panic", v)
		}
	}()
	if err := s.process(f); err != nil {
		slog.Warn("learning delivery: processing failed", "run", f.RunID, "err", err)
	}
}

// process counts one finished run. The offer is recorded here, at finish,
// rather than at start: a run that was aborted before it began (task gone,
// worktree missing) is then not counted, and RecordOffered is idempotent per
// (skill, run), so a re-delivered notification counts once.
func (s *Service) process(f runs.RunFinished) error {
	st := s.d.Store
	if err := st.RecordOffered(f.RunID, f.OfferedSkillIDs, f.OfferedMeasured); err != nil {
		return err
	}
	task, err := st.GetTask(f.TaskID)
	if err != nil {
		return err
	}
	if f.OfferedMeasured && f.Provider == string(taskrunner.ProviderClaudeNative) {
		if err := s.countLoads(f, task.WorkspaceID); err != nil {
			return err
		}
	}
	return s.applyLifecycle(task.WorkspaceID)
}

func (s *Service) countLoads(f runs.RunFinished, workspaceID int64) error {
	names := map[string]int64{}
	known := map[string]bool{}
	for _, id := range f.OfferedSkillIDs {
		k, err := s.d.Store.GetLearnedSkill(id)
		if err != nil {
			continue // deleted since: nothing to count
		}
		names[k.Name] = k.ID
		known[k.Name] = true
	}
	events, err := s.d.Store.ListRunEvents(f.RunID)
	if err != nil {
		return err
	}
	dirs := s.projectionDirs(workspaceID)
	type key struct {
		skill int64
		via   string
	}
	seen := map[key]bool{}
	for _, row := range events {
		e, err := runs.DecodeStoredEvent(row.EventData)
		if err != nil {
			continue
		}
		for _, l := range loads.Match(e, known, dirs...) {
			k := key{names[l.Skill], l.Via}
			if seen[k] {
				continue
			}
			seen[k] = true
			if err := s.d.Store.RecordLoad(f.RunID, k.skill, k.via); err != nil {
				return err
			}
		}
	}
	return nil
}

// projectionDirs lists the workspace's projection hash directories (the
// current one and the previous one GC keeps; temp build dirs excluded).
func (s *Service) projectionDirs(workspaceID int64) []string {
	wsDir := filepath.Join(s.d.Root, strconv.FormatInt(workspaceID, 10))
	entries, err := os.ReadDir(wsDir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, filepath.Join(wsDir, e.Name()))
		}
	}
	return dirs
}

// applyLifecycle archives what lifecycle.Evaluate says (ledger actor
// "system"); pinned skills are exempt inside Evaluate.
func (s *Service) applyLifecycle(workspaceID int64) error {
	cfg := s.d.Config()
	active, err := s.activeSkills(workspaceID)
	if err != nil {
		return err
	}
	in := make([]lifecycle.Skill, len(active))
	for i, k := range active {
		in[i] = lifecycle.Skill{ID: k.ID, Name: k.Name, Status: k.Status, Pinned: k.Pinned, Measured: k.Measured, OfferedRuns: k.OfferedRuns, LoadedRuns: k.LoadedRuns}
	}
	for _, dec := range lifecycle.Evaluate(in, lifecycle.Params{ProbationRuns: cfg.ProbationRuns, Capacity: cfg.Capacity}) {
		if dec.Action != lifecycle.ActionArchive {
			continue
		}
		if err := s.d.Store.SetSkillStatus(dec.ID, store.SkillArchived, "system", dec.Reason, ""); err != nil {
			slog.Warn("learning delivery: archive", "skill", dec.ID, "reason", dec.Reason, "err", err)
			continue
		}
		if s.d.Notify != nil {
			s.d.Notify(workspaceID, dec.Reason)
		}
	}
	return nil
}
