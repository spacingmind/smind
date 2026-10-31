//go:build !windows

package securefile

func runtimeIsWindows() bool { return false }

func grantEveryone(string) error { return nil }
