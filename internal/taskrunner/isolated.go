package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

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

// ErrIsolationViolated is returned when an isolated run attempted a tool
// call, or the CLI reported tools/MCP servers/commands/skills/plugins at
// init.
var ErrIsolationViolated = errors.New("taskrunner: isolated run attempted a tool call")

// ErrIsolatedRunFailed is returned when the CLI ended the run with an error
// result (error_max_turns, API error, ...).
var ErrIsolatedRunFailed = errors.New("taskrunner: isolated run failed")

// isolatedInitFields are the init-message keys that must be absent or empty
// in an isolated run. "agents" is deliberately not listed: built-in agent
// definitions are not tools.
var isolatedInitFields = []string{"tools", "mcp_servers", "slash_commands", "skills", "plugins"}

// initViolation returns the first isolatedInitFields entry that is present
// and non-empty in the raw init message, or "". Missing keys and unparsable
// payloads are ignored: the real init shape is not confirmed live, so the
// check must never fail on what it cannot read.
func initViolation(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for _, f := range isolatedInitFields {
		v, ok := m[f]
		if !ok {
			continue
		}
		var arr []json.RawMessage
		if json.Unmarshal(v, &arr) == nil {
			if len(arr) > 0 {
				return f
			}
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(v, &obj) == nil && len(obj) > 0 {
			return f
		}
	}
	return ""
}

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
// Every permission request is denied; callers cannot supply a decider.
//
// Defense in depth: --tools "" means the model has no tool to call, but if
// a tool_use block still arrives, or the CLI's init message lists tools,
// MCP servers, slash commands, skills or plugins, the run is cancelled
// (killing the CLI), a final Done event with StopReason
// isolationViolatedStop is sent, and an error wrapping ErrIsolationViolated
// is returned. A result carrying permission denials is a violation too; an
// error result returns ErrIsolatedRunFailed. Nothing from the violating
// turn onward is forwarded. err != nil means discard every event.
//
// The caller reads the model's answer with FinalText(events): the text
// chunks of the last assistant turn, i.e. the contiguous EventTypeText
// events before the trailing usage/Done events.
func (r *Runner) RunPromptIsolated(ctx context.Context, taskID, chatID int64, provider Provider, prompt string, model string, iso IsolationOptions, events chan<- Event) error {
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
	opts = append(opts, claudecode.WithPermissionPolicy(denyAllClaudePolicy{}))

	client, err := r.newClaudeClient(worktreePath, opts...)
	if err != nil {
		return fmt.Errorf("taskrunner: spawn isolated claude code agent: %w", err)
	}
	defer client.Close()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		violated string // what tripped the run; "" when nothing did
		tripped  bool
	)
	updates := make(chan claudecode.Message)
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for msg := range updates {
			if sys, ok := msg.(claudecode.SystemMessage); ok && sys.Subtype == "init" {
				if f := initViolation(sys.Raw); f != "" {
					mu.Lock()
					if !tripped {
						tripped, violated = true, "init reported "+f
						cancel()
					}
					mu.Unlock()
				}
			}
			for _, e := range claudeEvents(msg) {
				mu.Lock()
				if e.Type == EventTypeToolCall && !tripped {
					tripped, violated = true, fmt.Sprintf("tool call %q", e.ToolName)
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
	didTrip, reason := tripped, violated
	mu.Unlock()
	if !didTrip && err == nil && len(result.PermissionDenials) > 0 {
		didTrip, reason = true, "permission denied for a tool request"
	}
	if didTrip {
		select {
		case events <- Event{Type: EventTypeDone, StopReason: isolationViolatedStop}:
		case <-ctx.Done():
		}
		return fmt.Errorf("%w: %s", ErrIsolationViolated, reason)
	}
	if err != nil {
		return fmt.Errorf("taskrunner: isolated claude code prompt: %w", err)
	}
	if result.IsError {
		return fmt.Errorf("%w: %s", ErrIsolatedRunFailed, clipBytes(resultFailure(result), 200))
	}
	r.sendUsage(ctx, events, claudeUsage(result))

	select {
	case events <- Event{Type: EventTypeDone, StopReason: result.StopReason, Raw: result}:
	case <-ctx.Done():
	}
	return nil
}

// resultFailure describes an error ResultMessage for an error string.
func resultFailure(res claudecode.ResultMessage) string {
	parts := []string{}
	for _, p := range []string{res.TerminalReason, res.StopReason, res.Result} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	parts = append(parts, res.Errors...)
	if len(parts) == 0 {
		return "error result"
	}
	return strings.Join(parts, ": ")
}

// clipBytes truncates s to at most n bytes without splitting a rune.
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
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
