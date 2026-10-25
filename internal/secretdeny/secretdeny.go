// Package secretdeny builds the Claude CLI permission rules that keep an
// agent's built-in tools and the obvious shell readers away from smind's own
// secrets under one SMIND_HOME. It is a cheap provider-side defence, not a
// boundary: an agent running as the same OS user can still read these files
// with any program the rules do not name (public-docs/orchestration.md,
// "What an agent can read").
package secretdeny

import (
	"path/filepath"
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
				out = append(out, tool+"(/"+f+")")
			}
		}
		for _, d := range dirs {
			for _, tool := range []string{"Read", "Edit", "Write"} {
				out = append(out, tool+"(/"+d+"/**)")
			}
		}
		if strings.ContainsAny(h, "*?[]()") {
			continue // a glob in the path would turn the Bash patterns into a different rule
		}
		for _, f := range files {
			for _, cmd := range readers {
				out = append(out, "Bash("+cmd+" "+f+"*)")
			}
		}
		db := filepath.Join(h, "smind.db")
		out = append(out, "Bash(sqlite3 "+db+"*)")
	}
	return out
}
