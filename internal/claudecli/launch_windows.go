//go:build windows

package claudecli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"

	"golang.org/x/sys/windows"

	"github.com/spacingmind/smind/internal/winjob"
)

// LaunchSpec returns what the daemon needs to start the Claude CLI through
// the launcher: the path to hand claudecode.WithCLIPath (this smind.exe) and
// the env entries to merge into the CLI's environment (the resolved real
// claude.exe, see LaunchEnv). An error means no native claude.exe could be
// found; it names what was tried.
func LaunchSpec() (cliPath string, env []string, err error) {
	target, err := Resolve(exec.LookPath, os.ReadFile, fileExists, homeDir())
	if err != nil {
		return "", nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("claudecli: locating smind.exe: %w", err)
	}
	return self, []string{LaunchEnv + "=" + target}, nil
}

// MaybeRunLauncher turns this process into the CLI launcher when LaunchEnv is
// set: call it first thing in main. It reports handled=false (and does
// nothing) for a normal smind invocation.
func MaybeRunLauncher() (exitCode int, handled bool) {
	target := os.Getenv(LaunchEnv)
	if target == "" {
		return 0, false
	}
	code, err := RunLauncher(target, os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "smind (claude launcher): %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code, true
}

// RunLauncher runs target (a claude.exe, validated) with args inside a
// kill-on-close Job Object and returns its exit code. Stdin, stdout and
// stderr are the launcher's own handles passed straight through (the SDK
// talks JSON over them, so nothing may buffer or copy), and LaunchEnv is
// removed from the child's environment. The Job's only handle lives in this
// process: when the SDK kills the launcher -- its only way to stop the CLI --
// the OS closes the handle and terminates the whole tree, including the
// CLI's Bash tool commands and MCP servers, which Process.Kill alone would
// orphan.
func RunLauncher(target string, args []string) (int, error) {
	if err := ValidateTarget(target); err != nil {
		return 2, err
	}
	cmd := exec.Command(target, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = WithoutEnv(os.Environ(), LaunchEnv)
	// No new console: the launcher's std handles are explicit, and a fresh
	// console window per agent turn would flash on a desktop.
	job, err := winjob.Start(cmd, windows.CREATE_NO_WINDOW)
	if err != nil {
		return 1, fmt.Errorf("starting %s: %w", target, err)
	}
	defer job.Close()

	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 1, fmt.Errorf("waiting for %s: %w", target, err)
	}
	return 0, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}
