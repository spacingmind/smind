// Package spike is the P0 sandbox spike for the learning probe arm
// (ADR-0037 §3): it holds the candidate `--settings` sandbox JSON, the env
// allowlist launcher and the call/cost ledger used to measure, against the
// real claude CLI, whether the CLI's command sandbox confines an agent that
// runs a not-yet-approved skill. It is research code, not production: nothing
// outside this package imports it.
package spike

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SettingsJSON returns the candidate `--settings` value for the probe arm:
// the command sandbox on and mandatory (failIfUnavailable, no unsandboxed
// escape hatch), no network at all (strictAllowlist with an empty allowlist,
// no local binding), writes limited to armDir, and reads denied everywhere
// under the arm's parent and $HOME except armDir and projectionDir.
//
// UNPROVEN until the live tests run: the denyRead/allowRead shape is the
// documented mechanism (CHANGELOG "allowRead ... re-allow read access within
// denyRead regions") but its macOS behaviour is what the spike measures.
// armDir and projectionDir must be absolute; a relative path panics, because
// a relative sandbox path would silently mean something else to the CLI.
func SettingsJSON(armDir, projectionDir string) string {
	for _, p := range []string{armDir, projectionDir} {
		if !filepath.IsAbs(p) {
			panic("spike: sandbox path is not absolute: " + p)
		}
	}
	armDir, projectionDir = filepath.Clean(armDir), filepath.Clean(projectionDir)
	settings := map[string]any{
		"sandbox": map[string]any{
			"enabled":                  true,
			"failIfUnavailable":        true,
			"allowUnsandboxedCommands": false,
			"autoAllowBashIfSandboxed": true,
			"network": map[string]any{
				"strictAllowlist":   true,
				"allowedDomains":    []string{},
				"allowLocalBinding": false,
			},
			"filesystem": map[string]any{
				"allowWrite": []string{armDir},
				"denyRead":   []string{"~", filepath.Dir(armDir)},
				"allowRead":  []string{armDir, projectionDir},
			},
		},
	}
	b, err := json.Marshal(settings)
	if err != nil {
		panic("spike: marshal settings: " + err.Error()) // static shape; cannot fail
	}
	return string(b)
}

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// WriteLauncher writes an executable `#!/bin/sh` wrapper into dir that runs
// realCLI under `/usr/bin/env -i` with exactly the KEY=VALUE entries in env:
// the SDK's transport inherits the daemon's whole environment and can only
// add to it, so an allowlist needs a wrapper (WithCLIPath). It returns the
// script path. An entry that is not KEY=VALUE with a valid name, a relative
// realCLI or a relative dir is an error; values are single-quoted, so they
// are never interpreted by the shell.
func WriteLauncher(dir, realCLI string, env []string) (string, error) {
	if !filepath.IsAbs(dir) || !filepath.IsAbs(realCLI) {
		return "", errors.New("spike: launcher dir and real CLI path must be absolute")
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\nexec /usr/bin/env -i")
	for _, kv := range env {
		name, val, ok := strings.Cut(kv, "=")
		if !ok || !envNameRE.MatchString(name) {
			return "", fmt.Errorf("spike: launcher env entry %q is not NAME=VALUE", name)
		}
		b.WriteString(" " + name + "=" + shQuote(val))
	}
	b.WriteString(" " + shQuote(realCLI) + " \"$@\"\n")
	path := filepath.Join(dir, "claude-launcher.sh")
	if err := os.WriteFile(path, []byte(b.String()), 0o700); err != nil {
		return "", fmt.Errorf("spike: write launcher: %w", err)
	}
	return path, nil
}

// shQuote single-quotes s for /bin/sh.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Budget limits of the spike (brief: at most 12 model-backed calls, USD 0.60).
const (
	MaxCalls = 12
	MaxUSD   = 0.60
	// perCallReserveUSD is held back when deciding whether another call may
	// start: a call's cost is only known afterwards, so the harness refuses a
	// call unless a typical (generous, haiku) call still fits under MaxUSD.
	perCallReserveUSD = 0.05
)

// ErrBudget is returned by Ledger.Begin when a call would breach the budget.
var ErrBudget = errors.New("spike: live-call budget exhausted")

// Entry is one ledger line. It carries no secret: the purpose is a label and
// the rest is what the CLI's result message reported.
type Entry struct {
	Purpose  string    `json:"purpose"`
	CostUSD  float64   `json:"cost_usd"`
	Turns    int       `json:"turns"`
	Duration float64   `json:"duration_s"`
	Outcome  string    `json:"outcome"`
	At       time.Time `json:"at"`
}

// Ledger is the append-only JSONL record of model-backed invocations and the
// gate that refuses a call that would exceed MaxCalls or MaxUSD. Its totals
// are re-read from the file on every Begin, so separate test processes share
// one budget.
type Ledger struct {
	Path string
	mu   sync.Mutex
}

// Totals reads the ledger and returns the call count and cumulative cost.
func (l *Ledger) Totals() (calls int, usd float64, err error) {
	data, err := os.ReadFile(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return 0, 0, fmt.Errorf("spike: corrupt ledger line: %w", err)
		}
		calls++
		usd += e.CostUSD
	}
	return calls, usd, nil
}

// Begin checks the budget before a model-backed call starts. It returns an
// ErrBudget-wrapping error that names the running totals when the call would
// be the 13th, or when spent + the per-call reserve would pass MaxUSD.
func (l *Ledger) Begin(purpose string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	calls, usd, err := l.Totals()
	if err != nil {
		return err
	}
	if calls+1 > MaxCalls || usd+perCallReserveUSD > MaxUSD {
		return fmt.Errorf("%w: refusing %q: %d/%d calls, USD %.4f/%.2f spent", ErrBudget, purpose, calls, MaxCalls, usd, MaxUSD)
	}
	fmt.Printf("spike ledger: starting call %d/%d (%q), USD %.4f/%.2f spent\n", calls+1, MaxCalls, purpose, usd, MaxUSD)
	return nil
}

// Record appends one entry. Call it for every invocation, including failed
// ones: a call that ran is billed whatever its outcome.
func (l *Ledger) Record(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}
