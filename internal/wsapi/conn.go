package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

// handlerFunc handles one client request's params, returning either a
// result to marshal into the response or an error. ctx is cancelled when
// the connection closes or a matching task.cancel arrives for this
// request's id. A handler that wants to stream events before its terminal
// response (e.g. task.prompt) does so via rc.Emit.
type handlerFunc func(ctx context.Context, rc *requestContext, params json.RawMessage) (any, error)

// requestContext is the per-request handle a handlerFunc gets for emitting
// events correlated to its own request id.
type requestContext struct {
	conn *conn
	id   string
}

// Emit sends a server -> client event carrying params, correlated to this
// request's id.
func (rc *requestContext) Emit(event string, params any) {
	rc.conn.writeEnvelope(envelope{ID: rc.id, Event: event, Params: marshalOrNull(params)})
}

// inflightRequest tracks one in-progress request: cancel lets task.cancel
// abort it, terminated (guarded by conn.mu, same as the map itself) records
// whether its terminal response has already been sent.
type inflightRequest struct {
	cancel     context.CancelFunc
	terminated bool
}

// conn is the RPC engine for one WebSocket connection: it serializes writes
// (gorilla connections aren't safe for concurrent writers), dispatches each
// inbound request to its handler in its own goroutine so one connection can
// have many requests in flight at once, and guarantees exactly one terminal
// response is ever sent per request id.
//
// task.cancel racing against a handler's own natural completion is the one
// concurrency risk this package shares with internal/acp's conn (a single
// connection carrying concurrent request/response/event traffic), though
// the shape of the fix differs: acp's issue was a channel close racing a
// send; here it's "don't emit two terminal messages for the same id".
// cancel only ever calls the stored context.CancelFunc (safe to call any
// number of times, from any goroutine); the terminal write itself always
// goes through terminate, which -- guarded by the same mutex that protects
// the inflight map -- allows exactly one of sendResult/sendError to
// actually write for a given id, so a cancel landing concurrently with a
// handler's own response can never produce a duplicate or out-of-order
// message.
type conn struct {
	ws       *websocket.Conn
	handlers map[string]handlerFunc

	writeMu sync.Mutex

	mu       sync.Mutex
	inflight map[string]*inflightRequest
}

func newConn(ws *websocket.Conn, handlers map[string]handlerFunc) *conn {
	return &conn{
		ws:       ws,
		handlers: handlers,
		inflight: make(map[string]*inflightRequest),
	}
}

// serve reads and dispatches messages from the connection until it closes
// or errors, then returns. Every in-flight request's context is a child of
// ctx, so returning here (via the deferred cancel) aborts every
// still-running handler -- this is how a client disconnect stops an
// in-progress task.prompt turn.
func (c *conn) serve(ctx context.Context) {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		c.handleEnvelope(connCtx, env)
	}
}

func (c *conn) handleEnvelope(connCtx context.Context, env envelope) {
	switch {
	case env.Method == "task.cancel" && env.ID == "":
		c.handleCancel(env.Params)
	case env.Method != "" && env.ID != "":
		go c.dispatch(connCtx, env)
	}
}

func (c *conn) handleCancel(params json.RawMessage) {
	var p cancelParams
	if err := json.Unmarshal(params, &p); err != nil || p.ID == "" {
		return
	}
	c.mu.Lock()
	req, ok := c.inflight[p.ID]
	c.mu.Unlock()
	if ok {
		req.cancel()
	}
}

func (c *conn) dispatch(connCtx context.Context, env envelope) {
	h, ok := c.handlers[env.Method]
	if !ok {
		c.sendError(env.ID, fmt.Errorf("method not found: %s", env.Method))
		return
	}

	reqCtx, cancel := context.WithCancel(connCtx)
	defer cancel()

	c.mu.Lock()
	c.inflight[env.ID] = &inflightRequest{cancel: cancel}
	c.mu.Unlock()

	rc := &requestContext{conn: c, id: env.ID}
	result, err := h(reqCtx, rc, env.Params)
	if err != nil {
		c.sendError(env.ID, err)
		return
	}
	c.sendResult(env.ID, result)
}

// terminate reports whether id's terminal response has not yet been sent,
// and if so atomically marks it sent and removes it from the inflight map
// -- see conn's doc comment for why this is the mechanism that keeps
// sendResult/sendError from ever writing twice for the same id.
func (c *conn) terminate(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	req, ok := c.inflight[id]
	if !ok || req.terminated {
		return false
	}
	req.terminated = true
	delete(c.inflight, id)
	return true
}

func (c *conn) sendResult(id string, result any) {
	if !c.terminate(id) {
		return
	}
	c.writeEnvelope(envelope{ID: id, Result: marshalOrNull(result)})
}

func (c *conn) sendError(id string, err error) {
	if !c.terminate(id) {
		return
	}
	c.writeEnvelope(envelope{ID: id, Error: &rpcError{Message: err.Error()}})
}

func (c *conn) writeEnvelope(env envelope) {
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.WriteMessage(websocket.TextMessage, data)
}
