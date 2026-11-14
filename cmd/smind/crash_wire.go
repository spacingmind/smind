package main

import (
	"path/filepath"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/crash"
	"github.com/spacingmind/smind/internal/version"
)

// initCrash points the process-default crash recorder at <smind home>/crashes.
// It does no I/O: the directory is created by the first report, so a bad or
// read-only home can never block or fail startup. cmdServe calls it first,
// before anything that starts a goroutine or listens.
func initCrash() {
	home := config.Dir()
	crash.Init(crash.Config{
		Dir:       filepath.Join(home, "crashes"),
		Version:   version.Version,
		Source:    "daemon",
		SmindHome: home,
	})
}
