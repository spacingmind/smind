//go:build !linux && !darwin && !windows

package runsock

import (
	"errors"
	"testing"
)

func TestSystemProcTreeUnsupported(t *testing.T) {
	if _, err := SystemProcTree().Info(1); !errors.Is(err, ErrTreeUnsupported) {
		t.Errorf("Info(1) error = %v, want ErrTreeUnsupported", err)
	}
}
