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
type Run struct {
	ID         string
	TaskID     int64
	Provider   string
	Prompt     string
	Status     string
	StartedAt  time.Time
	FinishedAt *time.Time
	StopReason string
	ErrMsg     string
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
