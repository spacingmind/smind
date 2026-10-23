package wsapi

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newCloseTestAPI(t *testing.T) (*API, *httptest.Server) {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	return api, srv
}

func expectGoingAway(t *testing.T, ws *websocket.Conn) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := ws.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) {
		t.Fatalf("ReadMessage() error = %v, want *websocket.CloseError", err)
	}
	if ce.Code != websocket.CloseGoingAway {
		t.Fatalf("close code = %d, want %d", ce.Code, websocket.CloseGoingAway)
	}
}

func TestCloseConnectionsSendsGoingAway(t *testing.T) {
	api, srv := newCloseTestAPI(t)
	ws := dialWS(t, srv, "tok")
	// Let the handler register the connection before closing.
	deadline := time.Now().Add(2 * time.Second)
	for {
		api.connsMu.Lock()
		n := len(api.conns)
		api.connsMu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connection never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	api.CloseConnections()
	expectGoingAway(t, ws)
}

func TestCloseConnectionsRejectsLaterDials(t *testing.T) {
	api, srv := newCloseTestAPI(t)
	api.CloseConnections()
	ws := dialWS(t, srv, "tok")
	expectGoingAway(t, ws)
}

func TestCloseConnectionsTwice(t *testing.T) {
	api, srv := newCloseTestAPI(t)
	_ = dialWS(t, srv, "tok")
	api.CloseConnections()
	api.CloseConnections()
}
