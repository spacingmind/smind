package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/claudecli"
	"github.com/spacingmind/smind/internal/codex"
	"github.com/spacingmind/smind/internal/workspace"
)

// acpBackend is the subset of *acp.Client's methods RunPrompt needs to
// drive a GLM (or other ACP) turn. It exists only so tests can substitute a
// client wired to a fake agent binary via Runner.newACPClient; production
// code always gets it from acp.New, whose real *acp.Client satisfies it.
type acpBackend interface {
	Initialize(ctx context.Context) error
	NewSession(ctx context.Context, cwd string, mcpServers []any) (string, []acp.ConfigOption, error)
	LoadSession(ctx context.Context, sessionID, cwd string, mcpServers []any) ([]acp.ConfigOption, error)
	ResumeSession(ctx context.Context, sessionID, cwd string, mcpServers []any) ([]acp.ConfigOption, error)
	SupportsLoadSession() bool
	SupportsResumeSession() bool
	SupportsMcpStdio() bool
	SupportsMcpHttp() bool
	SupportsMcpSSE() bool
	SetSessionConfigOption(ctx context.Context, sessionID, configID, value string) ([]acp.ConfigOption, error)
	SessionModes(sessionID string) (acp.SessionModeState, bool)
	SetSessionMode(ctx context.Context, sessionID, modeID string) error
	Prompt(ctx context.Context, sessionID, text string, updates chan<- acp.SessionUpdate, opts ...acp.PromptOption) (string, error)
	PromptUsage(sessionID string) (acp.PromptUsage, bool)
	// InstanceID identifies the agent process behind this client.
	InstanceID() string
	Close() error
}

// claudeBackend is the subset of *claudecode.Client's methods RunPrompt
// needs to drive a Claude Code native turn. Exists for the same
// test-substitution reason as acpBackend.
type claudeBackend interface {
	Prompt(ctx context.Context, text string, updates chan<- claudecode.Message) (claudecode.ResultMessage, error)
	SetPermissionMode(ctx context.Context, mode string) error
	SetModel(ctx context.Context, model *string) error
	Close() error
}

// codexBackend is the subset of *codex.Client's methods RunPrompt needs to
// drive a Codex-native turn. Same shape as acpBackend (Codex's client also
// needs an explicit Initialize/NewSession handshake, unlike claudeBackend),
// same test-substitution reason.
type codexBackend interface {
	Initialize(ctx context.Context) error
	NewSession(ctx context.Context, cwd string) (string, error)
	ResumeSession(ctx context.Context, threadID, cwd string) (string, error)
	Prompt(ctx context.Context, threadID, text string, updates chan<- codex.Update) (string, error)
	ThreadTokenUsage(threadID string) (codex.TokenUsage, bool)
	ListModels(ctx context.Context) ([]codex.ModelInfo, error)
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

// WithClaudeCLIPath makes claude-native runs (RunPrompt and RunPromptIsolated)
// start the CLI at path, with env appended to the CLI's environment, instead
// of the platform default. The seam for tests that re-execute their own
// binary as the fake CLI (internal/taskrunner/fakeclaude).
func WithClaudeCLIPath(path string, env ...string) Option {
	return func(r *Runner) {
		r.claudeLaunch = func() (string, []string, error) { return path, env, nil }
	}
}

// WithCodexCommand overrides the command spawned for ProviderCodexNative
// turns, in place of the default codex.DefaultCommand(). Same reasoning and
// use (pointing at a compiled fake-agent binary in tests) as
// WithACPCommand.
func WithCodexCommand(command []string) Option {
	return func(r *Runner) { r.codexCommand = command }
}

// WithSessionStore overrides the SessionStore every provider's RunPrompt
// call reads a resume handle from and writes one back to, in place of
// New's own MemorySessionStore. Until ADR-0016 P1 lands (chats.agent_session),
// this is the only seam for a caller wanting session continuity to survive
// a daemon restart -- see SessionStore's doc comment.
func WithSessionStore(store SessionStore) Option {
	return func(r *Runner) { r.sessionStore = store }
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

	// acpSessions tracks each chat's most recent ACP session (see
	// acpSessionState in config_options.go), keyed by chat ID, so
	// ConfigOptions/SetSessionConfigOption can reach a chat's live session
	// without RunPrompt handing its per-turn client to anyone. Keyed by
	// chat, not task (docs/decisions/0016-multiple-chats-per-task.md
	// §4): two chats of the same task can now run concurrently, and a
	// task-keyed map would let one chat's teardown null out another
	// chat's live client. Guarded by sessionMu.
	acpSessions map[int64]*acpSessionState
	sessionMu   sync.Mutex

	// acpModes caches each ACP provider's discovered permission modes
	// (see acp_modes.go), guarded by modeMu. acpModeProbe enables
	// background discovery probes (WithACPModeProbe).
	acpModes map[Provider]acpModeCacheEntry

	// claudeClients holds each chat's live Claude Code client for the
	// duration of its turn (see live_mode.go), guarded by sessionMu.
	claudeClients map[int64]claudeBackend
	// claudeModels is each live Claude chat's current requested model
	// ("" = provider default), for the synthetic model config option.
	// Guarded by sessionMu, like claudeClients.
	claudeModels map[int64]string
	modeMu       sync.Mutex
	acpModeProbe bool

	// mcpServers, when set via WithMcpServers, supplies the configured
	// MCP servers passed to ACP and Claude-native turns (ADR-0018). Nil
	// means the feature is fully off. lookPath resolves a stdio server's
	// bare command (defaults to exec.LookPath; injectable for tests).
	mcpServers McpServerSource
	lookPath   func(string) (string, error)

	// sessionStore holds the resumable SessionHandle each provider's
	// RunPrompt call reads before a turn and writes after one, keyed by
	// chat ID -- see SessionHandle/SessionStore's doc comments. Defaults to
	// an empty MemorySessionStore; overridable via WithSessionStore.
	sessionStore SessionStore

	// newACPClient, newClaudeClient, and newCodexClient default to wrapping
	// acp.New, claudecode.New, and codex.New. Overridable only from within
	// this package's tests, to point at a fake agent binary / fake CLI
	// instead of a real one -- none of the three client packages expose a
	// constructor seam of their own, and a broader public abstraction isn't
	// warranted for a need this narrow.
	newACPClient    func(command []string, opts ...acp.Option) (acpBackend, error)
	newClaudeClient func(worktreePath string, opts ...claudecode.Option) (claudeBackend, error)
	newCodexClient  func(command []string, opts ...codex.Option) (codexBackend, error)

	// claudeLaunch reports how to start the Claude CLI: a CLI path to hand
	// claudecode.WithCLIPath and env entries for the CLI. Defaults to
	// claudecli.LaunchSpec, which is a no-op off Windows and, on Windows,
	// resolves the real claude.exe (never a .cmd shim) and routes the
	// start through a smind.exe launcher that puts the CLI in a kill-on-
	// close Job Object. Nil means "the SDK's own defaults" (tests that
	// point newClaudeClient at a fake CLI).
	claudeLaunch func() (cliPath string, env []string, err error)
}

// New returns a Runner backed by wm.
func New(wm *workspace.Manager, opts ...Option) *Runner {
	r := &Runner{
		wm:            wm,
		acpSessions:   map[int64]*acpSessionState{},
		acpModes:      map[Provider]acpModeCacheEntry{},
		claudeClients: map[int64]claudeBackend{},
		claudeModels:  map[int64]string{},
		sessionStore:  NewMemorySessionStore(),
		acpCommands: map[Provider][]string{
			ProviderGLM:  acp.GLMCommand(),
			ProviderKimi: acp.KimiCommand(),
		},
		codexCommand: codex.DefaultCommand(),
		lookPath:     exec.LookPath,
		claudeLaunch: claudecli.LaunchSpec,
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

// claudeLaunchSpec returns the CLI path and env entries to start the Claude
// CLI with, or "" when the SDK's own discovery and process handling apply.
func (r *Runner) claudeLaunchSpec() (cliPath string, env []string, err error) {
	if r.claudeLaunch == nil {
		return "", nil, nil
	}
	return r.claudeLaunch()
}

// lookPathFunc returns the Runner's command resolver, defaulting to
// exec.LookPath on a hand-built zero-value Runner.
func (r *Runner) lookPathFunc() func(string) (string, error) {
	if r.lookPath == nil {
		return exec.LookPath
	}
	return r.lookPath
}

// RunPrompt looks up taskID, spawns the agent backend named by provider
// rooted at the task's worktree, and drives one prompt turn: every text
// chunk the backend streams is translated into an Event and forwarded onto
// events as it arrives, followed by exactly one final EventTypeDone once
// the turn completes. events is always closed before RunPrompt returns,
// whether it returns an error or not, so a caller can unconditionally range
// over it.
//
// chatID identifies the docs/decisions/0016-multiple-chats-per-task.md chat
// this turn belongs to -- RunPrompt itself does no chat validation (that's
// internal/runs.Registry.Start's job, one layer up); it only uses chatID as
// the acpSessions key (so ConfigOptions/SetSessionConfigOption can reach
// the right live session even while a sibling chat of the same task has
// its own turn in flight) and as the sessionStore key (so a chat's resume
// handle -- SessionHandle -- is its own, distinct from any sibling chat's).
//
// decider, if non-nil, overrides the Runner-level acp.PermissionPolicy/
// claudecode.PermissionPolicy default for this call only -- see
// PermissionDecider's doc comment for why a human-in-the-loop decider is
// inherently per-call rather than Runner-wide configuration. nil preserves
// today's behavior exactly: each provider falls through to its own
// Runner-level default.
//
// perm is the run's provider-native permission settings (ADR-0019): Mode is
// one of the provider's own ModeInfo ids ("" for the provider's
// DefaultMode), applied natively -- Claude Code's --permission-mode,
// Codex's thread approvalPolicy/sandbox preset, an ACP agent's
// session/set_mode. AutoAccept (ACP only) approves every permission
// prompt; with a decider it's the decider's job (internal/runs records the
// auto-accept), without one acp.AutoApprovePolicy is installed. Whatever
// the provider's mode still escalates goes to decider.
//
// model is the requested model (ADR-0025): "" leaves the provider's
// default in place and changes nothing about the spawn; otherwise it is
// validated (syntax always, membership once the provider's catalog is
// discovered) before anything is spawned, then applied natively -- Claude's
// --model, an ACP session/set_config_option on the agent's model option,
// Codex's thread/start `model`.
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
func (r *Runner) RunPrompt(ctx context.Context, taskID, chatID int64, provider Provider, prompt string, decider PermissionDecider, perm PermissionSettings, thinkingLevel ThinkingLevel, model string, events chan<- Event) error {
	defer close(events)

	task, err := r.wm.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("taskrunner: get task %d: %w", taskID, err)
	}
	if task.WorktreePath == nil {
		return fmt.Errorf("taskrunner: task %d has no worktree (never materialized by CreateTask, or already archived)", taskID)
	}
	worktreePath := *task.WorktreePath

	if model != "" {
		if catalog, ok := r.ProviderCatalogFor(provider); ok {
			if err := ValidateModel(catalog, model); err != nil {
				return fmt.Errorf("taskrunner: %w", err)
			}
		}
	}

	prompt = r.promptWithAttachments(ctx, provider, prompt, events)

	switch provider {
	case ProviderGLM, ProviderKimi:
		return r.runACP(ctx, chatID, task.WorkspaceID, provider, worktreePath, prompt, decider, perm, model, events)
	case ProviderClaudeNative:
		return r.runClaudeNative(ctx, chatID, task.WorkspaceID, worktreePath, workspace.TaskEnv(task), prompt, decider, perm, thinkingLevel, model, events)
	case ProviderCodexNative:
		return r.runCodexNative(ctx, chatID, task.WorkspaceID, worktreePath, prompt, decider, perm, model, events)
	default:
		return fmt.Errorf("taskrunner: unknown provider %q", provider)
	}
}

// runACP drives one turn for any ACP-speaking provider (ProviderGLM,
// ProviderKimi, ...): they differ only in which command r.acpCommands maps
// them to -- everything else about the ACP session/prompt/streaming flow is
// identical, since it's the same wire protocol regardless of which agent is
// on the other end of it.
func (r *Runner) runACP(ctx context.Context, chatID int64, workspaceID int64, provider Provider, worktreePath, prompt string, decider PermissionDecider, perm PermissionSettings, model string, events chan<- Event) error {
	command, ok := r.acpCommands[provider]
	if !ok {
		return fmt.Errorf("taskrunner: no ACP command configured for provider %q", provider)
	}

	var opts []acp.Option
	switch {
	case decider != nil:
		opts = append(opts, acp.WithPermissionPolicy(acpDeciderAdapter{decider: decider, cwd: worktreePath}))
	case perm.AutoAccept:
		opts = append(opts, acp.WithPermissionPolicy(acp.AutoApprovePolicy{}))
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

	rows, err := r.mcpServersForWorkspace(workspaceID)
	if err != nil {
		return fmt.Errorf("taskrunner: %s mcp servers: %w", provider, err)
	}
	mcpWire, inactive, unresolved, states, err := acpMcpServers(rows, client, r.lookPathFunc())
	if err != nil {
		return fmt.Errorf("taskrunner: %s mcp servers: %w", provider, err)
	}
	if len(states) > 0 {
		select {
		case events <- Event{Type: EventTypeMcpServers, McpServers: states}:
		case <-ctx.Done():
		}
	}
	if len(unresolved) > 0 {
		// One unresolvable server must not block the whole run: skip it
		// and tell the user, as for a capability-dropped server.
		r.sendSessionNote(ctx, events, fmt.Sprintf(
			"MCP servers configured but not active for %s (skipped): %s",
			provider, strings.Join(unresolved, ", ")))
	}
	if len(inactive) > 0 {
		r.sendSessionNote(ctx, events, fmt.Sprintf(
			"MCP servers configured but not active for %s (agent does not support their transport): %s",
			provider, strings.Join(inactive, ", ")))
	}

	secrets := mcpSecrets(rows)
	sessionID, configOptions, resumed, err := r.newOrResumeACPSession(ctx, chatID, provider, client, worktreePath, mcpWire, secrets, events)
	if err != nil {
		return fmt.Errorf("taskrunner: %s new session: %w", provider, scrubError(err, secrets))
	}
	// Stored as soon as the session exists, not only after a successful
	// prompt: a cancelled turn (ctx cancellation aborting Prompt) must not
	// lose the handle, or the next prompt would silently start fresh.
	r.keepSessionHandle(chatID, provider, sessionID)
	r.recordSessionModes(provider, client, sessionID, configOptions)
	updated, err := applyACPMode(ctx, client, sessionID, configOptions, perm.Mode)
	if err != nil {
		return fmt.Errorf("taskrunner: %s set permission mode %q: %w", provider, perm.Mode, err)
	}
	updated, err = applyACPModel(ctx, client, sessionID, updated, model)
	if err != nil {
		return fmt.Errorf("taskrunner: %s set model %q: %w", provider, model, err)
	}
	// Published only once the start mode is applied: until then a
	// SetPermissionMode can't reach the session (it fails as "no live
	// session"), so a quick mid-run switch can never land first and then
	// be overwritten by the start-up switch.
	r.trackACPSession(chatID, sessionID, client, updated)
	defer r.endACPTurn(chatID)
	if mode, ok := currentACPMode(client, sessionID, updated); ok {
		select {
		case events <- Event{Type: EventTypePermissionModeChanged, Text: mode, Initial: true}:
		case <-ctx.Done():
		}
	}

	updates := make(chan acp.SessionUpdate)
	forwardDone := make(chan struct{})
	var (
		lastUsageUpdate acp.UsageUpdate
		gotUsageUpdate  bool
	)
	go func() {
		defer close(forwardDone)
		for u := range updates {
			if uu, ok := acp.ParseUsageUpdate(u); ok {
				lastUsageUpdate, gotUsageUpdate = uu, true
			}
			if u.Type == acp.SessionUpdateCurrentModeUpdate {
				// acp.Client has already applied it to SessionModes.
				if mode, ok := currentACPMode(client, sessionID, nil); ok {
					select {
					case events <- Event{Type: EventTypePermissionModeChanged, Text: mode}:
					case <-ctx.Done():
					}
				}
			}
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

	// ADR-0026 section 5: the learned-skill index rides on the FIRST prompt
	// of an agent session only: a resumed session already has it in
	// context, while a first, reset or failed-to-resume session is new.
	var promptOpts []acp.PromptOption
	if d, _ := DeliveryFrom(ctx); d.Preface != "" && !resumed {
		promptOpts = append(promptOpts, acp.WithPromptPrefix(d.Preface))
	}
	stopReason, err := client.Prompt(ctx, sessionID, prompt, updates, promptOpts...)
	<-forwardDone
	if err != nil {
		return fmt.Errorf("taskrunner: %s prompt: %w", provider, scrubError(err, secrets))
	}
	r.keepSessionHandle(chatID, provider, sessionID)

	promptUsage, hasPromptUsage := client.PromptUsage(sessionID)
	handle, _ := r.sessionStore.Get(chatID)
	usage, snap := diffACPUsage(usageSnapshotFromHandle(handle), sessionID, client.InstanceID(),
		newCumulativeUsage(promptUsage, hasPromptUsage, lastUsageUpdate, gotUsageUpdate))
	if snap != nil {
		// Marshaling a struct of numbers and strings can't fail.
		handle.Metadata, _ = json.Marshal(snap)
		r.sessionStore.Set(chatID, handle)
	}
	r.sendUsage(ctx, events, usage)

	select {
	case events <- Event{Type: EventTypeDone, StopReason: stopReason}:
	case <-ctx.Done():
	}
	return nil
}

// codexTurnUsage is the turn's own usage: codex reports the thread's
// cumulative tokenUsage.total, so it is differenced against the snapshot
// the thread's previous turn left in the session handle's Metadata (ADR-0020
// §2), and the new snapshot is stored back. Source not_reported with every
// field nil if codex reported nothing.
func (r *Runner) codexTurnUsage(chatID int64, client codexBackend, threadID string) Usage {
	total, ok := client.ThreadTokenUsage(threadID)
	handle, _ := r.sessionStore.Get(chatID)
	usage, snap := diffCodexUsage(usageSnapshotFromHandle(handle), threadID, total, ok)
	if snap != nil {
		// Marshaling a struct of numbers and strings can't fail.
		handle.Metadata, _ = json.Marshal(snap)
		r.sessionStore.Set(chatID, handle)
	}
	return usage
}

// keepSessionHandle stores the handle for provider's sessionID like
// sessionStore.Set, but keeps the handle's Metadata (the usage snapshot)
// when the stored handle is for the same provider and session; a handle for
// any other session starts without one.
func (r *Runner) keepSessionHandle(chatID int64, provider Provider, sessionID string) {
	h := SessionHandle{Provider: provider, SessionID: sessionID}
	if old, ok := r.sessionStore.Get(chatID); ok && old.Provider == provider && old.SessionID == sessionID {
		h.NativeHandle, h.Metadata = old.NativeHandle, old.Metadata
	}
	r.sessionStore.Set(chatID, h)
}

// sendUsage forwards a turn's normalized usage as one EventTypeUsage; every
// runner calls it immediately before sending EventTypeDone.
func (r *Runner) sendUsage(ctx context.Context, events chan<- Event, u Usage) {
	select {
	case events <- Event{Type: EventTypeUsage, Usage: &u}:
	case <-ctx.Done():
	}
}

// newOrResumeACPSession starts the ACP session this turn drives: if chatID
// has a stored SessionHandle for provider, it tries to resume that
// session's conversation -- session/load if client advertises the
// loadSession capability, else session/resume if it advertises
// sessionCapabilities.resume -- before falling back to a fresh session/new.
// A fresh session is also what's used when there's no stored handle at
// all, and whenever a resume attempt itself fails (a stale/unknown session
// id) or isn't offered by the agent at all: in every one of those
// fallback cases, a human-readable EventTypeSessionNote is sent on events
// first, so a stale/unsupported resume degrades to "new session, and the
// UI says so" rather than either failing the prompt or silently losing
// context. See ADR-0016 section 2 / docs/plans/completed/multi-chat-per-task.md's
// P2.3/P2.5, modeled on
// refs/paseo/packages/server/src/server/agent/providers/acp-agent.ts:1757-1800.
//
// The stored handle is keyed by chatID, not taskID: a chat's resumable
// session is its own, distinct from any sibling chat of the same task (see
// SessionStore's doc comment).
//
// secrets (see mcpSecrets) are scrubbed from agent error text placed in a
// session note: an agent may echo configured MCP env/header values back.
//
// resumed is true only when the stored session's conversation was resumed.
func (r *Runner) newOrResumeACPSession(ctx context.Context, chatID int64, provider Provider, client acpBackend, worktreePath string, mcpServers []any, secrets []string, events chan<- Event) (sessionID string, configOptions []acp.ConfigOption, resumed bool, err error) {
	newSession := func() (string, []acp.ConfigOption, bool, error) {
		id, opts, err := client.NewSession(ctx, worktreePath, mcpServers)
		return id, opts, false, err
	}
	handle, ok := r.sessionStore.Get(chatID)
	if !ok || handle.Provider != provider || handle.SessionID == "" {
		return newSession()
	}

	var method string
	switch {
	case client.SupportsLoadSession():
		method = "session/load"
		configOptions, err = client.LoadSession(ctx, handle.SessionID, worktreePath, mcpServers)
	case client.SupportsResumeSession():
		method = "session/resume"
		configOptions, err = client.ResumeSession(ctx, handle.SessionID, worktreePath, mcpServers)
	default:
		r.sendSessionNote(ctx, events, fmt.Sprintf(
			"%s does not support resuming a session (no loadSession or sessionCapabilities.resume); starting a new session -- prior context from session %s is not available this turn",
			provider, handle.SessionID))
		return newSession()
	}
	if err == nil {
		return handle.SessionID, configOptions, true, nil
	}

	r.sendSessionNote(ctx, events, fmt.Sprintf(
		"could not resume %s session %s via %s (%v); starting a new session instead",
		provider, handle.SessionID, method, scrubError(err, secrets)))
	return newSession()
}

// sendSessionNote logs note and, unless ctx is already done, forwards it as
// an EventTypeSessionNote on events -- the shared "degrade, don't fail"
// mechanism every runner's resume-fallback path uses (see
// EventTypeSessionNote's doc comment).
func (r *Runner) sendSessionNote(ctx context.Context, events chan<- Event, note string) {
	log.Printf("taskrunner: %s", note)
	select {
	case events <- Event{Type: EventTypeSessionNote, Text: note}:
	case <-ctx.Done():
	}
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

func (r *Runner) runClaudeNative(ctx context.Context, chatID, workspaceID int64, worktreePath string, taskEnv []string, prompt string, decider PermissionDecider, perm PermissionSettings, thinkingLevel ThinkingLevel, model string, events chan<- Event) error {
	var opts []claudecode.Option
	// Everything that must reach the CLI's environment is collected here and
	// handed over in ONE claudecode.WithEnv at the end: the SDK's WithEnv
	// replaces the list on every call, so separate calls silently drop each
	// other's variables (the task's SMIND_PORT used to be lost whenever a
	// decider added the dialog-timeout variable).
	//
	// ADR-0024: the task's SMIND_PORT (when a port range is configured).
	env := append([]string(nil), taskEnv...)
	cliPath, launchEnv, err := r.claudeLaunchSpec()
	if err != nil {
		return fmt.Errorf("taskrunner: claude code cli: %w", err)
	}
	if cliPath != "" {
		opts = append(opts, claudecode.WithCLIPath(cliPath))
		env = append(env, launchEnv...)
	}

	rows, err := r.mcpServersForWorkspace(workspaceID)
	if err != nil {
		return fmt.Errorf("taskrunner: claude mcp servers: %w", err)
	}
	if len(rows) > 0 {
		select {
		case events <- Event{Type: EventTypeMcpServers, McpServers: claudeMcpStates(rows, nil)}:
		case <-ctx.Done():
		}
		mcpConfig, err := claudeMcpConfig(rows)
		if err != nil {
			return fmt.Errorf("taskrunner: claude mcp servers: %w", err)
		}
		if mcpConfig != "" {
			// The JSON can carry secrets (env values, auth headers), so
			// hand the CLI a path to a private file instead of the inline
			// string, which would be visible in the process argv (ps).
			path, cleanup, err := writeClaudeMcpConfigFile(mcpConfig)
			if err != nil {
				return fmt.Errorf("taskrunner: claude mcp servers: %w", err)
			}
			defer cleanup()
			opts = append(opts, claudecode.WithMCPConfig(path))
		}
	}

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
	if model != "" && model != ClaudeModelDefault {
		opts = append(opts, claudecode.WithModel(model))
	}
	d, _ := DeliveryFrom(ctx)
	if dirs := append(append([]string(nil), d.AddDirs...), attachmentAddDirs(ctx, prompt)...); len(dirs) > 0 {
		// ADR-0026 section 5: the read-only learned-skill projection, plus
		// the prompt's attachment set (ADR-0038); no option at all without
		// either keeps the argv unchanged.
		opts = append(opts, claudecode.WithAddDirs(dirs...))
	}
	mode := perm.Mode
	if mode == "" {
		mode = ClaudeModeAcceptEdits
	}
	// The run's permission mode goes straight to the CLI (ADR-0019), and
	// --allow-dangerously-skip-permissions is always passed -- as Paseo
	// does -- so a mid-run switch into bypassPermissions (SetPermissionMode)
	// is accepted by the CLI rather than refused for a session that
	// wasn't launched with that capability. It grants nothing by itself.
	opts = append(opts,
		claudecode.WithPermissionMode(mode),
		claudecode.WithExtraArgs(map[string]*string{claudeAllowBypassFlag: nil}),
	)
	switch {
	case decider != nil:
		// Installed for every mode, bypassPermissions included: in bypass
		// the CLI never asks, but a mid-run switch to an asking mode must
		// still reach a human.
		opts = append(opts, claudecode.WithPermissionPolicy(claudeDeciderAdapter{decider: decider, cwd: worktreePath}))
		// Hold the CLI's own permission-dialog deadline open well past
		// smind's 5-minute manual-approval window, so the CLI's internal
		// auto-deny fallback can't silently cancel a pending request
		// before a human answers (see claudeDialogTimeoutEnv). Only in the
		// decider branch: the Runner-level policy defaults answer
		// programmatically and never wait on a dialog. An explicit value
		// already in the environment is forwarded as-is instead -- a
		// deployment-wide escape hatch.
		if v := os.Getenv(claudeDialogTimeoutEnv); v != "" {
			env = append(env, claudeDialogTimeoutEnv+"="+v)
		} else {
			env = append(env, claudeDialogTimeoutEnv+"="+claudeDialogTimeoutMS)
		}
	case r.claudePermissionPolicy != nil:
		opts = append(opts, claudecode.WithPermissionPolicy(r.claudePermissionPolicy))
	}
	if len(env) > 0 {
		opts = append(opts, claudecode.WithEnv(env...))
	}

	client, err := r.newClaudeClientWithResume(ctx, chatID, worktreePath, opts, events)
	if err != nil {
		return fmt.Errorf("taskrunner: spawn claude code agent: %w", err)
	}
	defer client.Close()
	r.trackClaudeClient(chatID, client)
	r.setClaudeModel(chatID, model)
	defer r.untrackClaudeClient(chatID)

	updates := make(chan claudecode.Message)
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for msg := range updates {
			// The CLI reports its session id on an "init" system message
			// -- the earliest point a handle exists at all on this
			// backend. Storing it here (never overwriting with an empty
			// id) means a cancelled turn doesn't lose the handle and the
			// next prompt resumes this session instead of starting fresh.
			if sys, ok := msg.(claudecode.SystemMessage); ok && sys.Subtype == "init" {
				var w struct {
					SessionID string `json:"session_id"`
				}
				if json.Unmarshal(sys.Raw, &w) == nil && w.SessionID != "" {
					r.sessionStore.Set(chatID, SessionHandle{Provider: ProviderClaudeNative, SessionID: w.SessionID})
				}
				// The CLI tolerates a failing MCP server and runs on
				// without it; surface that, naming servers only.
				if failed := claudeFailedMcpServers(sys.Raw); len(failed) > 0 {
					r.sendSessionNote(ctx, events, fmt.Sprintf(
						"MCP servers failed to start for claude-native: %s", strings.Join(failed, ", ")))
					// Latest event wins: repeat the mcp_servers event with
					// the CLI-reported failures marked failed, the rest
					// still active.
					if len(rows) > 0 {
						select {
						case events <- Event{Type: EventTypeMcpServers, McpServers: claudeMcpStates(rows, failed)}:
						case <-ctx.Done():
						}
					}
				}
			}
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
	r.sessionStore.Set(chatID, SessionHandle{Provider: ProviderClaudeNative, SessionID: result.SessionID})
	r.sendUsage(ctx, events, claudeUsage(result))

	select {
	case events <- Event{Type: EventTypeDone, StopReason: result.StopReason, Raw: result}:
	case <-ctx.Done():
	}
	return nil
}

// newClaudeClientWithResume spawns chatID's claude-native turn, resuming
// its stored session (via claudecode.WithResume) when one exists -- keyed
// by chatID, not taskID, same reasoning as newOrResumeACPSession. A
// resume attempt that fails at construction -- verified live 2026-09-27
// against a real `claude` CLI: an unknown/stale --resume session id fails
// the CLI's own initialize handshake, surfacing as claudecode.New itself
// erroring, before any prompt is ever sent -- degrades to a fresh session
// instead of failing the turn, surfacing an EventTypeSessionNote first.
// See ADR-0016 section 2 / docs/plans/completed/multi-chat-per-task.md's
// P2.2/P2.5.
func (r *Runner) newClaudeClientWithResume(ctx context.Context, chatID int64, worktreePath string, opts []claudecode.Option, events chan<- Event) (claudeBackend, error) {
	handle, ok := r.sessionStore.Get(chatID)
	if !ok || handle.Provider != ProviderClaudeNative || handle.SessionID == "" {
		return r.newClaudeClient(worktreePath, opts...)
	}

	resumeOpts := append(append([]claudecode.Option{}, opts...), claudecode.WithResume(handle.SessionID))
	client, err := r.newClaudeClient(worktreePath, resumeOpts...)
	if err == nil {
		return client, nil
	}

	r.sendSessionNote(ctx, events, fmt.Sprintf(
		"could not resume claude-native session %s (%v); starting a new session instead", handle.SessionID, err))
	return r.newClaudeClient(worktreePath, opts...)
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
func (r *Runner) runCodexNative(ctx context.Context, chatID, workspaceID int64, worktreePath, prompt string, decider PermissionDecider, perm PermissionSettings, model string, events chan<- Event) error {
	mode := perm.Mode
	if mode == "" {
		mode = CodexModeAuto
	}
	preset, ok := codexModePresets[mode]
	if !ok {
		return fmt.Errorf("taskrunner: unknown codex permission mode %q", mode)
	}
	// The mode's own approvalPolicy/sandbox pair (ADR-0019) -- Codex itself
	// decides what to escalate; whatever it does reaches decider.
	opts := []codex.Option{codex.WithThreadPolicy(preset.approvalPolicy, preset.sandbox)}
	if model != "" {
		opts = append(opts, codex.WithModel(model))
	}
	switch {
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
	if rows, err := r.mcpServersForWorkspace(workspaceID); err != nil {
		return fmt.Errorf("taskrunner: codex mcp servers: %w", err)
	} else if len(rows) > 0 {
		names := make([]string, len(rows))
		for i, row := range rows {
			names[i] = row.Name
		}
		r.sendSessionNote(ctx, events, fmt.Sprintf(
			"MCP servers configured but not supported for codex-native yet: %s", strings.Join(names, ", ")))
		select {
		case events <- Event{Type: EventTypeMcpServers, McpServers: codexMcpStates(rows)}:
		case <-ctx.Done():
		}
	}
	threadID, err := r.newOrResumeCodexThread(ctx, chatID, client, worktreePath, events)
	if err != nil {
		return fmt.Errorf("taskrunner: codex new thread: %w", err)
	}
	// Stored as soon as the thread exists, not only after a successful
	// prompt -- same stop-doesn't-lose-the-handle reasoning as runACP.
	r.keepSessionHandle(chatID, ProviderCodexNative, threadID)

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
	r.keepSessionHandle(chatID, ProviderCodexNative, threadID)
	r.sendUsage(ctx, events, r.codexTurnUsage(chatID, client, threadID))

	select {
	case events <- Event{Type: EventTypeDone, StopReason: stopReason}:
	case <-ctx.Done():
	}
	return nil
}

// newOrResumeCodexThread starts the codex thread this turn drives: if
// chatID has a stored SessionHandle, it tries codexBackend.ResumeSession
// (thread/resume, with the thread/loaded/list and archived->unarchive
// handling that method's own doc comment describes) before falling back to
// a fresh thread/start -- surfacing an EventTypeSessionNote first whenever
// the stored thread couldn't be resumed (a stale/unknown/deleted thread
// id), so that degrades gracefully rather than failing the prompt. See
// ADR-0016 section 2 / docs/plans/completed/multi-chat-per-task.md's
// P2.4/P2.5.
func (r *Runner) newOrResumeCodexThread(ctx context.Context, chatID int64, client codexBackend, worktreePath string, events chan<- Event) (string, error) {
	handle, ok := r.sessionStore.Get(chatID)
	if !ok || handle.Provider != ProviderCodexNative || handle.SessionID == "" {
		return client.NewSession(ctx, worktreePath)
	}

	threadID, err := client.ResumeSession(ctx, handle.SessionID, worktreePath)
	if err == nil {
		return threadID, nil
	}

	r.sendSessionNote(ctx, events, fmt.Sprintf(
		"could not resume codex thread %s (%v); starting a new thread instead", handle.SessionID, err))
	return client.NewSession(ctx, worktreePath)
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
