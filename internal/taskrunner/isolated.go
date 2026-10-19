package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
	// Timeout bounds the whole run; defaultIsolatedTimeout when <= 0.
	Timeout time.Duration
}

// defaultIsolatedTimeout bounds an isolated run that sets no Timeout.
const defaultIsolatedTimeout = 5 * time.Minute

// claudeAiMcpEnv opts the CLI out of claude.ai account connectors (the
// claude-code changelog's documented switch); isolated runs only.
const claudeAiMcpEnv = "ENABLE_CLAUDEAI_MCP_SERVERS"

// ErrIsolationCwd is returned when the run's working directory could leak
// ambient context into the CLI (project instructions, MCP config, a git
// checkout) or is not a usable absolute directory.
var ErrIsolationCwd = errors.New("taskrunner: isolated run cwd is not clean")

// isolationCwdForbidden are entries whose presence in the cwd makes the CLI
// load ambient context.
var isolationCwdForbidden = []string{"CLAUDE.md", ".claude", ".mcp.json", "AGENTS.md", ".git"}

// checkIsolationCwd verifies dir is an absolute existing directory holding
// none of isolationCwdForbidden.
func checkIsolationCwd(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%w: %q is not absolute", ErrIsolationCwd, dir)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("%w: %q is not an existing directory", ErrIsolationCwd, dir)
	}
	for _, name := range isolationCwdForbidden {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			return fmt.Errorf("%w: contains %s", ErrIsolationCwd, name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: lstat %s: %v", ErrIsolationCwd, name, err)
		}
	}
	return nil
}

// safeToolName makes a model-supplied tool name safe to embed in an error:
// at most 64 bytes, only [A-Za-z0-9_.:-], anything else becomes '_'.
func safeToolName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if b.Len() >= 64 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == ':', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
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

// builtinPluginPath is the path the CLI reports for a plugin compiled into
// the binary. Live-confirmed (claude 2.1.293): even with --setting-sources=
// and --strict-mcp-config the init message lists four such plugins
// ("cc-plugin-sec-default@builtin", ...). They are not user-installable and
// bring no tools, so they are not an isolation leak.
const builtinPluginPath = "builtin"

// isBuiltinPlugin reports whether one init "plugins" entry is a plugin
// compiled into the CLI: path "builtin" and source "<name>@builtin". Both
// must agree, so a spoof needs two coordinated fields; anything else
// (including a non-object entry) is not builtin.
func isBuiltinPlugin(raw json.RawMessage) bool {
	var p struct {
		Name   string `json:"name"`
		Path   string `json:"path"`
		Source string `json:"source"`
	}
	return json.Unmarshal(raw, &p) == nil && p.Path == builtinPluginPath &&
		p.Name != "" && p.Source == p.Name+"@"+builtinPluginPath
}

// initViolation returns the first isolatedInitFields entry that is present
// and non-empty in the raw init message, or "". Missing keys and unparsable
// payloads are ignored: the check must never fail on what it cannot read.
// Built-in plugins (isBuiltinPlugin) do not count; any other plugin does.
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
			for _, item := range arr {
				if f == "plugins" && isBuiltinPlugin(item) {
					continue
				}
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
	if err := checkIsolationCwd(worktreePath); err != nil {
		return err
	}

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
	// One WithEnv: the SDK's option replaces the list on every call.
	env := []string{claudeAiMcpEnv + "=false"}
	cliPath, launchEnv, err := r.claudeLaunchSpec()
	if err != nil {
		return fmt.Errorf("taskrunner: claude code cli: %w", err)
	}
	if cliPath != "" {
		opts = append(opts, claudecode.WithCLIPath(cliPath))
		env = append(env, launchEnv...)
	}
	opts = append(opts, claudecode.WithEnv(env...))
	if model != "" && model != ClaudeModelDefault {
		opts = append(opts, claudecode.WithModel(model))
	}
	opts = append(opts, claudecode.WithPermissionPolicy(denyAllClaudePolicy{}))

	client, err := r.newClaudeClient(worktreePath, opts...)
	if err != nil {
		return fmt.Errorf("taskrunner: spawn isolated claude code agent: %w", err)
	}
	defer client.Close()

	timeout := iso.Timeout
	if timeout <= 0 {
		timeout = defaultIsolatedTimeout
	}
	tctx, cancelTimeout := context.WithTimeout(ctx, timeout)
	defer cancelTimeout()
	runCtx, cancel := context.WithCancel(tctx)
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
					tripped, violated = true, "tool call "+safeToolName(e.ToolName)
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
		// Kill the CLI first: a slow consumer must not keep it alive while
		// the final Done event waits for a reader.
		client.Close()
		r.sendFailedUsage(ctx, events, result)
		select {
		case events <- Event{Type: EventTypeDone, StopReason: isolationViolatedStop}:
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
		return fmt.Errorf("%w: %s", ErrIsolationViolated, reason)
	}
	if err != nil && tctx.Err() != nil && ctx.Err() == nil {
		return fmt.Errorf("%w: timeout after %s", ErrIsolatedRunFailed, timeout)
	}
	if err != nil {
		return fmt.Errorf("taskrunner: isolated claude code prompt: %w", err)
	}
	if result.IsError {
		r.sendFailedUsage(ctx, events, result)
		return fmt.Errorf("%w: %s", ErrIsolatedRunFailed, clipBytes(resultFailure(result), 200))
	}
	r.sendUsage(ctx, events, claudeUsage(result))

	select {
	case events <- Event{Type: EventTypeDone, StopReason: result.StopReason, Raw: result}:
	case <-ctx.Done():
	}
	return nil
}

// sendFailedUsage meters a run that is about to fail: the model call was made
// and billed, so when the CLI's result message carries usage it is recorded
// before the run is reported failed (the daily learning budget reads
// run_usage whatever the run's final status). A result with no usage (the CLI
// was killed, or timed out, before it reported) sends nothing: nothing is
// estimated, and the budget prices such a run pessimistically instead.
func (r *Runner) sendFailedUsage(ctx context.Context, events chan<- Event, result claudecode.ResultMessage) {
	u := claudeUsage(result)
	if u.Source == UsageSourceNotReported {
		return
	}
	select {
	case events <- Event{Type: EventTypeUsage, Usage: &u}:
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
	}
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

// FinalText returns "" when the final Done event has StopReason
// isolation_violated. err != nil from RunPromptIsolated means discard every
// event, whatever FinalText says.
//
// Otherwise it returns the text of the last assistant turn in events: the
// contiguous EventTypeText chunks ending the stream, ignoring trailing
// non-text events (usage, done). Any earlier non-text event (thinking, a
// tool call) ends the turn's text. "" when there is none.
func FinalText(events []Event) string {
	if n := len(events); n > 0 && events[n-1].Type == EventTypeDone && events[n-1].StopReason == isolationViolatedStop {
		return ""
	}
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
