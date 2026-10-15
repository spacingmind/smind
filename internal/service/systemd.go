package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// systemd is the Linux backend: a user unit by default, an opt-in system unit
// that still runs as the invoking user (ADR-0031 decision 3).
type systemd struct{ env Env }

const systemUnitDir = "/etc/systemd/system"

func (s *systemd) Name(o Options) string {
	if o.System {
		return "systemd (system)"
	}
	return "systemd (user)"
}

// unitSpec is the input of renderUnit.
type unitSpec struct {
	Exe    string
	Path   string
	Home   string // SMIND_HOME; "" omits the variable
	User   string // User= for the system unit
	System bool
}

// renderUnit is the unit file text. Deliberately no sandboxing directives
// (ProtectHome and friends): agents must write to the user's repositories.
func renderUnit(sp unitSpec) string {
	var b strings.Builder
	b.WriteString("# Written by `smind service install`; the next install overwrites edits.\n")
	b.WriteString("[Unit]\nDescription=sMind daemon\n")
	if sp.System {
		b.WriteString("After=network-online.target\nWants=network-online.target\n")
	}
	b.WriteString("\n[Service]\nType=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s serve\n", quoteExec(sp.Exe))
	b.WriteString("Restart=on-failure\nRestartSec=5\nTimeoutStopSec=30\nKillSignal=SIGTERM\n")
	if sp.System {
		fmt.Fprintf(&b, "User=%s\n", sp.User)
	}
	fmt.Fprintf(&b, "Environment=%s\n", quoteEnv("PATH", sp.Path))
	if sp.Home != "" {
		fmt.Fprintf(&b, "Environment=%s\n", quoteEnv("SMIND_HOME", sp.Home))
	}
	fmt.Fprintf(&b, "Environment=%s\n", quoteEnv(SupervisorEnv, "systemd"))
	b.WriteString("\n[Install]\n")
	if sp.System {
		b.WriteString("WantedBy=multi-user.target\n")
	} else {
		b.WriteString("WantedBy=default.target\n")
	}
	return b.String()
}

// quoteExec quotes the executable for ExecStart= when it needs it. systemd
// splits ExecStart on whitespace, honours quotes and backslashes, and expands
// % specifiers and $ variables, so all of those are escaped.
func quoteExec(p string) string {
	if !strings.ContainsAny(p, " \t\"'\\$%;") {
		return p
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return `"` + r.Replace(p) + `"`
}

// quoteEnv is a whole "NAME=value" assignment quoted for Environment=.
func quoteEnv(name, value string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, "\n", `\n`)
	return `"` + name + "=" + r.Replace(value) + `"`
}

// scope picks the unit scope for the commands that act on an existing
// install: --system when asked, else the user unit, else a system unit when
// that is the only one that exists.
func (s *systemd) scope(o Options) Options {
	if o.System {
		return o
	}
	if _, err := os.Stat(s.unitPath(false, s.env.Home)); err == nil {
		return o
	}
	if _, err := os.Stat(s.unitPath(true, "")); err == nil {
		o.System = true
	}
	return o
}

func (s *systemd) unitPath(system bool, home string) string {
	if system {
		return filepath.Join(s.env.SystemUnitDir, UnitName)
	}
	cfg := s.env.Getenv("XDG_CONFIG_HOME")
	if cfg == "" || !filepath.IsAbs(cfg) {
		cfg = filepath.Join(home, ".config")
	}
	return filepath.Join(cfg, "systemd", "user", UnitName)
}

// ctl runs systemctl for the scope, wrapping a failure to even reach systemd
// with a hint.
func (s *systemd) ctl(ctx context.Context, system bool, args ...string) (Result, error) {
	if !system {
		args = append([]string{"--user"}, args...)
	}
	res, err := s.env.Runner.Run(ctx, "systemctl", args...)
	if err != nil {
		return res, fmt.Errorf("run systemctl: %w (is systemd installed?)", err)
	}
	if res.ExitCode != 0 && !system && (strings.Contains(res.Stderr, "Failed to connect to") || strings.Contains(res.Stderr, "No medium found")) {
		return res, fmt.Errorf("%s: there is no systemd user session for this login (XDG_RUNTIME_DIR unset, or `su`/`sudo` without a login session). Log in normally, or use `sudo smind service install --system`", strings.TrimSpace(res.Stderr))
	}
	return res, nil
}

// ctlOK is ctl that also turns a non-zero exit into an error.
func (s *systemd) ctlOK(ctx context.Context, system bool, args ...string) error {
	res, err := s.ctl(ctx, system, args...)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return fmt.Errorf("systemctl %s: exit %d: %s", strings.Join(args, " "), res.ExitCode, msg)
	}
	return nil
}

func (s *systemd) active(ctx context.Context, system bool) bool {
	res, err := s.ctl(ctx, system, "is-active", UnitName)
	return err == nil && res.ExitCode == 0 && strings.TrimSpace(res.Stdout) == "active"
}

func (s *systemd) booted() error {
	if s.env.SystemdBooted != nil && !s.env.SystemdBooted() {
		return errors.New("systemd is not running on this machine (a container, WSL without systemd, or another init). Run `smind serve` under your own init system instead")
	}
	return nil
}

// needRoot is the up-front check for acting on a system unit.
func (s *systemd) needRoot(o Options, what string) error {
	if o.System && s.env.Euid != 0 {
		return fmt.Errorf("--system %s the unit in %s: re-run with sudo (`sudo smind service %s --system`)", what, s.env.SystemUnitDir, strings.Fields(what)[0])
	}
	if !o.System {
		return s.env.refuseSudoForUserScope()
	}
	return nil
}

func (s *systemd) Install(ctx context.Context, o Options) error {
	env := s.env
	if err := s.booted(); err != nil {
		return err
	}
	if o.System && env.Euid != 0 {
		return errors.New("--system writes " + env.SystemUnitDir + ": re-run with `sudo smind service install --system`")
	}
	home, username, err := env.target(o.System)
	if err != nil {
		return err
	}
	smindHome, err := env.smindHome(o, home)
	if err != nil {
		return err
	}
	env.warnTemporaryExe()
	if o.System && o.Path == "" && env.Getenv("SUDO_USER") != "" {
		env.warnf("the PATH captured here is sudo's, not yours: tools in your home (~/.local/bin, nvm, ...) will not resolve inside the service. Re-run with --path \"$PATH\" from your own shell if agents need them.")
	}

	sp := unitSpec{Exe: env.Exe, Path: env.capturePath(o), User: username, System: o.System}
	// SMIND_HOME is written only when it was asked for (flag or shell); the
	// default home needs no variable.
	if o.Home != "" || env.Getenv("SMIND_HOME") != "" {
		sp.Home = smindHome
	}
	for _, v := range []string{sp.Exe, sp.Path, sp.Home} {
		if strings.ContainsAny(v, "\n\r") {
			return errors.New("refusing a path containing a newline")
		}
	}
	path := s.unitPath(o.System, home)
	changed, err := writeIfChanged(path, []byte(renderUnit(sp)), 0o644)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if changed {
		env.outf("wrote %s", path)
		if err := s.ctlOK(ctx, o.System, "daemon-reload"); err != nil {
			return err
		}
	} else {
		env.outf("%s is up to date", path)
	}
	if err := s.ctlOK(ctx, o.System, "enable", UnitName); err != nil {
		return err
	}
	env.outf("enabled %s (%s)", UnitName, s.Name(o))
	if !o.System {
		s.lingerHint(ctx, username)
	}

	running := s.active(ctx, o.System)
	port := env.port(smindHome)
	switch {
	case o.NoStart:
		env.outf("not starting (--no-start); start it with `smind service start`")
	case running:
		switch h := env.Probe(ctx, port); {
		case changed:
			env.outf("already running; run `smind service restart` to apply the new unit")
		case h.Smind && env.Version != "" && h.Version != "" && h.Version != env.Version:
			env.outf("already running smind %s, but this binary is %s: run `smind service restart` to switch", h.Version, env.Version)
		default:
			env.outf("already running")
		}
	default:
		if msg := env.foreignDaemonMessage(ctx, port); msg != "" {
			env.warnf("NOT starting the service: %s. The unit is installed and enabled; stop that daemon, then run `smind service start`.", msg)
			return nil
		}
		if err := s.ctlOK(ctx, o.System, "start", UnitName); err != nil {
			return err
		}
		if err := env.waitHealthy(ctx, port, "systemd"); err != nil {
			return err
		}
		env.outf("started; smind is listening on http://127.0.0.1:%d", port)
	}
	return nil
}

// lingerHint tells a user-unit installer that the unit only runs at boot, and
// without a login, once lingering is enabled.
func (s *systemd) lingerHint(ctx context.Context, username string) {
	if username == "" {
		return
	}
	res, err := s.env.Runner.Run(ctx, "loginctl", "show-user", username, "--property=Linger")
	if err != nil || res.ExitCode != 0 {
		return
	}
	if strings.TrimSpace(res.Stdout) == "Linger=no" {
		s.env.outf("note: lingering is off for %s, so the service only runs while you are logged in. To start it at boot:\n  sudo loginctl enable-linger %s", username, username)
	}
}

func (s *systemd) Uninstall(ctx context.Context, o Options) error {
	env := s.env
	o = s.scope(o)
	if err := s.needRoot(o, "removes"); err != nil {
		return err
	}
	home, _, err := env.target(o.System)
	if err != nil {
		home = env.Home // uninstall does not need the target user
	}
	path := s.unitPath(o.System, home)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		env.outf("not installed (%s does not exist); nothing to do", path)
		return nil
	}
	// A failure to even talk to systemd (no user session bus) must stop us
	// here: removing the file would leave the daemon running and enabled
	// with nothing left to manage it. A unit that is merely already stopped
	// or never enabled is a non-zero exit and does not block removal.
	if _, err := s.ctl(ctx, o.System, "stop", UnitName); err != nil {
		return err
	}
	_, _ = s.ctl(ctx, o.System, "disable", UnitName)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if err := s.ctlOK(ctx, o.System, "daemon-reload"); err != nil {
		return err
	}
	_, _ = s.ctl(ctx, o.System, "reset-failed", UnitName)
	env.outf("stopped and removed %s; your data in the smind home directory is untouched", path)
	return nil
}

// show reads the unit's properties.
func (s *systemd) show(ctx context.Context, system bool) (map[string]string, error) {
	res, err := s.ctl(ctx, system, "show", UnitName, "--no-pager",
		"--property=LoadState,ActiveState,SubState,UnitFileState,MainPID")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("systemctl show %s: exit %d: %s", UnitName, res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	props := map[string]string{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			props[k] = strings.TrimSpace(v)
		}
	}
	return props, nil
}

// installed reports whether the unit exists for the scope.
func (s *systemd) installed(ctx context.Context, system bool, home string) (bool, map[string]string, error) {
	props, err := s.show(ctx, system)
	if err != nil {
		return false, nil, err
	}
	if props["LoadState"] == "not-found" || props["LoadState"] == "" {
		_, statErr := os.Stat(s.unitPath(system, home))
		return statErr == nil, props, nil
	}
	return true, props, nil
}

var reUnitHome = regexp.MustCompile(`(?m)^Environment="SMIND_HOME=(.*)"$`)

// unquoteEnvValue undoes quoteEnv for the value part.
func unquoteEnvValue(v string) string {
	return strings.NewReplacer(`\\`, `\`, `\"`, `"`, `%%`, `%`, `\n`, "\n").Replace(v)
}

// InstalledHome implements Manager: the SMIND_HOME written into the unit.
func (s *systemd) InstalledHome(o Options) (string, bool) {
	o = s.scope(o)
	home, _, err := s.env.target(o.System)
	if err != nil {
		home = s.env.Home
	}
	data, err := os.ReadFile(s.unitPath(o.System, home))
	if err != nil {
		return "", false
	}
	unitHome := ""
	if m := reUnitHome.FindSubmatch(data); m != nil {
		unitHome = unquoteEnvValue(string(m[1]))
	}
	return serviceHome(unitHome, home), true
}

func (s *systemd) Status(ctx context.Context, o Options) (State, error) {
	env := s.env
	o = s.scope(o)
	home, _, err := env.target(o.System)
	if err != nil {
		home = env.Home
	}
	ok, props, err := s.installed(ctx, o.System, home)
	if err != nil {
		return NotInstalled, err
	}
	if !ok {
		env.outf("smind service: not installed (%s)", s.Name(o))
		return NotInstalled, nil
	}
	state := Stopped
	if props["ActiveState"] == "active" {
		state = Running
	}
	smindHome, _ := s.InstalledHome(o)
	port := env.port(smindHome)
	h := env.Probe(ctx, port)

	env.outf("smind service (%s)", s.Name(o))
	env.outf("  unit:    %s", s.unitPath(o.System, home))
	enabled := props["UnitFileState"]
	if enabled == "" {
		enabled = "unknown"
	}
	active := props["ActiveState"]
	if sub := props["SubState"]; sub != "" && sub != active {
		active += " (" + sub + ")"
	}
	pid := ""
	if state == Running && props["MainPID"] != "" && props["MainPID"] != "0" {
		pid = ", pid " + props["MainPID"]
	}
	env.outf("  state:   %s%s; enabled: %s", active, pid, enabled)
	env.outf("  home:    %s", smindHome)
	env.outf("  port:    %d", port)
	switch {
	case h.Smind:
		by := ""
		if h.ManagedBy != "" {
			by = ", managed by " + h.ManagedBy
		}
		env.outf("  daemon:  %s%s", versionNote(h.Version, env.Version), by)
	case h.Up:
		env.outf("  daemon:  port %d is held by something that is not smind", port)
	default:
		env.outf("  daemon:  not answering")
	}
	return state, nil
}

// requireInstalled returns ErrNotInstalled unless the unit exists, and the
// smind home the installed unit runs with.
func (s *systemd) requireInstalled(ctx context.Context, o Options) (smindHome string, err error) {
	home, _, terr := s.env.target(o.System)
	if terr != nil {
		home = s.env.Home
	}
	ok, _, err := s.installed(ctx, o.System, home)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNotInstalled
	}
	smindHome, _ = s.InstalledHome(o)
	return smindHome, nil
}

func (s *systemd) Start(ctx context.Context, o Options) error {
	o = s.scope(o)
	if err := s.needRoot(o, "starts"); err != nil {
		return err
	}
	smindHome, err := s.requireInstalled(ctx, o)
	if err != nil {
		return err
	}
	port := s.env.port(smindHome)
	if s.active(ctx, o.System) {
		s.env.outf("already running")
		return nil
	}
	if msg := s.env.foreignDaemonMessage(ctx, port); msg != "" {
		return fmt.Errorf("not starting: %s; stop it first", msg)
	}
	if err := s.ctlOK(ctx, o.System, "start", UnitName); err != nil {
		return err
	}
	if err := s.env.waitHealthy(ctx, port, "systemd"); err != nil {
		return err
	}
	s.env.outf("started")
	return nil
}

func (s *systemd) Stop(ctx context.Context, o Options) error {
	o = s.scope(o)
	if err := s.needRoot(o, "stops"); err != nil {
		return err
	}
	if _, err := s.requireInstalled(ctx, o); err != nil {
		return err
	}
	if err := s.ctlOK(ctx, o.System, "stop", UnitName); err != nil {
		return err
	}
	s.env.outf("stopped")
	return nil
}

func (s *systemd) Restart(ctx context.Context, o Options) error {
	o = s.scope(o)
	if err := s.needRoot(o, "restarts"); err != nil {
		return err
	}
	smindHome, err := s.requireInstalled(ctx, o)
	if err != nil {
		return err
	}
	port := s.env.port(smindHome)
	// A stopped unit with a foreign daemon on the port would crash on bind and
	// loop; refuse like Start does.
	if !s.active(ctx, o.System) {
		if msg := s.env.foreignDaemonMessage(ctx, port); msg != "" {
			return fmt.Errorf("not restarting: %s; stop it first", msg)
		}
	}
	if err := s.ctlOK(ctx, o.System, "restart", UnitName); err != nil {
		return err
	}
	if err := s.env.waitHealthy(ctx, port, "systemd"); err != nil {
		return err
	}
	s.env.outf("restarted")
	return nil
}
