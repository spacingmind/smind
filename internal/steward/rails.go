package steward

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/store"
)

// RailStore is the slice of *store.Store the rails read; a fake satisfies it
// in tests.
type RailStore interface {
	FindStewardReviewByKey(taskID int64, headSHA, contractSha, ciState string) (store.StewardReview, error)
	CountStewardReviewsSince(since time.Time) (int, error)
	CountStewardReviewsForTask(taskID int64) (int, error)
	ConsecutiveStewardFailures() (int, error)
}

// AutoPauseAfter is how many consecutive crashed or timed-out reviews pause
// the auto trigger; a manual review ends the run and resumes it (ADR-0047 §4).
const AutoPauseAfter = 3

// Review triggers (ADR-0047 §4): the Human asking, or the opt-in auto
// trigger. Only the auto one is ever refused by the rails' pause.
const (
	TriggerHuman = "human"
	TriggerAuto  = "auto"
)

// ReviewKey is the dedupe tuple (ADR-0047 §4): each part changes the key, so
// a new head, contract or CI state is a new review.
type ReviewKey struct {
	TaskID      int64
	HeadSHA     string
	ContractSha string
	CIState     string
}

// RailRequest is one review the rails judge. BudgetUSD and MaxMinutes are 0
// to use the configured defaults.
type RailRequest struct {
	Key        ReviewKey
	Trigger    string
	BudgetUSD  float64
	MaxMinutes int
}

var (
	ErrDisabled      = errors.New("steward reviews are off (steward.enabled is false or the config is invalid)")
	ErrPaused        = errors.New("auto review is paused after 3 consecutive crashed or timed-out reviews; run a manual review to resume")
	ErrHourlyCap     = errors.New("hourly review cap reached")
	ErrTaskCap       = errors.New("per-task review cap reached")
	ErrBudgetCeiling = errors.New("requested budget or minutes exceed the configured ceiling")
)

// DuplicateError reports that an identical review already exists for this
// head, contract and CI state.
type DuplicateError struct {
	ReviewID int64
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("an identical review (#%d) already exists for this head, contract and CI state", e.ReviewID)
}

// Rails are the review engine's guard rails (ADR-0047 §4): the switch, the
// auto pause, the budget ceiling, the dedupe key and the two rate caps, all
// read live from the config like plugins.enabled.
type Rails struct {
	Store  RailStore
	Config func() (config.StewardConfig, error)
	Now    func() time.Time // nil = time.Now
}

// Enabled reports whether reviews are on. An unreadable or invalid config
// counts as off, never as on.
func (r *Rails) Enabled() bool {
	cfg, err := r.Config()
	return err == nil && cfg.Enabled
}

// Limits returns the effective caps; a config error means the defaults.
func (r *Rails) Limits() config.StewardLimits {
	cfg, err := r.Config()
	if err != nil {
		return config.StewardConfig{}.Limits()
	}
	return cfg.Limits()
}

// AutoPaused reports whether the auto trigger is paused after
// AutoPauseAfter consecutive crashed or timed-out reviews. A Human trigger
// is never paused: a finished manual review ends the failure run in the
// store, which is how auto resumes.
func (r *Rails) AutoPaused() (bool, error) {
	n, err := r.Store.ConsecutiveStewardFailures()
	if err != nil {
		return false, fmt.Errorf("steward auto-pause check: %w", err)
	}
	return n >= AutoPauseAfter, nil
}

func (r *Rails) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Check judges one review request against every rail, in order: the switch,
// the auto pause, the budget ceiling, the dedupe key, then the two rate
// caps. It returns nil when the review may start.
func (r *Rails) Check(req RailRequest) error {
	cfg, err := r.Config()
	if err != nil || !cfg.Enabled {
		return ErrDisabled
	}
	if req.Trigger == TriggerAuto {
		paused, err := r.AutoPaused()
		if err != nil {
			return err
		}
		if paused {
			return ErrPaused
		}
	}
	ceiling := cfg.MaxBudgetUsd
	if ceiling <= 0 || ceiling > config.StewardMaxBudgetUsdCeiling {
		ceiling = config.StewardMaxBudgetUsdCeiling
	}
	if req.BudgetUSD < 0 || req.BudgetUSD > ceiling || req.MaxMinutes < 0 || req.MaxMinutes > config.StewardMaxMinutesCeiling {
		return ErrBudgetCeiling
	}
	if existing, err := r.Store.FindStewardReviewByKey(req.Key.TaskID, req.Key.HeadSHA, req.Key.ContractSha, req.Key.CIState); err == nil {
		return &DuplicateError{ReviewID: existing.ID}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("steward dedupe check: %w", err)
	}
	since, err := r.Store.CountStewardReviewsSince(r.now().Add(-time.Hour))
	if err != nil {
		return fmt.Errorf("steward hourly cap check: %w", err)
	}
	if since >= r.Limits().ReviewsPerHour {
		return ErrHourlyCap
	}
	perTask, err := r.Store.CountStewardReviewsForTask(req.Key.TaskID)
	if err != nil {
		return fmt.Errorf("steward per-task cap check: %w", err)
	}
	if perTask >= r.Limits().ReviewsPerTask {
		return ErrTaskCap
	}
	return nil
}

// EffectiveBudget resolves a request to the budget and minutes a review runs
// under: the request's values when > 0, else the limits'.
func EffectiveBudget(req RailRequest, l config.StewardLimits) (usd float64, minutes int) {
	usd = req.BudgetUSD
	if usd <= 0 {
		usd = l.BudgetUSD
	}
	minutes = req.MaxMinutes
	if minutes <= 0 {
		minutes = l.MaxMinutes
	}
	return usd, minutes
}
