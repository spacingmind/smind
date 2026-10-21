package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrBadPath is returned for a path that cannot be put into the sandbox
// settings safely: relative, unresolvable, containing "..", a glob
// metacharacter, or a control character.
var ErrBadPath = errors.New("sandbox: unusable sandbox path")

// SystemReadPaths are re-allowed under the read allow-list: what sh, git, cat,
// ls, curl and the CLI's per-session tmp need on macOS. Paths that do not
// exist are kept as given (the CLI ignores them).
func SystemReadPaths() []string {
	return []string{"/usr", "/bin", "/sbin", "/System", "/Library", "/private/etc", "/dev",
		"/opt/homebrew", "/private/var/db", "/private/var/select", "/private/tmp/claude-" + fmt.Sprint(os.Getuid())}
}

// SystemSymlinkNodes are the top-level symlinks (/var -> private/var, ...) whose
// link nodes must themselves be readable under the allow-list, otherwise every
// path that goes through them fails (xcode-select reads /var/select/developer_dir).
// They are added as given, NOT canonicalised.
var SystemSymlinkNodes = []string{"/var", "/tmp", "/etc"}

// Settings returns the `--settings` value for the probe arm, failing closed
// (an error, never a weaker config) on any path problem. The command sandbox
// is on and mandatory (failIfUnavailable, no unsandboxed escape hatch, no
// excludedCommands), the network is closed (strictAllowlist, empty allowlist,
// no local binding), writes are limited to armDir, and reads are an
// allow-list: "/" is denied and the arm, the projection and SystemReadPaths
// are re-allowed (measured: the unlisted sibling, the real checkout and the
// real $HOME are then unreadable while sh, git, cat, ls all run).
//
// projectionDir may be "": no projection rules are emitted. When set it is
// read-only: it gets permissions.deny on the edit tools (--add-dir makes it a
// working directory, writable by the built-in tools under acceptEdits,
// measured) and filesystem.denyWrite. Measured: with BOTH removed a sandboxed
// Bash write into the projection succeeds; keep both.
//
// Every path must be absolute, free of ".." and of glob metacharacters, and is
// canonicalised with filepath.EvalSymlinks (Clean only for the not-yet-existing
// tail). Whether the CLI itself canonicalises was not measured, hence the
// builder does.
//
// Plain git inside the arm fails with the real $HOME denied (it reads
// ~/.gitconfig): the launcher runs it with GIT_CONFIG_GLOBAL=/dev/null
// GIT_CONFIG_NOSYSTEM=1.
func Settings(armDir, projectionDir string) (string, error) {
	arm, err := canonPath(armDir)
	if err != nil {
		return "", fmt.Errorf("arm dir: %w", err)
	}
	var proj string
	if projectionDir != "" {
		if proj, err = canonPath(projectionDir); err != nil {
			return "", fmt.Errorf("projection dir: %w", err)
		}
	}
	allowRead := []string{arm}
	if proj != "" {
		allowRead = append(allowRead, proj)
	}
	for _, p := range SystemReadPaths() {
		c, err := canonPath(p)
		if err != nil {
			return "", fmt.Errorf("system read path %q: %w", p, err)
		}
		allowRead = append(allowRead, c)
	}
	allowRead = append(allowRead, SystemSymlinkNodes...)

	fs := map[string]any{"allowWrite": []string{arm}, "denyRead": []string{"/"}, "allowRead": dedupe(allowRead)}
	settings := map[string]any{
		"sandbox": map[string]any{
			"enabled":                  true,
			"failIfUnavailable":        true,
			"allowUnsandboxedCommands": false,
			"autoAllowBashIfSandboxed": true,
			"network":                  map[string]any{"allowedDomains": []string{}, "strictAllowlist": true, "allowLocalBinding": false},
			"filesystem":               fs,
		},
	}
	if proj != "" {
		fs["denyWrite"] = []string{proj}
		// "//" prefix = absolute filesystem path in permission rules.
		var deny []string
		for _, tool := range []string{"Edit", "Write", "NotebookEdit"} {
			deny = append(deny, tool+"(//"+strings.TrimPrefix(proj, "/")+"/**)")
		}
		settings["permissions"] = map[string]any{"deny": deny}
	}
	b, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("sandbox: marshal settings: %w", err) // static shape; cannot fail
	}
	return string(b), nil
}

// canonPath validates p and returns its canonical absolute form: symlinks
// followed through the longest existing prefix, the rest Cleaned.
func canonPath(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: %q is not absolute", ErrBadPath, p)
	}
	if strings.ContainsAny(p, "*?[](){}!\n\r\x00\\") || strings.ContainsFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
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
