package taskrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
)

// allowedOf is the CLI's --allowedTools value ("" and false when the flag is absent).
func allowedOf(args []string) (string, bool) { return flagValue(args, "--allowedTools") }

func preallowEnv(t *testing.T) (*Runner, *store.Store, store.Task) {
	t.Helper()
	r, st, task := newClaudeMcpEnv(t)
	r.claudeReportsPID = true
	return r, st, task
}

// A run that really receives the browser row (the pending-socket path built it and its socket is bound
// to the CLI) is pre-approved for the browser server's tools and for nothing else.
func TestClaudePreallow_OnlyTheBrowserWildcardForARunWithTheRow(t *testing.T) {
	t.Parallel()
	r, _, task := preallowEnv(t)
	spy := &pendingSpy{}
	args, cfg := runClaudeCtx(t, r, task, policyCtx(spy.policy()))
	got, ok := allowedOf(args)
	if !ok || got != "mcp__smind-browser__*" {
		t.Fatalf("--allowedTools = %q (present %v), want exactly mcp__smind-browser__*; argv %q", got, ok, args)
	}
	if !argPresent(args, "--strict-mcp-config") {
		t.Fatalf("a pre-allowed run must load only the daemon's MCP config (nothing from a project .mcp.json or user scope may answer to the reserved name): %q", args)
	}
	if strings.Count(strings.Join(args, "\x00"), "--allowedTools") != 1 {
		t.Fatalf("one --allowedTools flag only: %q", args)
	}
	if !strings.Contains(cfg, `"smind-browser"`) {
		t.Fatalf("the browser row is in the CLI's MCP config: %s", cfg)
	}
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.bound != 1 {
		t.Fatalf("the socket was bound to the CLI: %d", spy.bound)
	}
}

// No browser row, no pre-allow: not a single allowed tool.
func TestClaudePreallow_NoRowNoAllowedTools(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T) []string{
		"no browser policy": func(t *testing.T) []string {
			r, _, task := preallowEnv(t)
			args, _ := runClaudeCtx(t, r, task, policyCtx(&McpPolicy{}))
			return args
		},
		"no run contract at all": func(t *testing.T) []string {
			r, _, task := preallowEnv(t)
			args, _ := runClaudeCtx(t, r, task, t.Context())
			return args
		},
		"an SDK that cannot report the pid": func(t *testing.T) []string {
			r, _, task := newClaudeMcpEnv(t)
			r.claudeReportsPID = false
			args, _ := runClaudeCtx(t, r, task, policyCtx((&pendingSpy{}).policy()))
			return args
		},
		"a claude instance the reach table says no to": func(t *testing.T) []string {
			r, _, task := preallowEnv(t)
			reg := providers.New()
			if err := reg.Register(providers.Spec{ID: "cc-nomcp", Extends: providers.IDClaudeNative, NoMCPServers: true}); err != nil {
				t.Fatal(err)
			}
			WithProviderRegistry(reg)(r)
			spy := &pendingSpy{}
			args, _ := runClaudeProviderCtx(t, r, task, "cc-nomcp", policyCtx(spy.policy()))
			spy.mu.Lock()
			defer spy.mu.Unlock()
			if spy.opened != 0 {
				t.Fatalf("no socket for a provider without the row: %d", spy.opened)
			}
			return args
		},
		"a socket that cannot be opened": func(t *testing.T) []string {
			r, _, task := preallowEnv(t)
			spy := &pendingSpy{openErr: errors.New("no dir")}
			args, cfg := runClaudeCtx(t, r, task, policyCtx(spy.policy()))
			if strings.Contains(cfg, "smind-browser") {
				t.Fatalf("no socket, no row: %s", cfg)
			}
			return args
		},
	}
	for name, run := range cases {
		run := run
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, ok := allowedOf(run(t)); ok {
				t.Fatalf("no browser row must mean no allowed tools, got --allowedTools %q", got)
			}
		})
	}
}

// Whatever else is registered, the list is the one browser rule: never another server's tools, never the
// orchestration server's, never a built-in tool; a user server NAMED like the reserved one is stripped and
// never earns the rule.
func TestClaudePreallow_NeverAnotherServer(t *testing.T) {
	t.Parallel()
	t.Run("user servers beside the row", func(t *testing.T) {
		t.Parallel()
		r, st, task := preallowEnv(t)
		reg := mcpservers.New(st)
		for _, name := range []string{"pw", "files"} {
			if _, err := reg.Create(store.McpServer{Name: name, Transport: "stdio", Command: "npx", Enabled: true}); err != nil {
				t.Fatal(err)
			}
		}
		args, cfg := runClaudeCtx(t, r, task, policyCtx((&pendingSpy{}).policy()))
		if got, _ := allowedOf(args); got != "mcp__smind-browser__*" {
			t.Fatalf("--allowedTools = %q", got)
		}
		for _, name := range []string{`"pw"`, `"files"`} {
			if !strings.Contains(cfg, name) {
				t.Fatalf("the user server %s is still configured: %s", name, cfg)
			}
		}
	})
	t.Run("an orchestration seat gets no wildcard for the smind server", func(t *testing.T) {
		t.Parallel()
		r, _, task := preallowEnv(t)
		spy := &pendingSpy{}
		p := spy.policy()
		p.Orchestration = true
		p.Env = seatEnv()
		args, cfg := runClaudeCtx(t, r, task, policyCtx(p))
		got, _ := allowedOf(args)
		if got != "mcp__smind-browser__*" || strings.Contains(got, "mcp__smind__") || !strings.Contains(cfg, `"smind"`) {
			t.Fatalf("--allowedTools = %q (the seat's own smind server is configured but never pre-approved)", got)
		}
	})
	t.Run("a user server under the reserved prefix is not configured at all", func(t *testing.T) {
		t.Parallel()
		r, st, task := preallowEnv(t)
		if _, err := mcpservers.New(st).Create(store.McpServer{Name: "smind-browser__x", Transport: "stdio", Command: "evil", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		args, cfg := runClaudeCtx(t, r, task, policyCtx((&pendingSpy{}).policy()))
		if got, _ := allowedOf(args); got != "mcp__smind-browser__*" {
			t.Fatalf("--allowedTools = %q", got)
		}
		if strings.Contains(cfg, "smind-browser__x") || strings.Contains(cfg, "evil") {
			t.Fatalf("a server named smind-browser__x would sit under the browser wildcard: %s", cfg)
		}
	})
	t.Run("a user server named like the reserved one, no row", func(t *testing.T) {
		t.Parallel()
		r, st, task := preallowEnv(t)
		if _, err := mcpservers.New(st).Create(store.McpServer{Name: "smind-browser", Transport: "stdio", Command: "evil", Args: `["mcp","browser"]`, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		args, cfg := runClaudeCtx(t, r, task, policyCtx(&McpPolicy{}))
		if got, ok := allowedOf(args); ok {
			t.Fatalf("a user-registered smind-browser must never earn the rule: %q", got)
		}
		if strings.Contains(cfg, "evil") {
			t.Fatalf("the reserved name is stripped: %s", cfg)
		}
	})
	t.Run("a user server named like the reserved one, with the row", func(t *testing.T) {
		t.Parallel()
		r, st, task := preallowEnv(t)
		if _, err := mcpservers.New(st).Create(store.McpServer{Name: "smind-browser", Transport: "stdio", Command: "evil", Args: `["mcp","browser"]`, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		args, cfg := runClaudeCtx(t, r, task, policyCtx((&pendingSpy{}).policy()))
		if got, _ := allowedOf(args); got != "mcp__smind-browser__*" {
			t.Fatalf("--allowedTools = %q", got)
		}
		if strings.Contains(cfg, "evil") {
			t.Fatalf("only the daemon's row, not the user's: %s", cfg)
		}
	})
}

// The helper itself: the rule only for the daemon's own row with a bound socket.
func TestClaudePreallowRule(t *testing.T) {
	t.Parallel()
	row := func(name, transport, args string) store.McpServer {
		return store.McpServer{Name: name, Transport: transport, Command: "/opt/smind", Args: args, Enabled: true}
	}
	for name, tc := range map[string]struct {
		rows  []store.McpServer
		bound bool
		want  []string
	}{
		"the daemon's row, bound":          {[]store.McpServer{row("smind-browser", "stdio", `["mcp","browser"]`)}, true, []string{"mcp__smind-browser__*"}},
		"the daemon's row, unbound":        {[]store.McpServer{row("smind-browser", "stdio", `["mcp","browser"]`)}, false, nil},
		"no rows":                          {nil, true, nil},
		"another server":                   {[]store.McpServer{row("pw", "stdio", `["-y"]`)}, true, nil},
		"the orchestration server":         {[]store.McpServer{row("smind", "stdio", `["mcp","serve"]`)}, true, nil},
		"the reserved name, other command": {[]store.McpServer{row("smind-browser", "stdio", `["-y","evil"]`)}, true, nil},
		"the reserved name over http":      {[]store.McpServer{row("smind-browser", "http", `["mcp","browser"]`)}, true, nil},
		"a lookalike name":                 {[]store.McpServer{row("smind-browser-2", "stdio", `["mcp","browser"]`)}, true, nil},
	} {
		if got := claudeBrowserPreallow(tc.rows, tc.bound); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
	if browserToolsAllowRule != "mcp__smind-browser__*" {
		t.Fatalf("the rule is pinned: %q", browserToolsAllowRule)
	}
}

// ACP and Codex runs have no allowed-tools mechanism: the claude path is the only caller of the helper, and a
// provider whose row is not delivered never reaches it with a bound socket.
func TestClaudePreallow_OnlyTheClaudeRunnerIsWired(t *testing.T) {
	t.Parallel()
	reg := providers.New()
	for id, want := range map[string]bool{providers.IDClaudeNative: true, providers.IDCodexNative: false, providers.IDGLM: false, providers.IDOpenCode: false} {
		spec, _ := reg.Get(id)
		isClaude := spec.Runner == providers.RunnerClaude
		if isClaude != want {
			t.Errorf("%s: runner %s", id, spec.Runner)
		}
	}
}

// runClaudeProviderCtx is runClaudeCtx for any claude-harness provider id.
func runClaudeProviderCtx(t *testing.T, r *Runner, task store.Task, provider string, ctx context.Context) ([]string, string) {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, Provider(provider), "hi", denyAllDecider{}, PermissionSettings{}, "", testClaudeModel, events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "args"))
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(*task.WorktreePath, "mcp-config-content"))
	return strings.Split(string(data), "\n"), string(content)
}

func argPresent(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// No pre-allow, no change to how the run loads MCP servers: a browser-only run without the row keeps
// the user's own servers (strict mode is never added on its own).
func TestClaudePreallow_StrictOnlyWithThePreallow(t *testing.T) {
	t.Parallel()
	r, _, task := preallowEnv(t)
	r.claudeReportsPID = false // the SDK cannot say the pid: no row, no pre-allow
	args, _ := runClaudeCtx(t, r, task, policyCtx((&pendingSpy{}).policy()))
	if _, ok := allowedOf(args); ok || argPresent(args, "--strict-mcp-config") {
		t.Fatalf("no row: no pre-allow and no strict mode: %q", args)
	}
}
