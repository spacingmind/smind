// Package codex implements a client for OpenAI Codex CLI's "app-server"
// protocol (`codex app-server`): a JSON-RPC-*like* dialect spoken over a
// spawned subprocess's stdio -- deliberately not real JSON-RPC 2.0 (Codex's
// own protocol crate states verbatim it neither sends nor expects a
// `jsonrpc: "2.0"` field: refs/codex/codex-rs/app-server-protocol/src/rpc.rs).
// This package is architecturally close to internal/acp (same
// conn/readLoop/pending-request/dispatch-ordering design) but intentionally
// independent: the two wire dialects differ in real, documented ways (no
// jsonrpc field here; turn completion is a later async notification rather
// than the starting request's own response -- see client.go), so sharing one
// engine across both now would fit neither cleanly.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// RPCError is the error object shape a peer returns for a failed request, or
// that a RequestHandler returns to signal a request should fail. Codex's
// protocol still uses a JSON-RPC-shaped {code, message, data} error object
// despite not being true JSON-RPC 2.0 on the envelope itself.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("codex: rpc error %d: %s", e.Code, e.Message)
}

// ErrCodeInternalError is used for this package's own synthesized errors
// (e.g. failPending on an unexpected subprocess exit) -- not a value Codex
// itself is confirmed to send, just a reasonable generic code for a
// client-side failure that never reached the wire as a real response.
const ErrCodeInternalError = -32603

// RequestHandler handles an inbound request from the agent, returning either
// a result to marshal into the response or an RPCError.
type RequestHandler func(ctx context.Context, params json.RawMessage) (result any, rpcErr *RPCError)

// NotificationHandler handles an inbound notification from the agent.
// Notifications never get a response.
type NotificationHandler func(params json.RawMessage)

// rpcMessage is the wire shape of every message this package sends or
// receives: requests, responses, and notifications all fit this one struct,
// distinguished by which fields are present. Deliberately has no "jsonrpc"
// field -- see the package doc comment.
type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type rpcResult struct {
	result json.RawMessage
	err    *RPCError
}

// conn is the transport engine for one codex app-server connection: it owns
// the subprocess and the newline-delimited stdio transport, and dispatches
// inbound and outbound requests/notifications by method name. Shape and
// concurrency discipline mirror internal/acp/rpc.go's conn exactly (see that
// file's comments for the reasoning behind each piece); only the wire
// message shape itself differs.
type conn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	writeMu sync.Mutex

	idMu    sync.Mutex
	nextID  int64
	pending map[int64]chan rpcResult

	handlersMu    sync.RWMutex
	reqHandlers   map[string]RequestHandler
	notifHandlers map[string]NotificationHandler

	closeOnce sync.Once
	readDone  chan struct{}
}

// newConn spawns command[0] with command[1:] as arguments, wiring its
// stdin/stdout as the transport and stderr to logWriter.
func newConn(command []string, logWriter io.Writer) (*conn, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("codex: empty command")
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stderr = logWriter

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex: start %q: %w", command[0], err)
	}

	c := &conn{
		cmd:           cmd,
		stdin:         stdin,
		stdout:        bufio.NewReader(stdout),
		pending:       make(map[int64]chan rpcResult),
		reqHandlers:   make(map[string]RequestHandler),
		notifHandlers: make(map[string]NotificationHandler),
		readDone:      make(chan struct{}),
	}

	go c.readLoop()

	return c, nil
}

func (c *conn) handleRequest(method string, h RequestHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.reqHandlers[method] = h
}

func (c *conn) handleNotification(method string, h NotificationHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.notifHandlers[method] = h
}

// call sends a request and blocks until a matching response arrives or ctx
// is done. Callers driving a long-running turn (see client.go's Prompt)
// must pass a ctx with no short deadline of their own -- this package
// imposes no default request timeout, since real Codex turns can run for a
// very long time (a production client's own default is 14 days; see the
// package doc comment's cited source).
func (c *conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("codex: marshal params for %s: %w", method, err)
	}

	c.idMu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResult, 1)
	c.pending[id] = ch
	c.idMu.Unlock()

	idJSON, _ := json.Marshal(id)
	if err := c.writeMessage(rpcMessage{ID: idJSON, Method: method, Params: rawParams}); err != nil {
		c.idMu.Lock()
		delete(c.pending, id)
		c.idMu.Unlock()
		return nil, fmt.Errorf("codex: write %s request: %w", method, err)
	}

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return res.result, nil
	case <-ctx.Done():
		c.idMu.Lock()
		delete(c.pending, id)
		c.idMu.Unlock()
		return nil, ctx.Err()
	}
}

// notify sends a notification (no response expected).
func (c *conn) notify(method string, params any) error {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("codex: marshal params for %s: %w", method, err)
	}
	if err := c.writeMessage(rpcMessage{Method: method, Params: rawParams}); err != nil {
		return fmt.Errorf("codex: write %s notification: %w", method, err)
	}
	return nil
}

// respond sends the result of an inbound request back to the agent, using
// id exactly as received.
func (c *conn) respond(id json.RawMessage, result any, rpcErr *RPCError) error {
	msg := rpcMessage{ID: id}
	if rpcErr != nil {
		msg.Error = rpcErr
	} else {
		raw, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("codex: marshal response result: %w", err)
		}
		msg.Result = raw
	}
	return c.writeMessage(msg)
}

func (c *conn) writeMessage(msg rpcMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(data)
	return err
}

// readLoop parses newline-delimited messages from the agent's stdout until
// it errors or hits EOF, classifying each as a request (id+method), a
// notification (method, no id), or a response (id, no method).
func (c *conn) readLoop() {
	defer close(c.readDone)

	for {
		line, err := c.stdout.ReadString('\n')
		if len(line) > 0 {
			c.handleLine(line)
		}
		if err != nil {
			c.failPending(fmt.Errorf("codex: connection closed: %w", err))
			return
		}
	}
}

func (c *conn) handleLine(line string) {
	var msg rpcMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return
	}

	switch {
	case msg.Method != "" && len(msg.ID) > 0:
		go c.handleInboundRequest(msg)
	case msg.Method != "":
		// Dispatched inline, not in a goroutine -- same ordering reasoning
		// as internal/acp/rpc.go's handleLine: a caller (Prompt) relies on
		// every item/agentMessage/delta notification for a turn being
		// delivered before the turn/completed notification that ends it.
		// Dispatching notifications concurrently with reading further lines
		// would let turn/completed reach Prompt's waiter before an
		// already-in-flight delta notification, handled on a separate
		// goroutine, had actually been delivered.
		c.handlersMu.RLock()
		h := c.notifHandlers[msg.Method]
		c.handlersMu.RUnlock()
		if h != nil {
			h(msg.Params)
		}
	case len(msg.ID) > 0:
		c.handleResponse(msg)
	}
}

func (c *conn) handleInboundRequest(msg rpcMessage) {
	c.handlersMu.RLock()
	h := c.reqHandlers[msg.Method]
	c.handlersMu.RUnlock()

	if h == nil {
		_ = c.respond(msg.ID, nil, &RPCError{Code: ErrCodeInternalError, Message: "method not found: " + msg.Method})
		return
	}

	result, rpcErr := h(context.Background(), msg.Params)
	_ = c.respond(msg.ID, result, rpcErr)
}

func (c *conn) handleResponse(msg rpcMessage) {
	var id int64
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		return
	}

	c.idMu.Lock()
	ch, ok := c.pending[id]
	delete(c.pending, id)
	c.idMu.Unlock()

	if ok {
		ch <- rpcResult{result: msg.Result, err: msg.Error}
	}
}

func (c *conn) failPending(err error) {
	rpcErr := &RPCError{Code: ErrCodeInternalError, Message: err.Error()}

	c.idMu.Lock()
	pending := c.pending
	c.pending = make(map[int64]chan rpcResult)
	c.idMu.Unlock()

	for _, ch := range pending {
		ch <- rpcResult{err: rpcErr}
	}
}

// close closes stdin (signalling the agent to exit) and waits for the
// subprocess to exit, force-killing it if it hasn't within timeout.
func (c *conn) close(timeout time.Duration) error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
	})

	select {
	case <-c.readDone:
	case <-time.After(timeout):
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- c.cmd.Wait() }()

	select {
	case err := <-waitDone:
		return err
	case <-time.After(timeout):
		_ = c.cmd.Process.Kill()
		return <-waitDone
	}
}
