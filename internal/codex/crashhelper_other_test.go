//go:build !windows

package codex

// crashHelper is the Windows-only re-exec'd daemon-subprocess body (see
// procgroup_windows_test.go); nothing to do elsewhere.
func crashHelper(string) {}
