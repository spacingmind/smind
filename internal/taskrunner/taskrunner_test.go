package taskrunner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// fakeACPAgentPath is the compiled internal/taskrunner/fakeagent binary,
// built once for every test that exercises the GLM/Kimi path against a real
// ACP subprocess -- mirroring internal/acp's own TestMain pattern.
var fakeACPAgentPath string

// fakeCodexAgentPath is the compiled internal/codex/fakeagent binary, built
// once for every test that exercises the Codex-native path against a real
// app-server subprocess.
var fakeCodexAgentPath string

// TestMain doubles as the entrypoint for the fake Claude Code CLI: when
// TASKRUNNER_FAKE_CLAUDE_CLI=1 is set (only ever true in a subprocess this
// package's own tests spawn via claudecode.WithCLIPath(os.Executable())),
// it runs the fake CLI instead of the test suite. This is the same
// re-exec-self helper-process pattern claude-agent-sdk-go's own tests use,
// adapted because its scenario-selecting extraEnv option is unexported and
// only reachable from within that package itself; the
// worktree-relative "scenario" file convention from fakeagent above serves
// the same purpose here without needing it.
func TestMain(m *testing.M) {
	if os.Getenv("TASKRUNNER_FAKE_CLAUDE_CLI") == "1" {
		runFakeClaudeCLI()
		return
	}

	dir, err := os.MkdirTemp("", "smind-taskrunner-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	fakeACPAgentPath = filepath.Join(dir, "fakeagent")
	build := exec.Command("go", "build", "-o", fakeACPAgentPath, "./fakeagent")
	build.Dir, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	if out, err := build.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build fakeagent: %v: %s", err, out))
	}

	fakeCodexAgentPath = filepath.Join(dir, "codex-fakeagent")
	buildCodex := exec.Command("go", "build", "-o", fakeCodexAgentPath, "../codex/fakeagent")
	buildCodex.Dir, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	if out, err := buildCodex.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build codex fakeagent: %v: %s", err, out))
	}

	smindHome, err := os.MkdirTemp("", "smind-taskrunner-home-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(smindHome)
	os.Setenv("SMIND_HOME", smindHome)

	// Set once, for the lifetime of this (already-past-the-check) test
	// process, so any subprocess it spawns via os.Executable() inherits it
	// and runs as the fake CLI instead of re-running the test suite.
	os.Setenv("TASKRUNNER_FAKE_CLAUDE_CLI", "1")

	os.Exit(m.Run())
}

// runFakeClaudeCLI is a minimal NDJSON scripted stand-in for the `claude`
// CLI's headless stream-json protocol. Like fakeagent, its scenario comes
// from a "scenario" file in its working directory (claudecode.Client sets
// the subprocess's cmd.Dir to the task's worktree).
func runFakeClaudeCLI() {
	wd, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	scenario := "reply"
	if data, err := os.ReadFile(filepath.Join(wd, "scenario")); err == nil {
		scenario = strings.TrimSpace(string(data))
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

	case "hang":
		stdin.Scan() // consume the prompt line, then never respond or exit
		time.Sleep(time.Hour)

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

	default:
		fmt.Fprintf(os.Stderr, "fake claude cli: unknown scenario %q\n", scenario)
		os.Exit(1)
	}

	os.Exit(0)
}

func newTestWorkspaceManager(t *testing.T) *workspace.Manager {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})
	return workspace.New(s)
}

// newTestRepo creates a real git repository in a temp dir with one commit,
// so worktree creation (via workspace.Manager.CreateTask) has a commit to
// branch from.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGitT(t, dir, "init")
	runGitT(t, dir, "config", "user.email", "test@example.com")
	runGitT(t, dir, "config", "user.name", "Test")

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitT(t, dir, "add", "README.md")
	runGitT(t, dir, "commit", "-m", "initial commit")

	return dir
}

func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// newTestTask creates a workspace and a task with a real git worktree, and
// (if scenario is non-empty) writes it as the "scenario" file inside the
// worktree for the fake agent/CLI to read.
func newTestTask(t *testing.T, scenario string) (*workspace.Manager, store.Task) {
	t.Helper()

	wm := newTestWorkspaceManager(t)
	repo := newTestRepo(t)

	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := wm.CreateTask(ws.ID, nil, "Task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if scenario != "" {
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(scenario), 0o644); err != nil {
			t.Fatalf("write scenario file: %v", err)
		}
	}

	return wm, task
}
