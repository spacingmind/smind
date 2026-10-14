package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/selfupdate"
	"github.com/spacingmind/smind/internal/version"
)

const cmdUpdateUsage = "usage: smind update [--check] [--version <v>] [--force] [--dry-run] [--json]"

// updaterHook lets tests adjust the Updater cmdUpdate builds (running binary,
// version, version probe); nil in production.
var updaterHook func(*selfupdate.Updater)

// cmdUpdate implements `smind update` (ADR-0031 D1). Refusals (a guard said
// no, nothing changed) and failures both exit 1 with one line on stderr;
// usage errors exit 2.
func cmdUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		check, force, dryRun, asJSON bool
		target                       string
	)
	fs.BoolVar(&check, "check", false, "")
	fs.BoolVar(&force, "force", false, "")
	fs.BoolVar(&dryRun, "dry-run", false, "")
	fs.BoolVar(&asJSON, "json", false, "")
	fs.StringVar(&target, "version", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		if err != nil {
			fmt.Fprintf(os.Stderr, "smind update: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "smind update: unexpected argument %q\n", fs.Arg(0))
		}
		fmt.Fprintln(os.Stderr, cmdUpdateUsage)
		return 2
	}
	if check && (target != "" || force || dryRun) {
		fmt.Fprintf(os.Stderr, "smind update: --check only reports; it takes no --version, --force or --dry-run\n%s\n", cmdUpdateUsage)
		return 2
	}

	exe, err := runningExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "smind update: locate the running binary: %v\n", err)
		return 1
	}
	logw := io.Writer(os.Stdout)
	if asJSON {
		logw = os.Stderr // stdout carries only the JSON document
	}
	base := daemonBaseURL()
	u := &selfupdate.Updater{
		Source:     selfupdate.NewSource(),
		Current:    version.Version,
		Exe:        exe,
		Daemon:     &selfupdate.HealthzDaemon{BaseURL: base},
		ActiveWork: daemonActivity,
		Log:        logw,
	}
	if updaterHook != nil {
		updaterHook(u)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if check {
		res, err := u.Check(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "smind update: %v\n", err)
			return 1
		}
		if asJSON {
			return printJSON(res)
		}
		fmt.Printf("current: %s\nlatest:  %s\n", res.Current, res.Latest)
		if res.UpdateAvailable {
			fmt.Println("update available: run `smind update`")
		} else {
			fmt.Println("up to date")
		}
		return 0
	}

	res, err := u.Update(ctx, selfupdate.Options{Version: target, Force: force, DryRun: dryRun})
	if err != nil {
		var refusal *selfupdate.Refusal
		if errors.As(err, &refusal) {
			fmt.Fprintf(os.Stderr, "smind update: refused: %s\n", refusal.Reason)
		} else {
			fmt.Fprintf(os.Stderr, "smind update: %v\n", err)
		}
		return 1
	}
	if asJSON {
		return printJSON(res)
	}
	return 0
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "smind update: %v\n", err)
		return 1
	}
	return 0
}

// runningExecutable is the real file `smind update` replaces: the running
// binary with symlinks resolved, so updating through a symlink (e.g.
// ~/.local/bin/smind -> /opt/smind/smind) replaces the target, not the link.
func runningExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// daemonPort is the configured daemon port, defaulting like dialDaemon.
func daemonPort() (int, error) {
	cfg, err := config.Load()
	if err != nil {
		return 0, fmt.Errorf("config: %w", err)
	}
	if cfg.Server.Port == 0 {
		return 4648, nil
	}
	return cfg.Server.Port, nil
}

// daemonBaseURL is the daemon's loopback HTTP base. A config that cannot be
// read falls back to the default port: the worst case is probing the wrong
// port and reporting "no daemon", and ActiveWork surfaces the config error.
func daemonBaseURL() string {
	port, err := daemonPort()
	if err != nil {
		port = 4648
	}
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// daemonActivity asks the local daemon whether any run is running via
// run.list (a run blocked on a permission prompt is still a running run). No
// answer on /healthz means no daemon: nothing to protect. A daemon that
// answers /healthz but cannot be asked (token mismatch, a different program on
// the port) is an error, so the caller refuses rather than guesses.
func daemonActivity(ctx context.Context) (selfupdate.Activity, error) {
	port, err := daemonPort()
	if err != nil {
		return selfupdate.Activity{}, err
	}
	probe := &selfupdate.HealthzDaemon{BaseURL: "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), Client: http.DefaultClient}
	st, err := probe.Status(ctx)
	if err != nil {
		return selfupdate.Activity{}, err
	}
	if !st.Running {
		return selfupdate.Activity{}, nil
	}
	act := selfupdate.Activity{DaemonRunning: true}
	client, err := dialDaemon(ctx)
	if err != nil {
		return act, err
	}
	defer func() { _ = client.Close() }()
	var runs []struct {
		Status string
	}
	if err := client.Call(ctx, "run.list", map[string]any{"status": "running"}, &runs); err != nil {
		return act, fmt.Errorf("run.list: %w", err)
	}
	for _, r := range runs {
		if r.Status == "running" {
			act.RunningRuns++
		}
	}
	return act, nil
}
