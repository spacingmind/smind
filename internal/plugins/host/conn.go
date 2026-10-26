package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// JSON-RPC 2.0 error codes. ErrCodeForbidden is for a method the plugin may
// not call (PB.2 returns it for an undeclared capability group).
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
	ErrCodeForbidden      = -32001
	ErrCodeBusy           = -32002
)

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("plugin rpc error %d: %s", e.Code, e.Message) }

// Errors a host-to-plugin call can return besides an *RPCError.
var (
	ErrTimeout = errors.New("plugin request timed out")
	ErrBusy    = errors.New("too many plugin requests outstanding")
)

// Registry answers what a plugin sends the host. This PR wires DenyAll; the
// plugin-scoped session of PB.2 replaces it.
type Registry interface {
	// Request handles a plugin-to-host request. It must honour ctx (cancelled
	// when the connection ends) and may not block forever.
	Request(ctx context.Context, plugin, method string, params json.RawMessage) (any, *RPCError)
	// Notify handles a notification; it runs on the read loop and must return
	// quickly.
	Notify(plugin, method string, params json.RawMessage)
}

// DenyAll refuses every plugin-to-host request with method_not_found and
// drops notifications.
var DenyAll Registry = denyAll{}

type denyAll struct{}

func (denyAll) Request(_ context.Context, _, method string, _ json.RawMessage) (any, *RPCError) {
	return nil, &RPCError{Code: ErrCodeMethodNotFound, Message: "method " + clip(method) + " is not available to plugins"}
}
func (denyAll) Notify(string, string, json.RawMessage) {}

// inMsg is any message a peer can send; which fields are set tells the kind.
type inMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

type callResult struct {
	result json.RawMessage
	err    *RPCError
}

// maxIDBytes bounds a peer-chosen request id that is echoed back.
const maxIDBytes = 128

// conn is one JSON-RPC 2.0 connection over a plugin's stdio.
type conn struct {
	plugin string
	lim    Limits
	reg    Registry
	log    *Log
	in     *bufio.Reader
	out    io.Writer

	writeTimeout time.Duration
	onFail       func(error) // called once, with the typed reason

	wmu sync.Mutex

	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan callResult
	inflight int
	err      error

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	rdone  chan struct{} // closed when readLoop returns
	once   sync.Once
}

func newConn(plugin string, r io.Reader, w io.Writer, lim Limits, reg Registry, log *Log, writeTimeout time.Duration, onFail func(error)) *conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &conn{
		plugin: plugin, lim: lim, reg: reg, log: log, in: bufio.NewReaderSize(r, lim.MaxHeaderLine), out: w,
		writeTimeout: writeTimeout, onFail: onFail, pending: map[int64]chan callResult{},
		ctx: ctx, cancel: cancel, done: make(chan struct{}), rdone: make(chan struct{}),
	}
}

// Err is why the connection ended (nil while open; ErrClosed for a clean EOF).
func (c *conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// end closes the connection exactly once. A non-nil cause is a protocol
// failure: it is logged and onFail (which kills the process) runs.
func (c *conn) end(cause error) {
	c.once.Do(func() {
		c.mu.Lock()
		if cause == nil {
			c.err = ErrClosed
		} else {
			c.err = cause
		}
		c.mu.Unlock()
		c.cancel()
		close(c.done)
		if cause != nil {
			c.log.Hostf("%v", cause)
			if c.onFail != nil {
				c.onFail(cause)
			}
		}
	})
}

// readLoop reads frames until the stream ends or a frame is malformed.
func (c *conn) readLoop() {
	defer close(c.rdone)
	for {
		body, err := readFrame(c.in, c.lim)
		if err != nil {
			if errors.Is(err, io.EOF) {
				c.end(nil)
			} else {
				c.end(err)
			}
			return
		}
		var m inMsg
		if err := json.Unmarshal(body, &m); err != nil {
			c.end(frameErr(FrameBadJSON, "a frame body is not a JSON object"))
			return
		}
		if m.JSONRPC != "2.0" {
			c.end(frameErr(FrameBadMessage, "a message is not JSON-RPC 2.0"))
			return
		}
		hasID := len(m.ID) > 0 && string(m.ID) != "null"
		switch {
		case m.Method != "" && !hasID:
			c.notify(m)
		case m.Method != "":
			if len(m.ID) > maxIDBytes || (m.ID[0] != '"' && (m.ID[0] < '0' || m.ID[0] > '9') && m.ID[0] != '-') {
				c.end(frameErr(FrameBadMessage, "a request id must be a short string or number"))
				return
			}
			c.request(m)
		case hasID && (m.Result != nil || m.Error != nil):
			c.response(m)
		default:
			c.end(frameErr(FrameBadMessage, "a message is neither a request, a notification nor a response"))
			return
		}
	}
}

func (c *conn) notify(m inMsg) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("plugin notification handler panicked", "plugin", c.plugin, "panic", r)
		}
	}()
	c.reg.Notify(c.plugin, m.Method, m.Params)
}

func (c *conn) response(m inMsg) {
	var id int64
	if json.Unmarshal(m.ID, &id) != nil {
		return // not an id this host ever sent
	}
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- callResult{result: m.Result, err: m.Error}
	}
}

type okResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"`
}

type errResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   *RPCError       `json:"error"`
}

func (c *conn) request(m inMsg) {
	c.mu.Lock()
	if c.inflight >= c.lim.MaxInbound {
		c.mu.Unlock()
		c.reply(m.ID, nil, &RPCError{Code: ErrCodeBusy, Message: "too many requests in flight"})
		return
	}
	c.inflight++
	c.mu.Unlock()
	go func() {
		defer func() {
			c.mu.Lock()
			c.inflight--
			c.mu.Unlock()
		}()
		var (
			res  any
			rerr *RPCError
		)
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("plugin request handler panicked", "plugin", c.plugin, "panic", r)
					rerr = &RPCError{Code: ErrCodeInternal, Message: "internal error"}
				}
			}()
			res, rerr = c.reg.Request(c.ctx, c.plugin, m.Method, m.Params)
		}()
		c.reply(m.ID, res, rerr)
	}()
}

func (c *conn) reply(id json.RawMessage, res any, rerr *RPCError) {
	var body []byte
	var err error
	if rerr != nil {
		body, err = json.Marshal(errResp{JSONRPC: "2.0", ID: id, Error: rerr})
	} else {
		body, err = json.Marshal(okResp{JSONRPC: "2.0", ID: id, Result: res})
		if err == nil && len(body) > c.lim.MaxBody {
			err = errors.New("result over the frame cap")
		}
	}
	if err != nil {
		body, _ = json.Marshal(errResp{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: ErrCodeInternal, Message: "the result could not be sent"}})
	}
	_ = c.send(body)
}

// send writes one frame. A peer that stops reading would block the write
// forever, so a stalled write ends the connection (which kills the process).
func (c *conn) send(body []byte) error {
	if len(body) > c.lim.MaxBody {
		return frameErr(FrameBodyTooLarge, "outgoing frame of %d bytes is over the %d byte cap", len(body), c.lim.MaxBody)
	}
	select {
	case <-c.done:
		return c.Err()
	default:
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	t := time.AfterFunc(c.writeTimeout, func() {
		c.end(frameErr(FrameWriteStalled, "the plugin did not read its input for %s", c.writeTimeout))
	})
	err := writeFrame(c.out, body)
	t.Stop()
	return err
}

type outReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// notifyPeer sends a notification to the plugin.
func (c *conn) notifyPeer(method string, params any) error {
	body, err := json.Marshal(outReq{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	return c.send(body)
}

// call sends a request and waits for the answer, the timeout or the end of
// the connection. A plugin's error answer comes back as *RPCError.
func (c *conn) call(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	if len(c.pending) >= c.lim.MaxOutstanding {
		c.mu.Unlock()
		return nil, ErrBusy
	}
	c.nextID++
	id := c.nextID
	ch := make(chan callResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	drop := func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}
	body, err := json.Marshal(outReq{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		drop()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := c.send(body); err != nil {
		drop()
		return nil, err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return r.result, nil
	case <-ctx.Done():
		drop()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, ctx.Err()
	case <-c.done:
		drop()
		return nil, c.Err()
	}
}
