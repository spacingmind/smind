package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/version"
)

// TestRunVersionOutput checks both spellings -- the flag and the
// subcommand -- print the stamped version and exit 0 without any daemon
// running (AC3).
func TestRunVersionOutput(t *testing.T) {
	prevV, prevC := version.Version, version.Commit
	version.Version = "0.7.0-dev+abc1234"
	version.Commit = "abc1234"
	t.Cleanup(func() {
		version.Version, version.Commit = prevV, prevC
	})

	for _, args := range [][]string{{"--version"}, {"version"}} {
		var stdout bytes.Buffer
		if code := cmdVersion(&stdout); code != 0 {
			t.Errorf("cmdVersion(%v) = %d, want 0", args, code)
		}
		out := stdout.String()
		if !strings.Contains(out, "smind 0.7.0-dev+abc1234") {
			t.Errorf("cmdVersion(%v) output = %q, want it to contain the version", args, out)
		}
		if !strings.Contains(out, version.Commit) {
			t.Errorf("cmdVersion(%v) output = %q, want it to contain the commit", args, out)
		}
	}

	// run() must dispatch both spellings to cmdVersion rather than the
	// unknown-command path.
	for _, arg := range []string{"--version", "version"} {
		var stdout bytes.Buffer
		previous := osStdout
		osStdout = &stdout
		code := run([]string{arg})
		osStdout = previous
		if code != 0 {
			t.Errorf("run(%q) = %d, want 0", arg, code)
		}
		if !bytes.Contains(stdout.Bytes(), []byte(version.Version)) {
			t.Errorf("run(%q) stdout = %q, want it to contain %q", arg, stdout.String(), version.Version)
		}
	}
}
