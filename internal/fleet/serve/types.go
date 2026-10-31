// Package serve is the callee side of the fleet read (ADR-0048 F2a) and write (F3a) methods: provenance checks, grant enforcement, rate limits, budgets, the wall sweeper and the audit writer around a Reader and a Backend. It imports no wsapi code.
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

// Ledger is the store subset the write path needs; *store.Store satisfies it.
type Ledger interface {
	ListLiveOriginRuns(originHost string) ([]store.OriginRun, error)
	OriginCostSince(originHost string, since time.Time) (float64, error)
	GetFleetIdem(hostID, key string) (store.FleetIdem, bool, error)
	PutFleetIdem(i store.FleetIdem) (store.FleetIdem, bool, error)
	MarkRunWallLimit(runID string) error
}

var _ Ledger = (*store.Store)(nil)

// TaskInfo is a task the Backend created.
type TaskInfo struct {
	ID     int64
	Branch string
}

// Backend performs the write effects on this host with ITS OWN accounts and
// settings; the wsapi adapter implements it. The wire types cannot carry the
// caller's credentials or agent configuration, so the backend is the only
// place a provider or setting is ever resolved.
type Backend interface {
	ProviderConfigured(provider string) bool
	// CreateTask fetches branch or baseRef from the workspace's own origin
	// and creates the task worktree; a missing remote ref is a *Refusal
	// with wire.CodeBranchNotFound.
	CreateTask(workspaceID int64, title, branch, baseRef string) (TaskInfo, error)
	SetTaskOrigin(taskID int64, originHost, originTrace string) error
	// StartRun starts a run with the remote source, WhenBusy reject, the
	// callee's asking permission mode; a busy chat is a *Refusal with code
	// "run_busy".
	StartRun(taskID int64, provider, model, prompt, originHost string) (runID string, err error)
	// StopRun stops the run and waits for it to end.
	StopRun(runID string) error
	WaitingPermission(runID string) bool
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
	// Backend performs the write effects of the write methods; the wsapi
	// adapter implements it. Nil until the adapter is wired.
	Backend Backend
	// Ledger is the store the budget check and the wall sweeper read.
	Ledger Ledger
	// Tasks is the owner and latest-run lookups of the write path.
	Tasks TaskLookup
	// PollInterval is how often a fleet.run.wait long poll re-checks the
	// run; zero means 500 ms, applied by the wait handler.
	PollInterval time.Duration
	// WallEvery is how often the wall-limit sweeper sweeps; zero means 10 s.
	WallEvery time.Duration
}
