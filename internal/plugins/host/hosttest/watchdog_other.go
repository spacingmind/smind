//go:build !unix

package hosttest

import "os"

func startWatchdog() {}

func childEnv() []string { return os.Environ() }
