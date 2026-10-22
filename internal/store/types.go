package store

import "time"

// Account is a stored provider credential (e.g. an Anthropic or OpenAI login).
type Account struct {
	ID             int64
	Provider       string
	Label          string
	CredentialType string
	CredentialData string
	// Models is a JSON array of path.Match globs ("" = no list; stored as
	// NULL).
	Models string
	// Identity is the hashed vendor account identity (ADR-0033 D6; "" = none,
	// stored as NULL). Never put on the wire or in logs.
	Identity  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RoutingDecision records which account a session was routed to, for session
// affinity within Policy's TTL.
type RoutingDecision struct {
	ID         int64
	SessionKey string
	AccountID  int64
	Policy     string
	DecidedAt  time.Time
	ExpiresAt  time.Time
}

// QuotaSnapshot is a cached usage reading for an account.
type QuotaSnapshot struct {
	ID        int64
	AccountID int64
	UsageData string
	PolledAt  time.Time
	ExpiresAt time.Time
	// Kind is QuotaKindFetched or QuotaKindObserved; "" means fetched on write.
	Kind string
}

// Quota snapshot kinds (quota_snapshots.kind).
const (
	QuotaKindFetched  = "fetched"
	QuotaKindObserved = "observed"
)

// Workspace is a local filesystem root (typically a git repo checkout) with
// a routing policy and a pool of candidate accounts for requests scoped to
// it.
type Workspace struct {
	ID            int64
	Path          string
	Title         string
	RoutingPolicy string
	// SetupScript/TeardownScript are the human-authored worktree
	// setup/teardown shell scripts (ADR-0024); "" = none.
	// The script text is never serialised (ADR-0024: only workspace.get
	// returns it, via its own result type); the Has* flags are.
	SetupScript       string `json:"-"`
	TeardownScript    string `json:"-"`
	HasSetupScript    bool
	HasTeardownScript bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
	// LearningMode is "off" or "propose" (ADR-0026); "" on write = "off".
	LearningMode string `json:"learningMode"`
	// DefaultBase is the user's default-base-branch override (ADR-0039);
	// "" = auto-detect.
	DefaultBase string
}

// Space is an optional grouping layer within a workspace, carrying its own
// space-scoped environment data.
type Space struct {
	ID          int64
	WorkspaceID int64
	Title       string
	EnvData     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Task is a unit of work within a workspace, optionally scoped to a space.
// SpaceID, ParentTaskID, WorktreePath, Branch, and ArchivedAt are nullable:
// a task may not belong to a space, ParentTaskID is nil for a root task
// (otherwise it points at a task in the same workspace -- CreateTask
// validates this), and WorktreePath/Branch stay nil until a follow-up
// worktree-creation step materializes them.
type Task struct {
	ID           int64
	WorkspaceID  int64
	SpaceID      *int64
	ParentTaskID *int64
	Title        string
	Status       string
	WorktreePath *string
	Branch       *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ArchivedAt   *time.Time
	// SetupStatus is "" (no setup script), "running", "ok" or "failed"
	// (ADR-0024); SetupError/SetupLog hold the failure reason and the
	// output tail; Port is the allocated SMIND_PORT, nil when none.
	SetupStatus string
	SetupError  string
	SetupLog    string
	Port        *int64
	// Kind is "task" or "system" (ADR-0026: hidden tasks that own learning
	// runs); "" on write = "task". List queries return only non-system rows.
	Kind string
	// BaseRef is the branch the task is based on and its PR targets (a
	// branch name, not origin/<name>); BaseSha is the commit the task
	// branch forked from, the diff base. Either is "" when unknown.
	// BranchOwned is true when CreateTask created the branch (ADR-0039).
	BaseRef     string
	BaseSha     string
	BranchOwned bool
}

// Run is a persisted record of one internal/runs.Registry Run: a
// task.prompt/run.start turn, with a lifetime independent of the process
// that drove it (see internal/runs.Registry.CloseAll's doc comment). StopReason
// and ErrMsg mirror runs.RunStatus's fields of the same name. FinishedAt is
// nil while the run is (as far as this row's writer knew) still going.
// PermissionMode/AutoAccept mirror taskrunner.PermissionSettings (ADR-0019:
// the provider's own permission mode id, plus ACP's approve-every-prompt
// toggle) -- kept as plain values here rather than an import of
// internal/taskrunner, consistent with Provider also being a plain string
// rather than internal/taskrunner.Provider. The legacy approval_policy
// column is never read; the one-time backfill clears it (ADR-0019
// resolved decision 8), and new rows store it empty.
//
// ChatID is the chats row this run belongs to (docs/decisions/
// 0016-multiple-chats-per-task.md). The column itself is nullable (see
// schema.sql) purely so the migration that introduces it can add it to an
// existing runs table before backfilling every row -- every run any caller
// ever observes through this struct (CreateRun onward, and every row after
// the migration's backfill has run) always has one, so this field is a
// plain int64, not a pointer; scanRun treats a NULL here as a store bug, not
// a valid state to represent.
type Run struct {
	ID             string
	TaskID         int64
	ChatID         int64
	Provider       string
	Prompt         string
	Status         string
	StartedAt      time.Time
	FinishedAt     *time.Time
	StopReason     string
	ErrMsg         string
	PermissionMode string
	AutoAccept     bool
	// Model is the model the run was *asked* to use (ADR-0025); "" (stored
	// as NULL) means the provider's default. Distinct from run_usage.model,
	// which records what the agent reported using.
	Model string
	// BaseSHA/HeadSHA/Dirty are the run's git provenance (O1): the
	// worktree's HEAD at start and at the terminal state, and its dirty
	// state at the latest successful capture. nil = unknown (git failed,
	// no worktree, or a pre-O1 row); CreateRun never sets them -- they are
	// written by UpdateRunProvenance after the run exists.
	BaseSHA *string
	HeadSHA *string
	Dirty   *bool
	// Purpose is "user", "reflect" or "curate" (ADR-0026); "" on write =
	// "user". Non-user runs are hidden from ListRecentRuns and from the
	// main usage summaries.
	Purpose string
	// Origin is who authored the run's prompt ("human", "orchestrator",
	// "agent"); "" = unknown (pre-existing rows, plain Start callers).
	Origin string
	// Interrupt is true when the prompt was delivered as a mid-run
	// interrupt (a priority-1 queue item).
	Interrupt bool
}

// RunEvent is one persisted internal/runs.Event, in the order it was
// recorded for its run (Seq is strictly increasing per RunID, starting at
// 0). EventData is the JSON encoding of the event (see
// internal/runs.EncodeEvent) -- following this codebase's existing
// convention of storing structured payloads as JSON-in-TEXT rather than a
// normalized column per Event field (accounts.credential_data,
// quota_snapshots.usage_data, spaces.env_data).
type RunEvent struct {
	ID        int64
	RunID     string
	Seq       int64
	EventData string
	CreatedAt time.Time
}

// TerminalSession is a persisted record of one internal/terminal.Registry
// session: a task's PTY-backed shell, with a lifetime independent of the
// process that spawned it (see internal/terminal.Registry.CloseAll's doc
// comment). Scrollback is the session's scrollback buffer as of its last
// checkpoint (or its final state, once Status is "closed"/"interrupted") --
// unlike RunEvent.EventData, this is the raw buffer, not JSON, since a
// terminal session's history is a single opaque byte stream rather than a
// sequence of discrete structured events. ClosedAt is nil while the session
// is (as far as this row's writer knew) still running.
type TerminalSession struct {
	ID         string
	TaskID     int64
	Status     string
	StartedAt  time.Time
	ClosedAt   *time.Time
	Scrollback string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// AgentProfile is a named, daemon-global bundle of per-run settings
// (docs/decisions/0014-agent-profiles.md): a provider plus the
// PermissionMode/AutoAccept/ThinkingLevel values a composer would otherwise
// ask a user to pick every time. Provider/PermissionMode/ThinkingLevel are
// plain strings, not internal/taskrunner types, for the same reason Run's
// fields of the same name are -- internal/store does not import internal/
// taskrunner (see Run's doc comment above). PermissionMode/ThinkingLevel
// may be "" (unset): the composer's own default applies in that case, same
// as an omitted task.prompt field does today. Not scoped to any workspace
// -- see the ADR's Scope section.
type AgentProfile struct {
	ID             int64
	Name           string
	Provider       string
	PermissionMode string
	AutoAccept     bool
	ThinkingLevel  string
	// Model is the profile's requested model (ADR-0025); "" (NULL) is the
	// provider's default.
	Model     string
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// McpServer is a named, daemon-global MCP server definition (docs/
// decisions/0018-agent-mcp-servers.md) an agent run may be handed: a
// stdio subprocess (Playwright MCP) or an http/sse endpoint, optionally
// restricted to specific workspaces via workspace_mcp_servers (no
// restriction row = available to every workspace). Name is unique and is
// the identity every downstream protocol keys a server by (ACP's
// McpServerStdio.name, Claude's mcpServers map key), unlike
// AgentProfile.Name, which is only a label. Args/Env are JSON-encoded
// (array of strings, object of string->string) following the store's
// existing JSON-in-TEXT convention; Env/Headers are secret-bearing and
// must be redacted on every read path that leaves the daemon (ADR-0018
// "Secrets in env/headers"). Transport is a plain string ("stdio"|
// "http"|"sse") validated by internal/mcpservers, not here, for the same
// reason AgentProfile.Provider is a plain string.
type McpServer struct {
	ID        int64
	Name      string
	Transport string
	Command   string
	Args      string
	Env       string
	URL       string
	Headers   string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Chat is a conversation thread within a task (docs/decisions/
// 0016-multiple-chats-per-task.md): the unit a provider binds to and an
// agent session persists against, sharing its parent task's git worktree
// with any sibling chats. Provider is nil until the chat's first run binds
// it (immutable afterward); AgentSession is nil until a run persists a
// session handle to it -- P1 only stores it (SetChatAgentSession/its getter
// below), P2 is what actually populates it with a real
// {provider,sessionId,nativeHandle,metadata} handle. ArchivedAt is nil for
// an active chat; unlike Task, there is no cascading worktree/branch to
// clean up on archive, since a chat owns no git state of its own.
type Chat struct {
	ID           int64
	TaskID       int64
	Title        string
	Provider     *string
	AgentSession *string
	CreatedAt    time.Time
	ArchivedAt   *time.Time
	// LearnWatermark/LearnPending are the learning collector's bookkeeping
	// (ADR-0026): last reflected run seq and runs since.
	LearnWatermark int64
	LearnPending   int64
}

// RequestLog is one proxied-request trace row (docs/plans/active/
// orchestration-and-metering.md, "M1"): metadata about a single request
// through internal/server's /v1/messages or /v1/chat/completions handler,
// never its prompt or response content, and never a credential value.
//
// AccountID is nil when routing itself failed before an account was
// chosen (Outcome "route_error" with no candidate). Model and Stream are
// nil when the request body exceeded the parse cap before either field
// could be read (see proxy.go's requestBodyParseCap), not when the field
// was legitimately absent -- an absent "stream" in a real request means
// non-streaming, which is recorded as a false, not a nil. UpstreamStatus
// is nil whenever the proxy never got a real upstream response (route
// failure, or a network error dialing upstream); Status is always set, to
// whatever smind itself returned the caller.
//
// The five token fields are nil ("unknown") rather than 0 whenever usage
// couldn't be extracted -- over the response parse cap, a provider that
// omits a field, or a route/upstream failure before any usage was ever
// seen. This is a real distinction: 0 reasoning tokens on a
// non-reasoning model is a fact; nil is "we don't know."
type RequestLog struct {
	ID               int64
	StartedAt        time.Time
	Provider         string
	AccountID        *int64
	SessionKey       string
	Model            *string
	Stream           *bool
	Status           int
	UpstreamStatus   *int
	Outcome          string
	Error            *string
	TTFBMs           *int64
	DurationMs       int64
	InputTokens      *int64
	OutputTokens     *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
	ReasoningTokens  *int64
}

// RequestLogOutcome enumerates RequestLog.Outcome's valid values.
const (
	RequestLogOutcomeOK              = "ok"
	RequestLogOutcomeUpstreamError   = "upstream_error"
	RequestLogOutcomeRouteError      = "route_error"
	RequestLogOutcomeAborted         = "aborted"
	RequestLogOutcomeClientCancelled = "client_cancelled"
)

// UsageSummary is one grouped row of usage.summary: total requests and
// summed token counts for one key (an account id, a model name, or a day,
// depending on the groupBy the caller asked for). Summed token fields
// only include rows where that field was non-nil -- a run of rows with
// entirely unknown tokens sums to 0 for that field, same as SQL SUM over
// no non-NULL rows.
type UsageSummary struct {
	Key              string
	Count            int64
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	// Unreported lists the token fields for which no row in the group had a
	// non-NULL value (nil when every token field had at least one reported
	// value). A reported 0 is not unreported.
	Unreported []string
}

// RunUsage is one run's usage row (ADR-0020): what the agent itself
// reported for the run, never estimated. Every token/cost/context field is
// nil ("unknown") rather than 0 when the backend didn't report it.
// WorkspaceID is filled from the run's task at upsert time. SessionSnapshot
// is the ACP session-cumulative counters as JSON (nil elsewhere). No prompt
// text and no credential ever lands here.
type RunUsage struct {
	RunID             string
	WorkspaceID       *int64
	TaskID            int64
	ChatID            *int64
	Provider          string
	Model             *string
	InputTokens       *int64
	CachedInputTokens *int64
	CacheWriteTokens  *int64
	OutputTokens      *int64
	ReasoningTokens   *int64
	CostUSD           *float64
	ContextUsed       *int64
	ContextSize       *int64
	SessionSnapshot   *string
	Source            string
	UpdatedAt         time.Time
}

// RunUsageSummary is one grouped row of usage.summary's runs scope: the
// number of runs and the summed usage for one key (a workspace, task, chat,
// run, provider, or model, depending on groupBy). Like UsageSummary, sums
// only include non-NULL fields.
type RunUsageSummary struct {
	Key              string
	Count            int64
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	// CostUSD is nil when no run in the group reported a cost (unknown, not 0).
	CostUSD *float64
	// Unreported lists the token fields for which no run in the group had a
	// non-NULL value (nil when every token field had at least one reported
	// value). A reported 0 is not unreported.
	Unreported []string
}

// Learned skill statuses (learned_skills.status). ADR-0026: proposals
// (create/patch/bundle) all live in learned_skill_proposals; this table
// holds only approved skills.
const (
	SkillActive   = "active"
	SkillDisabled = "disabled"
	SkillArchived = "archived"
)

// LearningModeOff / LearningModePropose are the workspace learning modes
// (workspaces.learning_mode).
const (
	LearningModeOff     = "off"
	LearningModePropose = "propose"
)

// LearnedSkill is an approved self-learning skill (ADR-0026). Evidence is a
// JSON string the store does not interpret.
type LearnedSkill struct {
	ID                  int64
	WorkspaceID         int64
	Name                string
	Category            string
	Description         string
	Body                string
	Status              string
	Pinned              bool
	Source              string
	CreatedByRun        string
	Evidence            string
	PossibleDuplicateOf string
	Similarity          float64
	Version             int
	OfferedRuns         int
	LoadedRuns          int
	Loads               int
	LoadsViaSkill       int
	LoadsViaRead        int
	Patched             int
	ReusedAfterPatch    int
	Measured            bool
	LastOfferedAt       *time.Time
	LastLoadedAt        *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// LearnedSkillVersion is one historical version of a learned skill's
// description/body.
type LearnedSkillVersion struct {
	SkillID     int64
	Version     int
	Description string
	Body        string
	Actor       string
	CreatedAt   time.Time
}

// LearnedLedgerEntry is one append-only row of the skill audit ledger.
// SkillID is nil for actions not tied to an approved skill (e.g. a rejected
// proposal). Evidence is a JSON string the store does not interpret.
type LearnedLedgerEntry struct {
	ID           int64
	WorkspaceID  int64
	SkillID      *int64
	SkillName    string
	Action       string
	Reason       string
	Evidence     string
	AbsorbedInto string
	RunID        string
	Actor        string
	CreatedAt    time.Time
}

// LearnedProposal is a pending create/patch/bundle proposal awaiting human
// approval. Intents/Evidence are JSON strings the store does not interpret.
type LearnedProposal struct {
	ID                  int64
	WorkspaceID         int64
	Kind                string // create|patch|bundle
	Source              string // reflector|curator
	Name                string
	TargetSkillID       *int64
	JobID               *int64
	Intents             string
	Evidence            string
	PossibleDuplicateOf string
	Similarity          float64
	Status              string // pending|approved|rejected
	Note                string
	CreatedAt           time.Time
	DecidedAt           *time.Time
}

// LearningJob is one reflection/curation job over a chat event window.
// Verdicts is a JSON string the store does not interpret.
type LearningJob struct {
	ID                int64
	WorkspaceID       int64
	ChatID            *int64
	Kind              string // reflect|curate
	TriggerKind       string // cadence|explicit|tail|schedule
	WindowFromEventID int64
	WindowToEventID   int64
	Status            string // queued|running|landed|empty|failed|skipped
	SkipReason        string
	Verdicts          string
	RunID             string
	CostUSD           *float64
	Tokens            *int64
	Error             string
	CreatedAt         time.Time
	FinishedAt        *time.Time
}
