// Package secretdeny builds the Claude CLI permission rules that keep an
// agent's built-in tools and the obvious shell readers away from smind's own
// secrets under one SMIND_HOME. It is a cheap provider-side defence, not a
// boundary: an agent running as the same OS user can still read these files
// with any program the rules do not name (public-docs/orchestration.md,
// "What an agent can read").
package secretdeny

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spacingmind/smind/internal/auth"
)

// readers are the shell commands denied against a secret path. Arbitrary
// programs (python, node, a script) are out of scope.
var readers = []string{"cat", "less", "more", "head", "tail", "grep", "sed", "awk", "cp", "base64", "xxd", "strings"}

// Paths returns the secret files and directory globs under home, absolute.
// Directory entries end in "/**". The worktrees under home/worktrees are not
// listed: agents work there.
func Paths(home string) (files, dirs []string) {
	files = []string{
		auth.TokenPath(home),
		filepath.Join(home, "config.yaml"),
		filepath.Join(home, "relay-keypair.json"), // the relay E2EE key pair (internal/relay/e2ee)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		files = append(files, filepath.Join(home, "smind.db"+suffix))
	}
	// ADR-0042: each plugin's persistent data dir (settings and secrets live
	// in smind.db, covered above).
	dirs = []string{filepath.Join(home, "plugins", "*", "data")}
	return files, dirs
}

// Rules returns the `permissions.deny` entries for home: Read, Edit and Write
// of every secret (the "//" prefix marks an absolute path in the CLI's rule
// grammar), and Bash rules for the common readers of the exact files plus
// `sqlite3 <db>`. A home reached through a symlink (macOS /tmp) also gets its
// resolved spelling.
//
// On Windows the CLI normalises a path to POSIX form before matching
// (C:\Users\a becomes /c/Users/a), so every path in a rule is written that
// way; a native "\h\smind\token" would never match and the deny would be dead.
func Rules(home string) []string {
	homes := []string{filepath.Clean(home)}
	if real, err := filepath.EvalSymlinks(home); err == nil && real != homes[0] {
		homes = append(homes, real)
	}
	var out []string
	for _, h := range homes {
		files, dirs := Paths(h)
		for _, f := range files {
			for _, tool := range []string{"Read", "Edit", "Write"} {
				out = append(out, tool+"(/"+cliPath(f)+")")
			}
		}
		for _, d := range dirs {
			for _, tool := range []string{"Read", "Edit", "Write"} {
				out = append(out, tool+"(/"+cliPath(d)+"/**)")
			}
		}
		if strings.ContainsAny(h, "*?[]()") {
			continue // a glob in the path would turn the Bash patterns into a different rule
		}
		for _, f := range files {
			for _, cmd := range readers {
				out = append(out, "Bash("+cmd+" "+cliPath(f)+"*)")
			}
		}
		db := filepath.Join(h, "smind.db")
		out = append(out, "Bash(sqlite3 "+cliPath(db)+"*)")
	}
	return out
}

// cliPath spells an absolute path the way the CLI's rule matcher sees it:
// forward slashes, and a Windows drive letter as a leading lower-case
// directory ("C:\\Users" -> "/c/Users"). A POSIX path is returned unchanged.
func cliPath(p string) string {
	p = filepath.ToSlash(p)
	if len(p) >= 2 && p[1] == ':' && (p[0]|0x20) >= 'a' && (p[0]|0x20) <= 'z' {
		return "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

// envNamePattern is the POSIX environment variable name grammar; anything
// else (a value with a space, an empty string) cannot be a name a shell
// would expand, so it gets no per-name rules.
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// EnvRules returns the `permissions.deny` entries that keep the obvious
// shell and file readers away from a provider instance's secret environment
// variables: the environment dumpers, the /proc/.../environ readers, the
// per-name `echo` spellings and `printenv NAME`, and the providers dir under
// home where instance secrets are written. Like Rules this is a best-effort
// defence, not a boundary: it cannot cover every way a program can reach the
// environment (`python -c 'import os'` and friends are out of scope), only
// the commands an agent reaches for by habit.
func EnvRules(home string, secretEnvNames []string) []string {
	if len(secretEnvNames) == 0 {
		return nil
	}
	out := []string{
		"Bash(env)",
		"Bash(printenv*)",
		"Bash(set)",
		"Bash(export -p*)",
		"Bash(declare -x*)",
		"Bash(declare -p*)",
		"Bash(cat /proc/*/environ*)",
		"Bash(cat /proc/self/environ*)",
		"Bash(strings /proc/*/environ*)",
		"Bash(tr * /proc/*/environ*)",
		"Read(//proc/*/environ)",
		"Read(//proc/self/environ)",
	}
	homes := []string{filepath.Clean(home)}
	if real, err := filepath.EvalSymlinks(home); err == nil && real != homes[0] {
		homes = append(homes, real)
	}
	for _, h := range homes {
		out = append(out, "Read(/"+cliPath(filepath.Join(h, "providers"))+"/**)")
	}
	for _, name := range secretEnvNames {
		if !envNamePattern.MatchString(name) {
			continue
		}
		out = append(out,
			"Bash(echo $"+name+"*)",
			"Bash(echo ${"+name+"}*)",
			"Bash(echo \"$"+name+"\"*)",
			"Bash(echo \"${"+name+"}\"*)",
			"Bash(printenv "+name+"*)",
		)
	}
	seen := make(map[string]bool, len(out))
	deduped := out[:0]
	for _, r := range out {
		if !seen[r] {
			seen[r] = true
			deduped = append(deduped, r)
		}
	}
	return deduped
}
