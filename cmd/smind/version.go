package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spacingmind/smind/internal/version"
)

// cmdVersion prints the daemon's build-time version (AC3). It runs
// entirely from the stamped build info -- no daemon, no config -- so it
// works everywhere, including where nothing is running yet.
var osStdout io.Writer = os.Stdout

func cmdVersion(w io.Writer) int {
	if version.Commit != "" {
		fmt.Fprintf(w, "smind %s (%s)\n", version.Version, version.Commit)
		return 0
	}
	fmt.Fprintf(w, "smind %s\n", version.Version)
	return 0
}
