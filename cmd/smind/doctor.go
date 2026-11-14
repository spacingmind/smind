package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/crash"
	"github.com/spacingmind/smind/internal/doctor"
)

const doctorUsage = `Usage:
  smind doctor [--json]    check provider-side session retention (read-only; never edits a CLI setting); also lists recent crash reports, read-only
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

// doctorCrashesSummary is crash.Summary as doctor reports it; crash.Summary
// is not marshalled directly so the on-disk struct stays a private contract.
type doctorCrashesSummary struct {
	Dir          string `json:"dir"`
	Count        int    `json:"count"`
	Newest       string `json:"newest"`
	NewestSite   string `json:"newestSite"`
	NewestSource string `json:"newestSource"`
	Refused      bool   `json:"refused"`
	Err          string `json:"err"`
}

type doctorCrashes struct {
	Daemon  doctorCrashesSummary  `json:"daemon"`
	Desktop *doctorCrashesSummary `json:"desktop"`
}

func crashesJSON(s crash.Summary) doctorCrashesSummary {
	newest := ""
	if !s.Newest.IsZero() {
		newest = s.Newest.UTC().Format(time.RFC3339)
	}
	return doctorCrashesSummary{
		Dir:          s.Dir,
		Count:        s.Count,
		Newest:       newest,
		NewestSite:   printable(s.NewestSite, 40),
		NewestSource: printable(s.NewestSource, 40),
		Refused:      s.Refused,
		Err:          s.Err,
	}
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

	// crash.List is read-only: listing must never create the directory.
	daemonCrashes := crash.List(filepath.Join(config.Dir(), "crashes"))
	var desktopCrashes *crash.Summary
	if dir := crash.DesktopDir(); dir != "" {
		s := crash.List(dir)
		desktopCrashes = &s
	}
	var desktopJSON *doctorCrashesSummary
	if desktopCrashes != nil {
		s := crashesJSON(*desktopCrashes)
		desktopJSON = &s
	}

	if asJSON {
		out, _ := json.MarshalIndent(struct {
			Findings []doctor.Finding `json:"findings"`
			Sessions *doctorSessions  `json:"sessions"`
			Crashes  doctorCrashes    `json:"crashes"`
		}{findings, sessions, doctorCrashes{crashesJSON(daemonCrashes), desktopJSON}}, "", "  ")
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
	printCrashes(stdout, daemonCrashes, desktopCrashes)
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

// printCrashes writes the crashes section, one line per summary. The daemon
// line is always present; a desktop directory with no reports says nothing.
func printCrashes(w io.Writer, daemon crash.Summary, desktop *crash.Summary) {
	fmt.Fprintln(w, crashLine("crashes", daemon, true))
	if desktop == nil || (desktop.Count == 0 && !desktop.Refused && desktop.Err == "") {
		return
	}
	if line := crashLine("desktop crashes", *desktop, false); line != "" {
		fmt.Fprintln(w, line)
	}
}

// crashLine formats one summary as a single line, or "" when there is
// nothing worth saying (the none case of the desktop directory).
func crashLine(prefix string, s crash.Summary, daemon bool) string {
	switch {
	case s.Refused:
		return fmt.Sprintf("%s: %s is a link, so it was not read", prefix, s.Dir)
	case s.Err != "":
		return fmt.Sprintf("%s: %s could not be read: %s", prefix, s.Dir, s.Err)
	case s.Count == 0:
		if !daemon {
			return ""
		}
		return fmt.Sprintf("crashes: none recorded (daemon: %s)", s.Dir)
	default:
		return fmt.Sprintf("%s: %d recorded in %s; newest %s (%s, %s). Reports stay on this machine and are never uploaded.",
			prefix, s.Count, s.Dir, s.Newest.UTC().Format(time.RFC3339),
			printable(s.NewestSource, 40), printable(s.NewestSite, 40))
	}
}

// printable keeps only ASCII letters, digits and ._-: and replaces every
// other rune with ?, cut at max runes. Site and source come from report
// files, so they must not smuggle control bytes into terminal output.
func printable(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= max {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-', r == ':':
			b.WriteRune(r)
		default:
			b.WriteRune('?')
		}
	}
	return b.String()
}
