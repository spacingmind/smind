package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/logging"
	"github.com/spacingmind/smind/internal/service"
)

// Restart policy of `smind service run`: the same as the scheduled task's
// RestartOnFailure (3 restarts, 1 minute apart). Variables so tests are fast.
var (
	superviseRestarts = 3
	superviseDelay    = time.Minute
	// A daemon that ran this long before it crashed was healthy: its crash starts
	// the restart budget over instead of using up the next one.
	superviseHealthyAfter = 10 * time.Minute
)

// cmdServiceRun is what the Windows scheduled task executes (`smind.exe service
// run [--home <dir>]`): it marks the daemon as managed by the task, applies
// --home, and runs `smind serve` as a child, restarting it after a crash.
//
// It exists because a Scheduled Task action cannot carry environment variables
// (SMIND_SUPERVISOR, SMIND_HOME) and because the task launches the daemon
// through `conhost.exe --headless` (no console window), which exits 0 whatever
// the daemon's exit code: measured on windows-latest, a killed daemon was never
// restarted by the task's own RestartOnFailure. So the crash restart happens
// here, with the policy the task would have applied. A graceful exit (code 0,
// e.g. `smind stop`) ends it: nothing is restarted after a clean stop.
func cmdServiceRun(args []string) int {
	fs := flag.NewFlagSet("service run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var home string
	fs.StringVar(&home, "home", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(osStderr, "usage: smind service run [--home <dir>]   (run by the scheduled task; use `smind serve` by hand)")
		return 2
	}
	if err := applyServiceRunEnv(home); err != nil {
		fmt.Fprintf(osStderr, "smind service run: %v\n", err)
		return 2
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(osStderr, "smind service run: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logPath := filepath.Join(config.Dir(), logging.FileName)
	return supervise(ctx, func(ctx context.Context) (int, error) { return runDaemonChild(ctx, exe) },
		superviseRestarts, superviseDelay, superviseHealthyAfter, func(format string, a ...any) { appendDaemonLog(logPath, format, a...) })
}

// applyServiceRunEnv sets what the task's action cannot: SMIND_HOME from --home
// and the supervisor marker the daemon publishes as /healthz managedBy.
func applyServiceRunEnv(home string) error {
	if home != "" {
		if !filepath.IsAbs(home) {
			return fmt.Errorf("--home %q must be an absolute path", home)
		}
		_ = os.Setenv("SMIND_HOME", home)
	}
	_ = os.Setenv(service.SupervisorEnv, service.SupervisorTask)
	return nil
}

// supervise runs start until it exits 0 (or ctx ends), restarting up to
// maxRestarts times, delay apart, after a non-zero exit. It returns the last
// exit code.
//
// The budget is per incident: a daemon that ran at least healthyAfter before
// crashing gets all maxRestarts again, so three isolated crashes in a year do
// not leave it down for good.
func supervise(ctx context.Context, start func(context.Context) (int, error), maxRestarts int, delay, healthyAfter time.Duration, logf func(string, ...any)) int {
	for attempt := 0; ; attempt++ {
		began := time.Now()
		code, err := start(ctx)
		if time.Since(began) >= healthyAfter {
			attempt = 0
		}
		if err != nil {
			logf("service run: could not start the daemon: %v", err)
			code = 1
		}
		if code == 0 || ctx.Err() != nil {
			return code
		}
		if attempt >= maxRestarts {
			logf("service run: the daemon exited with code %d; giving up after %d restarts (it starts again at the next logon, or with `smind service start`)", code, maxRestarts)
			return code
		}
		logf("service run: the daemon exited with code %d; restarting in %s (restart %d of %d)", code, delay, attempt+1, maxRestarts)
		select {
		case <-ctx.Done():
			return code
		case <-time.After(delay):
		}
	}
}

// appendDaemonLog adds one timestamped line to daemon.log, opening and closing
// the file each time: the daemon child owns the long-lived handle (and
// rotates it), so this must not hold it open.
func appendDaemonLog(path, format string, a ...any) {
	line := time.Now().Format("2006/01/02 15:04:05") + " " + fmt.Sprintf(format, a...)
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	if f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600); err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
	fmt.Fprint(osStderr, line)
}

// daemonCommand is the child `smind serve`, inheriting the environment (and the
// console) of the supervisor.
func daemonCommand(ctx context.Context, exe string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, exe, "serve")
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, nil
	// A console event (logoff, shutdown, Ctrl+C) reaches the daemon too, which shuts
	// down gracefully; cancelling ctx must not race it with TerminateProcess. Only
	// if it has not exited after WaitDelay is it killed.
	cmd.Cancel = func() error { return nil }
	cmd.WaitDelay = 30 * time.Second
	return cmd
}
