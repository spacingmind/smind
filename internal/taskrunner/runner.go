package taskrunner

import (
	"context"
	"fmt"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/workspace"
)

// acpBackend is the subset of *acp.Client's methods RunPrompt needs to
// drive a GLM (or other ACP) turn. It exists only so tests can substitute a
// client wired to a fake agent binary via Runner.newACPClient; production
// code always gets it from acp.New, whose real *acp.Client satisfies it.
type acpBackend interface {
	Initialize(ctx context.Context) error
	NewSession(ctx context.Context, cwd string) (string, error)
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

	// acpCommands maps each ACP-speaking provider to the command spawned for
	// its turns. Seeded in New with every known ACP provider's real default
	// (acp.GLMCommand(), acp.KimiCommand()); overridable per-provider via
	// WithACPCommand. A provider with no entry (shouldn't happen for any
	// Provider constant this package defines) fails fast in runACP rather
	// than spawning an empty command.
	acpCommands map[Provider][]string

	// newACPClient and newClaudeClient default to wrapping acp.New and
	// claudecode.New. Overridable only from within this package's tests,
	// to point at a fake agent binary / fake CLI instead of a real one --
	// neither client package exposes a constructor seam of its own, and a
	// broader public abstraction isn't warranted for a need this narrow.
	newACPClient    func(command []string, opts ...acp.Option) (acpBackend, error)
	newClaudeClient func(worktreePath string, opts ...claudecode.Option) (claudeBackend, error)
}

// New returns a Runner backed by wm.
func New(wm *workspace.Manager, opts ...Option) *Runner {
	r := &Runner{
		wm: wm,
		acpCommands: map[Provider][]string{
			ProviderGLM:  acp.GLMCommand(),
			ProviderKimi: acp.KimiCommand(),
		},
		newACPClient: func(command []string, opts ...acp.Option) (acpBackend, error) {
			return acp.New(command, opts...)
		},
		newClaudeClient: func(worktreePath string, opts ...claudecode.Option) (claudeBackend, error) {
			return claudecode.New(worktreePath, opts...)
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
// The backend client spawned for this call is not reused: RunPrompt owns
// its subprocess end to end and closes it before returning. ctx cancellation
// propagates into the backend's turn call, aborting it, after which the
// client is still closed as normal -- so a cancelled RunPrompt does not
// leak the subprocess.
func (r *Runner) RunPrompt(ctx context.Context, taskID int64, provider Provider, prompt string, decider PermissionDecider, events chan<- Event) error {
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
		return r.runACP(ctx, provider, worktreePath, prompt, decider, events)
	case ProviderClaudeNative:
		return r.runClaudeNative(ctx, worktreePath, prompt, decider, events)
	default:
		return fmt.Errorf("taskrunner: unknown provider %q", provider)
	}
}

// runACP drives one turn for any ACP-speaking provider (ProviderGLM,
// ProviderKimi, ...): they differ only in which command r.acpCommands maps
// them to -- everything else about the ACP session/prompt/streaming flow is
// identical, since it's the same wire protocol regardless of which agent is
// on the other end of it.
func (r *Runner) runACP(ctx context.Context, provider Provider, worktreePath, prompt string, decider PermissionDecider, events chan<- Event) error {
	command, ok := r.acpCommands[provider]
	if !ok {
		return fmt.Errorf("taskrunner: no ACP command configured for provider %q", provider)
	}

	var opts []acp.Option
	switch {
	case decider != nil:
		opts = append(opts, acp.WithPermissionPolicy(acpDeciderAdapter{decider}))
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
	sessionID, err := client.NewSession(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("taskrunner: %s new session: %w", provider, err)
	}

	updates := make(chan acp.SessionUpdate)
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for u := range updates {
			text, ok := u.Text()
			if !ok {
				continue
			}
			select {
			case events <- Event{Type: EventTypeText, Text: text, Raw: u}:
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

func (r *Runner) runClaudeNative(ctx context.Context, worktreePath, prompt string, decider PermissionDecider, events chan<- Event) error {
	var opts []claudecode.Option
	switch {
	case decider != nil:
		opts = append(opts, claudecode.WithPermissionPolicy(claudeDeciderAdapter{decider}))
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
			// Only AssistantMessage carries text chunks from the agent.
			// SystemMessage is a lifecycle/init event and UserMessage
			// echoes turns fed back to the model (including tool results)
			// -- neither is "text from the agent", so both are dropped
			// here rather than passed through as opaque events.
			am, ok := msg.(claudecode.AssistantMessage)
			if !ok {
				continue
			}
			for _, block := range am.Content {
				tb, ok := block.(claudecode.TextBlock)
				if !ok {
					continue
				}
				select {
				case events <- Event{Type: EventTypeText, Text: tb.Text, Raw: msg}:
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
