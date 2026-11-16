package accounts

import (
	"os"
	"testing"
)

// TestMain pins SMIND_HOME to a temporary directory: the listener records this
// package's listeners write (for the agent browser's deny list) must never land
// in a developer's real ~/.smind.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "smind-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("SMIND_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
