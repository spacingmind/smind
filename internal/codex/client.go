package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// closeTimeout bounds how long Close waits for the agent to exit after its
// stdin is closed before force-killing the subprocess.
const closeTimeout = 5 * time.Second

// Update is one streamed piece of a turn's assistant text, forwarded from
// item/agentMessage/delta notifications. Only text is modeled -- Codex also
// streams reasoning/thinking deltas (item/reasoning/summaryTextDelta) and
// structured item lifecycle events, out of scope for this package's current
// callers (see docs/plans/active/provider-codex.md's Decisions).
type Update struct {
	Text string
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeParams struct {
	ClientInfo clientInfo `json:"clientInfo"`
}

type threadStartParams struct {
	Cwd string `json:"cwd,omitempty"`
}

type thread struct {
	ID string `json:"id"`
}

type threadStartResponse struct {
	Thread thread `json:"thread"`
}

type userInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type turnStartParams struct {
	ThreadID string      `json:"threadId"`
	Input    []userInput `json:"input"`
}

type turnError struct {
	Message string `json:"message"`
}

type turn struct {
	ID     string     `json:"id"`
	Status string     `json:"status"`
	Error  *turnError `json:"error"`
}

type turnStartResponse struct {
	Turn turn `json:"turn"`
}

type agentMessageDeltaNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type turnCompletedNotification struct {
	ThreadID string `json:"threadId"`
	Turn     turn   `json:"turn"`
}

// turnResult is what a completed (or failed/interrupted) turn resolves to,
// decoded from a turn/completed notification's Turn.Status/Error.
type turnResult struct {
	status string
	errMsg string
}

// Client is a codex app-server client: it spawns `codex app-server` (or an
// equivalent command), performs the initialize handshake, and drives turns
// on a thread while answering the agent's command-execution/file-change
// approval callbacks.
type Client struct {
	conn      *conn
	policy    PermissionPolicy
	logWriter io.Writer

	// mu guards updateSubs/turnWaiters, both keyed by threadID -- not
	// turnID, deliberately: a turn's own id is only known once turn/start's
	// response arrives, which would create a window (between issuing the
	// request and registering by that id) where an early
	// item/agentMessage/delta or turn/completed notification could be
	// missed. threadID is already known before turn/start is even called
	// (from NewSession's response), so registering by it first -- mirroring
	// internal/acp.Client.Prompt's identical sessionID-first reasoning --
	// closes that window entirely. This assumes at most one turn in flight
	// per thread at a time, which matches every real caller today (each
	// taskrunner.RunPrompt call spawns a fresh Client for exactly one
	// turn); it is not safe to call Prompt twice concurrently for the same
	// threadID.
	mu          sync.Mutex
	updateSubs  map[string]*updateSub
	turnWaiters map[string]chan turnResult
}

// updateSub tracks one thread's subscriber channel plus a WaitGroup of
// handleAgentMessageDelta sends currently in flight against it -- same
// shape and cleanup-ordering reasoning as internal/acp.Client's updateSub;
// see that type's doc comment for why the WaitGroup is needed to avoid a
// send-on-closed-channel race when Prompt returns via ctx cancellation.
type updateSub struct {
	ch chan<- Update
	wg sync.WaitGroup
}

// Option configures a Client constructed via New.
type Option func(*Client)

// WithLogWriter captures the agent's stderr (agent-side logging, never
// parsed as protocol) to w. Defaults to io.Discard.
func WithLogWriter(w io.Writer) Option {
	return func(c *Client) { c.logWriter = w }
}

// WithPermissionPolicy overrides the default PermissionPolicy. See
// AutoApprovePolicy for the default and why it was chosen.
func WithPermissionPolicy(p PermissionPolicy) Option {
	return func(c *Client) { c.policy = p }
}

// New spawns command[0] with command[1:] as arguments and wires up a codex
// app-server connection to it, registering this Client's approval-request
// handlers. Call Initialize before anything else.
func New(command []string, opts ...Option) (*Client, error) {
	c := &Client{
		policy:      AutoApprovePolicy{},
		logWriter:   io.Discard,
		updateSubs:  make(map[string]*updateSub),
		turnWaiters: make(map[string]chan turnResult),
	}
	for _, opt := range opts {
		opt(c)
	}

	conn, err := newConn(command, c.logWriter)
	if err != nil {
		return nil, err
	}
	c.conn = conn

	conn.handleRequest("item/commandExecution/requestApproval", c.handleCommandExecutionApproval)
	conn.handleRequest("item/fileChange/requestApproval", c.handleFileChangeApproval)
	conn.handleNotification("item/agentMessage/delta", c.handleAgentMessageDelta)
	conn.handleNotification("turn/completed", c.handleTurnCompleted)

	return c, nil
}

// Initialize performs the app-server initialize handshake: an "initialize"
// request, then a fire-and-forget "initialized" notification -- both
// required, and in that order, per a real working production client (see
// docs/plans/active/provider-codex.md's Decisions for the cited source).
func (c *Client) Initialize(ctx context.Context) error {
	if _, err := c.conn.call(ctx, "initialize", initializeParams{
		ClientInfo: clientInfo{Name: "smind", Version: "0.1.0"},
	}); err != nil {
		return fmt.Errorf("codex: initialize: %w", err)
	}
	if err := c.conn.notify("initialized", struct{}{}); err != nil {
		return fmt.Errorf("codex: initialized: %w", err)
	}
	return nil
}

// NewSession starts a new codex thread rooted at cwd (the task's worktree
// path), returning its thread id.
func (c *Client) NewSession(ctx context.Context, cwd string) (string, error) {
	raw, err := c.conn.call(ctx, "thread/start", threadStartParams{Cwd: cwd})
	if err != nil {
		return "", fmt.Errorf("codex: thread/start: %w", err)
	}

	var res threadStartResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("codex: thread/start: decode response: %w", err)
	}
	return res.Thread.ID, nil
}

// Prompt sends text as a single user-input turn on threadID and blocks
// until the turn reaches a terminal state. Unlike internal/acp.Client's
// Prompt (where the request's own response is the completion signal),
// Codex's turn/start response only acknowledges the turn was accepted --
// the real completion signal is a later, asynchronous turn/completed
// notification, which this method waits on separately. While the turn is
// in flight, every item/agentMessage/delta notification for threadID is
// forwarded onto updates as it arrives, in order; updates is closed when
// Prompt returns, whether it returns an error or not.
func (c *Client) Prompt(ctx context.Context, threadID, text string, updates chan<- Update) (string, error) {
	sub := &updateSub{ch: updates}
	waiter := make(chan turnResult, 1)
	c.mu.Lock()
	c.updateSubs[threadID] = sub
	c.turnWaiters[threadID] = waiter
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.updateSubs, threadID)
		delete(c.turnWaiters, threadID)
		c.mu.Unlock()
		sub.wg.Wait()
		close(updates)
	}()

	raw, err := c.conn.call(ctx, "turn/start", turnStartParams{
		ThreadID: threadID,
		Input:    []userInput{{Type: "text", Text: text}},
	})
	if err != nil {
		return "", fmt.Errorf("codex: turn/start: %w", err)
	}
	var res turnStartResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("codex: turn/start: decode response: %w", err)
	}

	select {
	case result := <-waiter:
		if result.status == "failed" {
			return result.status, fmt.Errorf("codex: turn failed: %s", result.errMsg)
		}
		return result.status, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *Client) handleAgentMessageDelta(raw json.RawMessage) {
	var n agentMessageDeltaNotification
	if err := json.Unmarshal(raw, &n); err != nil {
		return
	}

	c.mu.Lock()
	sub, ok := c.updateSubs[n.ThreadID]
	if ok {
		sub.wg.Add(1)
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	defer sub.wg.Done()
	sub.ch <- Update{Text: n.Delta}
}

func (c *Client) handleTurnCompleted(raw json.RawMessage) {
	var n turnCompletedNotification
	if err := json.Unmarshal(raw, &n); err != nil {
		return
	}

	c.mu.Lock()
	waiter, ok := c.turnWaiters[n.ThreadID]
	c.mu.Unlock()
	if !ok {
		return
	}

	result := turnResult{status: n.Turn.Status}
	if n.Turn.Error != nil {
		result.errMsg = n.Turn.Error.Message
	}
	// Buffered(1): Prompt may not have reached its select yet (still
	// blocked inside the turn/start call above), so this send must never
	// block the read loop.
	waiter <- result
}

// Close closes the connection to the agent, giving it closeTimeout to exit
// cleanly after its stdin is closed before force-killing the subprocess.
func (c *Client) Close() error {
	return c.conn.close(closeTimeout)
}
