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
// SpaceID, WorktreePath, Branch, and ArchivedAt are nullable: a task may not
// belong to a space, and WorktreePath/Branch stay nil until a follow-up
// worktree-creation step materializes them.
type Task struct {
	ID           int64
	WorkspaceID  int64
	SpaceID      *int64
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
