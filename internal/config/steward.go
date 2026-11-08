package config

import (
	"fmt"
	"math"
	"strings"
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
	// TwoAxis runs a review as two independent seat passes, Standards and Spec
	// (ADR-0054). Off by default: off is today's single run, unchanged.
	TwoAxis bool `yaml:"twoAxis"`
	// Passes caps each axis pass of a two-axis review (ADR-0054 §7). Zero
	// means the default; with twoAxis on the pass budgets together may not
	// exceed the total steward.budgetUsd.
	Passes StewardPassesConfig `yaml:"passes"`
	// Standards names the repository files the Standards pass judges against,
	// plus extra one-line rules (ADR-0054 §3). Human-authored config only.
	Standards StewardStandardsConfig `yaml:"standards"`
	// AutoReview lists workspace titles whose tasks are reviewed without a
	// human asking (exact match, like learning_mode's workspaces).
	AutoReview []string `yaml:"autoReview"`
	// Annotate turns on steward_annotate: an advisory recommendation on an escalated permission request (ADR-0047 §6, S2b). Off by default.
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

// StewardPassesConfig holds the per-axis caps of a two-axis review
// (ADR-0054 §7).
type StewardPassesConfig struct {
	Standards StewardPassConfig `yaml:"standards"`
	Spec      StewardPassConfig `yaml:"spec"`
}

// StewardPassConfig is one axis pass's budget, wall clock and seat profile.
// Zero means the default.
type StewardPassConfig struct {
	BudgetUsd  float64 `yaml:"budgetUsd"`
	MaxMinutes int     `yaml:"maxMinutes"`
	Profile    string  `yaml:"profile"`
}

// StewardStandardsConfig names the standards files the Standards pass judges
// against, plus extra rules (ADR-0054 §3): paths inside the repository,
// one-line text, both human-authored.
type StewardStandardsConfig struct {
	Files []string `yaml:"files"`
	Rules []string `yaml:"rules"`
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

// Two-axis pass defaults (ADR-0054 §7): the pass budgets sum to the default
// total (0.4 + 1.1 = 1.5), and a pass's minutes default and ceiling are the
// review's own (StewardDefaultMaxMinutes / StewardMaxMinutesCeiling).
const (
	StewardPassStandardsDefaultBudgetUsd = 0.4
	StewardPassSpecDefaultBudgetUsd      = 1.1
	StewardPassStandardsDefaultProfile   = "steward-standards"
	StewardPassSpecDefaultProfile        = "steward"
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
	for _, pass := range []struct {
		key string
		cfg StewardPassConfig
	}{
		{"standards", c.Passes.Standards},
		{"spec", c.Passes.Spec},
	} {
		if math.IsNaN(pass.cfg.BudgetUsd) || math.IsInf(pass.cfg.BudgetUsd, 0) {
			return fmt.Errorf("steward.passes.%s.budgetUsd must be a finite number, got %v", pass.key, pass.cfg.BudgetUsd)
		}
		if pass.cfg.BudgetUsd < 0 {
			return fmt.Errorf("steward.passes.%s.budgetUsd must be >= 0, got %v", pass.key, pass.cfg.BudgetUsd)
		}
		if pass.cfg.MaxMinutes < 0 {
			return fmt.Errorf("steward.passes.%s.maxMinutes must be >= 0, got %d", pass.key, pass.cfg.MaxMinutes)
		}
		if pass.cfg.MaxMinutes > StewardMaxMinutesCeiling {
			return fmt.Errorf("steward.passes.%s.maxMinutes must be <= %d, got %d", pass.key, StewardMaxMinutesCeiling, pass.cfg.MaxMinutes)
		}
	}
	if len(c.Standards.Files) > 20 {
		return fmt.Errorf("steward.standards.files must list at most 20 entries, got %d", len(c.Standards.Files))
	}
	for i, f := range c.Standards.Files {
		if f == "" {
			return fmt.Errorf("steward.standards.files[%d] must not be empty", i)
		}
		if strings.HasPrefix(f, "/") || hasDriveLetter(f) {
			return fmt.Errorf("steward.standards.files[%d] must be a relative path inside the repository, got %q", i, f)
		}
		if strings.Contains(f, `\`) {
			return fmt.Errorf("steward.standards.files[%d] must not contain a backslash, got %q", i, f)
		}
		if strings.Contains(f, "..") {
			escaped := false
			for _, el := range strings.Split(f, "/") {
				if el == ".." {
					escaped = true
					break
				}
			}
			if escaped {
				return fmt.Errorf("steward.standards.files[%d] must not contain a '..' path element, got %q", i, f)
			}
		}
		if strings.IndexByte(f, 0) >= 0 || strings.Contains(f, "\n") {
			return fmt.Errorf("steward.standards.files[%d] must not contain a NUL or newline, got %q", i, f)
		}
		if len(f) > 200 {
			return fmt.Errorf("steward.standards.files[%d] must be at most 200 bytes, got %d", i, len(f))
		}
	}
	if len(c.Standards.Rules) > 20 {
		return fmt.Errorf("steward.standards.rules must list at most 20 entries, got %d", len(c.Standards.Rules))
	}
	for i, r := range c.Standards.Rules {
		if r == "" {
			return fmt.Errorf("steward.standards.rules[%d] must not be empty", i)
		}
		if len(r) > 400 {
			return fmt.Errorf("steward.standards.rules[%d] must be at most 400 bytes, got %d", i, len(r))
		}
		if strings.Contains(r, "\n") {
			return fmt.Errorf("steward.standards.rules[%d] must be a single line, got %q", i, r)
		}
	}
	if c.TwoAxis {
		standards, spec := c.PassLimits()
		total := c.Limits().BudgetUSD
		for _, pass := range []struct {
			key    string
			limits StewardPassLimits
		}{
			{"standards", standards},
			{"spec", spec},
		} {
			if pass.limits.BudgetUSD > total {
				return fmt.Errorf("steward.passes.%s.budgetUsd must be <= steward.budgetUsd (%g), got %v", pass.key, total, pass.limits.BudgetUSD)
			}
		}
		if standards.BudgetUSD+spec.BudgetUSD > total {
			return fmt.Errorf("steward.passes budgets (%g + %g) exceed steward.budgetUsd %g", standards.BudgetUSD, spec.BudgetUSD, total)
		}
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

// StewardPassLimits is one axis pass's effective budget, wall clock and seat
// profile (ADR-0054 §7): defaults filled in for zero values, minutes clamped
// to the review ceiling, profile names trimmed.
type StewardPassLimits struct {
	BudgetUSD  float64
	MaxMinutes int
	Profile    string
}

// PassLimits resolves the configured per-axis knobs to what each pass of a
// two-axis review runs under.
func (c StewardConfig) PassLimits() (standards, spec StewardPassLimits) {
	standards = passLimits(c.Passes.Standards, StewardPassStandardsDefaultBudgetUsd, StewardPassStandardsDefaultProfile)
	spec = passLimits(c.Passes.Spec, StewardPassSpecDefaultBudgetUsd, StewardPassSpecDefaultProfile)
	return standards, spec
}

func passLimits(p StewardPassConfig, defaultBudget float64, defaultProfile string) StewardPassLimits {
	budget := p.BudgetUsd
	if budget <= 0 {
		budget = defaultBudget
	}
	minutes := p.MaxMinutes
	if minutes <= 0 {
		minutes = StewardDefaultMaxMinutes
	}
	if minutes > StewardMaxMinutesCeiling {
		minutes = StewardMaxMinutesCeiling
	}
	profile := strings.TrimSpace(p.Profile)
	if profile == "" {
		profile = defaultProfile
	}
	return StewardPassLimits{BudgetUSD: budget, MaxMinutes: minutes, Profile: profile}
}

// hasDriveLetter reports whether path starts with a Windows drive prefix
// like C:.
func hasDriveLetter(path string) bool {
	if len(path) < 2 {
		return false
	}
	c := path[0]
	isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	return isLetter && path[1] == ':'
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
