// Package spike is the P0 sandbox spike for the learning probe arm
// (ADR-0037 §3): it holds the candidate `--settings` sandbox JSON, the env
// allowlist launcher and the call/cost ledger used to measure, against the
// real claude CLI, whether the CLI's command sandbox confines an agent that
// runs a not-yet-approved skill. It is research code, not production: nothing
// outside this package imports it.
package spike

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// SettingsJSON returns the candidate `--settings` value for the probe arm:
// the command sandbox on and mandatory (failIfUnavailable, no unsandboxed
// escape hatch), no network at all (strictAllowlist with an empty allowlist,
// no local binding), writes limited to armDir, and reads denied under the
// arm's parent directory and the real $HOME (the launcher keeps the user's
// HOME so the CLI still authenticates, so the sandbox, not a private HOME,
// is what protects it) except armDir and projectionDir.
//
// Measured live on macOS (claude 2.1.303, see the live tests): denyRead of
// the real $HOME and of the arm's parent, with allowRead for the arm and the
// projection, blocks decoy reads and writes outside the arm while the arm and
// the projection stay readable. Two additions come from measurement:
// --add-dir directories are working directories (acceptEdits auto-approves
// edits there, and the CLI lists them in the sandbox write allowlist), so the
// read-only projection gets permissions.deny on the edit tools and a
// filesystem.denyWrite. Plain git inside the arm fails with the real $HOME
// denied (it reads ~/.gitconfig): run it with GIT_CONFIG_GLOBAL=/dev/null.
// armDir and projectionDir must be absolute; a relative path panics, because
// a relative sandbox path would silently mean something else to the CLI.
func SettingsJSON(armDir, projectionDir string) string {
	for _, p := range []string{armDir, projectionDir} {
		if !filepath.IsAbs(p) {
			panic("spike: sandbox path is not absolute: " + p)
		}
	}
	armDir, projectionDir = filepath.Clean(armDir), filepath.Clean(projectionDir)
	denyRead := []string{filepath.Dir(armDir)}
	if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
		denyRead = append([]string{filepath.Clean(home)}, denyRead...)
	}
	// --add-dir directories are working directories: acceptEdits auto-approves
	// edits there (measured, run 1), so the read-only projection needs an
	// explicit deny on the edit tools ("//" = absolute filesystem path).
	denyEdit := make([]string, 0, 3)
	for _, tool := range []string{"Edit", "Write", "NotebookEdit"} {
		denyEdit = append(denyEdit, tool+"(/"+projectionDir+"/**)")
	}
	settings := map[string]any{
		"permissions": map[string]any{"deny": denyEdit},
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
				"denyWrite":  []string{projectionDir},
				"denyRead":   denyRead,
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

// PolicyRecord is one can_use_tool request the CLI routed to ArmPolicy and
// what was decided. Input values are not stored, only the resolved path.
type PolicyRecord struct {
	Tool    string
	Path    string
	Allowed bool
	Reason  string
}

// ArmPolicy is the permission decider for the probe arm (a
// claudecode.PermissionPolicy). It allows the built-in file tools only on
// paths that resolve (symlinks followed) inside ArmDir (read and write) or
// ReadOnlyDirs (Read/Glob/Grep only), denies every other tool, and records
// every request so a test can tell whether a call reached the decider at all.
// AllowBash, when set, lets a Bash request through if it returns true for
// the command; the default denies Bash (autoAllowBashIfSandboxed already runs
// sandboxed commands without asking, so a Bash request means the CLI wants to
// leave the sandbox).
type ArmPolicy struct {
	ArmDir       string
	ReadOnlyDirs []string
	AllowBash    func(command string) bool

	mu  sync.Mutex
	log []PolicyRecord
}

var (
	policyReadTools  = map[string]bool{"Read": true, "Glob": true, "Grep": true}
	policyWriteTools = map[string]bool{"Edit": true, "Write": true, "NotebookEdit": true, "MultiEdit": true}
)

// Records returns a copy of every request seen so far.
func (p *ArmPolicy) Records() []PolicyRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]PolicyRecord(nil), p.log...)
}

// Decide implements claudecode.PermissionPolicy.
func (p *ArmPolicy) Decide(_ context.Context, req claudecode.CanUseToolRequest) (bool, map[string]any, string, []claudecode.PermissionUpdate, bool, error) {
	allow, path, reason := p.decide(req)
	p.mu.Lock()
	p.log = append(p.log, PolicyRecord{Tool: req.ToolName, Path: path, Allowed: allow, Reason: reason})
	p.mu.Unlock()
	if !allow {
		return false, nil, "denied by the probe arm policy: " + reason, nil, false, nil
	}
	return true, req.Input, "", nil, false, nil
}

func (p *ArmPolicy) decide(req claudecode.CanUseToolRequest) (allow bool, path, reason string) {
	if req.ToolName == "Bash" {
		cmd, _ := req.Input["command"].(string)
		if p.AllowBash != nil && p.AllowBash(cmd) {
			return true, "", "bash command allowed by test"
		}
		return false, "", "bash outside the sandbox is never allowed"
	}
	read, write := policyReadTools[req.ToolName], policyWriteTools[req.ToolName]
	if !read && !write {
		return false, "", "tool " + req.ToolName + " is not allowed"
	}
	var paths []string
	for _, k := range []string{"file_path", "path", "notebook_path"} {
		if v, ok := req.Input[k].(string); ok && v != "" {
			paths = append(paths, v)
		}
	}
	if pat, ok := req.Input["pattern"].(string); ok && req.ToolName == "Glob" {
		paths = append(paths, globPrefix(pat))
	}
	if len(paths) == 0 {
		paths = []string{p.ArmDir} // Glob/Grep without a path search the cwd
	}
	for _, raw := range paths {
		resolved := p.resolve(raw)
		path = resolved
		roots := []string{p.ArmDir}
		if read {
			roots = append(roots, p.ReadOnlyDirs...)
		}
		ok := false
		for _, root := range roots {
			if within(resolved, p.resolve(root)) {
				ok = true
				break
			}
		}
		if !ok {
			return false, resolved, "path is outside the arm"
		}
	}
	return true, path, "inside the arm"
}

// globPrefix is the literal directory part of a glob pattern, so an
// absolute or "../" pattern is judged by where it starts.
func globPrefix(pattern string) string {
	if i := strings.IndexAny(pattern, "*?[{"); i >= 0 {
		pattern = pattern[:i]
	}
	if pattern == "" {
		return "."
	}
	return pattern
}

// resolve makes raw absolute (relative to ArmDir), cleans it and follows
// symlinks through the longest existing prefix.
func (p *ArmPolicy) resolve(raw string) string {
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(p.ArmDir, raw)
	}
	raw = filepath.Clean(raw)
	rest := ""
	for cur := raw; ; cur = filepath.Dir(cur) {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		if cur == filepath.Dir(cur) {
			return raw
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
