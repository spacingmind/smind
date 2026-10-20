package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/spacingmind/smind/internal/learning"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// errCooldown is internal: a non-explicit trigger inside the cool-down.
var errCooldown = errors.New("reflection cool-down active")

// handleFinished is the observer's logic for one finished run. With the kill
// switch off it ignores everything: no counter changes at all.
func (s *Service) handleFinished(f runs.RunFinished) {
	cfg := s.d.Config()
	if !cfg.Enabled || f.ChatID == 0 || !learning.Eligible(f.Purpose, "") {
		return
	}
	cw, err := s.d.Store.GetWorkspaceByChat(f.ChatID)
	if err != nil {
		slog.Warn("learning: resolve chat of finished run", "run", f.RunID, "chat", f.ChatID, "err", err)
		return
	}
	if !learning.Eligible(f.Purpose, cw.TaskKind) || cw.LearningMode != store.LearningModePropose {
		return
	}
	if !learning.EmitsToolEvents(f.Provider) {
		return
	}
	pending, err := s.d.Store.AddChatLearnPending(f.ChatID, int64(f.ToolCalls))
	if err != nil {
		slog.Warn("learning: count tool calls", "run", f.RunID, "chat", f.ChatID, "err", err)
		return
	}
	if pending < int64(cfg.CadenceToolCalls) {
		return
	}
	// Over cadence: reflect unless the workspace is busy or cooling down, in
	// which case the pending count is kept and a later run finish retries.
	if _, err := s.enqueue(cw, f.ChatID, triggerCadence, cfg.CooldownMinutes); err != nil &&
		!errors.Is(err, ErrBusy) && !errors.Is(err, errCooldown) {
		slog.Warn("learning: enqueue cadence job", "chat", f.ChatID, "err", err)
	}
}

// tailFlush reflects once on chats that were archived or idle with enough
// pending tool calls. Tail jobs respect the cool-down.
func (s *Service) tailFlush() {
	cfg := s.d.Config()
	if !cfg.Enabled {
		return
	}
	idleBefore := s.now().Add(-time.Duration(cfg.TailIdleMinutes) * time.Minute)
	chats, err := s.d.Store.ListTailCandidates(int64(cfg.TailFlushPending), idleBefore)
	if err != nil {
		slog.Warn("learning: list tail candidates", "err", err)
		return
	}
	for _, c := range chats {
		cw, err := s.d.Store.GetWorkspaceByChat(c.ID)
		if err != nil || !learning.Eligible(learning.PurposeUser, cw.TaskKind) || cw.LearningMode != store.LearningModePropose {
			continue
		}
		if _, err := s.enqueue(cw, c.ID, triggerTail, cfg.CooldownMinutes); err != nil &&
			!errors.Is(err, ErrBusy) && !errors.Is(err, errCooldown) {
			slog.Warn("learning: enqueue tail job", "chat", c.ID, "err", err)
		}
	}
}

// Reflect is the explicit trigger ("Learn from this chat"). It bypasses the
// cadence and the cool-down but not the kill switch, the workspace mode,
// single-flight, the provider gate or the budget (the last two are decided by
// the job and recorded on its row). It returns once the job is queued.
func (s *Service) Reflect(ctx context.Context, chatID int64) error {
	_, err := s.ReflectJob(ctx, chatID)
	return err
}

// ReflectJob is Reflect returning the queued job's id.
func (s *Service) ReflectJob(ctx context.Context, chatID int64) (int64, error) {
	cfg := s.d.Config()
	if !cfg.Enabled {
		return 0, ErrDisabled
	}
	cw, err := s.d.Store.GetWorkspaceByChat(chatID)
	if err != nil {
		return 0, err
	}
	if !learning.Eligible(learning.PurposeUser, cw.TaskKind) {
		return 0, ErrNotEligible
	}
	if cw.LearningMode != store.LearningModePropose {
		return 0, ErrModeOff
	}
	chat, err := s.d.Store.GetChat(chatID)
	if err != nil {
		return 0, err
	}
	rs, err := s.d.Store.ListChatRunsWithEventsAfter(chatID, chat.LearnWatermark)
	if err != nil {
		return 0, err
	}
	if len(rs) == 0 {
		return 0, ErrNothingToReflect
	}
	job, err := s.enqueue(cw, chatID, triggerExplicit, 0)
	if err != nil {
		return 0, err
	}
	return job.ID, nil
}

// enqueue is the single place a reflect job is created. Under one lock it
// checks the in-memory single-flight set, the stored active-job state and
// (cooldownMinutes > 0) the cool-down, then writes the queued row and starts
// the job goroutine, so concurrent triggers can never both pass.
func (s *Service) enqueue(cw store.ChatWorkspace, chatID int64, trigger string, cooldownMinutes int) (store.LearningJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return store.LearningJob{}, ErrClosed
	}
	if s.active[cw.WorkspaceID] {
		return store.LearningJob{}, ErrBusy
	}
	busy, err := s.d.Store.HasActiveLearningJob(cw.WorkspaceID)
	if err != nil {
		return store.LearningJob{}, err
	}
	if busy {
		return store.LearningJob{}, ErrBusy
	}
	now := s.now()
	if cooldownMinutes > 0 {
		last, ok, err := s.d.Store.LastLearningJobAt(cw.WorkspaceID, jobKindReflect)
		if err != nil {
			return store.LearningJob{}, err
		}
		if ok && now.Sub(last) < time.Duration(cooldownMinutes)*time.Minute {
			return store.LearningJob{}, errCooldown
		}
	}
	chat, err := s.d.Store.GetChat(chatID)
	if err != nil {
		return store.LearningJob{}, err
	}
	job, err := s.d.Store.CreateLearningJob(store.LearningJob{
		WorkspaceID: cw.WorkspaceID, ChatID: &chatID, Kind: jobKindReflect, TriggerKind: trigger,
		WindowFromEventID: chat.LearnWatermark, Status: "queued", CreatedAt: now,
	})
	if err != nil {
		return store.LearningJob{}, fmt.Errorf("create reflect job: %w", err)
	}
	// Announce "queued" BEFORE the goroutine can announce "running": started
	// first, a busy scheduler (or a slow notifier) let a client see running or
	// even a terminal status before queued.
	s.d.Notify.NotifyJob(cw.WorkspaceID, job.ID, statusQueued)
	s.active[cw.WorkspaceID] = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.active, cw.WorkspaceID)
			s.mu.Unlock()
		}()
		s.runJob(job.ID)
	}()
	return job, nil
}
