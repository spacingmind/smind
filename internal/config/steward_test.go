package config

import (
	"fmt"
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

// TestStewardConfig_TwoAxisDefaultsOffAndReadsYAML pins ADR-0054's switch:
// absent means off (today's single review run), only a human edit turns it on.
func TestStewardConfig_TwoAxisDefaultsOffAndReadsYAML(t *testing.T) {
	var c StewardConfig
	if c.TwoAxis {
		t.Fatal("zero StewardConfig.TwoAxis = true, want off")
	}
	cfg, err := loadStewardYAML(t, "steward:\n  enabled: true\n")
	if err != nil || cfg.Steward.TwoAxis {
		t.Fatalf("no twoAxis key -> %v, %v; want off", cfg.Steward.TwoAxis, err)
	}
	cfg, err = loadStewardYAML(t, "steward:\n  twoAxis: true\n")
	if err != nil || !cfg.Steward.TwoAxis {
		t.Fatalf("steward.twoAxis: true -> %v, %v; want on", cfg.Steward.TwoAxis, err)
	}
}

// TestStewardConfig_PassLimitsDefaults pins the zero value of the two-axis
// pass knobs (ADR-0054 §7): 0.4 + 1.1 = the 1.5 total, 20 minutes each, the
// axis's own seat profile.
func TestStewardConfig_PassLimitsDefaults(t *testing.T) {
	var c StewardConfig
	if err := c.Validate(); err != nil {
		t.Fatalf("zero Validate() error = %v, want nil", err)
	}
	standards, spec := c.PassLimits()
	if want := (StewardPassLimits{BudgetUSD: 0.4, MaxMinutes: 20, Profile: "steward-standards"}); standards != want {
		t.Fatalf("standards PassLimits() = %+v, want %+v", standards, want)
	}
	if want := (StewardPassLimits{BudgetUSD: 1.1, MaxMinutes: 20, Profile: "steward"}); spec != want {
		t.Fatalf("spec PassLimits() = %+v, want %+v", spec, want)
	}
	if StewardPassStandardsDefaultBudgetUsd+StewardPassSpecDefaultBudgetUsd != StewardDefaultBudgetUsd {
		t.Fatal("pass budget defaults no longer sum to the total default")
	}
}

func TestStewardConfig_PassLimitsFromYAML(t *testing.T) {
	cfg, err := loadStewardYAML(t, `steward:
  twoAxis: true
  passes:
    standards:
      budgetUsd: 0.25
      maxMinutes: 10
      profile: "  steward-x  "
    spec:
      budgetUsd: 0.75
      maxMinutes: 30
      profile: my-spec
  standards:
    files: [AGENTS.md, docs/STANDARDS.md]
    rules:
      - "Every PR adds a test for new production code."
`)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	c := cfg.Steward
	standards, spec := c.PassLimits()
	if want := (StewardPassLimits{BudgetUSD: 0.25, MaxMinutes: 10, Profile: "steward-x"}); standards != want {
		t.Fatalf("standards PassLimits() = %+v, want %+v (profile trimmed)", standards, want)
	}
	if want := (StewardPassLimits{BudgetUSD: 0.75, MaxMinutes: 30, Profile: "my-spec"}); spec != want {
		t.Fatalf("spec PassLimits() = %+v, want %+v", spec, want)
	}
	if len(c.Standards.Files) != 2 || c.Standards.Files[0] != "AGENTS.md" || c.Standards.Files[1] != "docs/STANDARDS.md" {
		t.Fatalf("standards.files = %v", c.Standards.Files)
	}
	if len(c.Standards.Rules) != 1 || c.Standards.Rules[0] != "Every PR adds a test for new production code." {
		t.Fatalf("standards.rules = %v", c.Standards.Rules)
	}
}

// TestStewardConfig_PassBudgetsOverTotalRejectedOnlyWhenTwoAxis: the pass
// budgets bound the total only when the two-axis switch is on; off, the pass
// keys are inert and today's config loads.
func TestStewardConfig_PassBudgetsOverTotalRejectedOnlyWhenTwoAxis(t *testing.T) {
	const yaml = `  passes:
    standards:
      budgetUsd: 1
    spec:
      budgetUsd: 1
`
	if _, err := loadStewardYAML(t, "steward:\n  twoAxis: false\n"+yaml); err != nil {
		t.Fatalf("twoAxis false: Load() error = %v, want nil", err)
	}
	_, err := loadStewardYAML(t, "steward:\n  twoAxis: true\n"+yaml)
	if err == nil || !strings.Contains(err.Error(), "steward.passes budgets (1 + 1) exceed steward.budgetUsd 1.5") {
		t.Fatalf("twoAxis true: Load() = %v, want the sum-over-total error", err)
	}
	// One pass over the total on its own names its own key.
	if _, err := loadStewardYAML(t, "steward:\n  twoAxis: true\n  passes:\n    standards:\n      budgetUsd: 2\n"); err == nil ||
		!strings.Contains(err.Error(), "steward.passes.standards.budgetUsd must be <= steward.budgetUsd") {
		t.Fatalf("single pass over total: Load() = %v, want it to name steward.passes.standards.budgetUsd", err)
	}
}

func TestStewardConfig_PassBudgetsAtTotalAccepted(t *testing.T) {
	if _, err := loadStewardYAML(t, "steward:\n  twoAxis: true\n"); err != nil {
		t.Fatalf("defaults (0.4 + 1.1): Load() error = %v", err)
	}
	if _, err := loadStewardYAML(t, "steward:\n  twoAxis: true\n  budgetUsd: 1.5\n  passes:\n    standards:\n      budgetUsd: 0.75\n    spec:\n      budgetUsd: 0.75\n"); err != nil {
		t.Fatalf("0.75 + 0.75 = the total: Load() error = %v", err)
	}
	if _, err := loadStewardYAML(t, "steward:\n  twoAxis: true\n  budgetUsd: 2\n  passes:\n    standards:\n      budgetUsd: 0.4\n    spec:\n      budgetUsd: 1.6\n"); err != nil {
		t.Fatalf("pass budgets at a raised total: Load() error = %v", err)
	}
}

func TestStewardConfig_PassMinutesClampAndReject(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  StewardConfig
		want string
	}{
		{"negative standards minutes", StewardConfig{Passes: StewardPassesConfig{Standards: StewardPassConfig{MaxMinutes: -1}}}, "steward.passes.standards.maxMinutes"},
		{"spec minutes over the ceiling", StewardConfig{Passes: StewardPassesConfig{Spec: StewardPassConfig{MaxMinutes: 61}}}, "steward.passes.spec.maxMinutes"},
		{"negative standards budget", StewardConfig{Passes: StewardPassesConfig{Standards: StewardPassConfig{BudgetUsd: -0.1}}}, "steward.passes.standards.budgetUsd"},
		{"NaN spec budget", StewardConfig{Passes: StewardPassesConfig{Spec: StewardPassConfig{BudgetUsd: math.NaN()}}}, "steward.passes.spec.budgetUsd"},
		{"Inf standards budget", StewardConfig{Passes: StewardPassesConfig{Standards: StewardPassConfig{BudgetUsd: math.Inf(1)}}}, "steward.passes.standards.budgetUsd"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() = %v, want an error naming %s", err, tt.want)
			}
		})
	}
	if err := (StewardConfig{TwoAxis: true, Passes: StewardPassesConfig{Standards: StewardPassConfig{MaxMinutes: 60}, Spec: StewardPassConfig{MaxMinutes: 60}}}).Validate(); err != nil {
		t.Fatalf("60 minutes per pass rejected: %v", err)
	}
	for _, tt := range []struct {
		name string
		cfg  StewardPassConfig
		want int
	}{
		{"zero is the default", StewardPassConfig{}, 20},
		{"kept", StewardPassConfig{MaxMinutes: 25}, 25},
		{"clamped to the ceiling", StewardPassConfig{MaxMinutes: 99}, 60},
	} {
		t.Run(tt.name, func(t *testing.T) {
			standards, _ := StewardConfig{Passes: StewardPassesConfig{Standards: tt.cfg}}.PassLimits()
			if standards.MaxMinutes != tt.want {
				t.Fatalf("MaxMinutes = %d, want %d", standards.MaxMinutes, tt.want)
			}
		})
	}
}

// TestStewardConfig_StandardsFilesRejectsEscapes: the standards files are
// human-authored repo paths the daemon reads and copies into the Standards
// pass's scratch tree (ADR-0054 §3, D11), so anything that could name a file
// outside the repository is a load error.
func TestStewardConfig_StandardsFilesRejectsEscapes(t *testing.T) {
	bad := []string{
		"/etc/passwd",
		"../x",
		"a/../../x",
		`C:\x`,
		`a\b`,
		"",
		"a\nb",
		"a\x00b",
		strings.Repeat("a", 201),
	}
	for _, f := range bad {
		err := (StewardConfig{Standards: StewardStandardsConfig{Files: []string{f}}}).Validate()
		if err == nil || !strings.Contains(err.Error(), "steward.standards.files") {
			t.Errorf("files [%q]: Validate() = %v, want an error naming steward.standards.files", f, err)
		}
	}
	twentyOne := make([]string, 21)
	for i := range twentyOne {
		twentyOne[i] = fmt.Sprintf("f%d.md", i)
	}
	if err := (StewardConfig{Standards: StewardStandardsConfig{Files: twentyOne}}).Validate(); err == nil ||
		!strings.Contains(err.Error(), "steward.standards.files") {
		t.Errorf("21 files: Validate() = %v, want an error naming steward.standards.files", err)
	}
	if err := (StewardConfig{Standards: StewardStandardsConfig{Files: []string{"docs/STANDARDS.md", "AGENTS.md"}}}).Validate(); err != nil {
		t.Fatalf("repository-relative files rejected: %v", err)
	}
}

func TestStewardConfig_StandardsRulesCaps(t *testing.T) {
	for _, r := range []string{"", strings.Repeat("r", 401), "line one\nline two"} {
		err := (StewardConfig{Standards: StewardStandardsConfig{Rules: []string{r}}}).Validate()
		if err == nil || !strings.Contains(err.Error(), "steward.standards.rules") {
			t.Errorf("rules [%q]: Validate() = %v, want an error naming steward.standards.rules", r, err)
		}
	}
	twentyOneRules := make([]string, 21)
	for i := range twentyOneRules {
		twentyOneRules[i] = "rule"
	}
	if err := (StewardConfig{Standards: StewardStandardsConfig{Rules: twentyOneRules}}).Validate(); err == nil ||
		!strings.Contains(err.Error(), "steward.standards.rules") {
		t.Errorf("21 rules: Validate() = %v, want an error naming steward.standards.rules", err)
	}
	ok := make([]string, 20)
	for i := range ok {
		ok[i] = strings.Repeat("r", 400)
	}
	if err := (StewardConfig{Standards: StewardStandardsConfig{Rules: ok}}).Validate(); err != nil {
		t.Fatalf("20 rules of 400 bytes rejected: %v", err)
	}
}
