//go:build windows

package runsock

import (
	"errors"
	"testing"
)

// The browser is not offered on Windows until the run's job membership can be
// checked: the system hub refuses to exist. (Compile-checked from other
// platforms; it has not been run on Windows.)
func TestWindowsHubIsSwitchedOff(t *testing.T) {
	if windowsHubEnabled {
		t.Fatal("the Windows hub must stay off until a Job Object membership check exists")
	}
	if _, err := NewSystemHub(""); !errors.Is(err, ErrWindowsUnsupported) {
		t.Fatalf("NewSystemHub on Windows: %v", err)
	}
}
