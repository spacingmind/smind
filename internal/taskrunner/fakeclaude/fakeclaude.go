// Package fakeclaude is a scripted stand-in for the `claude` CLI's headless
// stream-json protocol, for tests only (never imported by production code).
// A test binary re-executes itself as the fake CLI: its TestMain calls Run
// when an env switch is set, and the Runner is pointed at os.Executable()
// (taskrunner.WithClaudeCLIPath).
package fakeclaude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/securefile"
)

// ScenarioEnv, when set, names the scenario instead of the "scenario" file in
// the working directory -- for callers whose cwd is cleaned between runs.
const ScenarioEnv = "FAKE_CLAUDE_SCENARIO"

// Run is a minimal NDJSON scripted stand-in for the `claude`
// CLI's headless stream-json protocol. Like fakeagent, its scenario comes
// from a "scenario" file in its working directory (claudecode.Client sets
// the subprocess's cmd.Dir to the task's worktree).
func Run() {
	wd, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	scenario := "reply"
	if v := os.Getenv(ScenarioEnv); v != "" {
		scenario = v
	} else if data, err := os.ReadFile(filepath.Join(wd, "scenario")); err == nil {
		scenario = strings.TrimSpace(string(data))
	}

	if scenario == "resume-fails" {
		for _, a := range os.Args[1:] {
			if strings.HasPrefix(a, "--resume=") {
				// Simulate a stale/unknown session id: exit before even
				// acknowledging the initialize control_request -- matching
				// the real `claude` CLI's observed live behavior (probed
				// 2026-09-27 with a bogus --resume value): the SDK's own
				// New() fails with "connection closed", before any prompt
				// is ever sent.
				os.Exit(1)
			}
		}
		// No --resume flag: behaves exactly like "reply" -- proving the
		// runner's own fallback retry (without WithResume) succeeds.
		scenario = "reply"
	}

	stdin := bufio.NewScanner(os.Stdin)
	stdin.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	writeLine := func(v any) {
		data, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(os.Stdout, "%s\n", data)
	}

	// The client always sends an initialize control_request in New(),
	// before any prompt -- mirrors claude-agent-sdk-go's own
	// fakecli_test.go ackInitialize helper. Every scenario below assumes
	// the control channel is already established by the time it starts
	// reading the actual prompt line.
	if !stdin.Scan() {
		os.Exit(1)
	}
	var initEnv struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(stdin.Bytes(), &initEnv); err != nil || initEnv.Type != "control_request" {
		fmt.Fprintf(os.Stderr, "fake claude cli: expected initialize control_request, got %q\n", stdin.Text())
		os.Exit(1)
	}
	writeLine(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": initEnv.RequestID,
			"response":   map[string]any{"success": true},
		},
	})

	switch scenario {
	case "reply":
		stdin.Scan() // consume the prompt line

		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"model": "claude-fake",
				"content": []any{
					map[string]any{"type": "text", "text": "hello "},
					map[string]any{"type": "text", "text": "from claude"},
				},
			},
		})

		writeLine(map[string]any{
			"type":        "result",
			"is_error":    false,
			"num_turns":   1,
			"session_id":  "sess-1",
			"stop_reason": "end_turn",
			"result":      "done",
		})

	case "stale-notification":
		// Resuming a session whose previous run left a background shell
		// command behind: before it handles the prompt the CLI reports the
		// command lost (task_notification) and answers that with its own
		// empty zero-turn result (observed against claude 2.1.303). The real
		// reply is the next result. The runner must not stop at the first.
		stdin.Scan() // consume the prompt line

		writeLine(map[string]any{
			"type":       "system",
			"subtype":    "task_notification",
			"task_id":    "bg-1",
			"status":     "stopped",
			"summary":    "Background shell command didn't finish before the previous session ended",
			"session_id": "sess-1",
		})
		writeLine(map[string]any{
			"type":        "result",
			"subtype":     "success",
			"is_error":    false,
			"num_turns":   0,
			"session_id":  "sess-1",
			"stop_reason": nil,
			"result":      "",
			"usage":       map[string]any{"input_tokens": 0, "output_tokens": 0},
			// Under the SDK's session the stale result repeats the previous
			// turn's modelUsage (observed live), so it is not empty.
			"modelUsage": map[string]any{"claude-fake": map[string]any{"inputTokens": 1220, "outputTokens": 185}},
		})
		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"model": "claude-fake",
				"content": []any{
					map[string]any{"type": "text", "text": "pong"},
				},
			},
		})
		writeLine(map[string]any{
			"type":        "result",
			"is_error":    false,
			"num_turns":   1,
			"session_id":  "sess-1",
			"stop_reason": "end_turn",
			"result":      "pong",
		})

	case "empty-result-no-notification":
		// A genuinely empty result with no task_notification before it (a
		// local slash command with no output): the run must end here, not
		// wait for another result.
		stdin.Scan() // consume the prompt line
		writeLine(map[string]any{
			"type":        "result",
			"is_error":    false,
			"num_turns":   0,
			"session_id":  "sess-1",
			"stop_reason": nil,
			"result":      "",
			"modelUsage":  map[string]any{},
		})

	case "usage", "usage-no-model":
		// A result carrying per-model usage and cost, for the usage event.
		stdin.Scan() // consume the prompt line
		result := map[string]any{
			"type":           "result",
			"subtype":        "success",
			"session_id":     "sess-1",
			"stop_reason":    "end_turn",
			"result":         "done",
			"total_cost_usd": 0.5,
			"modelUsage": map[string]any{
				"claude-small": map[string]any{"inputTokens": 10, "outputTokens": 5, "costUSD": 0.1, "contextWindow": 200000},
				"claude-big": map[string]any{
					"inputTokens": 100, "outputTokens": 40, "cacheReadInputTokens": 300,
					"cacheCreationInputTokens": 20, "costUSD": 0.4, "contextWindow": 1000000,
				},
			},
		}
		if scenario == "usage-no-model" {
			delete(result, "modelUsage")
			delete(result, "total_cost_usd")
		}
		writeLine(result)

	case "hang":
		stdin.Scan() // consume the prompt line, then never respond or exit
		time.Sleep(time.Hour)

	case "init-hang":
		// Emits the init system message carrying the session id, one text
		// chunk to synchronize on, then blocks forever -- so a test can
		// cancel the run after the session id exists but before the turn
		// completes, proving the handle was already stored (runClaudeNative
		// captures it from the init message, not only the result).
		stdin.Scan() // consume the prompt line
		writeLine(map[string]any{
			"type":       "system",
			"subtype":    "init",
			"session_id": "sess-1",
		})
		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"model": "claude-fake",
				"content": []any{
					map[string]any{"type": "text", "text": "before hang"},
				},
			},
		})
		// Unlike the plain "hang" scenario, block on stdin EOF rather than
		// sleeping: Close()'s stdin-close -> grace -> force-kill teardown
		// reaches this process's EOF immediately, so the test isn't slowed
		// by the grace period after a cancelled run.
		for stdin.Scan() {
		}

	case "echo-args":
		// Writes the CLI argv back to an "args" file in the working
		// directory, so a test can assert exactly which flags RunPrompt
		// spawned the CLI with (e.g. --permission-mode) --
		// the flags aren't observable through the wire protocol itself.
		if err := os.WriteFile(filepath.Join(wd, "args"), []byte(strings.Join(os.Args[1:], "\n")), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fake claude cli: write args: %v\n", err)
			os.Exit(1)
		}
		// Dumps the dialog-timeout env var the subprocess received, so a
		// test can assert runClaudeNative wired CLAUDE_CODE_USER_DIALOG_TIMEOUT_MS
		// onto the CLI's environment -- like argv, not observable through
		// the wire protocol.
		if err := os.WriteFile(filepath.Join(wd, "env"), []byte(os.Getenv("CLAUDE_CODE_USER_DIALOG_TIMEOUT_MS")), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fake claude cli: write env: %v\n", err)
			os.Exit(1)
		}
		// Same for the claude.ai-connectors opt-out, which only isolated
		// runs set.
		if err := os.WriteFile(filepath.Join(wd, "env-claudeai-mcp"), []byte(os.Getenv("ENABLE_CLAUDEAI_MCP_SERVERS")), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fake claude cli: write env-claudeai-mcp: %v\n", err)
			os.Exit(1)
		}
		// When --mcp-config names a file, snapshot its contents and the
		// file/dir permission bits at spawn time (the runner removes the
		// file once the run ends), so a test can assert on them.
		for i, a := range os.Args[1:] {
			if a != "--mcp-config" || i+2 >= len(os.Args) {
				continue
			}
			cfg := os.Args[i+2]
			if data, err := os.ReadFile(cfg); err == nil {
				_ = os.WriteFile(filepath.Join(wd, "mcp-config-content"), data, 0o644)
				fi, _ := os.Stat(cfg)
				di, _ := os.Stat(filepath.Dir(cfg))
				fileMode, dirMode := fmt.Sprintf("%o", fi.Mode().Perm()), fmt.Sprintf("%o", di.Mode().Perm())
				if runtime.GOOS == "windows" {
					// No POSIX mode bits: the file must be owner-only by
					// ACL (internal/securefile); the temp dir sits under the
					// user's %TEMP% and has no mode to assert.
					fileMode, dirMode = "not-private", "n/a"
					if private, err := securefile.IsPrivate(cfg); err == nil && private {
						fileMode = "600"
					}
				}
				modes := fileMode + " " + dirMode
				_ = os.WriteFile(filepath.Join(wd, "mcp-config-modes"), []byte(modes), 0o644)
			}
		}
		stdin.Scan() // consume the prompt line
		// The raw stream-json user message, for tests asserting what the
		// prompt text carried (e.g. appended attachment blocks).
		_ = os.WriteFile(filepath.Join(wd, "prompt-line"), []byte(stdin.Text()), 0o644)
		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": "ok"},
				},
			},
		})
		writeLine(map[string]any{
			"type":       "result",
			"subtype":    "success",
			"session_id": "sess-1",
			"result":     "ok",
		})

	case "mcp-init":
		// Emits an init system message reporting MCP server statuses the
		// way the real CLI does, so a test can assert failed servers are
		// surfaced.
		stdin.Scan() // consume the prompt line
		writeLine(map[string]any{
			"type":       "system",
			"subtype":    "init",
			"session_id": "sess-1",
			"mcp_servers": []map[string]any{
				{"name": "pw", "status": "connected"},
				{"name": "broken", "status": "failed"},
			},
		})
		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"role":    "assistant",
				"content": []map[string]any{{"type": "text", "text": "ok"}},
			},
		})
		writeLine(map[string]any{
			"type":       "result",
			"subtype":    "success",
			"session_id": "sess-1",
			"result":     "ok",
		})

	case "init-with-tools", "init-clean", "init-builtin-plugins", "init-user-plugin":
		// An init system message as the real CLI emits it: the isolated
		// run must abort when it lists tools, and proceed when it lists
		// only empty collections and the CLI's built-in plugins.
		stdin.Scan() // consume the prompt line
		initMsg := map[string]any{
			"type":           "system",
			"subtype":        "init",
			"session_id":     "sess-1",
			"tools":          []any{},
			"mcp_servers":    []any{},
			"slash_commands": []any{},
			"skills":         []any{},
			"plugins":        []any{},
			"agents":         []any{"general-purpose"},
		}
		builtin := func(name string) map[string]any {
			return map[string]any{"name": name, "path": "builtin", "source": name + "@builtin"}
		}
		switch scenario {
		case "init-with-tools":
			initMsg["tools"] = []any{"Bash"}
		case "init-builtin-plugins":
			// The real CLI lists its compiled-in plugins even with
			// --setting-sources= (live-confirmed, claude 2.1.293).
			initMsg["plugins"] = []any{builtin("cc-plugin-sec-default"), builtin("cc-plugin-agents-md")}
		case "init-user-plugin":
			initMsg["plugins"] = []any{builtin("cc-plugin-sec-default"), map[string]any{"name": "mine", "path": "/home/u/.claude/plugins/mine", "source": "mine@local"}}
		}
		writeLine(initMsg)
		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"model":   "claude-fake",
				"content": []any{map[string]any{"type": "text", "text": "after init"}},
			},
		})
		if scenario == "init-with-tools" || scenario == "init-user-plugin" {
			for stdin.Scan() {
			}
			return
		}
		writeLine(map[string]any{
			"type":        "result",
			"is_error":    false,
			"session_id":  "sess-1",
			"stop_reason": "end_turn",
			"result":      "done",
		})

	case "result-error":
		stdin.Scan() // consume the prompt line
		writeLine(map[string]any{
			"type":        "result",
			"subtype":     "error_max_turns",
			"is_error":    true,
			"session_id":  "sess-1",
			"stop_reason": "tool_use",
			"result":      "max turns reached",
		})

	case "result-error-usage", "result-denials-usage":
		// The failing results above, carrying the usage the real CLI
		// reports (a billed call): the isolated run must meter it.
		stdin.Scan() // consume the prompt line
		res := map[string]any{
			"type":           "result",
			"session_id":     "sess-1",
			"total_cost_usd": 0.0339,
			"modelUsage": map[string]any{"claude-sonnet-5-5": map[string]any{
				"inputTokens": 2, "outputTokens": 494, "cacheReadInputTokens": 100, "cacheCreationInputTokens": 7099, "costUSD": 0.0339,
			}},
		}
		if scenario == "result-error-usage" {
			res["subtype"], res["is_error"], res["stop_reason"], res["result"] = "error_max_turns", true, "tool_use", "max turns reached"
		} else {
			res["is_error"], res["stop_reason"], res["result"] = false, "end_turn", "done"
			res["permission_denials"] = []any{map[string]any{"tool_name": "Bash", "tool_use_id": "t1", "tool_input": map[string]any{}}}
		}
		writeLine(res)

	case "result-permission-denials":
		stdin.Scan() // consume the prompt line
		writeLine(map[string]any{
			"type":               "result",
			"is_error":           false,
			"session_id":         "sess-1",
			"stop_reason":        "end_turn",
			"result":             "done",
			"permission_denials": []any{map[string]any{"tool_name": "Bash", "tool_use_id": "t1", "tool_input": map[string]any{}}},
		})

	case "tool-attempt":
		// An assistant turn that calls a tool (which an isolated run, with
		// --tools "", must never do), followed by more text that must never
		// reach the caller, then blocks until the CLI is killed.
		stdin.Scan() // consume the prompt line
		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"model": "claude-fake",
				"content": []any{
					map[string]any{"type": "tool_use", "id": "tool-1", "name": "Bash", "input": map[string]any{"command": "echo hi"}},
				},
			},
		})
		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"model":   "claude-fake",
				"content": []any{map[string]any{"type": "text", "text": "after the tool"}},
			},
		})
		for stdin.Scan() {
		}

	case "permission":
		// Mirrors claude-agent-sdk-go's own fakecli_test.go
		// "streaming_and_permission" scenario: issues a real can_use_tool
		// control request and streams back which behavior the client
		// decided on, proving Runner's PermissionDecider wiring drives a
		// real control-request round trip end to end.
		stdin.Scan() // consume the prompt line

		writeLine(map[string]any{
			"type":       "control_request",
			"request_id": "req-1",
			"request": map[string]any{
				"subtype":     "can_use_tool",
				"tool_name":   "Bash",
				"input":       map[string]any{"command": "echo hi"},
				"tool_use_id": "tool-1",
			},
		})

		stdin.Scan()
		var resp struct {
			Response struct {
				Response struct {
					Behavior string `json:"behavior"`
				} `json:"response"`
			} `json:"response"`
		}
		_ = json.Unmarshal(stdin.Bytes(), &resp)
		behavior := resp.Response.Response.Behavior
		if behavior == "" {
			behavior = "unknown"
		}

		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"model": "claude-fake",
				"content": []any{
					map[string]any{"type": "text", "text": "chose:" + behavior},
				},
			},
		})

		writeLine(map[string]any{
			"type":        "result",
			"is_error":    false,
			"num_turns":   1,
			"session_id":  "sess-1",
			"stop_reason": "end_turn",
			"result":      "done",
		})

	case "tool_call":
		// Exercises every new claudecode.Message kind
		// docs/decisions/0008-structured-run-events.md added support for:
		// a ThinkingBlock, two ToolUseBlocks (one that later succeeds, one
		// that later fails), and their matching ToolResultBlocks arriving
		// on a subsequent "user" message -- proving Runner's claudeEvents
		// translates each into its own taskrunner.EventType.
		stdin.Scan() // consume the prompt line

		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"model": "claude-fake",
				"content": []any{
					map[string]any{"type": "thinking", "thinking": "let me check"},
					map[string]any{"type": "tool_use", "id": "tool-1", "name": "Bash", "input": map[string]any{"command": "echo hi"}},
					map[string]any{"type": "tool_use", "id": "tool-2", "name": "Bash", "input": map[string]any{"command": "false"}},
				},
			},
		})

		writeLine(map[string]any{
			"type":       "user",
			"session_id": "sess-1",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "tool-1", "content": "hi", "is_error": false},
					map[string]any{"type": "tool_result", "tool_use_id": "tool-2", "content": "boom", "is_error": true},
				},
			},
		})

		writeLine(map[string]any{
			"type":       "assistant",
			"session_id": "sess-1",
			"message": map[string]any{
				"model": "claude-fake",
				"content": []any{
					map[string]any{"type": "text", "text": "done"},
				},
			},
		})

		writeLine(map[string]any{
			"type":        "result",
			"is_error":    false,
			"num_turns":   1,
			"session_id":  "sess-1",
			"stop_reason": "end_turn",
			"result":      "done",
		})

	case "probe-ok", "probe-unreported", "probe-bad-tools", "probe-max-turns", "probe-max-budget",
		"probe-flood", "probe-hang", "probe-child", "probe-env", "probe-tools", "probe-orphan-result":
		// Probe arm (ADR-0037) stand-ins. Every one reports the arm's exact
		// tool allowlist at init except probe-bad-tools; the others differ in
		// how the run ends. They run with cwd = the arm dir.
		if scenario == "probe-env" {
			// Dump argv and the environment NAMES (never values) for the test.
			_ = os.WriteFile(filepath.Join(wd, "args"), []byte(strings.Join(os.Args[1:], "\n")), 0o644)
			var names []string
			for _, kv := range os.Environ() {
				k, v, _ := strings.Cut(kv, "=")
				if k == "ENABLE_CLAUDEAI_MCP_SERVERS" {
					k += "=" + v
				}
				names = append(names, k)
			}
			_ = os.WriteFile(filepath.Join(wd, "env-names"), []byte(strings.Join(names, "\n")), 0o644)
		}
		stdin.Scan() // consume the prompt line
		if pf := os.Getenv("FAKE_CLAUDE_CLOBBER_PIDFILE"); pf != "" {
			// Corrupt the launcher's pidfile, so the arm cleanup cannot trust it.
			_ = os.WriteFile(pf, []byte("garbage"), 0o600)
		}
		if scenario == "probe-child" {
			// A background process in the CLI's process group, like a command
			// the agent detached with &.
			cmd := exec.Command("sleep", "300")
			if err := cmd.Start(); err == nil {
				_ = os.WriteFile(filepath.Join(wd, "child.pid"), []byte(fmt.Sprint(cmd.Process.Pid)), 0o644)
			}
		}
		tools := []any{"Bash", "Read", "Edit", "Write", "Glob", "Grep", "NotebookEdit", "Skill"}
		if scenario == "probe-bad-tools" {
			tools = append(tools, "WebFetch")
		}
		writeLine(map[string]any{
			"type": "system", "subtype": "init", "session_id": "sess-1",
			"tools": tools, "mcp_servers": []any{}, "plugins": []any{},
			"slash_commands": []any{}, "skills": []any{},
		})
		text := func(s string) {
			writeLine(map[string]any{
				"type": "assistant", "session_id": "sess-1",
				"message": map[string]any{"model": "claude-fake", "content": []any{map[string]any{"type": "text", "text": s}}},
			})
		}
		text("after init")
		switch scenario {
		case "probe-tools", "probe-orphan-result":
			// A Bash call and its result: the result event carries the call id
			// but not the tool name (the real CLI does the same). The orphan
			// variant sends a result for an id that never had a tool_use.
			id := "tool-1"
			if scenario == "probe-tools" {
				writeLine(map[string]any{
					"type": "assistant", "session_id": "sess-1",
					"message": map[string]any{"model": "claude-fake", "content": []any{
						map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]any{"command": "echo hi"}}}},
				})
			} else {
				id = "tool-never-started"
			}
			writeLine(map[string]any{
				"type": "user", "session_id": "sess-1",
				"message": map[string]any{"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": id, "content": "hi", "is_error": false}}},
			})
		case "probe-flood":
			text(strings.Repeat("x", 200_000))
			for stdin.Scan() {
			}
			return
		case "probe-hang":
			for stdin.Scan() {
			}
			return
		case "probe-bad-tools":
			for stdin.Scan() {
			}
			return
		}
		result := map[string]any{
			"type": "result", "subtype": "success", "is_error": false, "session_id": "sess-1",
			"stop_reason": "end_turn", "result": "done", "num_turns": 2,
			"permission_denials": []any{"denied-1", "denied-2"},
		}
		if scenario != "probe-unreported" {
			result["total_cost_usd"] = 0.001
		}
		switch scenario {
		case "probe-max-turns":
			result["subtype"], result["is_error"], result["terminal_reason"] = "error_max_turns", true, "max_turns"
		case "probe-max-budget":
			result["subtype"], result["is_error"], result["terminal_reason"] = "error_max_budget_usd", true, "max_budget"
			result["total_cost_usd"] = 0.02
		}
		writeLine(result)

	default:
		fmt.Fprintf(os.Stderr, "fake claude cli: unknown scenario %q\n", scenario)
		os.Exit(1)
	}

	// Block until the real client closes its side (transport.close's
	// stdin.Close() is always its first shutdown step, ahead of any
	// SIGTERM/SIGKILL escalation) instead of exiting right after the last
	// write: claude-agent-sdk-go reaps this process (cmd.Wait, which closes
	// the stdout pipe once the process has exited) and drains its stdout
	// scanner in two independent, concurrently-started goroutines, so a CLI
	// that exits immediately races them -- Wait can win and have the pipe
	// closed before the scanner reads the already-written "result" line,
	// discarding it even though this process exited cleanly (exit 0).
	// Waiting for stdin EOF here guarantees the client already read every
	// line (Client.Close, which triggers the stdin close, only runs once
	// RunPrompt's Prompt call has returned) before this process -- and
	// therefore Wait -- can complete.
	for stdin.Scan() {
	}
	os.Exit(0)
}
