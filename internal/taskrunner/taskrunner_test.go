package taskrunner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/claudecli"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner/fakeclaude"
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
	// The Windows launcher test starts this binary as smind.exe in launcher
	// mode; that role is recognised by claudecli.LaunchEnv and must win over
	// the fake-CLI switch, which this process also inherits.
	if code, handled := claudecli.MaybeRunLauncher(); handled {
		os.Exit(code)
	}
	if os.Getenv("TASKRUNNER_FAKE_CLAUDE_CLI") == "1" {
		fakeclaude.Run()
		return
	}

	dir, err := os.MkdirTemp("", "smind-taskrunner-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	fakeACPAgentPath = filepath.Join(dir, exeName("fakeagent"))
	build := exec.Command("go", "build", "-o", fakeACPAgentPath, "./fakeagent")
	build.Dir, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	if out, err := build.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build fakeagent: %v: %s", err, out))
	}

	fakeCodexAgentPath = filepath.Join(dir, exeName("codex-fakeagent"))
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
	runGitT(t, dir, "config", "core.autocrlf", "false")
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
