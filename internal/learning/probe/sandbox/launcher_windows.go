//go:build windows

package sandbox

import "os"

// There is no measured sandbox on Windows; the launcher is never written.
func createExclusive(string) (*os.File, error) { return nil, ErrUnsupported }

func ownedByCurrentUser(os.FileInfo) bool { return false }
