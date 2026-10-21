//go:build windows

package arm

import (
	"context"
	"errors"
	"time"
)

// ErrNotArmProcess mirrors the Unix declaration.
var ErrNotArmProcess = errors.New("arm: pid is not this arm's CLI")

// KillTree is not supported: probes do not run on Windows (ADR-0037).
func KillTree(context.Context, string, string, time.Duration) error {
	return errors.New("arm: process-tree kill is not supported on windows")
}
