package steward

import (
	"sync"

	"github.com/spacingmind/smind/internal/config"
)

// RunDone is a finished run event the auto trigger listens to. Status is
// done, error or stopped; ParentTaskID 0 means a root run.
type RunDone struct {
	RunID          string
	TaskID         int64
	Status         string // done|error|stopped
	ProfileName    string
	ParentTaskID   int64 // 0 = root
	Branch         string
	HasHead        bool
	WorkspaceTitle string
}

// seenRunCap bounds the remembered run ids so a long-lived trigger cannot
// grow without limit.
const seenRunCap = 1024

// AutoTrigger enqueues one steward review when a root Lead run ends done
// with a pushed branch, for a workspace opted into steward.autoReview
// (ADR-0047 §4, default off).
type AutoTrigger struct {
	Config func() (config.StewardConfig, error)
	Fire   func(taskID int64)

	mu    sync.Mutex
	seen  map[string]struct{}
	order []string
}

// NewAutoTrigger builds a trigger reading cfg and calling fire for every
// review it enqueues.
func NewAutoTrigger(cfg func() (config.StewardConfig, error), fire func(taskID int64)) *AutoTrigger {
	return &AutoTrigger{Config: cfg, Fire: fire, seen: make(map[string]struct{})}
}

// OnRunFinished fires a review for ev's task when every trigger condition
// holds — the config readable and enabled, the workspace opted in, a root
// Lead run done with a branch and a head, and this run not seen before — and
// reports whether it fired. Fire runs outside the lock, so it may call back
// into the trigger.
func (a *AutoTrigger) OnRunFinished(ev RunDone) bool {
	cfg, err := a.Config()
	if err != nil || !cfg.Enabled || !cfg.AutoReviewFor(ev.WorkspaceTitle) {
		return false
	}
	if ev.Status != "done" || ev.ProfileName != "lead" || ev.ParentTaskID != 0 || ev.Branch == "" || !ev.HasHead {
		return false
	}
	a.mu.Lock()
	if _, dup := a.seen[ev.RunID]; dup {
		a.mu.Unlock()
		return false
	}
	a.seen[ev.RunID] = struct{}{}
	a.order = append(a.order, ev.RunID)
	if len(a.order) > seenRunCap {
		delete(a.seen, a.order[0])
		a.order = a.order[1:]
	}
	a.mu.Unlock()
	a.Fire(ev.TaskID)
	return true
}
