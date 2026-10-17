package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/logging"
	"github.com/spacingmind/smind/internal/service"
	"github.com/spacingmind/smind/internal/version"
)

const cmdServiceUsage = `usage: smind service <command>

  smind service install [--system] [--home <dir>] [--path <PATH>] [--no-start]
  smind service uninstall|status|start|stop|restart [--system]
  smind service logs [-f] [-n <lines>]

Runs the daemon under the OS service manager: a systemd unit on Linux (user
unit; --system for a system unit that still runs as you), a LaunchAgent on
macOS, or a per-user scheduled task on Windows (starts at logon, not at boot).
Port and other settings are in config.yaml, not flags.
status exits 0 when running, 3 when stopped, 4 when not installed.`

// serviceEnvHook lets tests replace the machine the service commands see
// (fake systemctl/launchctl, a temp HOME); nil in production.
var serviceEnvHook func(*service.Env)

// cmdService implements `smind service ...` (ADR-0031 D2).
func cmdService(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(osStderr, cmdServiceUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "-h", "--help", "help":
		fmt.Fprintln(osStdout, cmdServiceUsage)
		return 0
	case "logs":
		return cmdServiceLogs(rest)
	case "run":
		return cmdServiceRun(rest)
	case "install", "uninstall", "status", "start", "stop", "restart":
	default:
		fmt.Fprintf(osStderr, "smind service: unknown command %q\n%s\n", sub, cmdServiceUsage)
		return 2
	}

	fs := flag.NewFlagSet("service "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o service.Options
	fs.BoolVar(&o.System, "system", false, "")
	if sub == "install" {
		fs.StringVar(&o.Home, "home", "", "")
		fs.StringVar(&o.Path, "path", "", "")
		fs.BoolVar(&o.NoStart, "no-start", false, "")
	}
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		if err != nil {
			fmt.Fprintf(osStderr, "smind service %s: %v\n", sub, err)
		} else {
			fmt.Fprintf(osStderr, "smind service %s: unexpected argument %q\n", sub, fs.Arg(0))
		}
		fmt.Fprintln(osStderr, cmdServiceUsage)
		return 2
	}

	env, err := service.DefaultEnv(version.Version)
	if err != nil {
		fmt.Fprintf(osStderr, "smind service %s: %v\n", sub, err)
		return 1
	}
	env.Out, env.Err = osStdout, osStderr
	if serviceEnvHook != nil {
		serviceEnvHook(&env)
	}
	m, err := service.New(env)
	if err != nil {
		fmt.Fprintf(osStderr, "smind service %s: %v\n", sub, err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if sub == "status" {
		st, err := m.Status(ctx, o)
		if err != nil {
			fmt.Fprintf(osStderr, "smind service status: %v\n", err)
			return 1
		}
		return st.ExitCode()
	}
	switch sub {
	case "install":
		err = m.Install(ctx, o)
	case "uninstall":
		err = m.Uninstall(ctx, o)
	case "start":
		err = m.Start(ctx, o)
	case "stop":
		err = m.Stop(ctx, o)
	case "restart":
		err = m.Restart(ctx, o)
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, service.ErrNotInstalled):
		fmt.Fprintf(osStderr, "smind service %s: %v\n", sub, err)
		return service.ExitNotInstalled
	default:
		fmt.Fprintf(osStderr, "smind service %s: %v\n", sub, err)
		return 1
	}
}

// serviceLogDir is where the daemon writes daemon.log: the smind home of the
// installed service when there is one (its unit may set a SMIND_HOME this
// shell does not have), else this shell's own.
func serviceLogDir() string {
	env, err := service.DefaultEnv(version.Version)
	if err != nil {
		return config.Dir()
	}
	if serviceEnvHook != nil {
		serviceEnvHook(&env)
	}
	m, err := service.New(env)
	if err != nil {
		return config.Dir()
	}
	if h, ok := m.InstalledHome(service.Options{}); ok {
		return h
	}
	return config.Dir()
}

// cmdServiceLogs prints the tail of the daemon's own log file, daemon.log in
// the smind home (ADR-0031 decision 7), the same on every OS; -f keeps
// following it across rotations until Ctrl+C.
func cmdServiceLogs(args []string) int {
	fl := flag.NewFlagSet("service logs", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	var (
		follow bool
		lines  = 100
	)
	fl.BoolVar(&follow, "f", false, "")
	fl.BoolVar(&follow, "follow", false, "")
	fl.IntVar(&lines, "n", lines, "")
	fl.IntVar(&lines, "lines", lines, "")
	if err := fl.Parse(args); err != nil || fl.NArg() != 0 || lines < 0 {
		if err != nil {
			fmt.Fprintf(osStderr, "smind service logs: %v\n", err)
		}
		fmt.Fprintln(osStderr, "usage: smind service logs [-f] [-n <lines>]")
		return 2
	}
	path := filepath.Join(serviceLogDir(), logging.FileName)
	off, err := logging.TailFile(path, lines, osStdout)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(osStderr, "smind service logs: no daemon log yet at %s (the daemon writes it once it has run)\n", path)
		} else {
			fmt.Fprintf(osStderr, "smind service logs: %v\n", err)
		}
		return 1
	}
	if !follow {
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := logging.Follow(ctx, path, osStdout, off); err != nil {
		fmt.Fprintf(osStderr, "smind service logs: %v\n", err)
		return 1
	}
	return 0
}
