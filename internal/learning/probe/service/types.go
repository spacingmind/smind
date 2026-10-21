// Package probesvc is the learning-probe service (ADR-0037): it decides
// whether a pending skill proposal can be probed, estimates the cost, and
// runs the A / A2 / B arms in the attested sandbox, contrasts them and stores
// an advisory verdict. The verdict never changes a proposal's status, and a
// proposed skill reaches an agent only through projection.ProjectProbe for
// the B arm.
package probesvc

import (
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// Reason codes of an ineligible probe (the frozen wire contract).
const (
	ReasonNotPending          = "not_pending"
	ReasonProviderNotClaude   = "provider_not_claude_native"
	ReasonNoPrompt            = "no_prompt"
	ReasonNoBaseSHA           = "no_base_sha"
	ReasonDirtyCheckout       = "dirty_checkout"
	ReasonContinuationRun     = "continuation_run"
	ReasonSourceCostUnknown   = "source_cost_unknown"
	ReasonSandboxUnavailable  = "sandbox_unavailable"
	ReasonBudget              = "budget"
	ReasonQuota               = "quota"
	ReasonBusy                = "busy"
	ReasonWindows             = "windows"
	ReasonPlatformUnsupported = "platform_unsupported"
	ReasonLearningOff         = "learning_off"
	// ReasonCandidateInvalid: the proposal changes nothing, targets a skill
	// that is missing, disabled or archived, or was made against a stale
	// version of it, so there is no candidate skill set to probe.
	ReasonCandidateInvalid = "candidate_invalid"
)

// Reason is one stated cause of ineligibility; Message is one plain,
// user-facing sentence.
type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Estimate is the dry-run answer: eligibility, reasons and the money/time
// forecast. It is computed without a write or a model call.
type Estimate struct {
	Eligible        bool     `json:"eligible"`
	Reasons         []Reason `json:"reasons"`
	Quick           bool     `json:"quick"`
	Arms            int      `json:"arms"`
	EstUSD          float64  `json:"estUSD"`
	EstArmUSD       float64  `json:"estArmUSD"`
	ExpectedMinutes int      `json:"expectedMinutes"`
	PerProbeCapUSD  float64  `json:"perProbeCapUSD"`
	DailyCapUSD     float64  `json:"dailyCapUSD"`
	SpentTodayUSD   float64  `json:"spentTodayUSD"`
	QuotaPercent    *float64 `json:"quotaPercent"`
}

// ArmView is one planned arm as the UI shows it.
type ArmView struct {
	Role              string  `json:"role"`
	Status            string  `json:"status"`
	RunID             string  `json:"runId,omitempty"`
	StartedAt         string  `json:"startedAt,omitempty"`
	ElapsedMs         int64   `json:"elapsedMs"`
	CostUSD           float64 `json:"costUSD"`
	Turns             int     `json:"turns"`
	ToolCalls         int     `json:"toolCalls"`
	FailedToolCalls   int     `json:"failedToolCalls"`
	PermissionDenials int     `json:"permissionDenials"`
	LoadedSkill       *bool   `json:"loadedSkill,omitempty"`
	FailureRecurred   *bool   `json:"failureRecurred"`
	ProducedDiff      bool    `json:"producedDiff"`
	DiffBytes         int64   `json:"diffBytes"`
	Diff              string  `json:"diff,omitempty"`
	FinalMessage      string  `json:"finalMessage,omitempty"`
	Check             *Check  `json:"check,omitempty"`
}

// Check is the probeCheck outcome of one arm (present only when configured).
type Check struct {
	Passed bool   `json:"passed"`
	Output string `json:"output,omitempty"`
}

// MetricRow is one row of the contrast table.
type MetricRow struct {
	Key     string   `json:"key"`
	A       float64  `json:"a"`
	A2      *float64 `json:"a2"`
	B       float64  `json:"b"`
	DeltaAB float64  `json:"deltaAB"`
	Noise   *float64 `json:"noise"`
	Worse   bool     `json:"worse"`
}

// ContrastView is the wire contrast object of a done probe.
type ContrastView struct {
	Loaded     bool        `json:"loaded"`
	NoiseFloor bool        `json:"noiseFloor"`
	Metrics    []MetricRow `json:"metrics"`
}

// View is one probe as the UI shows it.
type View struct {
	ID            int64         `json:"id"`
	ProposalID    int64         `json:"proposalId"`
	WorkspaceID   int64         `json:"workspaceId"`
	Quick         bool          `json:"quick"`
	Status        string        `json:"status"`
	Verdict       *string       `json:"verdict"`
	VerdictReason string        `json:"verdictReason"`
	EstUSD        float64       `json:"estUSD"`
	CostUSD       float64       `json:"costUSD"`
	StartedAt     string        `json:"startedAt"`
	FinishedAt    string        `json:"finishedAt,omitempty"`
	ElapsedMs     int64         `json:"elapsedMs"`
	Arms          []ArmView     `json:"arms"`
	Contrast      *ContrastView `json:"contrast,omitempty"`
}

// Notifier receives the probe lifecycle. Calls carry ids, codes and short
// reasons only, never artifacts or secrets.
type Notifier interface {
	ProbeStarted(workspaceID, probeID, proposalID int64)
	ProbeArm(workspaceID, probeID, proposalID int64, role, status string)
	ProbeFinished(workspaceID, probeID, proposalID int64, status, verdict string)
	ProbeFailed(workspaceID, probeID, proposalID int64, reason string)
}

// SystemTasker yields the workspace's hidden system task and its single
// executor slot (satisfied by executor.Executor). Probe arms and reflect or
// curate runs share the system task's chat, so a probe holds the slot for
// its whole duration.
type SystemTasker interface {
	SystemTask(workspaceID int64) (store.Task, error)
	Acquire(workspaceID int64) (release func(), ok bool)
}

// Deps are the service's collaborators.
type Deps struct {
	Store  *store.Store
	WM     *workspace.Manager
	Runner *taskrunner.Runner
	Reg    *runs.Registry
	Exec   SystemTasker
	Config func() config.LearningConfig
	// Home is $SMIND_HOME. Directories: <Home>/probe/arms, <Home>/probe/launch,
	// <Home>/learning/probe-projections.
	Home string
	// Redact is the learning redactor; every stored artifact goes through it.
	Redact func(string) string
	Notify Notifier
	// QuotaPercent reads the CACHED account quota snapshot only. ok=false
	// means unknown, which does not skip a probe. There is deliberately no
	// provider usage function in Deps.
	QuotaPercent func() (percent float64, ok bool)
	// RealCLI resolves the claude binary path.
	RealCLI func() (string, error)
	// HumanSkillDirs lists the repo and user skill dirs the tripwire watches.
	HumanSkillDirs func(workspaceID int64) []string
	// Now is the clock; nil = time.Now.
	Now func() time.Time
	// GOOS overrides runtime.GOOS (tests); "" = runtime.GOOS.
	GOOS string
}
