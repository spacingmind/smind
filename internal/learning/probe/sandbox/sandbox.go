// Package sandbox is the production launch shape of the learning probe arm
// (ADR-0037 §3): the CLI's command-sandbox settings, the env-scrubbing
// launcher, the tool policy, the self-check canary that mints an attestation
// and the tripwire over the human's real files. Every fact in it was measured
// by the P0 spike (internal/learning/probe/spike); it fails closed: an error,
// never a weaker configuration.
//
// Residual risks accepted for now (found by the ADR-0037 security review):
//
//   - The tripwire compares the real checkout before and after the arm, so it
//     reports a false positive when the human edits that checkout (or their
//     skill dirs) while a probe runs. A probe that trips this way is marked
//     unsafe and discarded, never trusted.
//   - The CLI's per-session tmp directory (/private/tmp/claude-<uid>) is
//     readable inside the sandbox and shared with the user's other CLI
//     sessions; the arm can read what those sessions left there.
//   - There is no per-usage-event cost cap: the CLI reports usage only in its
//     final result, so --max-budget-usd (enforced by the CLI) is the only
//     in-flight bound and the arm's Meter charges the cap when usage is
//     missing.
//   - Whether a skill body's dynamic-context syntax (shell injection in a
//     skill file) is executed inside the sandbox, rather than being inert
//     text, has not been verified against the real CLI.
package sandbox

import (
	"errors"
	"runtime"
	"time"
)

// ErrUnsupported is returned off macOS: only macOS has a measured sandbox.
var ErrUnsupported = errors.New("sandbox: the probe sandbox is only measured on macOS")

// Supported reports whether this OS has a measured probe sandbox.
func Supported() bool { return runtime.GOOS == "darwin" }

// AttestationTTL is how long a passing canary keeps the probe enabled: a
// probe is up to three arms of 15 minutes each plus the canary, and every arm
// start re-checks it.
const AttestationTTL = 60 * time.Minute
