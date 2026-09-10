package wsapi

import (
	"crypto/subtle"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/terminal"
	"github.com/spacingmind/smind/internal/workspace"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// API bundles the /ws http.Handler with the registries New constructs
// internally, for the one caller (cmdServe, via internal/server.Server)
// that needs to reach a registry directly rather than only through the
// wire protocol -- specifically, Runs.CloseAll and Terminals.CloseAll on
// graceful daemon shutdown, so no agent subprocess or PTY-backed shell
// outlives the daemon itself (each Registry's own CloseAll doc comment
// covers what "outlives" is verified to mean). Every other caller
// (existing tests, wsclient) only needs the http.Handler and should keep
// using Handler below.
type API struct {
	Handler   http.Handler
	Runs      *runs.Registry
	Terminals *terminal.Registry
}

// New builds the full /ws API: one shared *runs.Registry and one shared
// *terminal.Registry, both backed by db (see runs.New's and terminal.New's
// doc comments on the reconciliation/rehydration each performs
// synchronously here, so New itself can fail if that startup work does),
// for every connection the returned Handler accepts (see Handler's doc
// comment for why a Run or terminal session's lifetime must be independent
// of any one connection), plus the http.Handler itself.
func New(wm *workspace.Manager, acctReg *accounts.Registry, runner *taskrunner.Runner, db *store.Store, token string) (*API, error) {
	reg, err := runs.New(db)
	if err != nil {
		return nil, fmt.Errorf("wsapi: new: %w", err)
	}
	treg, err := terminal.New(db)
	if err != nil {
		return nil, fmt.Errorf("wsapi: new: %w", err)
	}
	bus := newEventBus()
	wm.SetTaskNotifier(func(taskID int64, status string) {
		bus.Publish(Event{Topic: TopicTaskStatus, Payload: taskStatusPayload{TaskID: taskID, Status: status}})
	})
	reg.SetNotifier(busRunNotifier{bus: bus})

	coord := accounts.NewDefaultLoginCoordinator(acctReg)
	hs := methodHandlers(wm, acctReg, runner, reg, treg, coord)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		c := newConn(ws, hs)
		c.eventsSub = newSubscriber()
		c.eventsBus = bus
		c.serve(r.Context())
	})
	return &API{Handler: handler, Runs: reg, Terminals: treg}, nil
}

// busRunNotifier adapts the shared event bus to runs.Notifier, translating
// each Registry lifecycle notification into its ADR-0005 wire payload.
type busRunNotifier struct {
	bus *eventBus
}

func (b busRunNotifier) NotifyRunStatus(s runs.RunStatus) {
	b.bus.Publish(Event{Topic: TopicRunStatus, Payload: runStatusPayload{
		RunID: s.ID, TaskID: s.TaskID, Status: string(s.Status),
		StopReason: s.StopReason, Err: s.Err,
	}})
}

func (b busRunNotifier) NotifyPermissionPending(runID string, taskID int64, requestID, summary string, options []taskrunner.PermissionOption) {
	b.bus.Publish(Event{Topic: TopicPermissionPending, Payload: permissionPendingPayload{
		RunID: runID, TaskID: taskID, RequestID: requestID, Summary: summary,
		Options: toPermissionOptionParams(options),
	}})
}

// Handler returns the http.Handler for the /ws endpoint alone -- a thin
// wrapper over New for callers that don't need direct registry access.
// See New's doc comment for what it wires up; see API's doc comment for
// why a separate constructor exists at all.
//
// Each accepted connection gets its own conn (see conn.go) serving requests
// until the client disconnects; conn.serve blocks for the connection's
// whole lifetime, so this handler doesn't return until then.
//
// All connections Handler accepts share one *runs.Registry, since a Run
// started on one connection (via task.prompt or a future run.start) must
// be reachable from any other connection's run.list/run.attach/run.logs/
// run.stop -- that's the whole point of tracking it server-side instead of
// inline in the request that started it. The same reasoning applies to the
// shared *terminal.Registry for terminal.create/attach/write/resize/close/
// list.
func Handler(wm *workspace.Manager, acctReg *accounts.Registry, runner *taskrunner.Runner, db *store.Store, token string) (http.Handler, error) {
	api, err := New(wm, acctReg, runner, db, token)
	if err != nil {
		return nil, err
	}
	return api.Handler, nil
}
