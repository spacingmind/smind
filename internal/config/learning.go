package config

import (
	"fmt"
	"strings"
)

// LearningConfig is the learning subsystem's knob surface (ADR-0026): a
// global kill switch plus the caps and budgets the plan fixes as defaults.
type LearningConfig struct {
	// Enabled is the GLOBAL kill switch: false stops collection, jobs and
	// delivery instantly. Default true; per-workspace learning_mode still
	// defaults to off elsewhere.
	Enabled bool `yaml:"enabled"`
	// CadenceToolCalls is how many tool calls trigger a reflection. Default 50.
	CadenceToolCalls int `yaml:"cadenceToolCalls"`
	// TailFlushPending is the pending count at which an archived or idle
	// chat is flushed. Default 10.
	TailFlushPending int `yaml:"tailFlushPending"`
	// TailIdleMinutes is the idle time after which a tail flush may run.
	// Default 30.
	TailIdleMinutes int `yaml:"tailIdleMinutes"`
	// CooldownMinutes bounds reflections per workspace. Default 30.
	CooldownMinutes int `yaml:"cooldownMinutes"`
	// WindowKB caps the rendered, redacted evidence window. Default 120.
	WindowKB int `yaml:"windowKB"`
	// RecordKB caps one record in the window. Default 4.
	RecordKB int `yaml:"recordKB"`
	// InputKB caps a tool-call input summary. Default 1.
	InputKB int `yaml:"inputKB"`
	// ResultKB caps a tool result. Default 1.
	ResultKB int `yaml:"resultKB"`
	// FailureResultKB caps a failed tool call's result. Default 2.
	FailureResultKB int `yaml:"failureResultKB"`
	// DailyBudgetUSD is the per-workspace daily USD budget. Default 0.25.
	DailyBudgetUSD float64 `yaml:"dailyBudgetUSD"`
	// DailyBudgetTokens is the per-workspace daily token budget, counting
	// input, output, cache-read and cache-write tokens as the CLI reports
	// them. Default 150000.
	DailyBudgetTokens int `yaml:"dailyBudgetTokens"`
	// ProbationRuns is the offered-run count under which a skill is in
	// probation. Default 25.
	ProbationRuns int `yaml:"probationRuns"`
	// Capacity is the max mature active skills per workspace. Default 30.
	Capacity int `yaml:"capacity"`
	// CuratorEveryLandings enqueues a curator job after this many landed
	// changes. Default 5.
	CuratorEveryLandings int `yaml:"curatorEveryLandings"`
	// CuratorMinSkills is the minimum library size for a curator run.
	// Default 8.
	CuratorMinSkills int `yaml:"curatorMinSkills"`
	// CuratorMinIntervalDays bounds curator runs in time. Default 7.
	CuratorMinIntervalDays int `yaml:"curatorMinIntervalDays"`
	// PrefaceBudgetBytes bounds the ACP inline preface. Default 6144.
	PrefaceBudgetBytes int `yaml:"prefaceBudgetBytes"`
	// ClaudeIndexPreface adds an index preface for claude-native runs
	// (ADR-0026 D8, off by default).
	ClaudeIndexPreface bool `yaml:"claudeIndexPreface"`
	// Similarity sets the duplicate thresholds.
	Similarity LearningSimilarity `yaml:"similarity"`
	// ReflectorModel is the claude-native model the reflector runs on.
	// Default DefaultHaikuModel, a full model id and not the "haiku" alias:
	// a live run of claude 2.1.293 answered `--model haiku` with Sonnet in
	// 3 of 3 calls (about 30x the cost) while the full id was answered by
	// Haiku. A cheap model is recommended, the reflector only distils a
	// bounded window; set a newer id here when one ships. "" = the provider
	// default.
	ReflectorModel string `yaml:"reflectorModel"`
	// CuratorModel is the model the curator runs on. Default
	// DefaultHaikuModel (same reasoning as ReflectorModel); "" = the
	// provider default.
	CuratorModel string `yaml:"curatorModel"`
	// Probe holds the learning-probe knobs (ADR-0037).
	Probe LearningProbe `yaml:"probe"`
}

// LearningProbe holds the stage-0 prevalence scan knobs (ADR-0037).
type LearningProbe struct {
	// PrevalenceDays is how far back the scan looks. Default 30.
	PrevalenceDays int `yaml:"prevalenceDays"`
	// PrevalenceMaxEvents caps the run events one scan reads. Default 20000.
	PrevalenceMaxEvents int `yaml:"prevalenceMaxEvents"`
}

// LearningSimilarity holds the near-duplicate thresholds: at/above Duplicate
// the gate rejects as duplicate; at/above Possible the inbox flags a
// possible duplicate.
type LearningSimilarity struct {
	Duplicate float64 `yaml:"duplicate"` // default 0.85
	Possible  float64 `yaml:"possible"`  // default 0.60
}

// EnvLearning overrides learning.enabled at load time; SMIND_LEARNING=0
// (or false/off/no/n/f/disabled) forces the kill switch off.
const EnvLearning = "SMIND_LEARNING"

// DefaultHaikuModel is the full id of the current Haiku, the default
// reflector and curator model (ADR-0026 D4, section 3).
const DefaultHaikuModel = "claude-haiku-5-5"

// DefaultLearning returns the built-in learning defaults.
func DefaultLearning() LearningConfig {
	return LearningConfig{
		Enabled:                true,
		CadenceToolCalls:       50,
		TailFlushPending:       10,
		TailIdleMinutes:        30,
		CooldownMinutes:        30,
		WindowKB:               120,
		RecordKB:               4,
		InputKB:                1,
		ResultKB:               1,
		FailureResultKB:        2,
		DailyBudgetUSD:         0.25,
		DailyBudgetTokens:      150000,
		ProbationRuns:          25,
		Capacity:               30,
		CuratorEveryLandings:   5,
		CuratorMinSkills:       8,
		CuratorMinIntervalDays: 7,
		PrefaceBudgetBytes:     6144,
		ClaudeIndexPreface:     false,
		Similarity:             LearningSimilarity{Duplicate: 0.85, Possible: 0.60},
		ReflectorModel:         DefaultHaikuModel,
		CuratorModel:           DefaultHaikuModel,
		Probe:                  LearningProbe{PrevalenceDays: 30, PrevalenceMaxEvents: 20000},
	}
}

// Validate checks every learning knob.
func (c LearningConfig) Validate() error {
	for _, kv := range []struct {
		key string
		val int
	}{
		{"cadenceToolCalls", c.CadenceToolCalls},
		{"tailFlushPending", c.TailFlushPending},
		{"tailIdleMinutes", c.TailIdleMinutes},
		{"cooldownMinutes", c.CooldownMinutes},
		{"windowKB", c.WindowKB},
		{"recordKB", c.RecordKB},
		{"inputKB", c.InputKB},
		{"resultKB", c.ResultKB},
		{"failureResultKB", c.FailureResultKB},
		{"dailyBudgetTokens", c.DailyBudgetTokens},
		{"probationRuns", c.ProbationRuns},
		{"capacity", c.Capacity},
		{"curatorEveryLandings", c.CuratorEveryLandings},
		{"curatorMinSkills", c.CuratorMinSkills},
		{"curatorMinIntervalDays", c.CuratorMinIntervalDays},
		{"prefaceBudgetBytes", c.PrefaceBudgetBytes},
		{"probe.prevalenceDays", c.Probe.PrevalenceDays},
		{"probe.prevalenceMaxEvents", c.Probe.PrevalenceMaxEvents},
	} {
		if kv.val < 1 {
			return fmt.Errorf("learning.%s must be >= 1, got %d", kv.key, kv.val)
		}
	}
	for key, m := range map[string]string{"reflectorModel": c.ReflectorModel, "curatorModel": c.CuratorModel} {
		if err := validModel(m); err != nil {
			return fmt.Errorf("learning.%s: %w", key, err)
		}
	}
	if c.DailyBudgetUSD <= 0 {
		return fmt.Errorf("learning.dailyBudgetUSD must be > 0, got %v", c.DailyBudgetUSD)
	}
	if s := c.Similarity; !(s.Possible > 0 && s.Duplicate > s.Possible && s.Duplicate <= 1) {
		return fmt.Errorf("learning.similarity needs 0 < possible < duplicate <= 1, got duplicate %v, possible %v", s.Duplicate, s.Possible)
	}
	return nil
}

// validModel mirrors the shape rules of taskrunner.ValidateModelSyntax
// (config cannot import it): the value reaches `--model <name>` as argv.
func validModel(m string) error {
	if len(m) > 100 {
		return fmt.Errorf("model %q is longer than 100 characters", m)
	}
	if strings.HasPrefix(m, "-") {
		return fmt.Errorf("model %q must not begin with '-'", m)
	}
	for _, c := range m {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune("._:/@[]-", c):
		default:
			return fmt.Errorf("model %q: only letters, digits and . _ : / @ [ ] - are allowed", m)
		}
	}
	return nil
}
