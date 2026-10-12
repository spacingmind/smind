package wsclient

import "runtime"

// exeName adds the platform's executable suffix to a test-built binary's
// name: Windows' CreateProcess won't start an extensionless file, which made
// every test spawning a "go build -o"-built fake agent fail there.
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
