package wsapi

import (
	"crypto/subtle"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/runs"
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
// *terminal.Registry for every connection the returned Handler accepts
// (see Handler's doc comment for why a Run or terminal session's
// lifetime must be independent of any one connection), plus the
// http.Handler itself.
func New(wm *workspace.Manager, runner *taskrunner.Runner, token string) *API {
	reg := runs.New()
	treg := terminal.New()
	hs := methodHandlers(wm, runner, reg, treg)
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

		newConn(ws, hs).serve(r.Context())
	})
	return &API{Handler: handler, Runs: reg, Terminals: treg}
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
func Handler(wm *workspace.Manager, runner *taskrunner.Runner, token string) http.Handler {
	return New(wm, runner, token).Handler
}
