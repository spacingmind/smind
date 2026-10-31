package config

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSteward_PolicyDisabledByDefaultAndReadFromYAML proves the policy
// engine's switch is off unless a human writes it on (ADR-0047 §2): with no
// steward block the permission path is unchanged, and -- like every other
// section, whose loader is not strict -- an unknown key under steward.policy
// is ignored rather than fatal.
func TestSteward_PolicyDisabledByDefaultAndReadFromYAML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Steward.Policy.Enabled {
		t.Fatal("no steward block: policy enabled, want off by default")
	}

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("steward:\n  policy:\n    enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Steward.Policy.Enabled {
		t.Fatal("steward.policy.enabled: true not read")
	}

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("steward:\n  policy:\n    enabled: true\n    bogus: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with an unknown key under steward.policy: %v", err)
	}
	if !cfg.Steward.Policy.Enabled {
		t.Fatal("an unknown key under steward.policy dropped the enabled value")
	}
}

// loadStewardYAML writes yaml as the home config.yaml and loads it, the same
// path a human edit takes.
func loadStewardYAML(t *testing.T, yaml string) (Config, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load()
}

// TestStewardConfig_DefaultsOff pins the zero value: nothing is enabled, no
// workspace is auto-reviewed, and Limits() resolves to the documented
// defaults (1.5 USD / 20 min / 6 per hour / 5 per task).
func TestStewardConfig_DefaultsOff(t *testing.T) {
	var c StewardConfig
	if err := c.Validate(); err != nil {
		t.Fatalf("zero Validate() error = %v, want nil", err)
	}
	if c.Enabled || c.Annotate || len(c.AutoReview) != 0 {
		t.Fatalf("zero value = enabled %v annotate %v autoReview %v, want all off", c.Enabled, c.Annotate, c.AutoReview)
	}
	got := c.Limits()
	want := StewardLimits{BudgetUSD: StewardDefaultBudgetUsd, MaxMinutes: StewardDefaultMaxMinutes,
		ReviewsPerHour: StewardDefaultReviewsPerHour, ReviewsPerTask: StewardDefaultReviewsPerTask}
	if got != want {
		t.Fatalf("Limits() = %+v, want %+v", got, want)
	}
	if want != (StewardLimits{BudgetUSD: 1.5, MaxMinutes: 20, ReviewsPerHour: 6, ReviewsPerTask: 5}) {
		t.Fatalf("defaults drifted: %+v", want)
	}
}

func TestStewardConfig_ValidateRejectsOverCeilingAndNegative(t *testing.T) {
	tests := []struct {
		name string
		cfg  StewardConfig
		want string
	}{
		{"budget over the ceiling", StewardConfig{BudgetUsd: 6}, "steward.budgetUsd"},
		{"budget over a lower maxBudgetUsd", StewardConfig{MaxBudgetUsd: 3, BudgetUsd: 4}, "steward.budgetUsd"},
		{"maxBudgetUsd over the ceiling", StewardConfig{MaxBudgetUsd: 5.5}, "steward.maxBudgetUsd"},
		{"negative budget", StewardConfig{BudgetUsd: -0.5}, "steward.budgetUsd"},
		{"negative maxBudgetUsd", StewardConfig{MaxBudgetUsd: -1}, "steward.maxBudgetUsd"},
		{"NaN budget", StewardConfig{BudgetUsd: math.NaN()}, "steward.budgetUsd"},
		{"Inf maxBudgetUsd", StewardConfig{MaxBudgetUsd: math.Inf(1)}, "steward.maxBudgetUsd"},
		{"maxMinutes over the ceiling", StewardConfig{MaxMinutes: 61}, "steward.maxMinutes"},
		{"negative maxMinutes", StewardConfig{MaxMinutes: -1}, "steward.maxMinutes"},
		{"maxReviewsPerHour over the ceiling", StewardConfig{MaxReviewsPerHour: 25}, "steward.maxReviewsPerHour"},
		{"negative maxReviewsPerHour", StewardConfig{MaxReviewsPerHour: -2}, "steward.maxReviewsPerHour"},
		{"negative maxReviewsPerTask", StewardConfig{MaxReviewsPerTask: -1}, "steward.maxReviewsPerTask"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want an error naming %s", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want it to name %s", err, tt.want)
			}
		})
	}
	if err := (StewardConfig{MaxBudgetUsd: 5, BudgetUsd: 5, MaxMinutes: 60, MaxReviewsPerHour: 24, MaxReviewsPerTask: 100}).Validate(); err != nil {
		t.Fatalf("at-the-ceiling values rejected: %v", err)
	}
}

func TestStewardConfig_LimitsClampToCeilings(t *testing.T) {
	tests := []struct {
		name string
		cfg  StewardConfig
		want StewardLimits
	}{
		{"defaults for zero", StewardConfig{}, StewardLimits{BudgetUSD: 1.5, MaxMinutes: 20, ReviewsPerHour: 6, ReviewsPerTask: 5}},
		{"budget clamped to the ceiling", StewardConfig{BudgetUsd: 10}, StewardLimits{BudgetUSD: 5, MaxMinutes: 20, ReviewsPerHour: 6, ReviewsPerTask: 5}},
		{"budget clamped to a lower maxBudgetUsd", StewardConfig{MaxBudgetUsd: 3, BudgetUsd: 4}, StewardLimits{BudgetUSD: 3, MaxMinutes: 20, ReviewsPerHour: 6, ReviewsPerTask: 5}},
		{"minutes clamped", StewardConfig{MaxMinutes: 99}, StewardLimits{BudgetUSD: 1.5, MaxMinutes: 60, ReviewsPerHour: 6, ReviewsPerTask: 5}},
		{"perHour clamped", StewardConfig{MaxReviewsPerHour: 100}, StewardLimits{BudgetUSD: 1.5, MaxMinutes: 20, ReviewsPerHour: 24, ReviewsPerTask: 5}},
		{"explicit values kept", StewardConfig{BudgetUsd: 2, MaxMinutes: 30, MaxReviewsPerHour: 12, MaxReviewsPerTask: 9},
			StewardLimits{BudgetUSD: 2, MaxMinutes: 30, ReviewsPerHour: 12, ReviewsPerTask: 9}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.Limits(); got != tt.want {
				t.Fatalf("Limits() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStewardConfig_AutoReviewForExactMatch(t *testing.T) {
	c := StewardConfig{AutoReview: []string{"alpha", "beta"}}
	for title, want := range map[string]bool{"alpha": true, "beta": true, "Alpha": false, "alpha ": false, "": false, "gamma": false} {
		if got := c.AutoReviewFor(title); got != want {
			t.Errorf("AutoReviewFor(%q) = %v, want %v", title, got, want)
		}
	}
}

func TestStewardConfig_YAMLRoundTrip(t *testing.T) {
	cfg, err := loadStewardYAML(t, `steward:
  policy:
    enabled: true
  enabled: true
  autoReview: [alpha, beta]
  annotate: true
  maxBudgetUsd: 4
  budgetUsd: 2
  maxMinutes: 30
  maxReviewsPerHour: 12
  maxReviewsPerTask: 9
`)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	c := cfg.Steward
	if !c.Policy.Enabled || !c.Enabled || !c.Annotate {
		t.Fatalf("switches = policy %v enabled %v annotate %v, want all on", c.Policy.Enabled, c.Enabled, c.Annotate)
	}
	if len(c.AutoReview) != 2 || c.AutoReview[0] != "alpha" || c.AutoReview[1] != "beta" {
		t.Fatalf("autoReview = %v, want [alpha beta]", c.AutoReview)
	}
	if c.MaxBudgetUsd != 4 || c.BudgetUsd != 2 || c.MaxMinutes != 30 || c.MaxReviewsPerHour != 12 || c.MaxReviewsPerTask != 9 {
		t.Fatalf("knobs = %+v", c)
	}
	if got := c.Limits(); got != (StewardLimits{BudgetUSD: 2, MaxMinutes: 30, ReviewsPerHour: 12, ReviewsPerTask: 9}) {
		t.Fatalf("Limits() = %+v", got)
	}
	if !c.AutoReviewFor("beta") || c.AutoReviewFor("gamma") {
		t.Fatal("AutoReviewFor disagrees with the parsed autoReview list")
	}

	if _, err := loadStewardYAML(t, "steward:\n  budgetUsd: 99\n"); err == nil || !strings.Contains(err.Error(), "steward.budgetUsd") {
		t.Fatalf("over-ceiling config Load() = %v, want an error naming steward.budgetUsd", err)
	}
}
