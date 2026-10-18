//go:build !windows

package logging

import "os"

// openShared opens path for reading. Elsewhere than Windows an open file never
// blocks a rename or delete, so this is os.Open.
func openShared(path string) (*os.File, error) { return os.Open(path) }
