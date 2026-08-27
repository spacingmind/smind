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
