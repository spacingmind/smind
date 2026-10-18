package service

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// golden compares got with testdata/<name>, or rewrites it under -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run `go test ./internal/service -update`)", err)
	}
	// A Windows checkout may have turned the golden's LF into CRLF.
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Errorf("%s differs from the golden file; run `go test ./internal/service -update` if intended.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// fakeDaemon stands in for whatever listens on the port.
type fakeDaemon struct {
	mu        sync.Mutex
	up        bool
	smind     bool
	version   string
	managedBy string
}

func (d *fakeDaemon) set(up, smind bool, version, managedBy string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.up, d.smind, d.version, d.managedBy = up, smind, version, managedBy
}

func (d *fakeDaemon) probe(_ context.Context, _ int) Health {
	d.mu.Lock()
	defer d.mu.Unlock()
	return Health{Up: d.up, Smind: d.up && d.smind, Version: d.version, ManagedBy: d.managedBy}
}

// fakeSystemctl is a state machine for systemctl and loginctl. The unit's
// existence is read from the real (temp) file system, like the real one.
type fakeSystemctl struct {
	mu          sync.Mutex
	userUnit    string
	systemUnit  string
	daemon      *fakeDaemon
	enabled     map[bool]bool // by system scope
	active      map[bool]bool
	linger      string // "yes"/"no"; "" = loginctl fails
	noBus       bool
	busMessage  string
	showFails   bool
	startFails  bool
	calls       []string
	daemonAfter string // version the daemon reports once started
}

func newFakeSystemctl(daemon *fakeDaemon, userUnit, systemDir string) *fakeSystemctl {
	return &fakeSystemctl{
		userUnit: userUnit, systemUnit: filepath.Join(systemDir, UnitName),
		daemon: daemon, enabled: map[bool]bool{}, active: map[bool]bool{},
		linger: "yes", daemonAfter: "1.0.0",
	}
}

func (f *fakeSystemctl) called(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeSystemctl) Run(_ context.Context, name string, args ...string) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch name {
	case "loginctl":
		if f.linger == "" {
			return Result{ExitCode: 1}, nil
		}
		return Result{Stdout: "Linger=" + f.linger + "\n"}, nil
	case "systemctl":
	default:
		return Result{}, fmt.Errorf("unexpected tool %s", name)
	}
	system := true
	if len(args) > 0 && args[0] == "--user" {
		system = false
		args = args[1:]
	}
	if f.noBus && !system {
		msg := f.busMessage
		if msg == "" {
			msg = "Failed to connect to bus: No medium found"
		}
		return Result{ExitCode: 1, Stderr: msg + "\n"}, nil
	}
	unit := f.systemUnit
	if !system {
		unit = f.userUnit
	}
	_, statErr := os.Stat(unit)
	exists := statErr == nil
	switch args[0] {
	case "daemon-reload", "reset-failed":
		return Result{}, nil
	case "enable":
		if !exists {
			return Result{ExitCode: 1, Stderr: "Failed to enable unit: Unit file smind.service does not exist.\n"}, nil
		}
		f.enabled[system] = true
		return Result{}, nil
	case "disable":
		f.enabled[system] = false
		return Result{}, nil
	case "start", "restart":
		if !exists || f.startFails {
			return Result{ExitCode: 1, Stderr: "Job for smind.service failed.\n"}, nil
		}
		f.active[system] = true
		by := "systemd"
		f.daemon.set(true, true, f.daemonAfter, by)
		return Result{}, nil
	case "stop":
		f.active[system] = false
		f.daemon.set(false, false, "", "")
		return Result{}, nil
	case "is-active":
		if f.active[system] {
			return Result{Stdout: "active\n"}, nil
		}
		return Result{Stdout: "inactive\n", ExitCode: 3}, nil
	case "show":
		if f.showFails {
			return Result{ExitCode: 1, Stderr: "boom\n"}, nil
		}
		load, active, sub, ufs, pid := "not-found", "inactive", "dead", "", "0"
		if exists {
			load = "loaded"
			ufs = "disabled"
			if f.enabled[system] {
				ufs = "enabled"
			}
			if f.active[system] {
				active, sub, pid = "active", "running", "4242"
			}
		}
		return Result{Stdout: fmt.Sprintf("LoadState=%s\nActiveState=%s\nSubState=%s\nUnitFileState=%s\nMainPID=%s\n", load, active, sub, ufs, pid)}, nil
	}
	return Result{ExitCode: 1, Stderr: "unknown verb " + args[0]}, nil
}

// fakeLaunchctl is the same for launchctl.
type fakeLaunchctl struct {
	mu                 sync.Mutex
	plist              string
	service            string
	domain             string
	daemon             *fakeDaemon
	loaded             bool
	running            bool
	noGUISession       bool
	bootoutFails       bool
	bootoutKeepsDaemon bool
	calls              []string
	daemonVersion      string
}

func newFakeLaunchctl(daemon *fakeDaemon, plist string, uid int) *fakeLaunchctl {
	dom := fmt.Sprintf("gui/%d", uid)
	return &fakeLaunchctl{plist: plist, domain: dom, service: dom + "/" + LaunchdLabel, daemon: daemon, daemonVersion: "1.0.0"}
}

func (f *fakeLaunchctl) called(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// crash simulates the daemon exiting by itself (loaded stays, not running).
func (f *fakeLaunchctl) exited() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = false
	f.daemon.set(false, false, "", "")
}

func (f *fakeLaunchctl) Run(_ context.Context, name string, args ...string) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name != "launchctl" {
		return Result{}, fmt.Errorf("unexpected tool %s", name)
	}
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	_, statErr := os.Stat(f.plist)
	switch args[0] {
	case "enable":
		return Result{}, nil
	case "print":
		if !f.loaded {
			return Result{ExitCode: 113, Stderr: "Could not find service \"" + LaunchdLabel + "\" in domain for user gui\n"}, nil
		}
		if f.running {
			return Result{Stdout: "com.spacingmind.smind = {\n\tstate = running\n\tpid = 777\n}\n"}, nil
		}
		return Result{Stdout: "com.spacingmind.smind = {\n\tstate = not running\n}\n"}, nil
	case "bootstrap":
		if f.noGUISession {
			return Result{ExitCode: 125, Stderr: "Bootstrap failed: 125: Domain does not support specified action\n"}, nil
		}
		if statErr != nil {
			return Result{ExitCode: 5, Stderr: "Bootstrap failed: 5: Input/output error\n"}, nil
		}
		if f.loaded {
			return Result{ExitCode: 5, Stderr: "Bootstrap failed: 5: Input/output error\n"}, nil
		}
		f.loaded, f.running = true, true // RunAtLoad
		f.daemon.set(true, true, f.daemonVersion, "launchd")
		return Result{}, nil
	case "bootout":
		if !f.loaded {
			return Result{ExitCode: 113, Stderr: "Boot-out failed: 3: No such process\n"}, nil
		}
		if f.bootoutFails {
			return Result{ExitCode: 5, Stderr: "Boot-out failed: 5: Input/output error\n"}, nil
		}
		f.loaded, f.running = false, false
		if !f.bootoutKeepsDaemon {
			f.daemon.set(false, false, "", "")
		}
		return Result{}, nil
	case "kickstart":
		if !f.loaded {
			return Result{ExitCode: 113, Stderr: "Could not find service\n"}, nil
		}
		f.running = true
		f.daemon.set(true, true, f.daemonVersion, "launchd")
		return Result{}, nil
	}
	return Result{ExitCode: 1, Stderr: "unknown verb " + args[0]}, nil
}

// testEnv is an Env over a temp HOME with fast timeouts and captured output.
type testEnv struct {
	Env
	out, errb *bytes.Buffer
	home      string
	daemon    *fakeDaemon
	environ   map[string]string
}

func newTestEnv(t *testing.T, goos string) *testEnv {
	t.Helper()
	home := t.TempDir()
	te := &testEnv{home: home, daemon: &fakeDaemon{}, out: &bytes.Buffer{}, errb: &bytes.Buffer{},
		environ: map[string]string{"PATH": "/opt/bin:/usr/bin:/bin"}}
	te.Env = Env{
		GOOS: goos, UID: 501, Euid: 1000, Username: "alice", Home: home,
		Getenv:        func(k string) string { return te.environ[k] },
		Exe:           "/usr/local/bin/smind",
		LookupUser:    func(name string) (string, int, error) { return "/home/" + name, 1000, nil },
		Probe:         te.daemon.probe,
		SystemdBooted: func() bool { return true },
		Version:       "1.0.0",
		SystemUnitDir: filepath.Join(home, "etc-systemd-system"),
		Out:           te.out, Err: te.errb,
		Poll: time.Millisecond, StartTimeout: 200 * time.Millisecond, StopTimeout: 200 * time.Millisecond,
	}
	return te
}

func (te *testEnv) userUnit() string {
	return filepath.Join(te.home, ".config", "systemd", "user", UnitName)
}

func (te *testEnv) plist() string {
	return filepath.Join(te.home, "Library", "LaunchAgents", LaunchdLabel+".plist")
}

func (te *testEnv) systemd(t *testing.T) (Manager, *fakeSystemctl) {
	t.Helper()
	fs := newFakeSystemctl(te.daemon, te.userUnit(), te.SystemUnitDir)
	te.Env.Runner = fs
	m, err := New(te.Env)
	if err != nil {
		t.Fatal(err)
	}
	return m, fs
}

func (te *testEnv) launchd(t *testing.T) (Manager, *fakeLaunchctl) {
	t.Helper()
	fl := newFakeLaunchctl(te.daemon, te.plist(), te.UID)
	te.Env.Runner = fl
	m, err := New(te.Env)
	if err != nil {
		t.Fatal(err)
	}
	return m, fl
}

func (te *testEnv) resetOutput() { te.out.Reset(); te.errb.Reset() }
