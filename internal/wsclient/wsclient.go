// Package wsclient implements a small, reusable client for smind's
// internal/wsapi WebSocket RPC protocol: one persistent connection carrying
// many independent request/response exchanges, some of which stream zero
// or more server-pushed events before their terminal result. It is the
// client-side counterpart to internal/wsapi/conn.go -- see that file's doc
// comment for the wire protocol both sides agree on.
//
// This package is used by cmd/smind's CLI subcommands to talk to a running
// smind daemon over /ws, in place of a one-shot HTTP call, for exactly the
// methods that need bidirectional or streamed traffic (task.prompt/
// run.start/run.attach) or that simply live on the same connection as those
// (workspace.*/task.*/run.*).
package wsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// envelope mirrors internal/wsapi's wire envelope exactly -- see that
// package's doc comment for the four shapes a message can take. Duplicated
// here rather than exported from wsapi, since wsapi's envelope is an
// internal implementation detail of the server side, not a shared API
// type; the wire format is the actual contract between the two packages,
// not a shared Go type.
type envelope struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
}

type rpcError struct {
	Message string `json:"message"`
}

type cancelParams struct {
	ID string `json:"id"`
}

// RPCError is returned by Call/CallStream when the server's terminal
// response for a request was an error, as opposed to a transport-level
// failure (connection lost, context deadline, etc.) -- callers that care
// about that distinction can use errors.As.
type RPCError struct {
	Message string
}

func (e *RPCError) Error() string { return e.Message }

// EventFunc is invoked once per server-pushed event a streaming call
// receives, in arrival order, before its terminal result/error. It must not
// block for long: it runs on the connection's single read loop, so a slow
// or blocking EventFunc delays delivery of every other in-flight request's
// messages on the same connection too.
type EventFunc func(event string, params json.RawMessage)

// inflightRequest is one request this Client is waiting on a terminal
// response for. term is buffered (size 1) so the read loop's delivery never
// blocks on a caller that has already stopped reading (e.g. because its own
// ctx was already Done via some other path).
type inflightRequest struct {
	term    chan envelope
	onEvent EventFunc
}

// Client is one WebSocket connection to a smind daemon's /ws endpoint,
// supporting any number of concurrent in-flight requests (each identified
// by its own request id) the same way internal/wsapi/conn.go's conn does
// server-side. Safe for concurrent use.
type Client struct {
	ws      *websocket.Conn
	writeMu sync.Mutex

	mu       sync.Mutex
	inflight map[string]*inflightRequest
	closed   bool
	closeErr error

	nextID atomic.Uint64
}

// Dial connects to ws://addr/ws?token=token and starts serving it. The
// returned Client is usable immediately; Close (or the connection dropping
// on its own) ends its background read loop and fails any requests still
// waiting on a terminal response.
func Dial(ctx context.Context, addr, token string) (*Client, error) {
	u := url.URL{Scheme: "ws", Host: addr, Path: "/ws", RawQuery: "token=" + url.QueryEscape(token)}
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("wsclient: dial %s: %w", u.Host, err)
	}
	c := &Client{
		ws:       ws,
		inflight: make(map[string]*inflightRequest),
	}
	go c.readLoop()
	return c, nil
}

// Close closes the underlying connection. Any request still in flight on it
// fails with a transport error; a request that already received its
// terminal response is unaffected.
func (c *Client) Close() error {
	return c.ws.Close()
}

// Call issues method with params and blocks until its terminal response
// arrives, decoding a successful result into out (which may be nil, if the
// caller doesn't need it). It's Call's caller's responsibility to only use
// it for methods that don't stream events; use CallStream for those (a
// non-nil onEvent isn't required -- any events Call's underlying request
// happens to receive are silently discarded).
//
// If ctx is cancelled before the terminal response arrives, Call sends
// task.cancel for this request's own id and then waits for its (now very
// likely an error, but not necessarily -- the server's own completion can
// race the cancel and win) terminal response before returning, so the
// caller can rely on the request no longer being in flight by the time Call
// returns either way.
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	return c.CallStream(ctx, method, params, nil, out)
}

// CallStream is Call plus a callback invoked for every server-pushed event
// the request receives before its terminal response, in arrival order and
// as each one is received off the wire (not buffered until the terminal
// response) -- this is what makes real incremental streaming (e.g. `task
// send`'s live chunk-by-chunk stdout) possible.
func (c *Client) CallStream(ctx context.Context, method string, params any, onEvent EventFunc, out any) error {
	id := fmt.Sprintf("%d", c.nextID.Add(1))
	req := &inflightRequest{term: make(chan envelope, 1), onEvent: onEvent}

	c.mu.Lock()
	if c.closed {
		err := c.closeErr
		c.mu.Unlock()
		return fmt.Errorf("wsclient: connection closed: %w", err)
	}
	c.inflight[id] = req
	c.mu.Unlock()

	if err := c.send(envelope{ID: id, Method: method, Params: marshalOrNull(params)}); err != nil {
		c.mu.Lock()
		delete(c.inflight, id)
		c.mu.Unlock()
		return fmt.Errorf("wsclient: send %s: %w", method, err)
	}

	select {
	case env := <-req.term:
		return decodeTerminal(env, out)
	case <-ctx.Done():
		// Cancel just this one in-flight request -- not the whole
		// connection, which may have other work in flight on it (or may
		// simply be worth keeping open for a following call) -- and keep
		// waiting for its terminal response rather than returning
		// immediately, so a caller can be sure the request is no longer
		// in flight server-side once this returns.
		_ = c.send(envelope{Method: "task.cancel", Params: marshalOrNull(cancelParams{ID: id})})
		select {
		case env := <-req.term:
			if err := decodeTerminal(env, out); err == nil {
				// The server's own completion raced the cancel and won;
				// report the real success rather than manufacturing a
				// cancellation error for a request that in fact finished.
				return nil
			}
			return ctx.Err()
		case <-c.closedCh():
			return ctx.Err()
		}
	}
}

// closedCh returns a channel that is (already, if the connection is
// already closed) closed once the connection's read loop exits, so a
// select waiting on a terminal response that will now never come (because
// the connection just died) doesn't block forever.
func (c *Client) closedCh() <-chan struct{} {
	ch := make(chan struct{})
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		close(ch)
	}
	// Note: this doesn't catch a connection that closes *after* this call
	// but before the request's own term channel is fulfilled by failAll --
	// but failAll always delivers a terminal envelope to every still
	// in-flight request's term channel before returning (see readLoop), so
	// that case is already covered by the other select case.
	return ch
}

func decodeTerminal(env envelope, out any) error {
	if env.Error != nil {
		return &RPCError{Message: env.Error.Message}
	}
	if out == nil || env.Result == nil {
		return nil
	}
	return json.Unmarshal(env.Result, out)
}

func (c *Client) readLoop() {
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			c.failAll(err)
			return
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil || env.ID == "" {
			continue
		}

		c.mu.Lock()
		req, ok := c.inflight[env.ID]
		c.mu.Unlock()
		if !ok {
			continue
		}

		if env.Event != "" {
			if req.onEvent != nil {
				req.onEvent(env.Event, env.Params)
			}
			continue
		}

		c.mu.Lock()
		delete(c.inflight, env.ID)
		c.mu.Unlock()
		req.term <- env
	}
}

// failAll marks the connection closed and delivers a synthetic error
// terminal response to every request still waiting on one, so no caller of
// Call/CallStream can block forever past the connection dying.
func (c *Client) failAll(err error) {
	c.mu.Lock()
	reqs := c.inflight
	c.inflight = make(map[string]*inflightRequest)
	c.closed = true
	c.closeErr = err
	c.mu.Unlock()

	for _, req := range reqs {
		req.term <- envelope{Error: &rpcError{Message: err.Error()}}
	}
}

func (c *Client) send(env envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

func marshalOrNull(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return raw
}
