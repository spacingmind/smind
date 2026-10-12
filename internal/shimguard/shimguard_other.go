//go:build !windows

package shimguard

// validate is a no-op off Windows: batch shims are a cmd.exe phenomenon.
func validate(string, []string) error { return nil }
