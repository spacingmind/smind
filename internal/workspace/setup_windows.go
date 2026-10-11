//go:build windows

package workspace

import (
	"context"
	"time"
)

func runScript(_ context.Context, _, _ string, _ []string, _ time.Duration, _ int) (string, error) {
	return "", ErrSetupUnsupported
}
