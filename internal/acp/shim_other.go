//go:build !windows

package acp

// validateShimCommand is a no-op off Windows: batch shims are a cmd.exe
// phenomenon.
func validateShimCommand([]string) error { return nil }
