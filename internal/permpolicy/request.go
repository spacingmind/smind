// Package permpolicy is the deterministic permission-policy engine of
// ADR-0047 §2: a pure Decide(Request, Policy) called on the permission hot
// path, no LLM, no network. Anything it cannot fully judge is `ask`, never
// `allow` (default-deny).
package permpolicy

// Action is a policy verdict. `ask` is the outcome for every request no rule
// covers and for anything the engine cannot fully parse or resolve: the human
// keeps the decision (ADR-0017).
type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
	ActionAsk   Action = "ask"
)

// Request is one permission request as the daemon sees it: a tool name, the
// raw command for Bash-like tools, and the paths it names, all
// agent-controlled and therefore untrusted. Truncated is set by the provider
// adapter when it clipped the command, which makes the request unjudgeable.
type Request struct {
	Tool    string
	Command string
	Paths   []string

	Cwd        string
	Worktree   string
	TaskBranch string
	Seat       string
	Workspace  string

	Truncated bool

	Home      string
	SmindHome string

	RunID     string
	RequestID string

	TaskID      int64
	WorkspaceID int64

	// path flavour; nil = the host's. Tests set it so the Windows rules run on any OS.
	plat *platform
}

// pl is the path flavour this request is judged under.
func (r Request) pl() *platform {
	if r.plat != nil {
		return r.plat
	}
	return hostPlatform()
}

// SegmentDecision is the per-simple-command explanation `policy.test` shows
// and the audit keeps: what the segment was and which rule or floor rule
// decided it ("" = nothing matched).
type SegmentDecision struct {
	Index   int
	Text    string
	Action  Action
	RuleID  string
	FloorID string
	Reasons []string
}

// Decision is the engine's answer. Why is the matched rule's `why` (or the
// floor's reason) for the card; Normalized is the redacted, heredoc-elided
// command; Paths are the resolved paths the decision was based on.
type Decision struct {
	Action     Action
	RuleID     string
	FloorID    string
	Why        string
	PolicySha  string
	Reasons    []string
	Normalized string
	Paths      []string
	Segments   []SegmentDecision
}
