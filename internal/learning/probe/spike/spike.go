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
	"syscall"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// ErrBadPath is returned by SettingsJSON for a path that cannot be put into
// the sandbox settings safely: relative, unresolvable, containing "..", a
// glob metacharacter, or a control character.
var ErrBadPath = errors.New("spike: unusable sandbox path")

// ReadMode selects how the sandbox restricts reads.
type ReadMode int

const (
	// ReadDenyList denies reads under the real $HOME, the arm's parent and
	// every extraDenyRead root; everything else stays readable (measured).
	ReadDenyList ReadMode = iota
	// ReadAllowList denies "/" and re-allows the arm, the projection and the
	// system paths in SystemReadPaths (experiment E1).
	ReadAllowList
)

// DefaultReadMode is the mode SettingsJSON uses: the allow-list, because
// experiment E1 (call #24) showed it works on macOS (sh, git with a scrubbed
// config, cat, ls, curl all run; the unlisted sibling, the real checkout and
// every decoy are unreadable). The deny-list stays available as a fallback
// (ReadDenyList): it only denies what it names (measured negative control).
const DefaultReadMode = ReadAllowList

// SystemReadPaths are re-allowed under ReadAllowList: what sh, git, cat, ls,
// curl and the CLI's per-session tmp need on macOS. Paths that do not exist
// are kept as given (the CLI ignores them).
func SystemReadPaths() []string {
	return []string{"/usr", "/bin", "/sbin", "/System", "/Library", "/private/etc", "/dev",
		"/opt/homebrew", "/private/var/db", "/private/var/select", "/private/tmp/claude-" + fmt.Sprint(os.Getuid())}
}

// SystemSymlinkNodes are the top-level symlinks (/var -> private/var, ...) whose
// link nodes must themselves be readable under the allow-list, otherwise every
// path that goes through them fails (xcode-select reads /var/select/developer_dir).
// They are added as given, NOT canonicalised.
var SystemSymlinkNodes = []string{"/var", "/tmp", "/etc"}

// SettingsOptions are the knobs of SettingsJSONWith. The zero value is the
// deny-list production settings; the Omit* fields exist only for ablations.
type SettingsOptions struct {
	ReadMode      ReadMode
	ExtraDenyRead []string // deny-list mode: the real repo root, worktree roots, ...

	OmitNetworkKeys     bool // ablation: drop strictAllowlist and allowLocalBinding
	OmitDenyWrite       bool // ablation: drop filesystem.denyWrite
	OmitPermissionsDeny bool // ablation: drop permissions.deny
}

// SettingsJSON returns the `--settings` value for the probe arm in
// DefaultReadMode, failing closed (an error, never a weaker config) on any
// path problem; extraDenyRead are additional roots to deny in deny-list mode.
func SettingsJSON(armDir, projectionDir string, extraDenyRead []string) (string, error) {
	return SettingsJSONWith(armDir, projectionDir, SettingsOptions{ReadMode: DefaultReadMode, ExtraDenyRead: extraDenyRead})
}

// SettingsJSONWith is SettingsJSON with explicit options. The command sandbox
// is on and mandatory (failIfUnavailable, no unsandboxed escape hatch), the
// network is closed (strictAllowlist, empty allowlist, no local binding),
// writes are limited to armDir, and the projection is read-only: it gets
// permissions.deny on the edit tools (--add-dir makes it a working directory,
// writable by the built-in tools under acceptEdits, measured) and
// filesystem.denyWrite. Measured: with BOTH removed a sandboxed Bash write into
// the projection succeeds (call #23); with only denyWrite removed it was
// refused (call #18, earlier JSON); denyWrite alone (without permissions.deny)
// was not measured. Keep both. Reads follow ReadMode. The launcher keeps the user's
// real HOME so the CLI authenticates, so in deny-list mode the real $HOME is
// denied explicitly: an unresolvable or relative $HOME is an error, not a
// silently dropped rule.
//
// Every path must be absolute, free of ".." and of glob metacharacters, and is
// canonicalised with filepath.EvalSymlinks (Clean only for the not-yet-existing
// tail). Whether the CLI itself canonicalises was not measured, hence the
// builder does.
//
// Plain git inside the arm fails with the real $HOME denied (it reads
// ~/.gitconfig): run it with GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1.
func SettingsJSONWith(armDir, projectionDir string, opt SettingsOptions) (string, error) {
	arm, err := canonPath(armDir)
	if err != nil {
		return "", fmt.Errorf("arm dir: %w", err)
	}
	proj, err := canonPath(projectionDir)
	if err != nil {
		return "", fmt.Errorf("projection dir: %w", err)
	}
	// Extra roots are validated in every mode (bad input fails closed); only the
	// deny-list mode needs them, the allow-list already denies everything.
	var extra []string
	for _, p := range opt.ExtraDenyRead {
		c, err := canonPath(p)
		if err != nil {
			return "", fmt.Errorf("extra deny-read path: %w", err)
		}
		extra = append(extra, c)
	}
	var denyRead, allowRead []string
	switch opt.ReadMode {
	case ReadAllowList:
		denyRead = []string{"/"}
		allowRead = []string{arm, proj}
		for _, p := range SystemReadPaths() {
			c, err := canonPath(p)
			if err != nil {
				return "", fmt.Errorf("system read path %q: %w", p, err)
			}
			allowRead = append(allowRead, c)
		}
		allowRead = append(allowRead, SystemSymlinkNodes...)
	case ReadDenyList:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%w: $HOME is not resolvable: %v", ErrBadPath, err)
		}
		if !filepath.IsAbs(home) {
			return "", fmt.Errorf("%w: $HOME %q is not absolute", ErrBadPath, home)
		}
		homeC, err := canonPath(home)
		if err != nil {
			return "", fmt.Errorf("$HOME: %w", err)
		}
		denyRead = append([]string{homeC, filepath.Dir(arm)}, extra...)
		allowRead = []string{arm, proj}
	default:
		return "", fmt.Errorf("spike: unknown read mode %d", opt.ReadMode)
	}
	denyRead, allowRead = dedupe(denyRead), dedupe(allowRead)

	fs := map[string]any{"allowWrite": []string{arm}, "denyRead": denyRead, "allowRead": allowRead}
	if !opt.OmitDenyWrite {
		fs["denyWrite"] = []string{proj}
	}
	network := map[string]any{"allowedDomains": []string{}}
	if !opt.OmitNetworkKeys {
		network["strictAllowlist"] = true
		network["allowLocalBinding"] = false
	}
	settings := map[string]any{
		"sandbox": map[string]any{
			"enabled":                  true,
			"failIfUnavailable":        true,
			"allowUnsandboxedCommands": false,
			"autoAllowBashIfSandboxed": true,
			"network":                  network,
			"filesystem":               fs,
		},
	}
	if !opt.OmitPermissionsDeny {
		// "//" prefix = absolute filesystem path in permission rules.
		var deny []string
		for _, tool := range []string{"Edit", "Write", "NotebookEdit"} {
			deny = append(deny, tool+"(//"+strings.TrimPrefix(proj, "/")+"/**)")
		}
		settings["permissions"] = map[string]any{"deny": deny}
	}
	b, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("spike: marshal settings: %w", err) // static shape; cannot fail
	}
	return string(b), nil
}

// canonPath validates p and returns its canonical absolute form: symlinks
// followed through the longest existing prefix, the rest Cleaned.
func canonPath(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: %q is not absolute", ErrBadPath, p)
	}
	if strings.ContainsAny(p, "*?[](){}!\n\r\x00\\") {
		return "", fmt.Errorf("%w: %q contains a glob or control character", ErrBadPath, p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: %q contains ..", ErrBadPath, p)
		}
	}
	clean := filepath.Clean(p)
	rest := ""
	for cur := clean; ; cur = filepath.Dir(cur) {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest), nil
		}
		if cur == filepath.Dir(cur) {
			return clean, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// DefaultEnvAllowlist is the set of variable NAMES the launcher passes to the
// real CLI. Measured (claude 2.1.303, macOS): with HOME and CLAUDE_CONFIG_DIR
// (the user's normal login) plus USER the CLI authenticates; without USER it
// answers "Not logged in". SHELL, TERM, LOGNAME were not needed. Only names
// the parent process has are passed. Values are never logged.
var DefaultEnvAllowlist = []string{"PATH", "LANG", "TMPDIR", "HOME", "CLAUDE_CONFIG_DIR", "USER"}

// connectorOptOut keeps claude.ai account connectors out of an isolated run.
const connectorOptOut = "ENABLE_CLAUDEAI_MCP_SERVERS=false"

// AllowlistEnv returns the KEY=VALUE entries for WriteLauncher: every name in
// DefaultEnvAllowlist that lookup reports as set, plus the connector opt-out.
func AllowlistEnv(lookup func(string) (string, bool)) []string {
	var env []string
	for _, n := range DefaultEnvAllowlist {
		if v, ok := lookup(n); ok {
			env = append(env, n+"="+v)
		}
	}
	return append(env, connectorOptOut)
}

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// WriteLauncher writes an executable `#!/bin/sh` wrapper into dir that runs
// realCLI under `/usr/bin/env -i` with exactly the KEY=VALUE entries in env:
// the SDK's transport inherits the daemon's whole environment and can only
// add to it, so an allowlist needs a wrapper (WithCLIPath). It returns the
// script path.
//
// The env VALUES are written to disk in the script (and are briefly visible
// in env's argv): only ever pass the non-secret allowlist (PATH, LANG, TMPDIR,
// HOME, CLAUDE_CONFIG_DIR, USER); never add a token or key. dir must be a
// real directory (not a symlink) owned by the caller and not group/world
// writable; the file is created with O_EXCL|O_NOFOLLOW and mode 0700, so a
// pre-planted file or symlink is an error, never followed. dir must never be
// inside the arm. An entry that is not KEY=VALUE with a valid name, or a
// relative realCLI or dir, is an error; values are single-quoted, so they are
// never interpreted by the shell.
func WriteLauncher(dir, realCLI string, env []string) (string, error) {
	if !filepath.IsAbs(dir) || !filepath.IsAbs(realCLI) {
		return "", errors.New("spike: launcher dir and real CLI path must be absolute")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("spike: launcher dir: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("spike: launcher dir is not a real directory")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("spike: launcher dir %s is group/world writable (%v)", dir, info.Mode().Perm())
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return "", errors.New("spike: launcher dir is not owned by the current user")
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
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o700)
	if err != nil {
		return "", fmt.Errorf("spike: create launcher: %w", err)
	}
	if _, err := f.WriteString(b.String()); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("spike: write launcher: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("spike: write launcher: %w", err)
	}
	return path, nil
}

// shQuote single-quotes s for /bin/sh.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Budget limits of the spike: 12 calls / USD 0.60 originally, +6 / +0.10 for
// the R3 follow-up, +6 / +0.10 for the hardening round (each extension was
// authorised by the Lead for that round only).
const (
	MaxCalls = 24
	MaxUSD   = 0.80
	// perCallReserveUSD is held back when deciding whether another call may
	// start: a call's cost is only known afterwards, so the harness refuses a
	// call unless a typical (generous, haiku) call still fits under MaxUSD.
	perCallReserveUSD = 0.05
)

// ErrBudget is returned by Ledger.Begin when a call would breach the budget.
// Begin returns the call's sequence number (its 1-based position in the ledger).
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
// exceed MaxCalls, or when spent + the per-call reserve would pass MaxUSD.
func (l *Ledger) Begin(purpose string) (seq int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	calls, usd, err := l.Totals()
	if err != nil {
		return 0, err
	}
	if calls+1 > MaxCalls || usd+perCallReserveUSD > MaxUSD {
		return 0, fmt.Errorf("%w: refusing %q: %d/%d calls, USD %.4f/%.2f spent", ErrBudget, purpose, calls, MaxCalls, usd, MaxUSD)
	}
	fmt.Printf("spike ledger: starting call %d/%d (%q), USD %.4f/%.2f spent\n", calls+1, MaxCalls, purpose, usd, MaxUSD)
	return calls + 1, nil
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
