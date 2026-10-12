//go:build !windows

package claudecli

// LaunchSpec is a no-op off Windows: the SDK finds and stops the CLI itself
// (process-group kill), so no launcher is involved.
func LaunchSpec() (cliPath string, env []string, err error) { return "", nil, nil }

// MaybeRunLauncher never handles anything off Windows.
func MaybeRunLauncher() (exitCode int, handled bool) { return 0, false }
