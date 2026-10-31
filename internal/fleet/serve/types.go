// Package serve is the callee side of the fleet read methods (ADR-0048 F2a): provenance checks, grant enforcement, rate limits and the audit writer around a Reader. It imports no wsapi code.
package serve

import (
	"errors"
	"time"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

// Refusal is a refused call; Code is the wire error code.
type Refusal struct{ Code, Msg string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Msg }

func refuse(code, msg string) *Refusal { return &Refusal{Code: code, Msg: msg} }

// NewRefusal builds a Refusal for a Reader or Source.
func NewRefusal(code, msg string) *Refusal { return refuse(code, msg) }

// ErrNotFound is how a Source or Reader reports absence (or a *Refusal).
var ErrNotFound = errors.New("not found")

// Reader answers the read methods. g is the caller's effective grant.
type Reader interface {
	Info(g registry.Grant) (wire.Info, error)
	Workspaces(g registry.Grant) (wire.WorkspaceList, error)
	TaskList(g registry.Grant, p wire.TaskListParams) (wire.TaskList, error)
	TaskGet(g registry.Grant, p wire.TaskGetParams) (wire.Task, error)
	RunStatus(g registry.Grant, p wire.RunStatusParams) (wire.RunStatus, error)
	Activity(g registry.Grant, p wire.TaskActivityParams) (wire.Activity, error)
}

// WorkspaceInfo is the workspace data a Source exposes.
type WorkspaceInfo struct {
	ID                            int64
	Title, RepoURL, DefaultBranch string
}

// Source is the data a Reader draws from. Absence is an error wrapping
// ErrNotFound.
type Source interface {
	ListWorkspaces() ([]WorkspaceInfo, error)
	// ListTasks returns the non-archived, non-system tasks of one workspace, ordered by id.
	ListTasks(workspaceID int64) ([]store.Task, error)
	GetTask(id int64) (store.Task, error)
	TaskOrigin(taskID int64) (host, trace string, err error)
	GetRun(runID string) (store.Run, error)
	LatestRun(taskID int64) (store.Run, bool, error)
	RunCostUSD(runID string) (*float64, error)
	// RunEvents returns events with seq > afterSeq ordered by seq, at most limit.
	RunEvents(runID string, afterSeq int64, limit int) ([]store.RunEvent, error)
}

// Authorizer is satisfied by *registry.Registry.
type Authorizer interface {
	Authorize(hostID, op string, workspaceID int64) (registry.Grant, error)
}

// AuditSink is satisfied by *store.Store.
type AuditSink interface {
	AppendFleetAudit(r store.FleetAuditRow) (int64, error)
}

// Options configure a Server. Zero RatePerSec/Burst mean 20/s and 40.
type Options struct {
	HostID     string
	Authorizer Authorizer
	Audit      AuditSink
	Reader     Reader
	Now        func() time.Time
	RatePerSec float64
	Burst      int
	Redact     func(string) string
}
