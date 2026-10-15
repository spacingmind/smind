package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/logging"
	"github.com/spacingmind/smind/internal/service"
)

// runSvc runs the CLI with osStdout and osStderr (what cmdService writes to)
// captured into one buffer.
func runSvc(t *testing.T, args []string) (int, string) {
	t.Helper()
	var buf syncedBuffer
	prevOut, prevErr := osStdout, osStderr
	osStdout, osStderr = &buf, &buf
	defer func() { osStdout, osStderr = prevOut, prevErr }()
	code := run(args)
	return code, buf.String()
}

// isolateServiceEnv points the service commands at a throwaway home and an OS
// with no backend, so a test never looks at (or into) a service installed on
// the machine running the tests.
func isolateServiceEnv(t *testing.T) {
	t.Helper()
	serviceEnvHook = func(e *service.Env) { e.GOOS, e.Home = "windows", t.TempDir() }
	t.Cleanup(func() { serviceEnvHook = nil })
}

func writeDaemonLog(t *testing.T, home string, lines int) {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "2026/10/15 10:00:%02d line %d\n", i%60, i)
	}
	if err := os.WriteFile(filepath.Join(home, logging.FileName), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestServiceLogsTailsDaemonLog: `smind service logs` prints the tail of the
// daemon's own log on every OS, honouring -n.
func TestServiceLogsTailsDaemonLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	isolateServiceEnv(t)
	writeDaemonLog(t, home, 500)

	code, out := runSvc(t, []string{"service", "logs", "-n", "3"})
	if code != 0 {
		t.Fatalf("exit = %d, output %q", code, out)
	}
	want := "line 498\n" // the three last lines are 498, 499, 500
	if !strings.Contains(out, want) || !strings.HasSuffix(out, "line 500\n") || strings.Contains(out, "line 497") {
		t.Errorf("tail -n 3 = %q", out)
	}

	// Default is 100 lines.
	if _, out := runSvc(t, []string{"service", "logs"}); strings.Count(out, "\n") != 100 {
		t.Errorf("default tail = %d lines, want 100", strings.Count(out, "\n"))
	}
}

func TestServiceLogsWithoutALogFile(t *testing.T) {
	t.Setenv("SMIND_HOME", t.TempDir())
	isolateServiceEnv(t)
	code, out := runSvc(t, []string{"service", "logs"})
	if code != 1 || !strings.Contains(out, "no daemon log yet") {
		t.Errorf("exit = %d, output %q", code, out)
	}
}

func TestServiceLogsFollowStopsOnContextAndSeesAppends(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	isolateServiceEnv(t)
	writeDaemonLog(t, home, 2)

	var out syncedBuffer
	prev := osStdout
	osStdout = &out
	t.Cleanup(func() { osStdout = prev })

	done := make(chan int, 1)
	go func() { done <- cmdServiceLogs([]string{"-f", "-n", "1"}) }()

	waitUntil(t, "initial tail", func() bool { return strings.Contains(out.String(), "line 2\n") })
	f, err := os.OpenFile(filepath.Join(home, logging.FileName), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("appended later\n")
	_ = f.Close()
	waitUntil(t, "appended line", func() bool { return strings.Contains(out.String(), "appended later\n") })
	if strings.Contains(out.String(), "line 1\n") {
		t.Errorf("-n 1 must not print the first line: %q", out.String())
	}
	// Ctrl+C: the command installs its own signal handler; deliver one to ourselves.
	p, _ := os.FindProcess(os.Getpid())
	_ = p.Signal(os.Interrupt)
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("logs -f did not stop on SIGINT")
	}
}

type syncedBuffer struct {
	mu  chan struct{}
	buf bytes.Buffer
}

func (s *syncedBuffer) lock() func() {
	if s.mu == nil {
		s.mu = make(chan struct{}, 1)
	}
	s.mu <- struct{}{}
	return func() { <-s.mu }
}

func (s *syncedBuffer) Write(p []byte) (int, error) { defer s.lock()(); return s.buf.Write(p) }
func (s *syncedBuffer) String() string              { defer s.lock()(); return s.buf.String() }

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestStderrStillReceivesLogs: adding the file must not take stderr away, and
// the same line lands in both.
func TestStderrStillReceivesLogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	prevOut, prevFlags := log.Writer(), log.Flags()
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	log.SetFlags(0)

	var stderr bytes.Buffer
	closeLog := setupDaemonLog(config.Default().Log, &stderr)
	log.Printf("hello from the daemon")
	closeLog()

	if got := stderr.String(); got != "hello from the daemon\n" {
		t.Errorf("stderr = %q", got)
	}
	b, err := os.ReadFile(filepath.Join(home, logging.FileName))
	if err != nil || string(b) != "hello from the daemon\n" {
		t.Errorf("daemon.log = %q, %v", b, err)
	}
}

// TestDaemonLogFailureFallsBackToStderr: an unwritable home never stops the
// daemon from logging.
func TestDaemonLogFailureFallsBackToStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions")
	}
	home := filepath.Join(t.TempDir(), "file-not-dir")
	if err := os.WriteFile(home, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMIND_HOME", home)
	prevOut := log.Writer()
	t.Cleanup(func() { log.SetOutput(prevOut) })
	var stderr bytes.Buffer
	log.SetOutput(&stderr)

	closeLog := setupDaemonLog(config.Default().Log, &stderr)
	defer closeLog()
	log.Printf("still logging")
	if !strings.Contains(stderr.String(), "logging to stderr only") || !strings.Contains(stderr.String(), "still logging") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// TestServe_DaemonLogAndManagedBy runs the real daemon: with SMIND_SUPERVISOR
// set (as the unit does) /healthz says who manages it, daemon.log exists with
// mode 0600 and holds what stderr got, `smind service logs` reads it back, and
// without the variable /healthz has no managedBy.
func TestServe_DaemonLogAndManagedBy(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the daemon")
	}
	bin := buildSmind(t)
	for _, supervisor := range []string{"systemd", ""} {
		t.Run("SMIND_SUPERVISOR="+supervisor, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("SMIND_HOME", home) // nothing in this test may fall back to the real ~/.smind
			port := freePort(t)
			writePortConfig(t, home, port)
			serve := exec.Command(bin, "serve")
			serve.Env = append(smindEnv(home), service.SupervisorEnv+"="+supervisor)
			var serveOut stdBuffer
			serve.Stdout, serve.Stderr = &serveOut, &serveOut
			if err := serve.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = serve.Process.Kill(); _, _ = serve.Process.Wait() })
			waitDaemonHealthy(t, port, 20*time.Second)

			resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/healthz")
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			err = json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			by, has := body["managedBy"]
			if supervisor != "" && by != supervisor {
				t.Errorf("managedBy = %v, want %s (%v)", by, supervisor, body)
			}
			if supervisor == "" && has {
				t.Errorf("managedBy must be absent when SMIND_SUPERVISOR is unset: %v", body)
			}

			logPath := filepath.Join(home, logging.FileName)
			fileLog, err := os.ReadFile(logPath)
			if err != nil || !strings.Contains(string(fileLog), "smind listening on") {
				t.Fatalf("daemon.log = %q, %v (stderr: %s)", fileLog, err, serveOut.String())
			}
			if runtime.GOOS != "windows" {
				if fi, _ := os.Stat(logPath); fi.Mode().Perm() != 0o600 {
					t.Errorf("daemon.log mode = %v, want 0600", fi.Mode().Perm())
				}
			}
			if !strings.Contains(serveOut.String(), "smind listening on") {
				t.Errorf("stderr no longer receives the log: %q", serveOut.String())
			}

			logsCmd := exec.Command(bin, "service", "logs", "-n", "50")
			logsCmd.Env = smindEnv(home)
			logs, err := logsCmd.CombinedOutput()
			if err != nil || !strings.Contains(string(logs), "smind listening on") {
				t.Errorf("service logs = %q, %v", logs, err)
			}
			// Always with the temp SMIND_HOME: `stop` must only ever reach this test's daemon.
			stopCmd := exec.Command(bin, "stop")
			stopCmd.Env = smindEnv(home)
			if out, err := stopCmd.CombinedOutput(); err != nil {
				t.Errorf("stop: %v %s", err, out)
			}
		})
	}
}

// ---------------------------------------------------------- the CLI surface

// scriptedRunner answers tool calls from a function, for exit-code mapping.
type scriptedRunner func(name string, args ...string) service.Result

func (f scriptedRunner) Run(_ context.Context, name string, args ...string) (service.Result, error) {
	return f(name, args...), nil
}

func withServiceEnv(t *testing.T, goos string, r service.Runner) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("SMIND_HOME", filepath.Join(home, ".smind"))
	serviceEnvHook = func(e *service.Env) {
		e.GOOS, e.Home, e.UID, e.Euid, e.Username = goos, home, 501, 1000, "alice"
		e.Exe = "/usr/local/bin/smind"
		e.Runner = r
		e.SystemdBooted = func() bool { return true }
		e.Probe = func(context.Context, int) service.Health { return service.Health{} }
		e.Poll, e.StartTimeout, e.StopTimeout = time.Millisecond, 50*time.Millisecond, 50*time.Millisecond
	}
	t.Cleanup(func() { serviceEnvHook = nil })
	return home
}

func TestServiceStatusExitCodes(t *testing.T) {
	// systemd: not installed -> 4, installed+inactive -> 3, active -> 0.
	state := "not-found"
	active := "inactive"
	home := withServiceEnv(t, "linux", scriptedRunner(func(name string, args ...string) service.Result {
		return service.Result{Stdout: fmt.Sprintf("LoadState=%s\nActiveState=%s\nSubState=x\nUnitFileState=enabled\nMainPID=1\n", state, active)}
	}))
	_ = home
	for _, tc := range []struct {
		state, active string
		want          int
	}{{"not-found", "inactive", 4}, {"loaded", "inactive", 3}, {"loaded", "active", 0}} {
		state, active = tc.state, tc.active
		if code, out := runSvc(t, []string{"service", "status"}); code != tc.want {
			t.Errorf("LoadState=%s ActiveState=%s: exit = %d, want %d (%q)", tc.state, tc.active, code, tc.want, out)
		}
	}

	// start/stop/restart/uninstall on a missing service: 4 (like status).
	state = "not-found"
	for _, sub := range []string{"start", "stop", "restart"} {
		if code, out := runSvc(t, []string{"service", sub}); code != 4 || !strings.Contains(out, "not installed") {
			t.Errorf("service %s on a missing service: exit = %d (%q)", sub, code, out)
		}
	}
	if code, _ := runSvc(t, []string{"service", "uninstall"}); code != 0 {
		t.Errorf("uninstall of a missing service must be a no-op, exit = %d", code)
	}
}

func TestServiceUsageErrorsExitTwo(t *testing.T) {
	withServiceEnv(t, "linux", scriptedRunner(func(string, ...string) service.Result { return service.Result{} }))
	for _, args := range [][]string{
		{"service"},
		{"service", "bogus"},
		{"service", "install", "--port", "9"}, // there is no --port: config.yaml owns it
		{"service", "status", "extra"},
		{"service", "logs", "-n", "-3"},
		{"service", "start", "--no-start"}, // install-only flag
	} {
		if code, out := runSvc(t, args); code != 2 {
			t.Errorf("%v: exit = %d, want 2 (%q)", args, code, out)
		}
	}
	if code, out := runSvc(t, []string{"service", "--help"}); code != 0 || !strings.Contains(out, "smind service install") {
		t.Errorf("--help: exit = %d (%q)", code, out)
	}
}

func TestServiceOnUnsupportedOS(t *testing.T) {
	withServiceEnv(t, "windows", scriptedRunner(func(string, ...string) service.Result { return service.Result{} }))
	code, out := runSvc(t, []string{"service", "status"})
	if code != 1 || !strings.Contains(out, "not available on this OS yet") {
		t.Errorf("exit = %d (%q)", code, out)
	}
}

// TestServiceCLIWithFakeServiceManager runs the real binary against a fake
// systemctl / launchctl on PATH and a temp HOME: flag parsing, the real
// ExecRunner, the files written, and the exit codes, end to end.
func TestServiceCLIWithFakeServiceManager(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	var tool, unitRel string
	switch runtime.GOOS {
	case "linux":
		if _, err := os.Stat("/run/systemd/system"); err != nil {
			t.Skip("systemd is not running here; the backend refuses to install")
		}
		tool, unitRel = "systemctl", filepath.Join(".config", "systemd", "user", service.UnitName)
	case "darwin":
		tool, unitRel = "launchctl", filepath.Join("Library", "LaunchAgents", service.LaunchdLabel+".plist")
	default:
		t.Skip("no service backend on " + runtime.GOOS)
	}
	bin := buildSmind(t)

	home := t.TempDir()
	toolDir := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls.log")
	// The unit/plist existing is what "installed" means; nothing is ever
	// running, so `print` / `is-active` say so.
	script := `#!/bin/sh
echo "$@" >> "` + calls + `"
case "$1" in
  print) exit 113 ;;
  is-active|--user) case "$*" in
      *is-active*) echo inactive; exit 3 ;;
      *show*) if [ -e "` + filepath.Join(home, unitRel) + `" ]; then echo "LoadState=loaded"; else echo "LoadState=not-found"; fi
              echo "ActiveState=inactive"; echo "UnitFileState=enabled"; exit 0 ;;
    esac ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(toolDir, tool), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// loginctl: lingering on, so the hint is not printed.
	_ = os.WriteFile(filepath.Join(toolDir, "loginctl"), []byte("#!/bin/sh\necho Linger=yes\n"), 0o755)

	smindHome := filepath.Join(home, ".smind")
	run := func(args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		// XDG_CONFIG_HOME is pinned: a runner may set it, which moves where a
		// systemd user unit is written.
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "SMIND_HOME="+smindHome, "PATH="+toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("run %v: %v", args, err)
		}
		return code, string(out)
	}

	if code, out := run("service", "status"); code != 4 {
		t.Fatalf("status before install: exit %d: %s", code, out)
	}
	if code, out := run("service", "install", "--no-start"); code != 0 {
		t.Fatalf("install: exit %d: %s", code, out)
	}
	unit, err := os.ReadFile(filepath.Join(home, unitRel))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"serve", service.SupervisorEnv, smindHome} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if code, out := run("service", "status"); code != 3 {
		t.Fatalf("status after install: exit %d: %s", code, out)
	}
	if code, out := run("service", "install", "--no-start"); code != 0 || !strings.Contains(out, "up to date") {
		t.Fatalf("second install: exit %d: %s", code, out)
	}
	if code, out := run("service", "uninstall"); code != 0 {
		t.Fatalf("uninstall: exit %d: %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, unitRel)); err == nil {
		t.Error("uninstall left the unit behind")
	}
	if code, _ := run("service", "status"); code != 4 {
		t.Errorf("status after uninstall: exit %d", code)
	}
	if _, err := os.Stat(calls); err != nil {
		t.Error("the fake service manager was never called")
	}
}

// A broken stderr (closed terminal, a pipe the desktop app dropped) must not
// stop the line reaching daemon.log: io.MultiWriter would otherwise stop at
// the first failing writer.
func TestDaemonLogSurvivesABrokenStderr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	prevOut, prevFlags := log.Writer(), log.Flags()
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	log.SetFlags(0)

	closeLog := setupDaemonLog(config.Default().Log, failingWriter{})
	log.Printf("must reach the file")
	closeLog()
	if b, _ := os.ReadFile(filepath.Join(home, logging.FileName)); string(b) != "must reach the file\n" {
		t.Errorf("daemon.log = %q", b)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("broken pipe") }

// The supervisor variable is consumed: a child of the daemon must not see it.
func TestTakeSupervisorEnvRemovesItFromTheEnvironment(t *testing.T) {
	t.Setenv(service.SupervisorEnv, " systemd ")
	if got := takeSupervisorEnv(); got != "systemd" {
		t.Errorf("value = %q", got)
	}
	if _, set := os.LookupEnv(service.SupervisorEnv); set {
		t.Error("SMIND_SUPERVISOR must be unset after it was read")
	}
	if got := takeSupervisorEnv(); got != "" {
		t.Errorf("second read = %q", got)
	}
}

// `smind service logs` reads the log of the INSTALLED service, whose unit may
// set a SMIND_HOME this shell does not have.
func TestServiceLogsReadTheInstalledServicesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", filepath.Join(home, "shell-home")) // not where the daemon logs
	svcHome := filepath.Join(home, "svc-home")
	if err := os.MkdirAll(svcHome, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDaemonLog(t, svcHome, 3)

	serviceEnvHook = func(e *service.Env) { e.GOOS, e.Home, e.Euid, e.UID = "linux", home, 1000, 501 }
	t.Cleanup(func() { serviceEnvHook = nil })
	unit := filepath.Join(home, ".config", "systemd", "user", service.UnitName)
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	body := "[Service]\nEnvironment=\"SMIND_HOME=" + svcHome + "\"\n"
	if err := os.WriteFile(unit, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runSvc(t, []string{"service", "logs", "-n", "1"})
	if code != 0 || !strings.Contains(out, "line 3") {
		t.Errorf("exit = %d, output %q", code, out)
	}
}
