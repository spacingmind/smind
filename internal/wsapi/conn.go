package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/spacingmind/smind/internal/runs"
)

// Transport is the minimal duplex byte-message stream a conn serves RPC
// dispatch over: receive one message, send one message, close. A real
// gorilla WebSocket (wrapped by wsTransport, see server.go) and a relay
// *client.DataConn (internal/relay/client, whose Send/Receive/Close already
// match this shape exactly) both satisfy it, so the same conn/dispatch code
// serves a browser tab and a relay-bridged mobile session identically.
type Transport interface {
	// Receive blocks for and returns the next message's raw bytes.
	Receive() ([]byte, error)
	// Send writes one message.
	Send([]byte) error
	// Close closes the underlying connection.
	Close() error
}

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
	// runScoped is true when the caller is an agent run rather than a
	// user surface; see conn.runScoped.
	runScoped bool
	// ident is the run identity of a run-scoped caller (zero otherwise).
	ident runIdentity
	// local is true when the caller holds the daemon token on /ws, false for
	// a relay-served (paired-device) session; see conn.local.
	local bool
	// deviceID is the verified relay device id of this session, "" for the
	// local /ws connection and for legacy relay clients; see conn.deviceID.
	deviceID string
	// channel is how the connection reached the daemon; see conn.channel.
	channel Channel
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
	tr       Transport
	handlers map[string]handlerFunc

	writeMu sync.Mutex

	mu       sync.Mutex
	inflight map[string]*inflightRequest

	// eventsSub/eventsBus, when non-nil, arm this connection's ADR-0005
	// event subscription: serve registers the subscriber on the shared
	// bus, layers the per-connection events.subscribe/events.unsubscribe
	// handlers over the shared handler map, and runs pumpEvents until the
	// connection closes.
	eventsSub *subscriber
	eventsBus *eventBus

	// runScoped marks a connection that identified itself as an agent run
	// through session.identify (ADR-0041 S2b; see runIdentity): its forced
	// usage refreshes are downgraded to touches, only RunScopedMethods are
	// served, and it dies with its run. ident holds the run's ids. Both are
	// guarded by identMu (set by a request goroutine, read by all).
	runScoped bool
	ident     runIdentity
	identMu   sync.Mutex

	// runs answers the per-request "is my run still running" check for a
	// run-scoped connection; nil disables it.
	runs *runs.Registry

	// admit applies the ADR-0041 S3 seat policy to a run-scoped
	// connection's requests; nil disables it.
	admit *seatAdmission

	// local marks the token-authenticated /ws connection. A relay-served
	// session (ServeTransport) is not local, so handlers that administer the
	// daemon itself (relay.device.*) refuse it.
	local bool

	// deviceID is the verified relay device id of this session, "" for the
	// local /ws connection and for legacy relay clients.
	deviceID string

	// channel is the connection's transport class (loopback, tls, e2ee,
	// plain); the zero value is treated as plain.
	channel Channel

	// focus, when non-nil, is the daemon-wide task.gitFocus registry; this
	// connection's entry is dropped when serve returns.
	focus *focusRegistry

	// maxMessage is the largest message this connection's transport carries
	// (DirectMaxMessage on /ws, RelayMaxMessage over a relay); 0 = relay's.
	maxMessage int
}

func newConn(tr Transport, handlers map[string]handlerFunc) *conn {
	return &conn{
		tr:       tr,
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
	if c.focus != nil {
		defer c.focus.drop(c)
	}

	if c.eventsSub != nil {
		hs := make(map[string]handlerFunc, len(c.handlers)+2)
		for k, v := range c.handlers {
			hs[k] = v
		}
		hs["events.subscribe"] = handleEventsSubscribe(c.eventsSub)
		hs["events.unsubscribe"] = handleEventsUnsubscribe(c.eventsSub)
		c.handlers = hs

		c.eventsBus.register(c.eventsSub)
		defer func() {
			c.eventsBus.unregister(c.eventsSub)
			c.eventsSub.close()
		}()
		go c.pumpEvents(connCtx, c.eventsSub)
	}

	for {
		data, err := c.tr.Receive()
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
	// Before the handler lookup: a run-scoped connection fails closed, so
	// even an unregistered method name gets "not available to agents".
	if err := c.gate(env.Method); err != nil {
		c.writeEnvelope(envelope{ID: env.ID, Error: codedError(err)})
		return
	}
	h, ok := c.handlers[env.Method]
	if !ok {
		// Answered directly, not through sendError: that only writes for a
		// request registered in flight (it exists to guarantee ONE terminal
		// response per in-flight id), and this request never is — routing it
		// through there silently dropped the answer, leaving the client waiting
		// forever for a "method not found". Not registering it also keeps a
		// stray duplicate id from clobbering a running request's entry.
		c.writeEnvelope(envelope{ID: env.ID, Error: codedError(fmt.Errorf("method not found: %s", env.Method))})
		return
	}

	reqCtx, cancel := context.WithCancel(connCtx)
	defer cancel()

	c.mu.Lock()
	c.inflight[env.ID] = &inflightRequest{cancel: cancel}
	c.mu.Unlock()

	ident, scoped := c.identity()
	rc := &requestContext{conn: c, id: env.ID, runScoped: scoped, ident: ident, local: c.local, deviceID: c.deviceID, channel: c.channel}
	if scoped {
		if err := c.admit.admit(rc, env.Method, env.Params); err != nil {
			c.sendError(env.ID, err)
			return
		}
	}
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
	c.writeEnvelope(envelope{ID: id, Error: codedError(err)})
}

func (c *conn) writeEnvelope(env envelope) {
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.tr.Send(data)
}
