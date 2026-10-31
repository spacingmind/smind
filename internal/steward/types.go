package steward

// Gate states (ADR-0047 §3.3). A gate the daemon cannot decide yet is
// "unknown"; a gate that does not apply to this task is "n/a".
const (
	GatePass    = "pass"
	GateFail    = "fail"
	GateUnknown = "unknown"
	GateNA      = "n/a"
)

// Verdict values (ADR-0047 §3.5). The seat may only lower these; the clamp
// picks the minimum of the seat's verdict and the gates.
const (
	VerdictAccepted = "ACCEPTED"
	VerdictChanges  = "CHANGES"
	VerdictBlocked  = "BLOCKED"
)

// GateIDs is the fixed order of all gates.
var GateIDs = []string{
	"ci",
	"validator",
	"frames_fresh",
	"no_binaries",
	"no_churn",
	"no_secrets",
	"not_stacked",
	"pr_hygiene",
	"scope",
	"mutations_committed",
	"peer_claims",
}

// Gate is one objective check computed by the daemon over Inputs. Detail is a
// human-readable reason, at most 300 bytes.
type Gate struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// Failing reports whether the gate failed.
func (g Gate) Failing() bool { return g.State == GateFail }

// TaskInfo identifies the reviewed task and the two ends of the diff.
type TaskInfo struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Branch  string `json:"branch"`
	HeadSHA string `json:"headSha"`
	BaseSHA string `json:"baseSha"`
}

// CommitInfo is one commit on the branch.
type CommitInfo struct {
	SHA            string `json:"sha"`
	Subject        string `json:"subject"`
	HasAttribution bool   `json:"hasAttribution"`
}

// StackInfo is whether the branch is stacked on another open PR.
type StackInfo struct {
	BaseReachableFromDevelop bool     `json:"baseReachableFromDevelop"`
	ForeignCommits           []string `json:"foreignCommits"`
}

// PRInfo is the pull request for the branch, if any.
type PRInfo struct {
	Number int    `json:"number"`
	Base   string `json:"base"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// CIInfo is the forge status at head (ADR-0039).
type CIInfo struct {
	State   string   `json:"state"` // success|failure|pending|none
	Failing []string `json:"failing"`
}

// MutationInfo is one contract mutation check and its result.
type MutationInfo struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Restored bool   `json:"restored"`
}

// ValidationInfo is the latest validator verdict (ADR-0045).
type ValidationInfo struct {
	ID             int64          `json:"id"`
	Status         string         `json:"status"`
	CommitSHA      string         `json:"commitSha"`
	MergeReady     bool           `json:"mergeReady"`
	ScopeViolation bool           `json:"scopeViolation"`
	Mutations      []MutationInfo `json:"mutations"`
}

// FrameInfo is one qaframes screenshot. Viewport is the width in px: 1280 or 390.
type FrameInfo struct {
	Path        string `json:"path"`
	SizeBytes   int64  `json:"sizeBytes"`
	CapturedSHA string `json:"capturedSha"`
	Viewport    int    `json:"viewport"`
	Theme       string `json:"theme"`
}

// HandoffInfo is a Peer's json handoff block (a claim, not evidence).
type HandoffInfo struct {
	Status      string   `json:"status"`
	NotVerified []string `json:"notVerified"`
	Notes       string   `json:"notes"`
}

// PolicyAuditCounts summarises the task's permission-policy decisions.
type PolicyAuditCounts struct {
	Denied    int `json:"denied"`
	Escalated int `json:"escalated"`
}

// TestRatio is production files changed vs test files changed.
type TestRatio struct {
	Prod int `json:"prod"`
	Test int `json:"test"`
}

// Inputs is everything the daemon collected; Build/Gates are pure over it.
type Inputs struct {
	Task        TaskInfo          `json:"task"`
	Numstat     string            `json:"numstat"` // raw `git diff --numstat base...head` text
	Commits     []CommitInfo      `json:"commits"`
	HasContract bool              `json:"hasContract"`
	WriteScope  []string          `json:"writeScope"` // contract writeScope.paths
	PlanFile    string            `json:"planFile"`
	Stacking    StackInfo         `json:"stacking"`
	PR          *PRInfo           `json:"pr"`
	CI          CIInfo            `json:"ci"`
	Validation  *ValidationInfo   `json:"validation"`
	Frames      []FrameInfo       `json:"frames"`
	FramesDir   string            `json:"framesDir"`
	Handoffs    []HandoffInfo     `json:"handoffs"`
	SecretHits  map[string]int    `json:"secretHits"` // label -> count, never values
	PolicyAudit PolicyAuditCounts `json:"policyAudit"`
}
