package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

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

// isolationViolatedStop is the StopReason of the final Done event of a run
// aborted for attempting a tool call.
const isolationViolatedStop = "isolation_violated"

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
// consulted instead.
//
// Defense in depth: --tools "" means the model has no tool to call, but if
// a tool_use block still arrives the run is cancelled (killing the CLI), a
// final Done event with StopReason isolationViolatedStop is sent, and an
// error wrapping ErrIsolationViolated and naming the tool is returned.
// Nothing from the violating turn onward is forwarded.
//
// The caller reads the model's answer with FinalText(events): the text
// chunks of the last assistant turn, i.e. the contiguous EventTypeText
// events before the trailing usage/Done events.
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

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		violated string // tool name of the first tool call; "" when none
		tripped  bool
	)
	updates := make(chan claudecode.Message)
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for msg := range updates {
			for _, e := range claudeEvents(msg) {
				mu.Lock()
				if e.Type == EventTypeToolCall && !tripped {
					tripped, violated = true, e.ToolName
					cancel()
				}
				drop := tripped
				mu.Unlock()
				if drop {
					continue
				}
				select {
				case events <- e:
				case <-runCtx.Done():
				}
			}
		}
	}()

	result, err := client.Prompt(runCtx, prompt, updates)
	<-forwardDone
	mu.Lock()
	didTrip, toolName := tripped, violated
	mu.Unlock()
	if didTrip {
		select {
		case events <- Event{Type: EventTypeDone, StopReason: isolationViolatedStop}:
		case <-ctx.Done():
		}
		return fmt.Errorf("%w: %q", ErrIsolationViolated, toolName)
	}
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

// FinalText returns the text of the last assistant turn in events: the
// contiguous EventTypeText chunks ending the stream, ignoring trailing
// non-text events (usage, done). Any earlier non-text event (thinking, a
// tool call) ends the turn's text. "" when there is none.
func FinalText(events []Event) string {
	end := len(events)
	for end > 0 && events[end-1].Type != EventTypeText {
		end--
	}
	start := end
	for start > 0 && events[start-1].Type == EventTypeText {
		start--
	}
	var b strings.Builder
	for _, e := range events[start:end] {
		b.WriteString(e.Text)
	}
	return b.String()
}
