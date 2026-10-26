package main

import "testing"

// setHome points os.UserHomeDir at dir on every OS: it reads $HOME on Unix and
// %USERPROFILE% on Windows, so a test that only sets HOME writes into (and reads
// from) the real home directory of the Windows runner.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
