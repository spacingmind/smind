//go:build windows

package claudecli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// stageFakeClaude copies the test binary to <tmp>\claude.exe: the launcher
// only runs a file with that name.
func stageFakeClaude(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst := filepath.Join(t.TempDir(), "claude.exe")
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, src); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return dst
}

// launcherCmd builds the launcher invocation: this test binary with
// LaunchEnv pointing at target, as the daemon starts smind.exe.
func launcherCmd(t *testing.T, target string, extraEnv []string, args ...string) *exec.Cmd {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, args...)
	cmd.Env = append(os.Environ(), LaunchEnv+"="+target)
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd
}

func processAlive(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, 0)
	return err == nil && ev == uint32(windows.WAIT_TIMEOUT)
}

func killPid(pid uint32) {
	if h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid); err == nil {
		_ = windows.TerminateProcess(h, 1)
		_ = windows.CloseHandle(h)
	}
}

func TestLauncher_PassesArgsStdinAndExitCodeAndStripsTheEnvVar(t *testing.T) {
	target := stageFakeClaude(t)
	args := []string{
		"--output-format", "stream-json",
		"--mcp-config", `{"mcpServers":{"a b":{"command":"npx","args":["-y","x&y"]}}}`,
		"plain with spaces",
		"a&b|c<d>e^f",
		"%PATH%",
		`quote"inside`,
	}
	cmd := launcherCmd(t, target, []string{fakeClaudeEnv + "=1", fakeExitEnv + "=7", fakeBootNoteEnv + "=kept"}, args...)
	cmd.Stdin = strings.NewReader("hello from the SDK")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("launcher exit = %v (stderr %q), want exit code 7", err, stderr.String())
	}

	var got struct {
		Args             []string `json:"args"`
		LaunchEnvPresent bool     `json:"launchEnvPresent"`
		BootMarker       string   `json:"bootMarker"`
		Stdin            string   `json:"stdin"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout %q is not the fake claude's report: %v", stdout.String(), err)
	}
	if strings.Join(got.Args, "\x00") != strings.Join(args, "\x00") {
		t.Errorf("args arrived as\n  %q\nwant\n  %q", got.Args, args)
	}
	if got.LaunchEnvPresent {
		t.Errorf("%s leaked into the CLI's environment", LaunchEnv)
	}
	if got.BootMarker != "kept" {
		t.Errorf("other env vars must pass through, marker = %q", got.BootMarker)
	}
	if got.Stdin != "hello from the SDK" {
		t.Errorf("stdin = %q", got.Stdin)
	}
}

func TestLauncher_KillingTheLauncherKillsTheWholeTree(t *testing.T) {
	target := stageFakeClaude(t)
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	cmd := launcherCmd(t, target, []string{fakeClaudeEnv + "=1", fakeLingerEnv + "=1", fakeGrandchildEnv + "=" + pidFile})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	launcherPid := uint32(cmd.Process.Pid)
	var grandchild uint32
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if grandchild != 0 {
			killPid(grandchild) // never leave the sleeper behind, even on failure
		}
	})

	deadline := time.Now().Add(15 * time.Second)
	for grandchild == 0 && time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				grandchild = uint32(n)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if grandchild == 0 {
		t.Fatal("the fake claude never started its grandchild")
	}
	if !processAlive(grandchild) {
		t.Fatal("grandchild is not running before the kill")
	}

	// What the SDK does when it stops the CLI: Process.Kill on the launcher.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = cmd.Process.Wait()

	for i := 0; i < 100 && processAlive(grandchild); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(grandchild) {
		t.Fatalf("grandchild %d survived the launcher %d being killed", grandchild, launcherPid)
	}
}

func TestLauncher_RefusesATargetThatIsNotClaudeExe(t *testing.T) {
	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	for _, target := range []string{cmdExe, "claude.exe", `.\claude.exe`} {
		cmd := launcherCmd(t, target, nil, "/c", "echo SHOULD-NOT-RUN")
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
			t.Errorf("target %q: launcher exit = %v, want a non-zero exit", target, err)
		}
		if strings.Contains(out.String(), "SHOULD-NOT-RUN") {
			t.Errorf("target %q was executed", target)
		}
		if !strings.Contains(out.String(), "claude.exe") {
			t.Errorf("target %q: output %q should explain the refusal", target, out.String())
		}
	}
}

func TestLauncher_NothingSetMeansNormalSmind(t *testing.T) {
	if code, handled := MaybeRunLauncher(); handled || code != 0 {
		t.Fatalf("MaybeRunLauncher with %s unset = (%d, %v), want (0, false)", LaunchEnv, code, handled)
	}
}

func TestLaunchSpec_PointsAtThisExecutableWithTheResolvedTarget(t *testing.T) {
	// Needs a claude.exe on this machine; the CI Windows runners do not
	// have one, and Resolve's own tests cover every other path.
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("no claude on PATH")
	}
	cli, env, err := LaunchSpec()
	if err != nil {
		t.Skipf("claude on PATH but no native exe behind it: %v", err)
	}
	self, _ := os.Executable()
	if !strings.EqualFold(cli, self) {
		t.Errorf("cliPath = %q, want this executable %q", cli, self)
	}
	if len(env) != 1 || !strings.HasPrefix(env[0], LaunchEnv+"=") {
		t.Fatalf("env = %v", env)
	}
	if err := ValidateTarget(strings.TrimPrefix(env[0], LaunchEnv+"=")); err != nil {
		t.Errorf("the launch target fails its own validation: %v", err)
	}
}
