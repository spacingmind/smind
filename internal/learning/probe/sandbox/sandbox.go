// Package sandbox is the production launch shape of the learning probe arm
// (ADR-0037 §3): the CLI's command-sandbox settings, the env-scrubbing
// launcher, the tool policy, the self-check canary that mints an attestation
// and the tripwire over the human's real files. Every fact in it was measured
// by the P0 spike (internal/learning/probe/spike); it fails closed: an error,
// never a weaker configuration.
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

// AttestationTTL is how long a passing canary keeps the probe enabled.
const AttestationTTL = 10 * time.Minute
