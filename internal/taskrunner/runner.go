package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/codex"
	"github.com/spacingmind/smind/internal/workspace"
)

// acpBackend is the subset of *acp.Client's methods RunPrompt needs to
// drive a GLM (or other ACP) turn. It exists only so tests can substitute a
// client wired to a fake agent binary via Runner.newACPClient; production
// code always gets it from acp.New, whose real *acp.Client satisfies it.
type acpBackend interface {
	Initialize(ctx context.Context) error
	NewSession(ctx context.Context, cwd string) (string, []acp.ConfigOption, error)
	SetSessionConfigOption(ctx context.Context, sessionID, configID, value string) ([]acp.ConfigOption, error)
	Prompt(ctx context.Context, sessionID, text string, updates chan<- acp.SessionUpdate) (string, error)
	Close() error
}

// claudeBackend is the subset of *claudecode.Client's methods RunPrompt
// needs to drive a Claude Code native turn. Exists for the same
// test-substitution reason as acpBackend.
type claudeBackend interface {
	Prompt(ctx context.Context, text string, updates chan<- claudecode.Message) (claudecode.ResultMessage, error)
	Close() error
}

// codexBackend is the subset of *codex.Client's methods RunPrompt needs to
// drive a Codex-native turn. Same shape as acpBackend (Codex's client also
// needs an explicit Initialize/NewSession handshake, unlike claudeBackend),
// same test-substitution reason.
type codexBackend interface {
	Initialize(ctx context.Context) error
	NewSession(ctx context.Context, cwd string) (string, error)
	Prompt(ctx context.Context, threadID, text string, updates chan<- codex.Update) (string, error)
	Close() error
}

// Option configures a Runner constructed via New.
type Option func(*Runner)

// WithACPPermissionPolicy sets the acp.PermissionPolicy passed to every GLM
// (or other ACP) client this Runner constructs. Left unset, acp.New's own
// default (AutoApprovePolicy) applies.
func WithACPPermissionPolicy(p acp.PermissionPolicy) Option {
	return func(r *Runner) { r.acpPermissionPolicy = p }
}

// WithClaudeCodePermissionPolicy sets the claudecode.PermissionPolicy
// passed to every Claude Code native client this Runner constructs. Left
// unset, claudecode.New's own default (AutoDenyPolicy) applies.
func WithClaudeCodePermissionPolicy(p claudecode.PermissionPolicy) Option {
	return func(r *Runner) { r.claudePermissionPolicy = p }
}

// WithACPCommand overrides the command spawned for provider's turns, in
// place of its default (acp.GLMCommand() for ProviderGLM,
// acp.KimiCommand() for ProviderKimi). This is the only seam RunPrompt's
// ACP path exposes for pointing a given ACP-speaking provider at something
// other than its real agent: acpBackend/claudeBackend and the
// newACPClient/newClaudeClient fields that satisfy them from within this
// package's own tests are unexported, so a caller in another package (e.g.
// internal/server's tests, which need to drive RunPrompt without a real
// `npx`/GLM or `pip`/Kimi install) has no way to substitute a fake backend
// directly. Overriding just the command -- letting it point at a compiled
// fake-agent binary that still speaks real ACP over stdio -- covers that
// need without exporting the backend interfaces themselves. Each provider's
// override is independent: overriding ProviderGLM's command has no effect
// on ProviderKimi's, and vice versa.
func WithACPCommand(provider Provider, command []string) Option {
	return func(r *Runner) { r.acpCommands[provider] = command }
}

// WithCodexPermissionPolicy sets the codex.PermissionPolicy passed to every
// Codex-native client this Runner constructs. Left unset, codex.New's own
// default (AutoApprovePolicy) applies.
func WithCodexPermissionPolicy(p codex.PermissionPolicy) Option {
	return func(r *Runner) { r.codexPermissionPolicy = p }
}

// WithCodexCommand overrides the command spawned for ProviderCodexNative
// turns, in place of the default codex.DefaultCommand(). Same reasoning and
// use (pointing at a compiled fake-agent binary in tests) as
// WithACPCommand.
func WithCodexCommand(command []string) Option {
	return func(r *Runner) { r.codexCommand = command }
}

// Runner drives task turns against a real agent backend (ACP or Claude Code
// native), translating each backend's native streaming updates into the
// unified Event type.
//
// Permission policy is a Runner-level default rather than a RunPrompt
// parameter: acp.PermissionPolicy and claudecode.PermissionPolicy are
// already the real seam for this decision (an option-list vs. an
// allow/deny/updated-input decision, shaped that way for good
// protocol-specific reasons in their own packages), and nothing here needs
// a third, unified policy abstraction on top of them -- there's no caller
// yet whose requirements would justify designing one. RunPrompt's own
// signature is fixed by callers above this layer (task id, provider,
// prompt, events); a policy is deployment-wide configuration, not a
// per-call decision, so it belongs at construction time, consistent with
// how acp.New and claudecode.New already take it as an option rather than
// a per-call argument.
type Runner struct {
	wm *workspace.Manager

	acpPermissionPolicy    acp.PermissionPolicy
	claudePermissionPolicy claudecode.PermissionPolicy
	codexPermissionPolicy  codex.PermissionPolicy

	// acpCommands maps each ACP-speaking provider to the command spawned for
	// its turns. Seeded in New with every known ACP provider's real default
	// (acp.GLMCommand(), acp.KimiCommand()); overridable per-provider via
	// WithACPCommand. A provider with no entry (shouldn't happen for any
	// Provider constant this package defines) fails fast in runACP rather
	// than spawning an empty command.
	acpCommands map[Provider][]string

	// codexCommand is the command spawned for ProviderCodexNative turns.
	// Defaults to codex.DefaultCommand(); overridable via WithCodexCommand.
	// Not part of acpCommands: Codex isn't an ACP-speaking provider (see
	// internal/codex's package doc comment), so it doesn't belong in that
	// map.
	codexCommand []string

	// acpSessions tracks each task's most recent ACP session (see
	// acpSessionState in config_options.go), keyed by task ID, so
	// ConfigOptions/SetSessionConfigOption can reach a task's live session
	// without RunPrompt handing its per-turn client to anyone. Guarded by
	// sessionMu.
	acpSessions map[int64]*acpSessionState
	sessionMu   sync.Mutex

	// newACPClient, newClaudeClient, and newCodexClient default to wrapping
	// acp.New, claudecode.New, and codex.New. Overridable only from within
	// this package's tests, to point at a fake agent binary / fake CLI
	// instead of a real one -- none of the three client packages expose a
	// constructor seam of their own, and a broader public abstraction isn't
	// warranted for a need this narrow.
	newACPClient    func(command []string, opts ...acp.Option) (acpBackend, error)
	newClaudeClient func(worktreePath string, opts ...claudecode.Option) (claudeBackend, error)
	newCodexClient  func(command []string, opts ...codex.Option) (codexBackend, error)
}

// New returns a Runner backed by wm.
func New(wm *workspace.Manager, opts ...Option) *Runner {
	r := &Runner{
		wm:          wm,
		acpSessions: map[int64]*acpSessionState{},
		acpCommands: map[Provider][]string{
			ProviderGLM:  acp.GLMCommand(),
			ProviderKimi: acp.KimiCommand(),
		},
		codexCommand: codex.DefaultCommand(),
		newACPClient: func(command []string, opts ...acp.Option) (acpBackend, error) {
			return acp.New(command, opts...)
		},
		newClaudeClient: func(worktreePath string, opts ...claudecode.Option) (claudeBackend, error) {
			return claudecode.New(worktreePath, opts...)
		},
		newCodexClient: func(command []string, opts ...codex.Option) (codexBackend, error) {
			return codex.New(command, opts...)
		},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// RunPrompt looks up taskID, spawns the agent backend named by provider
// rooted at the task's worktree, and drives one prompt turn: every text
// chunk the backend streams is translated into an Event and forwarded onto
// events as it arrives, followed by exactly one final EventTypeDone once
// the turn completes. events is always closed before RunPrompt returns,
// whether it returns an error or not, so a caller can unconditionally range
// over it.
//
// decider, if non-nil, overrides the Runner-level acp.PermissionPolicy/
// claudecode.PermissionPolicy default for this call only -- see
// PermissionDecider's doc comment for why a human-in-the-loop decider is
// inherently per-call rather than Runner-wide configuration. nil preserves
// today's behavior exactly: each provider falls through to its own
// Runner-level default.
//
// approvalPolicy is the run's ApprovalPolicy (see policy.go). For
// ApprovalPolicyManual/ApprovalPolicyAutoSafe it only matters when decider
// is non-nil, and only widens what may run without a human --
// ApprovalPolicyAutoSafe pre-approves the allowlisted verification commands
// at the provider's own gate (see SafeBashRules for why the decider-side
// check alone is not enough for claude-native) in addition to
// auto-allowing them in the decider itself. The zero value
// (ApprovalPolicyManual) preserves today's behavior exactly.
// ApprovalPolicyFullAccess is different: it takes priority over decider
// entirely -- no decider is installed at all, regardless of whether the
// caller supplied one, and each provider's own native "auto-approve
// everything" mechanism is used instead (see runClaudeNative/
// runCodexNative/runACP).
//
// thinkingLevel is Claude-only (see ThinkingLevel's doc comment): every
// other provider ignores it entirely, regardless of what it's set to.
// ThinkingLevelUnspecified (the zero value) preserves today's behavior
// exactly -- no thinking Option is added to the Claude Code session at all.
//
// The backend client spawned for this call is not reused: RunPrompt owns
// its subprocess end to end and closes it before returning. ctx cancellation
// propagates into the backend's turn call, aborting it, after which the
// client is still closed as normal -- so a cancelled RunPrompt does not
// leak the subprocess.
func (r *Runner) RunPrompt(ctx context.Context, taskID int64, provider Provider, prompt string, decider PermissionDecider, approvalPolicy ApprovalPolicy, thinkingLevel ThinkingLevel, events chan<- Event) error {
	defer close(events)

	task, err := r.wm.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("taskrunner: get task %d: %w", taskID, err)
	}
	if task.WorktreePath == nil {
		return fmt.Errorf("taskrunner: task %d has no worktree (never materialized by CreateTask, or already archived)", taskID)
	}
	worktreePath := *task.WorktreePath

	switch provider {
	case ProviderGLM, ProviderKimi:
		return r.runACP(ctx, taskID, provider, worktreePath, prompt, decider, approvalPolicy, events)
	case ProviderClaudeNative:
		return r.runClaudeNative(ctx, worktreePath, prompt, decider, approvalPolicy, thinkingLevel, events)
	case ProviderCodexNative:
		return r.runCodexNative(ctx, worktreePath, prompt, decider, approvalPolicy, events)
	default:
		return fmt.Errorf("taskrunner: unknown provider %q", provider)
	}
}

// runACP drives one turn for any ACP-speaking provider (ProviderGLM,
// ProviderKimi, ...): they differ only in which command r.acpCommands maps
// them to -- everything else about the ACP session/prompt/streaming flow is
// identical, since it's the same wire protocol regardless of which agent is
// on the other end of it.
func (r *Runner) runACP(ctx context.Context, taskID int64, provider Provider, worktreePath, prompt string, decider PermissionDecider, approvalPolicy ApprovalPolicy, events chan<- Event) error {
	command, ok := r.acpCommands[provider]
	if !ok {
		return fmt.Errorf("taskrunner: no ACP command configured for provider %q", provider)
	}

	var opts []acp.Option
	switch {
	case approvalPolicy == ApprovalPolicyFullAccess:
		// ACP has no native permission-mode concept to defer to (unlike
		// Claude/Codex) -- AutoApprovePolicy answering allow_once/
		// allow_always on every request already is this provider's real
		// ceiling. No decider is installed at all, same as the other two
		// providers' full-access branch.
		opts = append(opts, acp.WithPermissionPolicy(acp.AutoApprovePolicy{}))
	case decider != nil:
		opts = append(opts, acp.WithPermissionPolicy(acpDeciderAdapter{decider: decider, worktreePath: worktreePath, approvalPolicy: approvalPolicy}))
	case r.acpPermissionPolicy != nil:
		opts = append(opts, acp.WithPermissionPolicy(r.acpPermissionPolicy))
	}

	client, err := r.newACPClient(command, opts...)
	if err != nil {
		return fmt.Errorf("taskrunner: spawn %s agent: %w", provider, err)
	}
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("taskrunner: initialize %s agent: %w", provider, err)
	}
	sessionID, configOptions, err := client.NewSession(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("taskrunner: %s new session: %w", provider, err)
	}
	r.trackACPSession(taskID, sessionID, client, configOptions)
	defer r.endACPTurn(taskID)

	updates := make(chan acp.SessionUpdate)
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for u := range updates {
			e, ok := acpEvent(u)
			if !ok {
				continue
			}
			select {
			case events <- e:
			case <-ctx.Done():
			}
		}
	}()

	stopReason, err := client.Prompt(ctx, sessionID, prompt, updates)
	<-forwardDone
	if err != nil {
		return fmt.Errorf("taskrunner: %s prompt: %w", provider, err)
	}

	select {
	case events <- Event{Type: EventTypeDone, StopReason: stopReason}:
	case <-ctx.Done():
	}
	return nil
}

// acpEvent translates one ACP SessionUpdate into its taskrunner.Event.
// Every update is forwarded: a recognized text-chunk or tool-call kind
// becomes its typed event; a recognized text-chunk kind whose content
// isn't a text block (e.g. an image/audio/resource chunk -- see
// acp.SessionUpdate's doc comment) is still dropped, since that's a
// narrower, out-of-scope gap this package's scope doesn't cover; anything
// else -- an update kind acpEvent doesn't recognize at all, such as ACP's
// "plan" updates or a kind a future ACP revision adds -- becomes
// EventTypeRaw rather than being silently dropped. See
// docs/decisions/0010-preserve-unknown-acp-event-kinds.md.
func acpEvent(u acp.SessionUpdate) (Event, bool) {
	if text, ok := u.Text(); ok {
		switch u.Type {
		case acp.SessionUpdateUserMessageChunk:
			return Event{Type: EventTypeUserMessage, Text: text, Raw: u}, true
		case acp.SessionUpdateAgentThoughtChunk:
			return Event{Type: EventTypeThinking, Text: text, Raw: u}, true
		default: // acp.SessionUpdateAgentMessageChunk
			return Event{Type: EventTypeText, Text: text, Raw: u}, true
		}
	}
	if u.IsToolCall() {
		status := acpToolStatus(u.Status)
		if status == "" && u.Type == acp.SessionUpdateToolCall {
			// An initial tool_call announces a call that by definition
			// hasn't finished, and ACP's ToolCall.status is optional with
			// a "pending" default -- so an absent (or unrecognized, e.g.
			// a status added by a later ACP revision) status here means
			// "running", not "no status at all". Only tool_call_update's
			// absent status genuinely means "unchanged".
			status = ToolStatusRunning
		}
		return Event{
			Type:       EventTypeToolCall,
			Raw:        u,
			ToolCallID: u.ToolCallID,
			ToolName:   u.Kind,
			ToolTitle:  u.Title,
			ToolStatus: status,
			ToolInput:  u.RawInput,
			ToolResult: u.Content,
		}, true
	}
	switch u.Type {
	case acp.SessionUpdateUserMessageChunk, acp.SessionUpdateAgentMessageChunk, acp.SessionUpdateAgentThoughtChunk:
		// A recognized text-chunk kind, but Text() couldn't extract a text
		// block -- a non-text content block, not an unrecognized kind. Out
		// of scope for this ADR (see the doc comment above); still dropped.
		return Event{}, false
	default:
		return Event{Type: EventTypeRaw, Raw: u, RawKind: u.Type, RawPayload: u.Raw}, true
	}
}

// acpToolStatus maps ACP's four-value ToolCallStatus to taskrunner's
// unified three-value ToolStatus -- see ToolStatusRunning's doc comment
// for why "pending" and "in_progress" collapse together. An update that
// doesn't repeat status (a partial tool_call_update) maps "" to "", which
// EventTypeToolCall's own doc comment documents as "no change reported".
func acpToolStatus(status string) string {
	switch status {
	case "completed":
		return ToolStatusSuccess
	case "failed":
		return ToolStatusFailure
	case "pending", "in_progress":
		return ToolStatusRunning
	default:
		return ""
	}
}

// claudeDialogTimeoutEnv is the env var the real `claude` CLI reads for its
// own internal auto-deny deadline on an unanswered permission dialog
// (can_use_tool control request). When the CLI's deadline fires it sends
// control_cancel_request, cancelling the per-request ctx inside
// runPermissionDecider.Decide out from under smind -- see
// docs/plans/active/claude-native-permission-cancellation.md.
const claudeDialogTimeoutEnv = "CLAUDE_CODE_USER_DIALOG_TIMEOUT_MS"

// claudeDialogTimeoutMS is the value wired into the CLI when a human
// decider is attached: 60 minutes, comfortably above internal/runs'
// 5-minute defaultPermissionTimeout (registry.go) so smind's own timeout
// is always the one that fires first and gets its distinguishable
// PermissionResolvedByTimeout event. Defined locally, not imported from
// internal/runs, because runs imports taskrunner (import cycle).
const claudeDialogTimeoutMS = "3600000"

func (r *Runner) runClaudeNative(ctx context.Context, worktreePath, prompt string, decider PermissionDecider, approvalPolicy ApprovalPolicy, thinkingLevel ThinkingLevel, events chan<- Event) error {
	var opts []claudecode.Option
	switch thinkingLevel {
	case ThinkingLevelOff:
		opts = append(opts, claudecode.WithDisabledThinking())
	case ThinkingLevelStandard:
		opts = append(opts, claudecode.WithAdaptiveThinking())
	case ThinkingLevelExtended:
		opts = append(opts, claudecode.WithThinkingBudget(extendedThinkingBudgetTokens))
	case ThinkingLevelUnspecified:
		// No thinking Option at all -- preserves today's SDK default
		// exactly, for an older client or any request that never set the
		// field.
	}
	switch {
	case approvalPolicy == ApprovalPolicyFullAccess:
		// No decider at all -- the CLI's own bypassPermissions mode is
		// Claude Code's real "skip every permission prompt" mode, the same
		// one its own CLI exposes under that name.
		opts = append(opts, claudecode.WithPermissionMode("bypassPermissions"))
	case decider != nil:
		// A human decider is wired up, so ask the CLI for permission-mode
		// "acceptEdits" (not its "default"): under "default", the CLI blocks
		// file edits itself without ever emitting a can_use_tool request,
		// so a headless run with a decider silently loses every edit --
		// the exact dogfood failure that motivated this (2026-09-11).
		// "acceptEdits" lets edits through and still routes Bash and other
		// sensitive tools through can_use_tool -> the decider -> the UI.
		opts = append(opts,
			claudecode.WithPermissionMode("acceptEdits"),
			claudecode.WithPermissionPolicy(claudeDeciderAdapter{decider}),
		)
		// The CLI's own session gate blocks allowlisted Bash commands
		// before can_use_tool ever fires (see SafeBashRules), so under
		// auto-safe the allowlist has to be handed to the CLI at spawn
		// time too -- the decider-side check alone only helps ACP/codex
		// backends, whose permission flow starts at the decider.
		if approvalPolicy == ApprovalPolicyAutoSafe {
			opts = append(opts, claudecode.WithAllowedTools(SafeBashRules()...))
		}
		// Hold the CLI's own permission-dialog deadline open well past
		// smind's 5-minute manual-approval window, so the CLI's internal
		// auto-deny fallback can't silently cancel a pending request
		// before a human answers (see claudeDialogTimeoutEnv). Only in the
		// decider branch: the Runner-level policy defaults answer
		// programmatically and never wait on a dialog. An explicit value
		// already in the environment is forwarded as-is instead -- a
		// deployment-wide escape hatch.
		if v := os.Getenv(claudeDialogTimeoutEnv); v != "" {
			opts = append(opts, claudecode.WithEnv(claudeDialogTimeoutEnv+"="+v))
		} else {
			opts = append(opts, claudecode.WithEnv(claudeDialogTimeoutEnv+"="+claudeDialogTimeoutMS))
		}
	case r.claudePermissionPolicy != nil:
		opts = append(opts, claudecode.WithPermissionPolicy(r.claudePermissionPolicy))
	}

	client, err := r.newClaudeClient(worktreePath, opts...)
	if err != nil {
		return fmt.Errorf("taskrunner: spawn claude code agent: %w", err)
	}
	defer client.Close()

	updates := make(chan claudecode.Message)
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for msg := range updates {
			for _, e := range claudeEvents(msg) {
				select {
				case events <- e:
				case <-ctx.Done():
				}
			}
		}
	}()

	result, err := client.Prompt(ctx, prompt, updates)
	<-forwardDone
	if err != nil {
		return fmt.Errorf("taskrunner: claude code prompt: %w", err)
	}

	select {
	case events <- Event{Type: EventTypeDone, StopReason: result.StopReason, Raw: result}:
	case <-ctx.Done():
	}
	return nil
}

// claudeEvents translates one claudecode.Message into zero or more
// taskrunner.Events. Both AssistantMessage and UserMessage are walked the
// same way -- the CLI decodes both with the same block decoder, and a
// tool_result block can arrive on either one (see claudecode's
// ToolResultBlock doc comment) -- differing only in that a UserMessage's
// text blocks are dropped rather than becoming EventTypeText: a user turn
// is either an echo of the human's own prompt (which the UI already has,
// see EventTypeUserMessage's doc comment) or the envelope carrying tool
// results back to the model, never new assistant prose. SystemMessage,
// ResultMessage (handled separately by runClaudeNative itself), and every
// other message type yield nothing.
func claudeEvents(msg claudecode.Message) []Event {
	var (
		blocks    []claudecode.ContentBlock
		assistant bool
	)
	switch m := msg.(type) {
	case claudecode.AssistantMessage:
		blocks, assistant = m.Content, true
	case claudecode.UserMessage:
		blocks = m.Content
	default:
		return nil
	}

	var out []Event
	for _, block := range blocks {
		switch b := block.(type) {
		case claudecode.TextBlock:
			if !assistant {
				continue
			}
			out = append(out, Event{Type: EventTypeText, Text: b.Text, Raw: msg})
		case claudecode.ThinkingBlock:
			out = append(out, Event{Type: EventTypeThinking, Text: b.Thinking, Raw: msg})
		case claudecode.ToolUseBlock:
			out = append(out, claudeToolUseEvent(msg, b.ID, b.Name, b.Input))
		case claudecode.ServerToolUseBlock:
			// A server-side tool (WebSearch/WebFetch) is still a tool call
			// as far as a timeline card is concerned -- same id/name/input
			// shape, just executed by the CLI rather than locally.
			out = append(out, claudeToolUseEvent(msg, b.ID, b.Name, b.Input))
		case claudecode.ToolResultBlock:
			status := ToolStatusSuccess
			if b.IsError {
				status = ToolStatusFailure
			}
			out = append(out, Event{
				Type:       EventTypeToolCall,
				Raw:        msg,
				ToolCallID: b.ToolUseID,
				ToolStatus: status,
				ToolResult: b.Content,
			})
		case claudecode.ServerToolResultBlock:
			// No IsError equivalent on the wire for these -- an errored
			// server tool reports the failure inside Content instead.
			out = append(out, Event{
				Type:       EventTypeToolCall,
				Raw:        msg,
				ToolCallID: b.ToolUseID,
				ToolStatus: ToolStatusSuccess,
				ToolResult: b.Content,
			})
		}
	}
	return out
}

// claudeToolUseEvent builds the "call started" event shared by
// ToolUseBlock and ServerToolUseBlock.
func claudeToolUseEvent(msg claudecode.Message, id, name string, input map[string]any) Event {
	// json.Marshal on a map[string]any built by decoding the CLI's own
	// NDJSON never fails, so the error is ignored -- same reasoning as
	// every other json.Marshal call in this package that round-trips
	// already-decoded provider data.
	raw, _ := json.Marshal(input)
	return Event{
		Type:       EventTypeToolCall,
		Raw:        msg,
		ToolCallID: id,
		ToolName:   name,
		ToolStatus: ToolStatusRunning,
		ToolInput:  raw,
	}
}

// runCodexNative drives one turn against a Codex-native agent (internal/codex).
// Shaped like runACP (an explicit Initialize/NewSession handshake, unlike
// runClaudeNative), since codex.Client needs the same two-step setup ACP
// clients do.
func (r *Runner) runCodexNative(ctx context.Context, worktreePath, prompt string, decider PermissionDecider, approvalPolicy ApprovalPolicy, events chan<- Event) error {
	var opts []codex.Option
	switch {
	case approvalPolicy == ApprovalPolicyFullAccess:
		// No decider at all -- codex.AutoApprovePolicy{} auto-resolves every
		// commandExecution/fileChange approval request, the closest smind
		// gets to driving Codex's own never/danger-full-access combination
		// without owning its native approval_policy/sandbox_mode config
		// surface (see docs/plans/active/task-move-approval-thinking.md).
		opts = append(opts, codex.WithPermissionPolicy(codex.AutoApprovePolicy{}))
	case decider != nil:
		opts = append(opts, codex.WithPermissionPolicy(codexDeciderAdapter{decider}))
	case r.codexPermissionPolicy != nil:
		opts = append(opts, codex.WithPermissionPolicy(r.codexPermissionPolicy))
	}

	client, err := r.newCodexClient(r.codexCommand, opts...)
	if err != nil {
		return fmt.Errorf("taskrunner: spawn codex agent: %w", err)
	}
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("taskrunner: initialize codex agent: %w", err)
	}
	threadID, err := client.NewSession(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("taskrunner: codex new thread: %w", err)
	}

	updates := make(chan codex.Update)
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for u := range updates {
			select {
			case events <- Event{Type: EventTypeText, Text: u.Text, Raw: u}:
			case <-ctx.Done():
			}
		}
	}()

	stopReason, err := client.Prompt(ctx, threadID, prompt, updates)
	<-forwardDone
	if err != nil {
		return fmt.Errorf("taskrunner: codex prompt: %w", err)
	}

	select {
	case events <- Event{Type: EventTypeDone, StopReason: stopReason}:
	case <-ctx.Done():
	}
	return nil
}

// CommitTask commits whatever is currently staged in taskID's worktree
// under an agent-authored message -- the same commit primitive the UI's
// task.commit uses, plus the ADR 0006 Smind-Agent/Smind-Task trailers so
// review tooling can filter agent commits. provider names the committing
// agent and lands in the Smind-Agent trailer.
//
// Deliberately a taskrunner-level helper, not an agent-visible tool: no
// agent asks for it today, so exposing it over the agent protocol would be
// speculative surface (see docs/plans/active/commit-flow.md's Decisions).
func (r *Runner) CommitTask(taskID int64, provider Provider, message string) (workspace.CommitResult, error) {
	result, err := r.wm.CommitTask(taskID, message, "agent", string(provider))
	if err != nil {
		return workspace.CommitResult{}, fmt.Errorf("taskrunner: commit task %d: %w", taskID, err)
	}
	return result, nil
}
