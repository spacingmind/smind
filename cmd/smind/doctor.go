package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/doctor"
)

const doctorUsage = `Usage:
  smind doctor [--json]    check provider-side session retention (read-only; never edits a CLI setting)
`

// doctorDial is a variable so tests can stub the daemon connection.
var doctorDial = func(ctx context.Context) (wsCaller, func(), error) {
	client, err := dialDaemon(ctx)
	if err != nil {
		return nil, nil, err
	}
	return client.Call, func() { _ = client.Close() }, nil
}

func cmdDoctor(args []string) int {
	return runDoctor(context.Background(), args, osStdout, osStderr)
}

type doctorSessions struct {
	Runs     int `json:"runs"`
	WithRef  int `json:"withRef"`
	Readable int `json:"readable"`
	TooLarge int `json:"tooLarge"`
	Gone     int `json:"gone"`
}

// runDoctor is cmdDoctor with its environment injected. Exit codes: 0 ok (a
// warning is still 0), 2 usage. It never writes anything.
func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	asJSON := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == "--json":
		asJSON = true
	default:
		fmt.Fprint(stderr, doctorUsage)
		return 2
	}
	days := config.DefaultRunHistoryRetentionDays
	if cfg, err := config.Load(); err == nil {
		days = cfg.Runs.HistoryRetentionDays
	}
	home, _ := os.UserHomeDir()
	findings := doctor.ProviderRetention(doctor.Env{
		SmindHome: config.Dir(), Home: home, Getenv: os.Getenv,
		ReadFile: os.ReadFile, ReadDir: os.ReadDir, RetentionDays: days,
	})
	if findings == nil {
		findings = []doctor.Finding{}
	}
	hasWarn := false
	for _, f := range findings {
		if f.Level == doctor.LevelWarn {
			hasWarn = true
		}
	}

	var sessions *doctorSessions
	if call, closeFn, err := doctorDial(ctx); err == nil {
		var s doctorSessions
		if err := call(ctx, "doctor.sessions", map[string]any{}, &s); err == nil {
			sessions = &s
		}
		closeFn()
	}

	if asJSON {
		out, _ := json.MarshalIndent(struct {
			Findings []doctor.Finding `json:"findings"`
			Sessions *doctorSessions  `json:"sessions"`
		}{findings, sessions}, "", "  ")
		fmt.Fprintln(stdout, string(out))
		return 0
	}
	if len(findings) == 0 {
		fmt.Fprintln(stdout, "no Claude Code or Codex config found on this machine")
	}
	for i, f := range findings {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "[%s] %s: %s\n", f.Level, f.Provider, f.Message)
		if f.Level == doctor.LevelWarn && f.Fix != "" {
			fmt.Fprintf(stdout, "  fix: %s\n", f.Fix)
		}
	}
	fmt.Fprintln(stdout)
	if sessions == nil {
		fmt.Fprintln(stdout, "sessions: the daemon is not running, so smind cannot count the provider sessions it can still read")
		return 0
	}
	fmt.Fprintf(stdout, "sessions: %d of %d runs with a provider session can still be rebuilt (%d already gone", sessions.Readable, sessions.WithRef, sessions.Gone)
	if sessions.TooLarge > 0 {
		fmt.Fprintf(stdout, ", %d over the 64 MiB rebuild limit", sessions.TooLarge)
	}
	fmt.Fprintln(stdout, ")")
	if sessions.Gone > 0 && hasWarn {
		fmt.Fprintln(stdout, `Run "smind doctor" again after changing the setting; sessions that are already deleted cannot be restored.`)
	}
	return 0
}
