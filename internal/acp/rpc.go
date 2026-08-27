// Package acp implements an Agent Client Protocol (ACP) client: it spawns
// an ACP agent as a subprocess and speaks JSON-RPC 2.0 with it over stdio,
// per https://agentclientprotocol.com and refs/agent-client-protocol.
package acp

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

// RPCError is a JSON-RPC 2.0 error object, returned by a peer in response to
// a request, or by a RequestHandler to signal that a request should fail.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("acp: rpc error %d: %s", e.Code, e.Message)
}

// JSON-RPC and ACP-specific error codes, per
// refs/agent-client-protocol/schema/v1/schema.json's ErrorCode.
const (
	ErrCodeParseError       = -32700
	ErrCodeInvalidRequest   = -32600
	ErrCodeMethodNotFound   = -32601
	ErrCodeInvalidParams    = -32602
	ErrCodeInternalError    = -32603
	ErrCodeCancelled        = -32800
	ErrCodeResourceNotFound = -32002
)

// RequestHandler handles an inbound JSON-RPC request from the agent,
// returning either a result to marshal into the response or an RPCError.
type RequestHandler func(ctx context.Context, params json.RawMessage) (result any, rpcErr *RPCError)

// NotificationHandler handles an inbound JSON-RPC notification from the
// agent. Notifications never get a response.
type NotificationHandler func(params json.RawMessage)

// rpcMessage is the wire shape of every JSON-RPC message this package sends
// or receives: requests, responses, and notifications all fit this one
// struct, distinguished by which fields are present.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type rpcResult struct {
	result json.RawMessage
	err    *RPCError
}

// conn is the JSON-RPC 2.0 engine for one ACP connection: it owns the
// subprocess and the newline-delimited stdio transport, and dispatches
// inbound and outbound requests/notifications by method name.
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
// stdin/stdout as the JSON-RPC transport and stderr to logWriter.
func newConn(command []string, logWriter io.Writer) (*conn, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("acp: empty command")
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stderr = logWriter

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start %q: %w", command[0], err)
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

// call sends a JSON-RPC request and blocks until a matching response
// arrives or ctx is done.
func (c *conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("acp: marshal params for %s: %w", method, err)
	}

	c.idMu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResult, 1)
	c.pending[id] = ch
	c.idMu.Unlock()

	idJSON, _ := json.Marshal(id)
	if err := c.writeMessage(rpcMessage{JSONRPC: "2.0", ID: idJSON, Method: method, Params: rawParams}); err != nil {
		c.idMu.Lock()
		delete(c.pending, id)
		c.idMu.Unlock()
		return nil, fmt.Errorf("acp: write %s request: %w", method, err)
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

// notify sends a JSON-RPC notification (no response expected).
func (c *conn) notify(method string, params any) error {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("acp: marshal params for %s: %w", method, err)
	}
	if err := c.writeMessage(rpcMessage{JSONRPC: "2.0", Method: method, Params: rawParams}); err != nil {
		return fmt.Errorf("acp: write %s notification: %w", method, err)
	}
	return nil
}

// respond sends the result of an inbound request back to the agent, using
// id exactly as received (it may be a string or a number on the wire).
func (c *conn) respond(id json.RawMessage, result any, rpcErr *RPCError) error {
	msg := rpcMessage{JSONRPC: "2.0", ID: id}
	if rpcErr != nil {
		msg.Error = rpcErr
	} else {
		raw, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("acp: marshal response result: %w", err)
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

// readLoop parses newline-delimited JSON-RPC messages from the agent's
// stdout until it errors or hits EOF, classifying each as a request
// (id+method), a notification (method, no id), or a response (id, no
// method) per the JSON-RPC 2.0 wire format.
func (c *conn) readLoop() {
	defer close(c.readDone)

	for {
		line, err := c.stdout.ReadString('\n')
		if len(line) > 0 {
			c.handleLine(line)
		}
		if err != nil {
			c.failPending(fmt.Errorf("acp: connection closed: %w", err))
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
		// Dispatched inline, not in a goroutine: notifications and
		// responses share this one stream, and a caller (e.g. Prompt)
		// relies on every session/update for a turn being delivered
		// before the response that ends the turn. Dispatching
		// notifications concurrently with reading further lines broke
		// that ordering guarantee — a PromptResponse read right after
		// its last session/update notification could reach the pending
		// call and unblock Prompt (closing its updates channel) before
		// the notification handler, running in a separate goroutine,
		// had actually delivered that notification.
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
		_ = c.respond(msg.ID, nil, &RPCError{Code: ErrCodeMethodNotFound, Message: "method not found: " + msg.Method})
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
