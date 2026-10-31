// Package wire holds the Go types of the fleet read methods (ADR-0048 F2a),
// shared by the callee (internal/fleet/serve) and the caller client so the two
// cannot drift. Every type is a closed struct: none has a path, env, token,
// account or credential field.
package wire

import (
	"time"

	"github.com/spacingmind/smind/internal/runs/failure"
)

// Method names of the fleet read methods.
const (
	MethodInfo          = "fleet.info"
	MethodWorkspaceList = "fleet.workspace.list"
	MethodTaskList      = "fleet.task.list"
	MethodTaskGet       = "fleet.task.get"
	MethodRunStatus     = "fleet.run.status"
	MethodTaskActivity  = "fleet.task.activity"
)

// ReadMethods lists every read method.
var ReadMethods = []string{
	MethodInfo,
	MethodWorkspaceList,
	MethodTaskList,
	MethodTaskGet,
	MethodRunStatus,
	MethodTaskActivity,
}

// The wire error code of a refused call is one of these.
const (
	CodePrincipalMismatch = "principal_mismatch"
	CodeCycle             = "cycle"
	CodeHopLimit          = "hop_limit"
	CodeStale             = "stale"
	CodeReplay            = "replay"
	CodeNotGranted        = "not_granted"
	CodeRateLimited       = "rate_limited"
	CodeNotFound          = "not_found"
	CodeInvalid           = "invalid_params"
)

// Limits of the read methods.
const (
	MaxReplayWindow   = 10 * time.Minute
	MaxClockSkew      = 120 * time.Second
	MaxEventTextBytes = 1024
	DefaultPageSize   = 50
	MaxPageSize       = 200
)

// Prov is the provenance carried by every call. TS is unix seconds.
type Prov struct {
	Origin     string   `json:"origin"`
	Trace      string   `json:"trace"`
	Hops       []string `json:"hops"`
	OriginTask string   `json:"originTask,omitempty"`
	ReqID      string   `json:"reqId"`
	TS         int64    `json:"ts"`
}

// InfoParams are the params of fleet.info.
type InfoParams struct {
	Prov Prov `json:"prov"`
}

// WorkspaceListParams are the params of fleet.workspace.list.
type WorkspaceListParams struct {
	Prov Prov `json:"prov"`
}

// TaskListParams are the params of fleet.task.list.
type TaskListParams struct {
	Prov        Prov  `json:"prov"`
	WorkspaceID int64 `json:"workspaceId,omitempty"`
	Cursor      int64 `json:"cursor,omitempty"`
	Limit       int   `json:"limit,omitempty"`
}

// TaskGetParams are the params of fleet.task.get.
type TaskGetParams struct {
	Prov Prov  `json:"prov"`
	ID   int64 `json:"id"`
}

// RunStatusParams are the params of fleet.run.status.
type RunStatusParams struct {
	Prov   Prov   `json:"prov"`
	RunID  string `json:"runId,omitempty"`
	TaskID int64  `json:"taskId,omitempty"`
}

// TaskActivityParams are the params of fleet.task.activity.
type TaskActivityParams struct {
	Prov   Prov   `json:"prov"`
	TaskID int64  `json:"taskId"`
	RunID  string `json:"runId,omitempty"`
	Cursor int64  `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// Limits are the grant limits a host reports.
type Limits struct {
	Ops            []string `json:"ops"`
	MaxLiveAgents  int      `json:"maxLiveAgents"`
	MaxWallSeconds int      `json:"maxWallSeconds"`
	MaxCostUSD     float64  `json:"maxCostUsd"`
}

// Info is the result of fleet.info.
type Info struct {
	HostID  string   `json:"hostId"`
	Version string   `json:"version"`
	FleetV  int      `json:"fleetV"`
	Caps    []string `json:"caps"`
	OS      string   `json:"os"`
	Arch    string   `json:"arch"`
	Limits  Limits   `json:"limits"`
}

// Workspace is one workspace as seen by a peer.
type Workspace struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	RepoURL       string `json:"repoUrl"`
	DefaultBranch string `json:"defaultBranch"`
}

// WorkspaceList is the result of fleet.workspace.list.
type WorkspaceList struct {
	Workspaces []Workspace `json:"workspaces"`
}

// Origin names the host and trace a task came from.
type Origin struct {
	Host  string `json:"host"`
	Trace string `json:"trace"`
}

// Task is one task as seen by a peer.
type Task struct {
	ID          int64     `json:"id"`
	WorkspaceID int64     `json:"workspaceId"`
	Title       string    `json:"title"`
	Branch      string    `json:"branch"`
	State       string    `json:"state"`
	ParentID    *int64    `json:"parentId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Origin      *Origin   `json:"origin,omitempty"`
}

// TaskList is the result of fleet.task.list. NextCursor 0 means no more.
type TaskList struct {
	Tasks      []Task `json:"tasks"`
	NextCursor int64  `json:"nextCursor,omitempty"`
}

// RunStatus is the result of fleet.run.status.
type RunStatus struct {
	RunID       string     `json:"runId"`
	TaskID      int64      `json:"taskId"`
	State       string     `json:"state"`
	Provider    string     `json:"provider"`
	Model       string     `json:"model,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	StopReason  string     `json:"stopReason,omitempty"`
	FailureKind string     `json:"failureKind,omitempty"`
	// Action is the failure\'s next step; a newer peer may send a kind this daemon
	// does not know, which round-trips untouched.
	Action  *failure.Action `json:"action,omitempty"`
	CostUSD *float64        `json:"costUsd"`
	// WaitingPermission is true while a permission request sits in the
	// callee's inbox; the caller sees the flag but can never answer it (AC19).
	WaitingPermission bool `json:"waitingPermission,omitempty"`
}

// Event is one activity event of a run.
type Event struct {
	Seq        int64     `json:"seq"`
	TS         time.Time `json:"ts"`
	Type       string    `json:"type"`
	Text       string    `json:"text,omitempty"`
	Truncated  bool      `json:"truncated,omitempty"`
	ToolName   string    `json:"toolName,omitempty"`
	ToolStatus string    `json:"toolStatus,omitempty"`
	StopReason string    `json:"stopReason,omitempty"`
}

// Activity is the result of fleet.task.activity.
type Activity struct {
	RunID      string  `json:"runId"`
	Events     []Event `json:"events"`
	NextCursor int64   `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
}
