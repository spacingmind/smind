package codex

import (
	"context"
	"encoding/json"
	"errors"
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
	Cwd            string `json:"cwd,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy,omitempty"`
	Sandbox        string `json:"sandbox,omitempty"`
	Model          string `json:"model,omitempty"`

	ModelProvider string         `json:"modelProvider,omitempty"`
	Config        map[string]any `json:"config,omitempty"`

	DeveloperInstructions string `json:"developerInstructions,omitempty"`
}

type thread struct {
	ID string `json:"id"`
}

type threadStartResponse struct {
	Thread thread `json:"thread"`
}

// userInput is one turn/start input entry: {type:"text", text} or
// {type:"localImage", path}. Text is a pointer so an empty text input still
// serialises its "text" key while a localImage entry carries none.
type userInput struct {
	Type string  `json:"type"`
	Text *string `json:"text,omitempty"`
	Path string  `json:"path,omitempty"`
}

// PromptOption tunes one Prompt call.
type PromptOption func(*promptOptions)

type promptOptions struct{ images []string }

// WithLocalImages appends {type:"localImage", path} inputs after the text
// input; paths must be absolute.
func WithLocalImages(paths []string) PromptOption {
	return func(o *promptOptions) { o.images = paths }
}

type threadLoadedListResponse struct {
	Data []string `json:"data"`
}

type threadResumeParams struct {
	ThreadID       string `json:"threadId"`
	Cwd            string `json:"cwd,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy,omitempty"`
	Sandbox        string `json:"sandbox,omitempty"`
	Model          string `json:"model,omitempty"`

	ModelProvider string         `json:"modelProvider,omitempty"`
	Config        map[string]any `json:"config,omitempty"`

	DeveloperInstructions string `json:"developerInstructions,omitempty"`
}

type threadUnarchiveParams struct {
	ThreadID string `json:"threadId"`
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

// TokenUsage is one tokenUsage breakdown decoded from
// thread/tokenUsage/updated (ThreadTokenUsageUpdatedNotification). Client
// keeps the notification's `total`: the thread's cumulative usage so far.
// (`last` is only the most recent single model response, so a turn that
// loops through tools has several of them.) Every field is a pointer so a
// field missing on the wire stays nil.
type TokenUsage struct {
	InputTokens           *int64 `json:"inputTokens"`
	CachedInputTokens     *int64 `json:"cachedInputTokens"`
	CacheWriteInputTokens *int64 `json:"cacheWriteInputTokens"`
	OutputTokens          *int64 `json:"outputTokens"`
	ReasoningOutputTokens *int64 `json:"reasoningOutputTokens"`
	TotalTokens           *int64 `json:"totalTokens"`
	// ModelContextWindow is tokenUsage.modelContextWindow.
	ModelContextWindow *int64 `json:"-"`
}

type threadTokenUsageUpdatedNotification struct {
	ThreadID   string `json:"threadId"`
	TokenUsage struct {
		Total              TokenUsage `json:"total"`
		ModelContextWindow *int64     `json:"modelContextWindow"`
	} `json:"tokenUsage"`
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
	env       []string // WithEnv: appended to the inherited environment

	// approvalPolicy/sandbox are Codex's own AskForApproval and
	// SandboxMode values sent on every thread/start and thread/resume (see
	// WithThreadPolicy); empty leaves Codex's own configured default.
	approvalPolicy string
	sandbox        string

	// model is sent as `model` on every thread/start and thread/resume
	// (see WithModel); empty omits the field, leaving Codex's default.
	model string
	// developerInstructions is sent as `developerInstructions` on every
	// thread/start and thread/resume (see WithDeveloperInstructions).
	developerInstructions string

	// modelProvider/threadConfig are sent as `modelProvider`/`config` on
	// every thread/start and thread/resume (see WithModelProvider); an
	// empty modelProvider omits both, leaving Codex's own provider.
	modelProvider string
	threadConfig  map[string]any

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

	// tokenUsage is each thread's latest tokenUsage.total this turn (the
	// thread-cumulative figure), reset by Prompt; guarded by mu.
	tokenUsage map[string]TokenUsage
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

// WithEnv adds NAME=VALUE entries to the app-server's environment, after the
// inherited one (last entry wins), e.g. CODEX_HOME.
func WithEnv(env ...string) Option {
	return func(c *Client) { c.env = append(c.env, env...) }
}

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

// WithThreadPolicy sets Codex's own approvalPolicy (AskForApproval, e.g.
// "on-request", "never") and sandbox (SandboxMode, e.g. "workspace-write",
// "danger-full-access") on every thread this Client starts or resumes --
// the app-server's native permission surface (codex-rs
// app-server-protocol v2 ThreadStartParams/ThreadResumeParams). Either
// may be empty to leave Codex's configured default in place.
func WithThreadPolicy(approvalPolicy, sandbox string) Option {
	return func(c *Client) { c.approvalPolicy, c.sandbox = approvalPolicy, sandbox }
}

// WithDeveloperInstructions sets the `developerInstructions` sent on every
// thread/start and thread/resume. Empty omits the field.
func WithDeveloperInstructions(text string) Option {
	return func(c *Client) { c.developerInstructions = text }
}

// WithModel sets the `model` sent on every thread/start and thread/resume.
// Empty (the default) omits the field entirely.
func WithModel(model string) Option {
	return func(c *Client) { c.model = model }
}

// WithModelProvider sets the `modelProvider` and `config` sent on every
// thread/start and thread/resume. Empty modelProvider / nil config (the
// default) omit both fields entirely.
func WithModelProvider(modelProvider string, config map[string]any) Option {
	return func(c *Client) { c.modelProvider, c.threadConfig = modelProvider, config }
}

// ModelInfo is one entry of the app-server's model/list.
type ModelInfo struct {
	ID          string
	DisplayName string
	Description string
	IsDefault   bool
}

type modelListParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type modelListResponse struct {
	Data []struct {
		ID          string `json:"id"`
		Model       string `json:"model"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		Hidden      bool   `json:"hidden"`
		IsDefault   bool   `json:"isDefault"`
	} `json:"data"`
	NextCursor string `json:"nextCursor"`
}

// maxModelListPages bounds ListModels' cursor loop against a misbehaving
// server that never stops returning a cursor.
const maxModelListPages = 20

// ListModels returns the models the app-server offers (model/list),
// following pagination and skipping hidden ones. The model id is the
// entry's `model` field (what thread/start's `model` accepts), falling back
// to `id`.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	var out []ModelInfo
	cursor := ""
	for page := 0; page < maxModelListPages; page++ {
		raw, err := c.conn.call(ctx, "model/list", modelListParams{Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("codex: model/list: %w", err)
		}
		var res modelListResponse
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("codex: model/list: decode response: %w", err)
		}
		for _, m := range res.Data {
			if m.Hidden {
				continue
			}
			id := m.Model
			if id == "" {
				id = m.ID
			}
			out = append(out, ModelInfo{ID: id, DisplayName: m.DisplayName, Description: m.Description, IsDefault: m.IsDefault})
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
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
		tokenUsage:  make(map[string]TokenUsage),
	}
	for _, opt := range opts {
		opt(c)
	}

	conn, err := newConn(command, c.logWriter, c.env)
	if err != nil {
		return nil, err
	}
	c.conn = conn

	conn.handleRequest("item/commandExecution/requestApproval", c.handleCommandExecutionApproval)
	conn.handleRequest("item/fileChange/requestApproval", c.handleFileChangeApproval)
	conn.handleNotification("item/agentMessage/delta", c.handleAgentMessageDelta)
	conn.handleNotification("turn/completed", c.handleTurnCompleted)
	conn.handleNotification("thread/tokenUsage/updated", c.handleTokenUsageUpdated)

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
	raw, err := c.conn.call(ctx, "thread/start", threadStartParams{Cwd: cwd, ApprovalPolicy: c.approvalPolicy, Sandbox: c.sandbox, Model: c.model, ModelProvider: c.modelProvider, Config: c.threadConfig, DeveloperInstructions: c.developerInstructions})
	if err != nil {
		return "", fmt.Errorf("codex: thread/start: %w", err)
	}

	var res threadStartResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("codex: thread/start: decode response: %w", err)
	}
	return res.Thread.ID, nil
}

// ResumeSession resumes an existing Codex thread (threadID) rooted at cwd,
// returning its thread id (always threadID unchanged) once the agent
// confirms the thread is available for turns again. thread/loaded/list is
// checked first: a thread the app-server already has loaded in memory
// (e.g. a concurrent client on the same thread) doesn't need -- and per
// the real CLI, may reject -- a second thread/resume. If thread/resume
// itself reports the thread is archived, thread/unarchive is tried once
// before retrying thread/resume, exactly mirroring
// refs/paseo/packages/server/src/server/agent/providers/codex-app-server-agent.ts:3955-3990
// (ensureThreadLoaded). Any other error (e.g. an unknown/stale thread id)
// is returned as-is for the caller to fall back to a fresh thread.
func (c *Client) ResumeSession(ctx context.Context, threadID, cwd string) (string, error) {
	loaded, err := c.threadLoaded(ctx, threadID)
	if err != nil {
		return "", fmt.Errorf("codex: thread/loaded/list: %w", err)
	}
	if loaded {
		return threadID, nil
	}

	if err := c.resumeThread(ctx, threadID, cwd); err != nil {
		if !isArchivedThreadError(err, threadID) {
			return "", err
		}
		if _, uerr := c.conn.call(ctx, "thread/unarchive", threadUnarchiveParams{ThreadID: threadID}); uerr != nil {
			return "", fmt.Errorf("codex: thread/unarchive: %w", uerr)
		}
		if err := c.resumeThread(ctx, threadID, cwd); err != nil {
			return "", err
		}
	}
	return threadID, nil
}

func (c *Client) threadLoaded(ctx context.Context, threadID string) (bool, error) {
	raw, err := c.conn.call(ctx, "thread/loaded/list", struct{}{})
	if err != nil {
		return false, err
	}
	var res threadLoadedListResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	for _, id := range res.Data {
		if id == threadID {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) resumeThread(ctx context.Context, threadID, cwd string) error {
	if _, err := c.conn.call(ctx, "thread/resume", threadResumeParams{ThreadID: threadID, Cwd: cwd, ApprovalPolicy: c.approvalPolicy, Sandbox: c.sandbox, Model: c.model, ModelProvider: c.modelProvider, Config: c.threadConfig, DeveloperInstructions: c.developerInstructions}); err != nil {
		return fmt.Errorf("codex: thread/resume: %w", err)
	}
	return nil
}

// isArchivedThreadError reports whether err is the RPC error codex's
// thread/resume returns for a thread that's been archived -- the exact
// message format refs/paseo/.../codex-app-server-agent.ts:126-131 matches,
// verified live 2026-09-27 against codex-cli 0.149.1's "no archived
// rollout found"/"no rollout found" wording for the sibling not-found
// cases.
func isArchivedThreadError(err error, threadID string) bool {
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	want := fmt.Sprintf("session %s is archived. Run `codex unarchive %s` to unarchive it first.", threadID, threadID)
	return rpcErr.Message == want
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
func (c *Client) Prompt(ctx context.Context, threadID, text string, updates chan<- Update, opts ...PromptOption) (string, error) {
	var po promptOptions
	for _, o := range opts {
		o(&po)
	}
	input := []userInput{{Type: "text", Text: &text}}
	for _, p := range po.images {
		input = append(input, userInput{Type: "localImage", Path: p})
	}
	sub := &updateSub{ch: updates}
	waiter := make(chan turnResult, 1)
	c.mu.Lock()
	c.updateSubs[threadID] = sub
	c.turnWaiters[threadID] = waiter
	delete(c.tokenUsage, threadID)
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
		Input:    input,
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

// ThreadTokenUsage returns the latest tokenUsage.total threadID's current
// (or just finished) turn reported, and false if none arrived. It is the
// thread's cumulative usage as of the turn's end, not the turn's own: callers
// difference it against the previous turn's figure. Only the last
// notification is kept, not a sum: each carries the running thread total.
func (c *Client) ThreadTokenUsage(threadID string) (TokenUsage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	u, ok := c.tokenUsage[threadID]
	return u, ok
}

func (c *Client) handleTokenUsageUpdated(raw json.RawMessage) {
	var n threadTokenUsageUpdatedNotification
	if err := json.Unmarshal(raw, &n); err != nil {
		return
	}
	u := n.TokenUsage.Total
	u.ModelContextWindow = n.TokenUsage.ModelContextWindow

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.turnWaiters[n.ThreadID]; ok {
		c.tokenUsage[n.ThreadID] = u
	}
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
