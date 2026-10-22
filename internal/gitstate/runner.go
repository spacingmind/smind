package gitstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Deadlines (ADR-0039 §4 hard rule 6).
const (
	LocalTimeout   = 10 * time.Second
	NetworkTimeout = 20 * time.Second
	waitDelay      = 500 * time.Millisecond
)

// Runner runs git in dir with argv only and returns stdout. On a non-zero
// exit it still returns the stdout and a *RunError.
type Runner func(ctx context.Context, dir string, args ...string) (string, error)

// RunError is a failed git invocation. ExitCode is -1 when git did not run to
// completion (killed, not found).
type RunError struct {
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *RunError) Error() string {
	return fmt.Sprintf("git %v: %v: %s", e.Args, e.Err, e.Stderr)
}

func (e *RunError) Unwrap() error { return e.Err }

func exitCode(err error) int {
	var re *RunError
	if errors.As(err, &re) {
		return re.ExitCode
	}
	return -1
}

// DefaultRunner is the hardened runner: core.fsmonitor forced off (an
// agent-editable repo config git would execute), no credential prompts, no
// optional locks, a C locale, a deadline (LocalTimeout unless ctx already has
// one) that kills git, and the process waited out at most waitDelay after.
func DefaultRunner(ctx context.Context, dir string, args ...string) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, LocalTimeout)
		defer cancel()
	}
	full := append([]string{"-c", "core.fsmonitor=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	cmd.WaitDelay = waitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		code := -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return stdout.String(), &RunError{Args: args, ExitCode: code, Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}
