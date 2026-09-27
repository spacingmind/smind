package store

import "time"

// Account is a stored provider credential (e.g. an Anthropic or OpenAI login).
type Account struct {
	ID             int64
	Provider       string
	Label          string
	CredentialType string
	CredentialData string
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
}

// Workspace is a local filesystem root (typically a git repo checkout) with
// a routing policy and a pool of candidate accounts for requests scoped to
// it.
type Workspace struct {
	ID            int64
	Path          string
	Title         string
	RoutingPolicy string
	CreatedAt     time.Time
	UpdatedAt     time.Time
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
}

// Run is a persisted record of one internal/runs.Registry Run: a
// task.prompt/run.start turn, with a lifetime independent of the process
// that drove it (see internal/runs.Registry.CloseAll's doc comment). StopReason
// and ErrMsg mirror runs.RunStatus's fields of the same name. FinishedAt is
// nil while the run is (as far as this row's writer knew) still going.
// ApprovalPolicy mirrors taskrunner.ApprovalPolicy's underlying string (see
// that type for the values) -- kept as a plain string here rather than an
// import of internal/taskrunner, consistent with Provider also being a
// plain string rather than internal/taskrunner.Provider.
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
	ApprovalPolicy string
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
// ApprovalPolicy/ThinkingLevel values a composer would otherwise ask a
// user to pick every time. Provider/ApprovalPolicy/ThinkingLevel are plain
// strings, not internal/taskrunner types, for the same reason Run's fields
// of the same name are -- internal/store does not import internal/
// taskrunner (see Run's doc comment above). ApprovalPolicy/ThinkingLevel
// may be "" (unset): the composer's own default applies in that case, same
// as an omitted task.prompt field does today. Not scoped to any workspace
// -- see the ADR's Scope section.
type AgentProfile struct {
	ID             int64
	Name           string
	Provider       string
	ApprovalPolicy string
	ThinkingLevel  string
	Notes          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
}
