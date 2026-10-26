// Package automation is the signal -> task engine of ADR-0044: human-authored
// rules, a daemon-owned engine that applies the rails (cap, dedupe,
// auto-pause, single flight, budget, audit) to every signal, and the
// per-automation memory. It knows nothing about GitHub or cron beyond the
// Source interface and the built-in cron schedule.
package automation

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Signal kinds (ADR-0044 section 2).
const (
	KindCron        = "cron"
	KindWorkflowRun = "github.workflow_run"
	KindManual      = "manual"

	// OriginAutomation is the run/queue origin of every task an automation
	// starts. It is not "agent" (the tree budget would refuse the first run)
	// and not "human" (it never gains human authority).
	OriginAutomation = "automation"

	maxPromptTemplate  = 8 << 10
	maxNameLen         = 64
	defaultMaxRuns     = 2
	ceilingMaxRuns     = 12
	defaultPauseAfter  = 3
	defaultDedupeCI    = 1440
	defaultBudgetUSD   = 3.0
	defaultRunMinutes  = 45
	ceilingRunMinutes  = 240
	defaultMemoryBytes = 16 << 10
)

// Signal is what a Source hands the engine: a kind, a fingerprint (ADR-0044
// section 6.2) and a payload of scalar fields, all of it untrusted data.
type Signal struct {
	Kind        string
	Fingerprint string
	Payload     map[string]any
	// Catchup marks a cron fire that was late but inside the grace window.
	Catchup bool
}

// Template is the task an automation starts.
type Template struct {
	ProfileID      int64   `json:"profileId"`
	Model          string  `json:"model"`
	Mode           string  `json:"mode"`
	AutoAccept     bool    `json:"autoAccept,omitempty"`
	TitleTemplate  string  `json:"titleTemplate"`
	PromptTemplate string  `json:"promptTemplate"`
	BudgetUSD      float64 `json:"budgetUsd"`
	MaxRunMinutes  int     `json:"maxRunMinutes"`
	BaseRef        string  `json:"baseRef,omitempty"`
}

// Rails are the per-automation limits (ADR-0044 section 6).
type Rails struct {
	MaxRunsPerHour      int  `json:"maxRunsPerHour"`
	DedupeWindowMinutes *int `json:"dedupeWindowMinutes,omitempty"`
	PauseAfterFailures  int  `json:"pauseAfterFailures"`
	MemoryEnabled       bool `json:"memoryEnabled"`
	MemoryMaxBytes      int  `json:"memoryMaxBytes"`
	// PollMinutes is the poll interval of a polled source (default 5, floor 2).
	PollMinutes int `json:"pollMinutes,omitempty"`
}

// SignalSpec is the signal definition. Cron uses Cron; github.workflow_run
// uses Workflow, Branch and Conclusions.
type SignalSpec struct {
	Kind        string   `json:"kind"`
	Cron        string   `json:"cron,omitempty"`
	Workflow    string   `json:"workflow,omitempty"`
	Branch      string   `json:"branch,omitempty"`
	Conclusions []string `json:"conclusions,omitempty"`
}

// Def is the human-authored part of an automation, as the RPCs carry it.
type Def struct {
	Name        string     `json:"name"`
	WorkspaceID int64      `json:"workspaceId"`
	Signal      SignalSpec `json:"signal"`
	Template    Template   `json:"template"`
	Rails       Rails      `json:"rails"`
}

// Limits are the daemon-wide ceilings and opt-ins from config.yaml.
type Limits struct {
	MaxBudgetUSD         float64
	AllowUnattendedModes bool
}

var (
	nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]*$`)
	// placeholderRe matches {{signal.<field>}}; only the fields in
	// scalarFields may be referenced (ids, sha, branch: never free text).
	placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.]+)\s*\}\}`)
	scalarFields  = map[string]bool{
		"signal.repo": true, "signal.workflow": true, "signal.workflowId": true, "signal.branch": true,
		"signal.headSha": true, "signal.runId": true, "signal.runUrl": true, "signal.conclusion": true,
		"signal.event": true, "signal.scheduledAt": true, "signal.firedAt": true, "signal.catchup": true,
	}
	unattendedModes = map[string]bool{"full-access": true, "bypassPermissions": true, "bypass": true, "dontAsk": true}
)

// WithDefaults fills the documented defaults into d (never overriding a value
// the human set).
func (d Def) WithDefaults() Def {
	if d.Rails.MaxRunsPerHour == 0 {
		d.Rails.MaxRunsPerHour = defaultMaxRuns
	}
	if d.Rails.PauseAfterFailures == 0 {
		d.Rails.PauseAfterFailures = defaultPauseAfter
	}
	if d.Rails.MemoryMaxBytes == 0 {
		d.Rails.MemoryMaxBytes = defaultMemoryBytes
	}
	if d.Rails.DedupeWindowMinutes == nil {
		v := 0
		if d.Signal.Kind != KindCron {
			v = defaultDedupeCI
		}
		d.Rails.DedupeWindowMinutes = &v
	}
	if d.Template.BudgetUSD == 0 {
		d.Template.BudgetUSD = defaultBudgetUSD
	}
	if d.Template.MaxRunMinutes == 0 {
		d.Template.MaxRunMinutes = defaultRunMinutes
	}
	if d.Signal.Kind == KindWorkflowRun {
		if d.Signal.Branch == "" {
			d.Signal.Branch = "develop"
		}
		if len(d.Signal.Conclusions) == 0 {
			d.Signal.Conclusions = []string{"failure"}
		}
	}
	return d
}

// Validate checks a definition (after WithDefaults) against ADR-0044 sections
// 5.5 and 6. lim carries the config ceilings. It does not check that the
// workspace or profile exist: the caller does, and the engine repeats that at
// fire time.
func (d Def) Validate(lim Limits) error {
	if len(d.Name) == 0 || len(d.Name) > maxNameLen || !nameRe.MatchString(d.Name) {
		return fmt.Errorf("name must be 1-%d chars of letters, digits, space, '.', '_' or '-'", maxNameLen)
	}
	if d.WorkspaceID <= 0 {
		return fmt.Errorf("workspaceId is required")
	}
	switch d.Signal.Kind {
	case KindCron:
		if _, err := ParseCron(d.Signal.Cron); err != nil {
			return err
		}
	case KindWorkflowRun:
		if strings.TrimSpace(d.Signal.Workflow) == "" || len(d.Signal.Workflow) > 128 || hasControl(d.Signal.Workflow) || strings.HasPrefix(d.Signal.Workflow, "-") {
			return fmt.Errorf("signal.workflow is required (the workflow's name or file, 128 chars max)")
		}
		if !validBranch(d.Signal.Branch) {
			return fmt.Errorf("signal.branch %q is not a valid branch name", d.Signal.Branch)
		}
		for _, c := range d.Signal.Conclusions {
			switch c {
			case "failure", "cancelled", "timed_out":
			default:
				return fmt.Errorf("signal.conclusions: %q is not one of failure, cancelled, timed_out", c)
			}
		}
	default:
		return fmt.Errorf("signal.kind %q is not supported (cron, github.workflow_run)", d.Signal.Kind)
	}
	t := d.Template
	if t.ProfileID <= 0 {
		return fmt.Errorf("template.profileId is required")
	}
	if strings.TrimSpace(t.Model) == "" {
		return fmt.Errorf("template.model is required (an explicit model, ADR-0025)")
	}
	if strings.TrimSpace(t.TitleTemplate) == "" || len(t.TitleTemplate) > 200 {
		return fmt.Errorf("template.titleTemplate is required (200 chars max)")
	}
	if strings.TrimSpace(t.PromptTemplate) == "" || len(t.PromptTemplate) > maxPromptTemplate {
		return fmt.Errorf("template.promptTemplate is required (%d bytes max)", maxPromptTemplate)
	}
	if !utf8.ValidString(t.PromptTemplate) {
		return fmt.Errorf("template.promptTemplate must be UTF-8")
	}
	for _, field := range []string{t.TitleTemplate, t.PromptTemplate} {
		for _, m := range placeholderRe.FindAllStringSubmatch(field, -1) {
			if !scalarFields[m[1]] {
				return fmt.Errorf("placeholder {{%s}} is not allowed: only scalar signal fields (repo, workflow, branch, headSha, runId, runUrl, conclusion, event, scheduledAt, firedAt, catchup)", m[1])
			}
		}
	}
	if (t.AutoAccept || unattendedModes[t.Mode]) && !lim.AllowUnattendedModes {
		return fmt.Errorf("template.mode %q / autoAccept is an unattended mode: set automations.allowUnattendedModes: true in config.yaml to allow it", t.Mode)
	}
	if t.BudgetUSD <= 0 || t.BudgetUSD > lim.MaxBudgetUSD {
		return fmt.Errorf("template.budgetUsd must be > 0 and <= %.2f (automations.maxBudgetUsd)", lim.MaxBudgetUSD)
	}
	if t.MaxRunMinutes < 1 || t.MaxRunMinutes > ceilingRunMinutes {
		return fmt.Errorf("template.maxRunMinutes must be 1-%d", ceilingRunMinutes)
	}
	r := d.Rails
	if r.MaxRunsPerHour < 1 || r.MaxRunsPerHour > ceilingMaxRuns {
		return fmt.Errorf("rails.maxRunsPerHour must be 1-%d", ceilingMaxRuns)
	}
	if r.PauseAfterFailures < 1 || r.PauseAfterFailures > 20 {
		return fmt.Errorf("rails.pauseAfterFailures must be 1-20")
	}
	if r.DedupeWindowMinutes != nil && (*r.DedupeWindowMinutes < 0 || *r.DedupeWindowMinutes > 30*24*60) {
		return fmt.Errorf("rails.dedupeWindowMinutes must be 0-%d", 30*24*60)
	}
	if r.MemoryMaxBytes < 256 || r.MemoryMaxBytes > 64<<10 {
		return fmt.Errorf("rails.memoryMaxBytes must be 256-%d", 64<<10)
	}
	if r.PollMinutes != 0 && (r.PollMinutes < 2 || r.PollMinutes > 24*60) {
		return fmt.Errorf("rails.pollMinutes must be 2-%d", 24*60)
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// validBranch is a conservative git ref-format check: no leading '-' or '/',
// no "..", no control or glob characters, no trailing ".lock", '.' or '/'.
func validBranch(b string) bool {
	if b == "" || len(b) > 200 || b[0] == '-' || b[0] == '/' || strings.HasSuffix(b, "/") || strings.HasSuffix(b, ".lock") ||
		strings.Contains(b, "..") || strings.Contains(b, "//") || strings.Contains(b, "@{") || strings.HasSuffix(b, ".") {
		return false
	}
	for _, r := range b {
		if r <= 0x20 || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	return true
}

// DedupeWindow is the effective dedupe window.
func (r Rails) DedupeWindow() time.Duration {
	if r.DedupeWindowMinutes == nil {
		return 0
	}
	return time.Duration(*r.DedupeWindowMinutes) * time.Minute
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // plain structs of scalars only
	}
	return string(b)
}
