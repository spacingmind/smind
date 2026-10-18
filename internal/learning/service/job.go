package service

import (
	"log/slog"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/store"
)

// Job statuses and skip reasons recorded on learning_jobs.
const (
	statusQueued  = "queued"
	statusRunning = "running"
	statusLanded  = "landed"
	statusEmpty   = "empty"
	statusFailed  = "failed"
	statusSkipped = "skipped"

	skipDisabled = "disabled"
	skipModeOff  = "mode_off"
)

// setStatus moves job to status, applying mutate first, and tells the
// notifier. A terminal status stamps finished_at.
func (s *Service) setStatus(job *store.LearningJob, status string, mutate func(*store.LearningJob)) {
	job.Status = status
	if mutate != nil {
		mutate(job)
	}
	if status != statusQueued && status != statusRunning {
		t := s.now()
		job.FinishedAt = &t
	}
	if err := s.d.Store.UpdateLearningJob(*job); err != nil {
		slog.Error("learning: update job", "job", job.ID, "status", status, "err", err)
		return
	}
	s.d.Notify.NotifyJob(job.WorkspaceID, job.ID, status)
}

func (s *Service) skip(job *store.LearningJob, reason string) {
	s.setStatus(job, statusSkipped, func(j *store.LearningJob) { j.SkipReason = reason })
}

func (s *Service) fail(job *store.LearningJob, msg string) {
	s.setStatus(job, statusFailed, func(j *store.LearningJob) { j.Error = clipError(msg) })
}

// runJob drives one queued job to a terminal state. Every path ends with an
// UpdateLearningJob; only the pipeline below decides about watermarks.
func (s *Service) runJob(jobID int64) {
	if s.onJobStart != nil {
		s.onJobStart(jobID)
	}
	job, err := s.d.Store.GetLearningJob(jobID)
	if err != nil {
		slog.Error("learning: load job", "job", jobID, "err", err)
		return
	}
	cfg := s.d.Config()
	if !cfg.Enabled {
		s.skip(&job, skipDisabled)
		return
	}
	if job.ChatID == nil {
		s.fail(&job, "job has no chat")
		return
	}
	cw, err := s.d.Store.GetWorkspaceByChat(*job.ChatID)
	if err != nil {
		s.fail(&job, "resolve chat: "+err.Error())
		return
	}
	if cw.LearningMode != store.LearningModePropose {
		s.skip(&job, skipModeOff)
		return
	}
	s.reflect(&job, cw, cfg)
}

// reflect is the reflect pipeline (filled in by the next commit).
func (s *Service) reflect(job *store.LearningJob, cw store.ChatWorkspace, cfg config.LearningConfig) {
	s.skip(job, "pipeline_pending")
}

const maxErrorBytes = 200

// clipError bounds text stored on a job row: error strings may embed model
// output, which must never be persisted beyond a short, rune-safe clip.
func clipError(msg string) string {
	if len(msg) <= maxErrorBytes {
		return msg
	}
	n := maxErrorBytes
	for n > 0 && msg[n]&0xC0 == 0x80 {
		n--
	}
	return msg[:n]
}
