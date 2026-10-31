package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/runs/failure"
	"github.com/spacingmind/smind/internal/store"
)

// Hooks (ADR-0042 section 4, PB.3). A plugin process may subscribe to two
// events, declared in its manifest `hooks`:
//
//   - run.finished: observe only. A `hook.run.finished` notification with a
//     redacted summary, best effort, delivered from a per-plugin queue so a
//     slow or stuck plugin never delays the daemon.
//   - run.beforeStart: request/response. `hook.run.beforeStart` is answered
//     with {additionalContext, env, mcpServers}. The HOST validates the answer
//     (never the plugin's say-so): it may append instructions, add environment
//     variables and name MCP servers the same plugin contributes. Any other
//     field (provider, cwd, model, profile, permission mode, auto-accept, a
//     permission decision, a rewritten tool input, ...) fails the hook with a
//     typed error. There is no hook, field or method that answers a permission
//     request (ADR-0019).
//
// Names follow Claude Code's hook vocabulary where the meaning is the same
// (`additionalContext` is SessionStart's field; `Stop` is run.finished). Exit
// codes do not apply: a plugin hook answers with a JSON-RPC result, and a
// JSON-RPC error is how a `closed` hook stops a start.

// Hook events and the host-to-plugin methods that carry them.
const (
	HookRunBeforeStart = "run.beforeStart"
	HookRunFinished    = "run.finished"

	hookMethodBeforeStart = "hook.run.beforeStart"
	hookMethodFinished    = "hook.run.finished"

	// CapHooksBeforeRun is the capability a plugin must declare for its
	// run.beforeStart hook to run.
	CapHooksBeforeRun = "hooks.beforeRun"
)

// HookTimeout bounds one hook call (a variable only so tests can shorten it;
// TestHook_Timeout_DefaultIsTenSeconds pins the default).
var HookTimeout = 10 * time.Second

// Hook limits. The numbers are documented in ADR-0042's PB.3 note.
const (
	MaxHookContextBytes  = 8 << 10
	MaxHookContextTotal  = 16 << 10
	MaxHookEnvPerHook    = 16
	MaxHookEnvTotal      = 32
	MaxHookEnvValueBytes = 4 << 10
	MaxHookMcpPerHook    = 16
	finishedQueueLen     = 64
)

// Hook error codes (HookError.Code).
const (
	HookErrTimeout       = "timeout"
	HookErrNotRunning    = "not_running"
	HookErrNoCapability  = "capability_missing"
	HookErrPlugin        = "plugin_error"
	HookErrForbiddenEdit = "forbidden_edit"
	HookErrUnknownField  = "unknown_field"
	HookErrInvalid       = "invalid_result"
	HookErrLimit         = "limit"
	HookErrEnv           = "env_refused"
	HookErrMcp           = "mcp_refused"
)

// HookError is a typed hook failure. Closed is the hook's onError policy:
// a closed failure stops the run start, an open one is surfaced and the run
// goes on.
type HookError struct {
	Plugin string
	Event  string
	Code   string
	Reason string
	Closed bool
}

func (e *HookError) Error() string {
	return fmt.Sprintf("plugin %s hook %s: %s: %s", e.Plugin, e.Event, e.Code, e.Reason)
}

// BeforeStartInput is what run.beforeStart is told about the run: ids and the
// settings the run was resolved with, never the prompt.
type BeforeStartInput struct {
	RunID          string `json:"runId"`
	TaskID         int64  `json:"taskId"`
	ChatID         int64  `json:"chatId"`
	WorkspaceID    int64  `json:"workspaceId"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	PermissionMode string `json:"permissionMode"`
	Origin         string `json:"origin"`
}

// BeforeStartResult is what the validated hook answers add up to.
type BeforeStartResult struct {
	// Suffix is appended to the run's prompt: one labeled block per plugin.
	Suffix string
	// Env holds variables to add to the agent's environment (never an
	// existing key; the runner re-checks against the real environment).
	Env map[string]string
	// Mcp holds the run-named rows of servers a hook named (all owned by the
	// plugin that named them).
	Mcp []store.McpServer
	// Failures are the hooks that failed, open ones included, in order.
	Failures []*HookError
	// Notes say, for the run's timeline, what each plugin contributed.
	Notes []string
}

// Empty reports that nothing was contributed.
func (r BeforeStartResult) Empty() bool {
	return r.Suffix == "" && len(r.Env) == 0 && len(r.Mcp) == 0
}

// RunFinishedInput is the run.finished payload source: no prompt, no output.
type RunFinishedInput struct {
	RunID       string `json:"runId"`
	TaskID      int64  `json:"taskId"`
	ChatID      int64  `json:"chatId"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Origin      string `json:"origin"`
	Status      string `json:"status"`
	StopReason  string `json:"stopReason,omitempty"`
	FailureKind string `json:"failureKind,omitempty"`
	// Action is the failure's next step; its label and target are redacted.
	Action    *failure.Action `json:"action,omitempty"`
	ToolCalls int             `json:"toolCalls"`
}

// hookState is the Manager's hook bookkeeping: the last problem per plugin and
// event (shown as lastError and cleared by the next success or a reload) and
// the per-plugin run.finished queues.
type hookState struct {
	mu       sync.Mutex
	problems map[string]map[string]string // plugin -> event -> text
	fin      map[string]chan RunFinishedInput
	closed   bool
}

type hookReg struct {
	plugin  string
	event   string
	onError string
	caps    []string
}

func (r hookReg) closedPolicy() bool { return r.onError == "closed" }

// hookRegs lists the active plugins' registrations for event, ordered by
// plugin id then manifest order.
func (m *Manager) hookRegs(event string) []hookReg {
	if m.proc == nil || !m.GlobalEnabled() {
		return nil
	}
	infos := m.Active()
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	var out []hookReg
	for _, in := range infos {
		man := m.loadedManifest(in)
		if man == nil || man.Process == nil {
			continue
		}
		for _, h := range man.Contributes.Hooks {
			if h.Event == event {
				out = append(out, hookReg{plugin: in.ID, event: event, onError: h.OnError, caps: man.Capabilities})
			}
		}
	}
	return out
}

func (m *Manager) redactStr(s string) string {
	if m.proc == nil {
		return s
	}
	if r := m.proc.red.Load(); r != nil {
		return r.String(s)
	}
	return s
}

// notePlugin records a problem for a plugin (shown as its lastError until a
// success or a reload clears it), writes it to the plugin's log and publishes
// plugin.updated.
func (m *Manager) notePlugin(plugin, key, text string) {
	text = m.redactStr(text)
	m.hooks.mu.Lock()
	if m.hooks.problems == nil {
		m.hooks.problems = map[string]map[string]string{}
	}
	if m.hooks.problems[plugin] == nil {
		m.hooks.problems[plugin] = map[string]string{}
	}
	prev := m.hooks.problems[plugin][key]
	m.hooks.problems[plugin][key] = text
	m.hooks.mu.Unlock()
	if m.proc != nil {
		m.proc.h.Logf(plugin, "%s", text)
	}
	if prev != text {
		m.changed(plugin)
	}
}

// NotePluginProblem is notePlugin for the daemon's own plugin-facing code (the
// task.send limiter): the text is what the Human sees on the Plugins page.
func (m *Manager) NotePluginProblem(plugin, key, text string) { m.notePlugin(plugin, key, text) }

func (m *Manager) clearProblem(plugin, key string) {
	m.hooks.mu.Lock()
	_, had := m.hooks.problems[plugin][key]
	delete(m.hooks.problems[plugin], key)
	m.hooks.mu.Unlock()
	if had {
		m.changed(plugin)
	}
}

func (m *Manager) clearProblems(plugin string) {
	m.hooks.mu.Lock()
	delete(m.hooks.problems, plugin)
	m.hooks.mu.Unlock()
}

// problemText is the plugin's most relevant open problem, "" when none.
func (m *Manager) problemText(plugin string) string {
	m.hooks.mu.Lock()
	defer m.hooks.mu.Unlock()
	ps := m.hooks.problems[plugin]
	if len(ps) == 0 {
		return ""
	}
	keys := make([]string, 0, len(ps))
	for k := range ps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return ps[keys[0]]
}

func (m *Manager) changed(id string) {
	if m.proc == nil {
		return
	}
	if f := m.proc.onChange.Load(); f != nil {
		(*f)(id)
	}
}

// RunBeforeStart runs every active plugin's run.beforeStart hook, in plugin id
// then registration order, and returns the validated sum of their answers. The
// error is non-nil only for a `closed` hook that failed (the start must stop;
// it is a *HookError) or for ctx ending. Open failures are in Failures.
func (m *Manager) RunBeforeStart(ctx context.Context, in BeforeStartInput) (BeforeStartResult, error) {
	var res BeforeStartResult
	regs := m.hookRegs(HookRunBeforeStart)
	if len(regs) == 0 {
		return res, nil
	}
	in.Provider, in.Model, in.PermissionMode, in.Origin = m.redactStr(in.Provider), m.redactStr(in.Model), m.redactStr(in.PermissionMode), m.redactStr(in.Origin)
	acc := &hookAccum{env: map[string]string{}, mcp: map[string]bool{}}
	for _, reg := range regs {
		edit, herr := m.callBeforeStart(ctx, reg, in)
		if herr == nil {
			herr = acc.add(reg.plugin, edit)
		}
		if herr != nil {
			herr.Closed = reg.closedPolicy()
			res.Failures = append(res.Failures, herr)
			m.notePlugin(reg.plugin, HookRunBeforeStart, fmt.Sprintf("hook %s failed (%s): %s", HookRunBeforeStart, herr.Code, herr.Reason))
			slog.Warn("plugins: hook failed", "plugin", reg.plugin, "event", HookRunBeforeStart, "code", herr.Code, "onError", herrPolicy(reg), "run", in.RunID)
			if herr.Closed {
				res.fill(acc)
				return res, herr
			}
			continue
		}
		m.clearProblem(reg.plugin, HookRunBeforeStart)
		slog.Info("plugins: hook ok", "plugin", reg.plugin, "event", HookRunBeforeStart, "run", in.RunID,
			"contextBytes", len(edit.context), "envKeys", len(edit.env), "mcpServers", len(edit.mcp))
		if m.proc != nil {
			m.proc.h.Logf(reg.plugin, "hook %s: %d bytes of instructions, %d env vars, %d MCP servers", HookRunBeforeStart, len(edit.context), len(edit.env), len(edit.mcp))
		}
	}
	res.fill(acc)
	return res, ctx.Err()
}

func herrPolicy(r hookReg) string {
	if r.closedPolicy() {
		return "closed"
	}
	return "open"
}

func (r *BeforeStartResult) fill(a *hookAccum) {
	r.Suffix, r.Env, r.Mcp, r.Notes = a.suffix.String(), a.env, a.rows, a.notes
	if len(r.Env) == 0 {
		r.Env = nil
	}
}

func (m *Manager) callBeforeStart(ctx context.Context, reg hookReg, in BeforeStartInput) (hookEdit, *HookError) {
	fail := func(code, format string, a ...any) (hookEdit, *HookError) {
		return hookEdit{}, &HookError{Plugin: reg.plugin, Event: reg.event, Code: code, Reason: m.redactStr(fmt.Sprintf(format, a...))}
	}
	if !contains(reg.caps, CapHooksBeforeRun) {
		return fail(HookErrNoCapability, "the plugin declares run.beforeStart but not the %s capability", CapHooksBeforeRun)
	}
	if st, ok := m.ProcessStatus(reg.plugin); !ok || st.State != host.StateRunning {
		state := "not running"
		if ok {
			state = string(st.State)
		}
		return fail(HookErrNotRunning, "skipped: the plugin process is %s", state)
	}
	cctx, cancel := context.WithTimeout(ctx, HookTimeout)
	defer cancel()
	raw, err := m.proc.h.Call(cctx, reg.plugin, hookMethodBeforeStart, in)
	if err != nil {
		var rpc *host.RPCError
		switch {
		case ctx.Err() != nil:
			return fail(HookErrPlugin, "the run was stopped before the hook answered")
		case errors.Is(err, context.DeadlineExceeded) || cctx.Err() != nil:
			return fail(HookErrTimeout, "no answer within %s", HookTimeout)
		case errors.As(err, &rpc):
			return fail(HookErrPlugin, "%s", clip(rpc.Message, 300))
		case errors.Is(err, host.ErrNotRunning):
			return fail(HookErrNotRunning, "skipped: the plugin process is not running")
		}
		return fail(HookErrPlugin, "%s", clip(err.Error(), 300))
	}
	edit, herr := m.validateBeforeStart(reg.plugin, raw)
	if herr != nil {
		herr.Event = reg.event
		return hookEdit{}, herr
	}
	return edit, nil
}

// hookEdit is one hook's validated answer.
type hookEdit struct {
	context string
	env     map[string]string
	mcp     []store.McpServer
}

// forbiddenHookFields are answers a hook may never give: they name something
// the host decides (provider, cwd, model, profile, permission mode,
// auto-accept) or something that would make a plugin a permission oracle or a
// tool-call rewriter (ADR-0019). Compared in lower case.
var forbiddenHookFields = map[string]bool{
	"provider": true, "cwd": true, "workdir": true, "workingdirectory": true, "worktree": true,
	"model": true, "profile": true, "profileid": true, "seat": true,
	"permissionmode": true, "mode": true, "autoaccept": true, "approvalpolicy": true, "sandbox": true,
	"permissiondecision": true, "permissiondecisionreason": true, "decision": true, "approve": true, "approved": true,
	"deny": true, "denied": true, "allow": true, "behavior": true, "permissions": true, "updatedpermissions": true,
	"updatedinput": true, "toolinput": true, "tools": true, "allowedtools": true, "disallowedtools": true,
	"prompt": true, "blocked": true, "continue": true, "stop": true, "args": true, "command": true,
}

// validateBeforeStart parses a run.beforeStart answer strictly. Anything but
// the three known fields, in their exact types and within the caps, fails the
// hook; nothing is applied from a failed answer.
func (m *Manager) validateBeforeStart(plugin string, raw json.RawMessage) (hookEdit, *HookError) {
	fail := func(code, format string, a ...any) (hookEdit, *HookError) {
		return hookEdit{}, &HookError{Plugin: plugin, Code: code, Reason: m.redactStr(fmt.Sprintf(format, a...))}
	}
	var fields map[string]json.RawMessage
	if t := strings.TrimSpace(string(raw)); t == "" || t == "null" {
		return hookEdit{}, nil
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fail(HookErrInvalid, "the result is not a JSON object")
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch k {
		case "additionalContext", "env", "mcpServers":
		default:
			if forbiddenHookFields[strings.ToLower(k)] {
				return fail(HookErrForbiddenEdit, "the hook tried to set %q: a hook may only append instructions, add environment variables and name its own MCP servers", clip(k, 40))
			}
			return fail(HookErrUnknownField, "unknown result field %q (allowed: additionalContext, env, mcpServers)", clip(k, 40))
		}
	}
	var e hookEdit
	if v, ok := fields["additionalContext"]; ok && string(v) != "null" {
		if err := json.Unmarshal(v, &e.context); err != nil {
			return fail(HookErrInvalid, "additionalContext must be a string")
		}
		if len(e.context) > MaxHookContextBytes {
			return fail(HookErrLimit, "additionalContext is %d bytes (the limit is %d)", len(e.context), MaxHookContextBytes)
		}
		e.context = strings.ReplaceAll(e.context, "\x00", "")
	}
	if v, ok := fields["env"]; ok && string(v) != "null" {
		var env map[string]string
		if err := json.Unmarshal(v, &env); err != nil {
			return fail(HookErrInvalid, "env must be an object of string values")
		}
		if len(env) > MaxHookEnvPerHook {
			return fail(HookErrLimit, "env has %d entries (the limit is %d)", len(env), MaxHookEnvPerHook)
		}
		names := make([]string, 0, len(env))
		for k := range env {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			if why := refuseHookEnv(k, env[k]); why != "" {
				return fail(HookErrEnv, "%s", why)
			}
		}
		e.env = env
	}
	if v, ok := fields["mcpServers"]; ok && string(v) != "null" {
		var names []string
		if err := json.Unmarshal(v, &names); err != nil {
			return fail(HookErrInvalid, "mcpServers must be a list of server names")
		}
		if len(names) > MaxHookMcpPerHook {
			return fail(HookErrLimit, "mcpServers has %d entries (the limit is %d)", len(names), MaxHookMcpPerHook)
		}
		rows, why := m.ownedMcp(plugin, names)
		if why != "" {
			return fail(HookErrMcp, "%s", why)
		}
		e.mcp = rows
	}
	return e, nil
}

var hookEnvKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// reservedEnvPrefixes and reservedEnvNames are variables a hook may not set:
// the daemon's own, every provider's auth and routing, loader and interpreter
// hooks that run code, and proxies.
var (
	reservedEnvPrefixes = []string{
		"SMIND_", "ANTHROPIC_", "CLAUDE", "OPENAI_", "CODEX_", "GLM_", "ZAI_", "Z_AI_", "KIMI_", "MOONSHOT_", "GOOGLE_", "GEMINI_",
		"AWS_", "AZURE_", "GCP_", "GIT_", "SSH_", "GH_", "GITHUB_", "LD_", "DYLD_", "NODE_", "NPM_", "PYTHON", "RUBY", "PERL", "JAVA_", "BASH_", "LC_",
	}
	reservedEnvNames = map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "PWD": true, "OLDPWD": true, "TMPDIR": true, "TEMP": true, "TMP": true,
		"IFS": true, "ENV": true, "PS4": true, "PROMPT_COMMAND": true, "LANG": true, "TZ": true, "EDITOR": true, "VISUAL": true, "PAGER": true,
		"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true, "NODE_OPTIONS": true, "SYSTEMROOT": true, "COMSPEC": true, "PATHEXT": true,
	}
	reservedEnvSuffixes = []string{"_KEY", "_TOKEN", "_SECRET", "_PASSWORD", "_PASSWD", "_CREDENTIALS", "_CREDENTIAL", "_PROXY", "_AUTH"}
)

// refuseHookEnv explains why a hook may not set key=value, "" when it may.
func refuseHookEnv(key, value string) string {
	if !hookEnvKey.MatchString(key) {
		return fmt.Sprintf("env name %q is not allowed: use upper-case letters, digits and underscores, starting with a letter", clip(key, 40))
	}
	if reservedEnvNames[key] {
		return fmt.Sprintf("env %s is reserved", key)
	}
	for _, p := range reservedEnvPrefixes {
		if strings.HasPrefix(key, p) {
			return fmt.Sprintf("env %s is reserved (%s* belongs to the daemon, a provider or a loader)", key, p)
		}
	}
	for _, s := range reservedEnvSuffixes {
		if strings.HasSuffix(key, s) {
			return fmt.Sprintf("env %s looks like a credential or proxy setting and is refused", key)
		}
	}
	if len(value) > MaxHookEnvValueBytes {
		return fmt.Sprintf("env %s is %d bytes (the limit is %d)", key, len(value), MaxHookEnvValueBytes)
	}
	for _, r := range value {
		if r < 0x20 && r != '\t' {
			return fmt.Sprintf("env %s holds a control character", key)
		}
	}
	return ""
}

// ownedMcp resolves names against the servers this plugin contributes (the
// manifest name or the run name `<plugin>__<name>`), as the rows a run gets.
// Any other name, another plugin's included, is refused.
func (m *Manager) ownedMcp(plugin string, names []string) ([]store.McpServer, string) {
	own := map[string]store.McpServer{}
	for _, row := range m.OwnedMcpForRun() {
		if row.OwnerPlugin == plugin {
			own[row.Name] = row
		}
	}
	var out []store.McpServer
	seen := map[string]bool{}
	for _, n := range names {
		run := n
		if !strings.HasPrefix(n, plugin+"__") {
			run = wireName(plugin, n)
		}
		row, ok := own[run]
		if !ok || n == "" {
			return nil, fmt.Sprintf("MCP server %q is not contributed by plugin %s: a hook may only name its own servers", clip(n, 60), plugin)
		}
		if seen[run] {
			continue
		}
		seen[run] = true
		out = append(out, row)
	}
	return out, ""
}

// hookAccum adds up the validated answers of the hooks of one start.
type hookAccum struct {
	suffix strings.Builder
	total  int
	env    map[string]string
	mcp    map[string]bool
	rows   []store.McpServer
	notes  []string
}

// add merges one plugin's answer. Instructions are appended in call order,
// each in its own labeled block; env is merged key by key and the first plugin
// to set a key keeps it (a later one never overrides, and values are never
// combined); servers are a union by run name. No deep merge exists.
func (a *hookAccum) add(plugin string, e hookEdit) *HookError {
	if a.total+len(e.context) > MaxHookContextTotal {
		return &HookError{Plugin: plugin, Code: HookErrLimit, Reason: fmt.Sprintf("the hooks' instructions add up to more than %d bytes", MaxHookContextTotal)}
	}
	keys := make([]string, 0, len(e.env))
	for k := range e.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fresh := 0
	for _, k := range keys {
		if _, taken := a.env[k]; !taken {
			fresh++
		}
	}
	if len(a.env)+fresh > MaxHookEnvTotal {
		return &HookError{Plugin: plugin, Code: HookErrLimit, Reason: fmt.Sprintf("the hooks' env adds up to more than %d variables", MaxHookEnvTotal)}
	}
	a.total += len(e.context)
	var did []string
	if e.context != "" {
		fmt.Fprintf(&a.suffix, "\n\n[plugin %s]\n%s\n[end plugin %s]", plugin, strings.TrimSpace(e.context), plugin)
		did = append(did, fmt.Sprintf("%d bytes of instructions", len(e.context)))
	}
	added := 0
	for _, k := range keys {
		if _, taken := a.env[k]; taken {
			continue
		}
		a.env[k] = e.env[k]
		added++
	}
	if added > 0 {
		did = append(did, fmt.Sprintf("%d env vars", added))
	}
	rows := 0
	for _, r := range e.mcp {
		if a.mcp[r.Name] {
			continue
		}
		a.mcp[r.Name] = true
		a.rows = append(a.rows, r)
		rows++
	}
	if rows > 0 {
		did = append(did, fmt.Sprintf("%d MCP servers", rows))
	}
	if len(did) > 0 {
		a.notes = append(a.notes, fmt.Sprintf("plugin %s run.beforeStart added %s", plugin, strings.Join(did, ", ")))
	}
	return nil
}

// NotifyRunFinished delivers run.finished to every active plugin that
// subscribed to it. It never blocks: each plugin has its own bounded queue and
// goroutine, an overflow drops the newest event, and a plugin that is not
// running is skipped.
func (m *Manager) NotifyRunFinished(in RunFinishedInput) {
	regs := m.hookRegs(HookRunFinished)
	if len(regs) == 0 {
		return
	}
	in.Provider, in.Model, in.Origin = m.redactStr(in.Provider), m.redactStr(in.Model), m.redactStr(in.Origin)
	in.Status, in.StopReason, in.FailureKind = m.redactStr(in.Status), m.redactStr(in.StopReason), m.redactStr(in.FailureKind)
	if in.Action != nil {
		a := *in.Action
		a.Label, a.Target = m.redactStr(a.Label), m.redactStr(a.Target)
		in.Action = &a
	}
	seen := map[string]bool{}
	for _, reg := range regs {
		if seen[reg.plugin] {
			continue
		}
		seen[reg.plugin] = true
		if ch := m.finishedQueue(reg.plugin); ch != nil {
			select {
			case ch <- in:
			default:
				slog.Warn("plugins: run.finished dropped (plugin queue full)", "plugin", reg.plugin, "run", in.RunID)
			}
		}
	}
}

// finishedQueue returns the plugin's run.finished queue, starting its
// delivery goroutine on first use; nil once the Manager shut down.
func (m *Manager) finishedQueue(plugin string) chan RunFinishedInput {
	m.hooks.mu.Lock()
	defer m.hooks.mu.Unlock()
	if m.hooks.closed {
		return nil
	}
	if m.hooks.fin == nil {
		m.hooks.fin = map[string]chan RunFinishedInput{}
	}
	ch := m.hooks.fin[plugin]
	if ch == nil {
		ch = make(chan RunFinishedInput, finishedQueueLen)
		m.hooks.fin[plugin] = ch
		go m.deliverFinished(plugin, ch)
	}
	return ch
}

func (m *Manager) deliverFinished(plugin string, ch chan RunFinishedInput) {
	for in := range ch {
		if st, ok := m.ProcessStatus(plugin); !ok || st.State != host.StateRunning {
			continue
		}
		if err := m.proc.h.Notify(plugin, hookMethodFinished, in); err != nil {
			slog.Debug("plugins: run.finished not delivered", "plugin", plugin, "err", err)
		}
	}
}

func (m *Manager) closeHooks() {
	m.hooks.mu.Lock()
	defer m.hooks.mu.Unlock()
	if m.hooks.closed {
		return
	}
	m.hooks.closed = true
	for _, ch := range m.hooks.fin {
		close(ch)
	}
}
