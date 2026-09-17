package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// protocolVersion is the ACP wire protocol version this client speaks,
// negotiated during initialize. See
// refs/agent-client-protocol/schema/v1/schema.json's ProtocolVersion: v1
// agents expect the integer 1 here, not a schema/package version.
const protocolVersion = 1

// closeTimeout bounds how long Close waits for the agent to exit after its
// stdin is closed before force-killing the subprocess.
const closeTimeout = 5 * time.Second

// ContentBlock is a single item of displayable content, per ACP's
// ContentBlock. Only the "text" variant is populated by this package: it's
// the only content type Prompt sends and the only one this package's
// SessionUpdate.Text helper understands. Other variants (image, audio,
// resource, resource_link) round-trip through SessionUpdate.Raw instead of
// getting dedicated fields, since nothing in this package's scope produces
// or consumes them.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// SessionUpdate is a client-side view of an ACP SessionUpdate notification
// payload. It decodes the fields common to the streaming-text and tool-call
// variants (the ones this package's scope cares about); Raw preserves the
// full JSON of the update for callers that need a variant's other fields
// (plan, available commands, mode/config changes, etc).
type SessionUpdate struct {
	Type       string          `json:"sessionUpdate"`
	Content    json.RawMessage `json:"content,omitempty"`
	MessageID  string          `json:"messageId,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	Title      string          `json:"title,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Status     string          `json:"status,omitempty"`
	// RawInput is a tool_call/tool_call_update's rawInput field: the
	// tool's arguments, in whatever shape the agent reported them. Absent
	// (nil) for every other update Type, and for a SessionUpdateToolCallUpdate
	// that doesn't repeat it -- ACP's tool_call_update only carries changed
	// fields (see ACP's ToolCallUpdate schema), so its absence means
	// "unchanged", not "cleared".
	RawInput json.RawMessage `json:"rawInput,omitempty"`
	Raw      json.RawMessage `json:"-"`
}

// SessionUpdate.Type values, per ACP's SessionUpdate discriminator.
const (
	SessionUpdateUserMessageChunk  = "user_message_chunk"
	SessionUpdateAgentMessageChunk = "agent_message_chunk"
	SessionUpdateAgentThoughtChunk = "agent_thought_chunk"
	SessionUpdateToolCall          = "tool_call"
	SessionUpdateToolCallUpdate    = "tool_call_update"
	SessionUpdatePlan              = "plan"
)

// Text returns the text payload of a streaming chunk update (user message,
// agent message, or agent thought), and false for any other update type or
// for a chunk whose content isn't a text block.
func (u SessionUpdate) Text() (string, bool) {
	switch u.Type {
	case SessionUpdateUserMessageChunk, SessionUpdateAgentMessageChunk, SessionUpdateAgentThoughtChunk:
	default:
		return "", false
	}
	var block ContentBlock
	if err := json.Unmarshal(u.Content, &block); err != nil || block.Type != "text" {
		return "", false
	}
	return block.Text, true
}

// IsToolCall reports whether u is a tool_call or tool_call_update update --
// the two ACP variants a caller translates into a taskrunner.EventTypeToolCall.
func (u SessionUpdate) IsToolCall() bool {
	return u.Type == SessionUpdateToolCall || u.Type == SessionUpdateToolCallUpdate
}

type sessionNotificationParams struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

type clientCapabilities struct {
	FS fileSystemCapabilities `json:"fs"`
}

type fileSystemCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type initializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities clientCapabilities `json:"clientCapabilities"`
}

type initializeResult struct {
	ProtocolVersion   int             `json:"protocolVersion"`
	AgentCapabilities json.RawMessage `json:"agentCapabilities"`
}

type newSessionParams struct {
	Cwd        string `json:"cwd"`
	McpServers []any  `json:"mcpServers"`
}

type newSessionResult struct {
	SessionID     string         `json:"sessionId"`
	ConfigOptions []ConfigOption `json:"configOptions"`
}

// ConfigOption mirrors ACP v2's SessionConfigOption: one entry of the
// configOptions list a session/new response may carry. CurrentValue holds
// the flattened kind's currentValue (a value id for select options, a bool
// for boolean options) keyed by Type; other kind fields are not captured.
type ConfigOption struct {
	ConfigID     string          `json:"configId"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Category     string          `json:"category,omitempty"`
	Type         string          `json:"type"`
	CurrentValue json.RawMessage `json:"currentValue,omitempty"`
}

// setConfigOptionParams is the wire shape of ACP v2's
// SetSessionConfigOptionRequest: sessionId, configId, and a flattened
// SessionConfigOptionValue (type discriminator + value). This package
// only sends type "id" values -- the option kind every config option
// smind surfaces so far (e.g. GLM's thinking level) uses.
type setConfigOptionParams struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Type      string `json:"type"`
	Value     string `json:"value"`
}

type setConfigOptionResult struct {
	ConfigOptions []ConfigOption `json:"configOptions"`
}

type promptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type promptResult struct {
	StopReason string `json:"stopReason"`
}

// Client is an ACP client: it spawns an agent subprocess, performs the
// initialize/session handshake, and drives prompt turns while answering the
// agent's filesystem and permission callbacks.
type Client struct {
	conn      *conn
	policy    PermissionPolicy
	logWriter io.Writer

	mu         sync.Mutex
	sessionCwd map[string]string
	updateSubs map[string]*updateSub

	ProtocolVersion   int
	AgentCapabilities json.RawMessage
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

// New spawns command[0] with command[1:] as arguments and wires up an ACP
// connection to it, registering this Client's fs/* and
// session/request_permission handlers. Call Initialize before anything
// else.
func New(command []string, opts ...Option) (*Client, error) {
	c := &Client{
		policy:     AutoApprovePolicy{},
		logWriter:  io.Discard,
		sessionCwd: make(map[string]string),
		updateSubs: make(map[string]*updateSub),
	}
	for _, opt := range opts {
		opt(c)
	}

	conn, err := newConn(command, c.logWriter)
	if err != nil {
		return nil, err
	}
	c.conn = conn

	conn.handleRequest("fs/read_text_file", c.handleReadTextFile)
	conn.handleRequest("fs/write_text_file", c.handleWriteTextFile)
	conn.handleRequest("session/request_permission", c.handleRequestPermission)
	conn.handleNotification("session/update", c.handleSessionUpdate)

	return c, nil
}

// Initialize performs the ACP initialize handshake, advertising fs
// read/write support (this package implements both callbacks) and no
// terminal support (out of scope). It stores the agent's negotiated
// protocol version and capabilities on the Client.
func (c *Client) Initialize(ctx context.Context) error {
	raw, err := c.conn.call(ctx, "initialize", initializeParams{
		ProtocolVersion: protocolVersion,
		ClientCapabilities: clientCapabilities{
			FS: fileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		},
	})
	if err != nil {
		return fmt.Errorf("acp: initialize: %w", err)
	}

	var res initializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("acp: initialize: decode response: %w", err)
	}
	c.ProtocolVersion = res.ProtocolVersion
	c.AgentCapabilities = res.AgentCapabilities
	return nil
}

// NewSession creates a new ACP session rooted at cwd (the task's worktree
// path), which also becomes the filesystem boundary that fs/read_text_file
// and fs/write_text_file requests for this session are validated against.
// It also returns the session's initial config options, if the agent sent
// any.
func (c *Client) NewSession(ctx context.Context, cwd string) (string, []ConfigOption, error) {
	raw, err := c.conn.call(ctx, "session/new", newSessionParams{Cwd: cwd, McpServers: []any{}})
	if err != nil {
		return "", nil, fmt.Errorf("acp: session/new: %w", err)
	}

	var res newSessionResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", nil, fmt.Errorf("acp: session/new: decode response: %w", err)
	}

	c.mu.Lock()
	c.sessionCwd[res.SessionID] = cwd
	c.mu.Unlock()

	return res.SessionID, res.ConfigOptions, nil
}

// SetSessionConfigOption sets one config option on a live session via ACP
// v2's session/set_config_option, returning the full list of the session's
// config options with their current values as the agent reports them after
// the change. Both a transport failure and an agent-side JSON-RPC error
// response (e.g. an unknown configId) come back as a non-nil error.
func (c *Client) SetSessionConfigOption(ctx context.Context, sessionID, configID, value string) ([]ConfigOption, error) {
	raw, err := c.conn.call(ctx, "session/set_config_option", setConfigOptionParams{
		SessionID: sessionID,
		ConfigID:  configID,
		Type:      "id",
		Value:     value,
	})
	if err != nil {
		return nil, fmt.Errorf("acp: session/set_config_option: %w", err)
	}

	var res setConfigOptionResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("acp: session/set_config_option: decode response: %w", err)
	}
	return res.ConfigOptions, nil
}

// updateSub tracks one session's subscriber channel plus a WaitGroup of
// handleSessionUpdate sends currently in flight against it. Prompt's cleanup
// waits on that WaitGroup after removing the subscription (guaranteeing no
// handleSessionUpdate call can newly start one) and before closing the
// channel -- see Prompt for why this matters.
type updateSub struct {
	ch chan<- SessionUpdate
	wg sync.WaitGroup
}

// Prompt sends text as a single user message to sessionID and blocks until
// the agent finishes the turn. While the request is in flight, every
// session/update the agent sends for this session is forwarded onto
// updates as it arrives, in order; updates is closed when Prompt returns,
// whether it returns an error or not.
//
// If ctx is cancelled, call returns as soon as it observes ctx.Done(), which
// can happen before the read loop goroutine has finished delivering a
// session/update that was already in flight for this session -- e.g. one
// the agent sent just before Prompt gave up waiting. Without waiting for
// that delivery to finish first, closing updates here could race with
// handleSessionUpdate's send on the same channel and panic ("send on closed
// channel"). sub.wg (incremented under the same lock as the subscriber
// lookup, so it can't race with the delete below) makes that impossible:
// once delete has run, no new send can start, and wg.Wait() blocks until
// every send that did start has finished.
func (c *Client) Prompt(ctx context.Context, sessionID, text string, updates chan<- SessionUpdate) (string, error) {
	sub := &updateSub{ch: updates}
	c.mu.Lock()
	c.updateSubs[sessionID] = sub
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.updateSubs, sessionID)
		c.mu.Unlock()
		sub.wg.Wait()
		close(updates)
	}()

	raw, err := c.conn.call(ctx, "session/prompt", promptParams{
		SessionID: sessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: text}},
	})
	if err != nil {
		return "", fmt.Errorf("acp: session/prompt: %w", err)
	}

	var res promptResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("acp: session/prompt: decode response: %w", err)
	}
	return res.StopReason, nil
}

func (c *Client) handleSessionUpdate(raw json.RawMessage) {
	var params sessionNotificationParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return
	}

	var update SessionUpdate
	if err := json.Unmarshal(params.Update, &update); err != nil {
		return
	}
	update.Raw = params.Update

	c.mu.Lock()
	sub, ok := c.updateSubs[params.SessionID]
	if ok {
		sub.wg.Add(1)
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	defer sub.wg.Done()
	sub.ch <- update
}

func (c *Client) sessionRoot(sessionID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	root, ok := c.sessionCwd[sessionID]
	return root, ok
}

// Close closes the connection to the agent, giving it closeTimeout to exit
// cleanly after its stdin is closed before force-killing the subprocess.
func (c *Client) Close() error {
	return c.conn.close(closeTimeout)
}
