package wsapi

import (
	"crypto/subtle"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// Handler returns the http.Handler for the /ws endpoint. A browser's
// WebSocket client can't set an Authorization header on the upgrade
// request, so the token travels as a query parameter instead
// (GET /ws?token=...) and is checked here, before the upgrade completes --
// a wrong or missing token gets a plain 401 HTTP response rather than a WS
// close frame, since no WebSocket connection exists yet to close.
//
// Each accepted connection gets its own conn (see conn.go) serving requests
// until the client disconnects; conn.serve blocks for the connection's
// whole lifetime, so this handler doesn't return until then.
func Handler(wm *workspace.Manager, runner *taskrunner.Runner, token string) http.Handler {
	hs := methodHandlers(wm, runner)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}
