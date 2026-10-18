package taskrunner

import (
	"context"
	"errors"
	"fmt"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// IsolationOptions configures a run that must have no tools and no ambient
// context (ADR-0026 reflector/curator).
type IsolationOptions struct {
	// SystemPrompt is required; it is passed via the SDK system-prompt option.
	SystemPrompt string
	// MaxTurns caps the agentic loop; 1 when <= 0.
	MaxTurns int
}

// ErrIsolationUnsupported is returned by RunPromptIsolated for any provider
// other than claude-native (ADR-0026 D13: ACP/Codex agents cannot be
// stripped of their tools).
var ErrIsolationUnsupported = errors.New("taskrunner: isolated runs are only supported on claude-native")

// ErrIsolationViolated is returned when an isolated run attempted a tool call.
var ErrIsolationViolated = errors.New("taskrunner: isolated run attempted a tool call")

// isolatedDenyMessage is the reason handed back for any permission request
// raised inside an isolated run.
const isolatedDenyMessage = "tools are disabled in isolated runs"

// denyAllClaudePolicy denies every can_use_tool request.
type denyAllClaudePolicy struct{}

func (denyAllClaudePolicy) Decide(_ context.Context, _ claudecode.CanUseToolRequest) (bool, map[string]any, string, []claudecode.PermissionUpdate, bool, error) {
	return false, nil, isolatedDenyMessage, nil, false, nil
}

// RunPromptIsolated runs ONE prompt on claude-native with hard isolation:
// no tools (--tools ""), no MCP (--strict-mcp-config, no --mcp-config), no
// extra directories, no settings/hooks/skills sources, no slash commands,
// permission mode plan, a caller-supplied system prompt and a turn cap. The
// run never resumes a stored session, never stores the new session handle
// and never registers its client for SetPermissionMode: the client is fresh
// and closed before return.
//
// Any provider other than claude-native returns ErrIsolationUnsupported
// before anything is spawned (ADR-0026 D13). Like RunPrompt, events is
// closed before returning in every case, and the task's worktree path is the
// CLI's cwd (callers pass a task whose worktree is an empty temp dir).
//
// decider nil denies every permission request; a non-nil decider is
// consulted instead. The caller reads the final message with FinalText.
func (r *Runner) RunPromptIsolated(ctx context.Context, taskID, chatID int64, provider Provider, prompt string, decider PermissionDecider, model string, iso IsolationOptions, events chan<- Event) error {
	defer close(events)

	if provider != ProviderClaudeNative {
		return fmt.Errorf("%w (got %q)", ErrIsolationUnsupported, provider)
	}
	if iso.SystemPrompt == "" {
		return errors.New("taskrunner: isolated run requires a system prompt")
	}
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

	maxTurns := iso.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 1
	}

	// Duplicated from runClaudeNative rather than shared: that function
	// interleaves MCP, thinking, bypass and dialog-timeout handling with
	// this setup, and extracting a helper would risk changing its argv.
	opts := []claudecode.Option{
		claudecode.WithTools(),
		claudecode.WithStrictMCPConfig(),
		claudecode.WithSettingSources(),
		claudecode.WithMaxTurns(maxTurns),
		claudecode.WithSystemPrompt(iso.SystemPrompt),
		claudecode.WithPermissionMode(ClaudeModePlan),
		claudecode.WithExtraArgs(map[string]*string{"disable-slash-commands": nil}),
	}
	cliPath, launchEnv, err := r.claudeLaunchSpec()
	if err != nil {
		return fmt.Errorf("taskrunner: claude code cli: %w", err)
	}
	if cliPath != "" {
		opts = append(opts, claudecode.WithCLIPath(cliPath))
		if len(launchEnv) > 0 {
			opts = append(opts, claudecode.WithEnv(launchEnv...))
		}
	}
	if model != "" && model != ClaudeModelDefault {
		opts = append(opts, claudecode.WithModel(model))
	}
	if decider != nil {
		opts = append(opts, claudecode.WithPermissionPolicy(claudeDeciderAdapter{decider: decider, cwd: worktreePath}))
	} else {
		opts = append(opts, claudecode.WithPermissionPolicy(denyAllClaudePolicy{}))
	}

	client, err := r.newClaudeClient(worktreePath, opts...)
	if err != nil {
		return fmt.Errorf("taskrunner: spawn isolated claude code agent: %w", err)
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
		return fmt.Errorf("taskrunner: isolated claude code prompt: %w", err)
	}
	r.sendUsage(ctx, events, claudeUsage(result))

	select {
	case events <- Event{Type: EventTypeDone, StopReason: result.StopReason, Raw: result}:
	case <-ctx.Done():
	}
	return nil
}
