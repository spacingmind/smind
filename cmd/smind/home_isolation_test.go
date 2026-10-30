package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/config"
)

// processHome is the temp HOME TestMain pins for the whole test process;
// realHomeAtStart is what HOME was before the pin.
var processHome, realHomeAtStart string

// isolateProcessHome pins HOME (and the XDG and smind homes under it) to a
// fresh temp directory for the whole test process, so no test -- and no
// `smind` subprocess it spawns -- can read the developer's real ~/.smind:
// serviceLogDir asks the service manager for the installed unit under HOME,
// and a really-installed unit would hand back the real daemon.log.
//
// GOCACHE/GOMODCACHE/GOPATH are captured first because buildSmind runs
// `go build`, whose defaults live under HOME and must stay warm.
//
// A re-exec'd helper child (daemon_crash_test.go, marked SMIND_CRASH_HELPER)
// inherits the SMIND_HOME its parent test chose; it keeps that instead of a
// fresh pin, so its daemon.log lands where the parent expects it.
func isolateProcessHome() func() {
	realHomeAtStart, _ = os.UserHomeDir()
	for _, key := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		if os.Getenv(key) != "" {
			continue
		}
		out, err := exec.Command("go", "env", key).Output()
		if err != nil {
			continue
		}
		if v := strings.TrimSpace(string(out)); v != "" {
			_ = os.Setenv(key, v)
		}
	}
	dir, err := os.MkdirTemp("", "smind-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "isolateProcessHome: %v\n", err)
		os.Exit(1)
	}
	processHome = dir
	for _, kv := range [][2]string{
		{"HOME", dir},
		{"USERPROFILE", dir},
		{"XDG_CONFIG_HOME", filepath.Join(dir, ".config")},
		{"XDG_DATA_HOME", filepath.Join(dir, ".local", "share")},
	} {
		_ = os.Setenv(kv[0], kv[1])
	}
	if os.Getenv("SMIND_CRASH_HELPER") == "" {
		_ = os.Setenv("SMIND_HOME", filepath.Join(dir, ".smind"))
	}
	return func() { _ = os.RemoveAll(dir) }
}

// TestProcessHomeIsHermetic pins down what TestMain already did: UserHomeDir
// is the process's temp home (never the real one), and config.Dir() and
// serviceLogDir() -- the exact call that once read the developer's real
// daemon.log through the installed unit -- both resolve under it.
func TestProcessHomeIsHermetic(t *testing.T) {
	got, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}
	if got != processHome {
		t.Errorf("os.UserHomeDir() = %q, want the pinned %q", got, processHome)
	}
	if realHomeAtStart != "" && got == realHomeAtStart {
		t.Errorf("os.UserHomeDir() = real HOME %q: the pin did not take", got)
	}

	t.Setenv("SMIND_HOME", "") // so config.Dir() falls back to $HOME/.smind
	if dir := config.Dir(); !strings.HasPrefix(filepath.Clean(dir), filepath.Clean(processHome)) {
		t.Errorf("config.Dir() = %q, want under %q", dir, processHome)
	}
	if serviceEnvHook != nil {
		t.Fatal("serviceEnvHook is set: serviceLogDir would not see the pinned HOME")
	}
	if dir := serviceLogDir(); !strings.HasPrefix(filepath.Clean(dir), filepath.Clean(processHome)) {
		t.Errorf("serviceLogDir() = %q, want under %q (no installed unit in the pinned HOME)", dir, processHome)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); !strings.HasPrefix(filepath.Clean(xdg), filepath.Clean(processHome)) {
		t.Errorf("XDG_CONFIG_HOME = %q, want under %q", xdg, processHome)
	}
}
