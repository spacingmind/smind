// Package service installs and drives the smind daemon as an OS service:
// a systemd unit on Linux and a LaunchAgent on macOS (ADR-0031 D2). The OS
// service manager is smind's supervisor; there is deliberately no background
// `smind start` of our own (decision 26).
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/config"
)

const (
	// SupervisorEnv is the environment variable the unit/plist defines so the
	// daemon can publish /healthz `managedBy` (decision 6).
	SupervisorEnv = "SMIND_SUPERVISOR"
	// UnitName is the systemd unit's file name, user or system scope.
	UnitName = "smind.service"
	// LaunchdLabel is the LaunchAgent's label and plist base name.
	LaunchdLabel = "com.spacingmind.smind"

	defaultPath = "/usr/local/bin:/usr/bin:/bin"
)

// Exit codes of `smind service status`, following systemctl.
const (
	ExitRunning      = 0
	ExitStopped      = 3
	ExitNotInstalled = 4
)

// ErrNotInstalled is returned by operations that need an installed service.
var ErrNotInstalled = errors.New("the smind service is not installed (run `smind service install`)")

// ErrUnsupported is returned by New on an OS without a backend.
var ErrUnsupported = errors.New("smind service is not available on this OS yet")

// State is what `status` observed.
type State int

// The three states, ordered like the exit codes they map to.
const (
	NotInstalled State = iota
	Stopped
	Running
)

// ExitCode is the `smind service status` exit code for s.
func (s State) ExitCode() int {
	switch s {
	case Running:
		return ExitRunning
	case Stopped:
		return ExitStopped
	default:
		return ExitNotInstalled
	}
}

// Options are the flags the subcommands share. Only Install uses Home, Path
// and NoStart.
type Options struct {
	// System selects the Linux system-wide unit instead of the user unit.
	System bool
	// Home writes SMIND_HOME=<Home> into the unit (default: only when the
	// installing shell has SMIND_HOME set).
	Home string
	// Path overrides the PATH the unit captures (default: the caller's).
	Path string
	// NoStart installs and enables without starting.
	NoStart bool
}

// Manager is one OS backend. Every method reports to Env.Out / Env.Err
// itself; a returned error is the reason the command failed.
type Manager interface {
	// Name names the backend for messages, e.g. "systemd (user)".
	Name(o Options) string
	Install(ctx context.Context, o Options) error
	Uninstall(ctx context.Context, o Options) error
	// Status prints the report and returns the observed state; err is only
	// for failures to find out (not for NotInstalled/Stopped).
	Status(ctx context.Context, o Options) (State, error)
	Start(ctx context.Context, o Options) error
	Stop(ctx context.Context, o Options) error
	Restart(ctx context.Context, o Options) error
	// InstalledHome is the smind home directory the installed service runs
	// with (the SMIND_HOME in its unit, else the user's default), and false
	// when no service is installed. Every command that acts on an existing
	// install uses it, so they agree with the daemon whatever the caller's
	// own SMIND_HOME is.
	InstalledHome(o Options) (string, bool)
}

// Result is one finished command.
type Result struct {
	Stdout, Stderr string
	ExitCode       int
}

// Runner runs the external tools (systemctl, loginctl, launchctl). A non-nil
// error means the tool could not run at all (not installed); a tool that ran
// and failed is a Result with a non-zero ExitCode.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

// ExecRunner is the real Runner.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, nil
	}
	return res, err
}

// Health is what a probe of the daemon's port saw.
type Health struct {
	// Up is whether anything accepted a connection on the port.
	Up bool
	// Smind is whether it answered /healthz like a smind daemon.
	Smind     bool
	Version   string
	ManagedBy string
}

// Env is everything the backends need from the machine, so tests can supply a
// fake systemctl/launchctl and a temp HOME.
type Env struct {
	GOOS      string
	UID, Euid int
	Username  string
	// Home is the invoking user's home directory.
	Home   string
	Getenv func(string) string
	// Exe is the absolute, symlink-resolved path of the running smind.
	Exe    string
	Runner Runner
	// LookupUser resolves a user name to its home directory and uid.
	LookupUser func(name string) (home string, uid int, err error)
	// Probe looks at the daemon's port (default: dial + GET /healthz).
	Probe func(ctx context.Context, port int) Health
	// SystemdBooted reports whether this machine runs systemd as init.
	SystemdBooted func() bool
	// Shutdown asks the daemon on port to stop (POST /api/shutdown with the
	// token in home). Default: the real request. Windows backend.
	Shutdown func(ctx context.Context, port int, home string) error
	// ForceStop kills this install's leftover processes (see forceStopScript):
	// its `service run` supervisors (after waiting up to wait for them to exit by
	// themselves) and, depending on mode, the process listening on port.
	// Default on Windows: a PowerShell script. Windows backend.
	ForceStop func(ctx context.Context, exe string, port int, mode KillMode, wait time.Duration) ([]int, error)
	// FileExists reports whether a path exists (default os.Stat).
	FileExists func(path string) bool
	// SystemUnitDir is where system units live (default /etc/systemd/system);
	// a field so tests never write under /etc.
	SystemUnitDir string
	// UserSID is the current user's SID (Windows), to recognise a scheduled
	// task that belongs to someone else.
	UserSID string
	// Version is this binary's version, for `status`.
	Version string
	// Out and Err receive the human output.
	Out, Err io.Writer

	// Poll, StartTimeout and StopTimeout bound the waits for the daemon to
	// come up or go down. Defaults: 250ms, 30s, 40s.
	Poll         time.Duration
	StartTimeout time.Duration
	StopTimeout  time.Duration
}

// DefaultEnv is the Env for the running process.
func DefaultEnv(version string) (Env, error) {
	exe, err := os.Executable()
	if err != nil {
		return Env{}, fmt.Errorf("locate the running binary: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, fmt.Errorf("find your home directory: %w", err)
	}
	var username, sid string
	if u, err := user.Current(); err == nil {
		username = u.Username
		if runtime.GOOS == "windows" {
			sid = u.Uid
		}
	}
	return Env{
		GOOS: runtime.GOOS, UID: os.Getuid(), Euid: os.Geteuid(),
		Username: username, UserSID: sid, Home: home, Getenv: os.Getenv, Exe: exe,
		Runner: ExecRunner{}, LookupUser: lookupUser, Probe: probeHTTP,
		SystemdBooted: func() bool { _, err := os.Stat("/run/systemd/system"); return err == nil },
		Version:       version, Out: os.Stdout, Err: os.Stderr,
	}, nil
}

func lookupUser(name string) (string, int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return "", 0, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	return u.HomeDir, uid, nil
}

// New returns the backend for env.GOOS.
func New(env Env) (Manager, error) {
	env = env.withDefaults()
	switch env.GOOS {
	case "linux":
		return &systemd{env: env}, nil
	case "darwin":
		return &launchd{env: env}, nil
	case "windows":
		return &taskSched{env: env}, nil
	default:
		return nil, fmt.Errorf("%w (%s)", ErrUnsupported, env.GOOS)
	}
}

func (e Env) withDefaults() Env {
	if e.Poll <= 0 {
		e.Poll = 250 * time.Millisecond
	}
	if e.StartTimeout <= 0 {
		e.StartTimeout = 30 * time.Second
	}
	if e.StopTimeout <= 0 {
		e.StopTimeout = 40 * time.Second
	}
	if e.FileExists == nil {
		e.FileExists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	}
	if e.ForceStop == nil && e.GOOS == "windows" {
		e.ForceStop = powershellForceStop(e.Runner)
	}
	if e.SystemUnitDir == "" {
		e.SystemUnitDir = systemUnitDir
	}
	if e.Getenv == nil {
		e.Getenv = os.Getenv
	}
	if e.Runner == nil {
		e.Runner = ExecRunner{}
	}
	if e.Probe == nil {
		e.Probe = probeHTTP
	}
	if e.Out == nil {
		e.Out = io.Discard
	}
	if e.Err == nil {
		e.Err = io.Discard
	}
	return e
}

func (e Env) outf(format string, a ...any)  { fmt.Fprintf(e.Out, format+"\n", a...) }
func (e Env) warnf(format string, a ...any) { fmt.Fprintf(e.Err, "smind service: "+format+"\n", a...) }

// target resolves the home directory and user the service runs as. For a
// system unit installed with sudo that is SUDO_USER, never root; plain root
// (no SUDO_USER, or SUDO_USER=root) is refused.
func (e Env) target(system bool) (home, username string, err error) {
	name := e.Getenv("SUDO_USER")
	if !system {
		if e.Euid == 0 && name != "" && name != "root" {
			return "", "", fmt.Errorf("you are running under sudo, but a user service belongs to your own account (%s): run it without sudo, or add --system for a system-wide unit that runs as %s", name, name)
		}
		return e.Home, e.Username, nil
	}
	if e.Euid != 0 {
		return e.Home, e.Username, nil
	}
	if name == "" || name == "root" {
		return "", "", errors.New("--system must not run the daemon as root: re-run as `sudo smind service install --system` from your own account so the unit gets User=<you>")
	}
	h, _, err := e.LookupUser(name)
	if err != nil {
		return "", "", fmt.Errorf("look up user %q (SUDO_USER): %w", name, err)
	}
	return h, name, nil
}

// smindHome is where the daemon keeps its data for a NEW install: an explicit
// --home, else the caller's SMIND_HOME, else the target user's default. It
// must be absolute: the service manager does not start in your shell's
// directory.
func (e Env) smindHome(o Options, targetHome string) (string, error) {
	h := o.Home
	if h == "" {
		h = e.Getenv("SMIND_HOME")
	}
	if h == "" {
		return config.DirFor(targetHome), nil
	}
	if !e.isAbs(h) {
		return "", fmt.Errorf("SMIND_HOME %q must be an absolute path to go into a service (pass --home <absolute dir>)", h)
	}
	return h, nil
}

// isAbs is filepath.IsAbs for the OS the service is for: a Windows path is
// absolute with a drive letter or as a UNC path, whatever OS runs the tests.
func (e Env) isAbs(p string) bool {
	if e.GOOS == "windows" {
		return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') || strings.HasPrefix(p, `\\`)
	}
	// Elsewhere: a rooted path, or whatever this host calls absolute (the
	// unit tests fake a Linux or macOS target on a Windows host).
	return strings.HasPrefix(p, "/") || filepath.IsAbs(p)
}

// serviceHome is the home of an EXISTING install: what its unit says, since
// that is what the daemon actually uses; the unit having no SMIND_HOME means
// the target user's default, whatever this shell has set.
func serviceHome(unitHome string, targetHome string) string {
	if unitHome != "" {
		return unitHome
	}
	return config.DirFor(targetHome)
}

// refuseSudoForUserScope refuses to act on a user-scope service as root
// under sudo: it would run `systemctl --user` as root, or write root-owned
// files into your home.
func (e Env) refuseSudoForUserScope() error {
	name := e.Getenv("SUDO_USER")
	if e.Euid == 0 && name != "" && name != "root" {
		return fmt.Errorf("you are running under sudo, but this service belongs to your own account (%s): run it without sudo", name)
	}
	return nil
}

// port is the daemon's configured port, read from that home's config.yaml.
func (e Env) port(home string) int {
	cfg, err := config.LoadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		e.warnf("%v (using the default port)", err)
		return config.Default().Server.Port
	}
	// The port local clients dial: the configured listen address's port (a
	// password-protected daemon may listen elsewhere than loopback).
	return cfg.Server.ClientPort()
}

// capturePath is the PATH to write into the unit.
func (e Env) capturePath(o Options) string {
	if o.Path != "" {
		return o.Path
	}
	if p := e.Getenv("PATH"); p != "" {
		return p
	}
	return defaultPath
}

// warnTemporaryExe warns when the binary the unit will run looks like it will
// not stay where it is.
func (e Env) warnTemporaryExe() {
	p := filepath.ToSlash(e.Exe)
	for _, frag := range []string{"/tmp/", "/var/tmp/", "/var/folders/", "/go-build"} {
		if strings.Contains(p, frag) {
			e.warnf("WARNING: %s looks temporary; the service will break when it is cleaned up. Install smind somewhere permanent (install.sh) and run `smind service install` from there.", e.Exe)
			return
		}
	}
	if strings.Contains(p, ".app/Contents/") {
		e.warnf("WARNING: %s is inside an application bundle; an app update replaces it. Prefer a standalone install (install.sh).", e.Exe)
	}
}

// waitHealthy polls until the smind daemon answers /healthz on port.
//
// supervisor is the value of managedBy the daemon must report: a daemon that
// answers without it is someone else's (a hand-started or app-managed one that
// held the port), not the one this service just started.
func (e Env) waitHealthy(ctx context.Context, port int, supervisor string) error {
	ctx, cancel := context.WithTimeout(ctx, e.StartTimeout)
	defer cancel()
	for {
		if h := e.Probe(ctx, port); h.Smind && h.ManagedBy == supervisor {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the daemon did not answer on port %d within %s (see `smind service logs`)", port, e.StartTimeout)
		case <-time.After(e.Poll):
		}
	}
}

// waitDown polls until nothing accepts connections on port.
func (e Env) waitDown(ctx context.Context, port int) error {
	ctx, cancel := context.WithTimeout(ctx, e.StopTimeout)
	defer cancel()
	for {
		if !e.Probe(ctx, port).Up {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the daemon was still answering on port %d after %s", port, e.StopTimeout)
		case <-time.After(e.Poll):
		}
	}
}

// foreignDaemonMessage explains why the service must not be started, or ""
// when the port is free. It is only meaningful when this service is not
// running: whatever answers then is not ours, and we never kill it.
func (e Env) foreignDaemonMessage(ctx context.Context, port int) string {
	h := e.Probe(ctx, port)
	if !h.Up {
		return ""
	}
	switch {
	case h.Smind && h.ManagedBy != "":
		return fmt.Sprintf("port %d is already served by another smind daemon (smind %s, managed by %s)", port, h.Version, h.ManagedBy)
	case h.Smind:
		return fmt.Sprintf("port %d is already served by a smind daemon (smind %s) that this service did not start, e.g. `smind serve` or the desktop app's daemon", port, h.Version)
	default:
		return fmt.Sprintf("port %d is in use by another program", port)
	}
}

func probeHTTP(ctx context.Context, port int) Health {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	d := net.Dialer{Timeout: 700 * time.Millisecond}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return Health{}
	}
	_ = conn.Close()
	h := Health{Up: true}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	if err != nil {
		return h
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return h
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Service   string `json:"service"`
		Version   string `json:"version"`
		ManagedBy string `json:"managedBy"`
	}
	if resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body) == nil && body.Service == "smind" {
		h.Smind, h.Version, h.ManagedBy = true, body.Version, body.ManagedBy
	}
	return h
}

// writeIfChanged writes data to path (creating parent directories) unless it
// already holds exactly data. It writes through a temp file and a rename, so
// a reader never sees half a unit.
func writeIfChanged(path string, data []byte, perm os.FileMode) (changed bool, err error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(dir, ".smind-service-*")
	if err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	return true, os.Rename(tmp.Name(), path)
}

// versionNote compares the daemon's version with this binary's.
func versionNote(daemon, binary string) string {
	switch {
	case daemon == "":
		return ""
	case binary == "" || daemon == binary:
		return fmt.Sprintf("smind %s", daemon)
	default:
		return fmt.Sprintf("smind %s (this binary is %s: `smind service restart` to switch)", daemon, binary)
	}
}
