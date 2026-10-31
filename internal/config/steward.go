package config

import (
	"fmt"
	"math"
)

// StewardConfig is the steward section (ADR-0047). Everything under it is a
// human edit of config.yaml only -- no RPC, agent or relay session can
// change it -- and the daemon reads the effective value on every decision
// (like plugins.enabled), so turning the engine on or off never needs a
// restart to take effect.
type StewardConfig struct {
	Policy StewardPolicyConfig `yaml:"policy"`
	// Enabled is the review engine's switch. Off by default.
	Enabled bool `yaml:"enabled"`
	// AutoReview lists workspace titles whose tasks are reviewed without a
	// human asking (exact match, like learning_mode's workspaces).
	AutoReview []string `yaml:"autoReview"`
	// Annotate writes the review's verdict on the task.
	Annotate bool `yaml:"annotate"`
	// MaxBudgetUsd is the hard ceiling BudgetUsd may not exceed. 0 = 5.
	MaxBudgetUsd float64 `yaml:"maxBudgetUsd"`
	// BudgetUsd caps one review. 0 = StewardDefaultBudgetUsd.
	BudgetUsd float64 `yaml:"budgetUsd"`
	// MaxMinutes bounds one review. 0 = StewardDefaultMaxMinutes.
	MaxMinutes int `yaml:"maxMinutes"`
	// MaxReviewsPerHour bounds reviews across all tasks per hour.
	// 0 = StewardDefaultReviewsPerHour.
	MaxReviewsPerHour int `yaml:"maxReviewsPerHour"`
	// MaxReviewsPerTask bounds reviews per task.
	// 0 = StewardDefaultReviewsPerTask.
	MaxReviewsPerTask int `yaml:"maxReviewsPerTask"`
}

// StewardPolicyConfig is the permission policy engine's switch (ADR-0047
// §2). Off by default: with no steward block the permission path is
// byte-for-byte today's. The switch being on says nothing about the policy
// file -- an invalid or unreadable file then means floor + ask, never
// allow-all -- and only a human edit turns it on.
type StewardPolicyConfig struct {
	Enabled bool `yaml:"enabled"`
}

// Steward budget and rate defaults plus the ceilings a human edit may not
// push past (ADR-0047 §8). Zero in config means "use the default", never a
// zero budget.
const (
	StewardDefaultBudgetUsd      = 1.5
	StewardMaxBudgetUsdCeiling   = 5
	StewardDefaultMaxMinutes     = 20
	StewardMaxMinutesCeiling     = 60
	StewardDefaultReviewsPerHour = 6
	StewardReviewsPerHourCeiling = 24
	StewardDefaultReviewsPerTask = 5
)

// Validate checks the review knobs. Zero is valid everywhere (it means "use
// the default"); negative, NaN or Inf numbers and anything over its ceiling
// are a load error.
func (c StewardConfig) Validate() error {
	for _, kv := range []struct {
		key string
		val float64
	}{
		{"maxBudgetUsd", c.MaxBudgetUsd},
		{"budgetUsd", c.BudgetUsd},
	} {
		if math.IsNaN(kv.val) || math.IsInf(kv.val, 0) {
			return fmt.Errorf("steward.%s must be a finite number, got %v", kv.key, kv.val)
		}
		if kv.val < 0 {
			return fmt.Errorf("steward.%s must be >= 0, got %v", kv.key, kv.val)
		}
	}
	if c.MaxBudgetUsd > StewardMaxBudgetUsdCeiling {
		return fmt.Errorf("steward.maxBudgetUsd must be <= %g, got %v", float64(StewardMaxBudgetUsdCeiling), c.MaxBudgetUsd)
	}
	ceiling := c.MaxBudgetUsd
	if ceiling <= 0 {
		ceiling = StewardMaxBudgetUsdCeiling
	}
	if c.BudgetUsd > ceiling {
		return fmt.Errorf("steward.budgetUsd must be <= steward.maxBudgetUsd (%g), got %v", ceiling, c.BudgetUsd)
	}
	for _, kv := range []struct {
		key string
		val int
	}{
		{"maxMinutes", c.MaxMinutes},
		{"maxReviewsPerHour", c.MaxReviewsPerHour},
		{"maxReviewsPerTask", c.MaxReviewsPerTask},
	} {
		if kv.val < 0 {
			return fmt.Errorf("steward.%s must be >= 0, got %d", kv.key, kv.val)
		}
	}
	if c.MaxMinutes > StewardMaxMinutesCeiling {
		return fmt.Errorf("steward.maxMinutes must be <= %d, got %d", StewardMaxMinutesCeiling, c.MaxMinutes)
	}
	if c.MaxReviewsPerHour > StewardReviewsPerHourCeiling {
		return fmt.Errorf("steward.maxReviewsPerHour must be <= %d, got %d", StewardReviewsPerHourCeiling, c.MaxReviewsPerHour)
	}
	return nil
}

// StewardLimits is the effective review budget and rate caps the daemon
// enforces: defaults filled in for zero values, everything clamped to its
// ceiling.
type StewardLimits struct {
	BudgetUSD      float64
	MaxMinutes     int
	ReviewsPerHour int
	ReviewsPerTask int
}

// Limits resolves the configured knobs to what a review runs under.
func (c StewardConfig) Limits() StewardLimits {
	budget := c.BudgetUsd
	if budget <= 0 {
		budget = StewardDefaultBudgetUsd
	}
	max := c.MaxBudgetUsd
	if max <= 0 {
		max = StewardMaxBudgetUsdCeiling
	}
	if budget > max {
		budget = max
	}
	if budget > StewardMaxBudgetUsdCeiling {
		budget = StewardMaxBudgetUsdCeiling
	}
	minutes := c.MaxMinutes
	if minutes <= 0 {
		minutes = StewardDefaultMaxMinutes
	}
	if minutes > StewardMaxMinutesCeiling {
		minutes = StewardMaxMinutesCeiling
	}
	perHour := c.MaxReviewsPerHour
	if perHour <= 0 {
		perHour = StewardDefaultReviewsPerHour
	}
	if perHour > StewardReviewsPerHourCeiling {
		perHour = StewardReviewsPerHourCeiling
	}
	perTask := c.MaxReviewsPerTask
	if perTask <= 0 {
		perTask = StewardDefaultReviewsPerTask
	}
	return StewardLimits{BudgetUSD: budget, MaxMinutes: minutes, ReviewsPerHour: perHour, ReviewsPerTask: perTask}
}

// AutoReviewFor reports whether tasks of the workspace titled workspaceTitle
// are reviewed without a human asking.
func (c StewardConfig) AutoReviewFor(workspaceTitle string) bool {
	for _, title := range c.AutoReview {
		if title == workspaceTitle {
			return true
		}
	}
	return false
}
