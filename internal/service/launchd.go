package service

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spacingmind/smind/internal/config"
)

// launchd is the macOS backend: a per-user LaunchAgent in the gui/<uid>
// domain (ADR-0031 decision 3; no LaunchDaemon in v1). It needs a logged-in
// GUI session to load.
type launchd struct{ env Env }

func (l *launchd) Name(Options) string { return "launchd (LaunchAgent)" }

// plistSpec is the input of renderPlist.
type plistSpec struct {
	Exe  string
	Path string
	Home string // SMIND_HOME; "" omits the variable
	Work string // WorkingDirectory (the user's home)
}

// renderPlist is the LaunchAgent plist. KeepAlive restarts the daemon after a
// crash but not after a clean exit, so `smind stop` leaves it stopped.
func renderPlist(sp plistSpec) string {
	esc := func(s string) string {
		var b strings.Builder
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Written by ` + "`smind service install`" + `; the next install overwrites edits. -->
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + LaunchdLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + esc(sp.Exe) + `</string>
		<string>serve</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>WorkingDirectory</key>
	<string>` + esc(sp.Work) + `</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>` + esc(sp.Path) + `</string>
`)
	if sp.Home != "" {
		b.WriteString("\t\t<key>SMIND_HOME</key>\n\t\t<string>" + esc(sp.Home) + "</string>\n")
	}
	b.WriteString("\t\t<key>" + SupervisorEnv + "</key>\n\t\t<string>launchd</string>\n")
	b.WriteString(`	</dict>
</dict>
</plist>
`)
	return b.String()
}

func (l *launchd) plistPath() string {
	return filepath.Join(l.env.Home, "Library", "LaunchAgents", LaunchdLabel+".plist")
}

func (l *launchd) domain() string  { return "gui/" + strconv.Itoa(l.env.UID) }
func (l *launchd) service() string { return l.domain() + "/" + LaunchdLabel }

func (l *launchd) ctl(ctx context.Context, args ...string) (Result, error) {
	res, err := l.env.Runner.Run(ctx, "launchctl", args...)
	if err != nil {
		return res, fmt.Errorf("run launchctl: %w", err)
	}
	return res, nil
}

func (l *launchd) ctlOK(ctx context.Context, args ...string) error {
	res, err := l.ctl(ctx, args...)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		hint := ""
		if args[0] == "bootstrap" || args[0] == "kickstart" {
			hint = fmt.Sprintf("\nA LaunchAgent loads into the %s domain, which exists only for a logged-in GUI session of this user (not over plain ssh on a machine nobody is logged into). The plist is installed and will load at the next login.", l.domain())
		}
		return fmt.Errorf("launchctl %s: exit %d: %s%s", strings.Join(args, " "), res.ExitCode, msg, hint)
	}
	return nil
}

var (
	rePID   = regexp.MustCompile(`(?m)^\s*pid = (\d+)\s*$`)
	reState = regexp.MustCompile(`(?m)^\s*state = (.+?)\s*$`)
	reHome  = regexp.MustCompile(`<key>SMIND_HOME</key>\s*<string>([^<]*)</string>`)
)

// job is what `launchctl print` said about the service.
type job struct {
	loaded  bool
	running bool
	pid     string
	state   string
}

func (l *launchd) job(ctx context.Context) (job, error) {
	res, err := l.ctl(ctx, "print", l.service())
	if err != nil {
		return job{}, err
	}
	if res.ExitCode != 0 {
		return job{}, nil // not loaded
	}
	j := job{loaded: true}
	if m := reState.FindStringSubmatch(res.Stdout); m != nil {
		j.state = m[1]
	}
	if m := rePID.FindStringSubmatch(res.Stdout); m != nil {
		j.pid = m[1]
	}
	j.running = j.state == "running" || j.pid != ""
	return j, nil
}

// parseLaunchdDisabled reads `launchctl print-disabled` output and returns
// launchd's override for LaunchdLabel: "enabled", "disabled", or "" when the
// label is absent or the output unrecognisable (newer macOS prints
// `=> disabled`, older prints `=> true` meaning disabled).
func parseLaunchdDisabled(out string) string {
	re := regexp.MustCompile(`(?m)^\s*"` + regexp.QuoteMeta(LaunchdLabel) + `"\s*=>\s*(\w+)\s*$`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	switch m[1] {
	case "disabled", "true":
		return "disabled"
	case "enabled", "false":
		return "enabled"
	}
	return ""
}

// enabledState is launchd's override for our label: "enabled", "disabled", or "" when it could not be read.
func (l *launchd) enabledState(ctx context.Context) string {
	res, err := l.ctl(ctx, "print-disabled", l.domain())
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	if state := parseLaunchdDisabled(res.Stdout); state != "" {
		return state
	}
	return "enabled" // no override listed for our label
}

func (l *launchd) rejectSystem(o Options) error {
	if o.System {
		return errors.New("--system is only available on Linux (macOS uses a per-user LaunchAgent)")
	}
	return nil
}

// guard is the check every acting command starts with: no --system, and not
// root (a LaunchAgent belongs to the logged-in user; as root the gui/<uid>
// domain is the wrong one and the plist would be root-owned in their home).
func (l *launchd) guard(o Options) error {
	if err := l.rejectSystem(o); err != nil {
		return err
	}
	if l.env.Euid == 0 {
		return errors.New("a LaunchAgent belongs to your own account: run `smind service` as yourself, without sudo")
	}
	return nil
}

// InstalledHome implements Manager: the SMIND_HOME written into the plist.
func (l *launchd) InstalledHome(Options) (string, bool) {
	data, err := os.ReadFile(l.plistPath())
	if err != nil {
		return "", false
	}
	unitHome := ""
	if m := reHome.FindSubmatch(data); m != nil {
		unitHome = html.UnescapeString(string(m[1]))
	}
	return serviceHome(unitHome, l.env.Home), true
}

func (l *launchd) Install(ctx context.Context, o Options) error {
	env := l.env
	if err := l.guard(o); err != nil {
		return err
	}
	smindHome, err := env.smindHome(o, env.Home)
	if err != nil {
		return err
	}
	env.warnTemporaryExe()
	sp := plistSpec{Exe: env.Exe, Path: env.capturePath(o), Work: env.Home}
	if o.Home != "" || env.Getenv("SMIND_HOME") != "" {
		sp.Home = smindHome
	}
	path := l.plistPath()
	changed, err := writeIfChanged(path, []byte(renderPlist(sp)), 0o644)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if changed {
		env.outf("wrote %s", path)
	} else {
		env.outf("%s is up to date", path)
	}
	// Clear a stale `launchctl disable`, so the agent loads at login.
	_, _ = l.ctl(ctx, "enable", l.service())
	env.outf("enabled %s (loads at every login of this user)", LaunchdLabel)

	port := env.port(smindHome)
	j, err := l.job(ctx)
	if err != nil {
		return err
	}
	switch {
	case o.NoStart:
		env.outf("not starting (--no-start); start it with `smind service start`")
	case j.running:
		switch h := env.Probe(ctx, port); {
		case changed:
			env.outf("already running; run `smind service restart` to apply the new plist")
		case h.Smind && env.Version != "" && h.Version != "" && h.Version != env.Version:
			env.outf("already running smind %s, but this binary is %s: run `smind service restart` to switch", h.Version, env.Version)
		default:
			env.outf("already running")
		}
	default:
		if msg := env.foreignDaemonMessage(ctx, port); msg != "" {
			env.warnf("NOT starting the service: %s. The agent is installed and enabled; stop that daemon, then run `smind service start`.", msg)
			return nil
		}
		if err := l.load(ctx, j); err != nil {
			return err
		}
		if err := env.waitHealthy(ctx, port, "launchd"); err != nil {
			return err
		}
		env.outf("started; smind is listening on http://127.0.0.1:%d", port)
	}
	return nil
}

// load starts the job. A job that is loaded but not running (after a graceful
// `smind stop`) is unloaded first: kickstart would run the OLD definition,
// while bootstrap re-reads the plist this install may just have changed.
func (l *launchd) load(ctx context.Context, j job) error {
	if j.loaded {
		if err := l.ctlOK(ctx, "bootout", l.service()); err != nil {
			return err
		}
	}
	return l.ctlOK(ctx, "bootstrap", l.domain(), l.plistPath())
}

func (l *launchd) Uninstall(ctx context.Context, o Options) error {
	env := l.env
	if err := l.guard(o); err != nil {
		return err
	}
	path := l.plistPath()
	_, statErr := os.Stat(path)
	j, err := l.job(ctx)
	if err != nil {
		return err
	}
	if errors.Is(statErr, os.ErrNotExist) && !j.loaded {
		env.outf("not installed (%s does not exist); nothing to do", path)
		return nil
	}
	if j.loaded {
		// bootout stops the job (SIGTERM, then SIGKILL after ExitTimeOut). If it
		// fails, keep the plist: removing it would orphan a running agent.
		if err := l.ctlOK(ctx, "bootout", l.service()); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	env.outf("stopped and removed %s; your data in the smind home directory is untouched", path)
	return nil
}

func (l *launchd) Status(ctx context.Context, o Options) (State, error) {
	env := l.env
	if err := l.rejectSystem(o); err != nil {
		return NotInstalled, err
	}
	_, statErr := os.Stat(l.plistPath())
	j, err := l.job(ctx)
	if err != nil {
		return NotInstalled, err
	}
	if statErr != nil && !j.loaded {
		env.outf("smind service: not installed (%s)", l.Name(o))
		return NotInstalled, nil
	}
	state := Stopped
	if j.running {
		state = Running
	}
	smindHome, _ := l.InstalledHome(o)
	if smindHome == "" {
		smindHome = config.DirFor(env.Home)
	}
	port := env.port(smindHome)
	h := env.Probe(ctx, port)

	env.outf("smind service (%s)", l.Name(o))
	env.outf("  plist:   %s", l.plistPath())
	loaded := "not loaded"
	if j.loaded {
		loaded = "loaded"
		if j.state != "" {
			loaded += ", " + j.state
		}
		if j.pid != "" {
			loaded += ", pid " + j.pid
		}
	}
	env.outf("  state:   %s", loaded)
	switch l.enabledState(ctx) {
	case "enabled":
		env.outf("  enabled: yes (loads at every login of this user)")
	case "disabled":
		env.outf("  enabled: NO (disabled with launchctl; `smind service install` enables it again)")
	default:
		env.outf("  enabled: unknown (could not read launchctl print-disabled)")
	}
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

// requireInstalled returns the home of the installed agent, or ErrNotInstalled.
func (l *launchd) requireInstalled(o Options) (string, error) {
	if err := l.guard(o); err != nil {
		return "", err
	}
	h, ok := l.InstalledHome(o)
	if !ok {
		return "", ErrNotInstalled
	}
	return h, nil
}

func (l *launchd) Start(ctx context.Context, o Options) error {
	smindHome, err := l.requireInstalled(o)
	if err != nil {
		return err
	}
	j, err := l.job(ctx)
	if err != nil {
		return err
	}
	if j.running {
		l.env.outf("already running")
		return nil
	}
	port := l.env.port(smindHome)
	if msg := l.env.foreignDaemonMessage(ctx, port); msg != "" {
		return fmt.Errorf("not starting: %s; stop it first", msg)
	}
	if err := l.load(ctx, j); err != nil {
		return err
	}
	if err := l.env.waitHealthy(ctx, port, "launchd"); err != nil {
		return err
	}
	l.env.outf("started")
	return nil
}

func (l *launchd) Stop(ctx context.Context, o Options) error {
	if _, err := l.requireInstalled(o); err != nil {
		return err
	}
	j, err := l.job(ctx)
	if err != nil {
		return err
	}
	if !j.loaded {
		l.env.outf("already stopped")
		return nil
	}
	if err := l.ctlOK(ctx, "bootout", l.service()); err != nil {
		return err
	}
	l.env.outf("stopped (it loads again at your next login; `smind service uninstall` removes it)")
	return nil
}

// Restart unloads and reloads the job, which (unlike kickstart -k) re-reads
// the plist, then waits for the port to be released in between so the new
// daemon does not race the old one for it.
func (l *launchd) Restart(ctx context.Context, o Options) error {
	smindHome, err := l.requireInstalled(o)
	if err != nil {
		return err
	}
	port := l.env.port(smindHome)
	j, err := l.job(ctx)
	if err != nil {
		return err
	}
	if j.loaded {
		if err := l.ctlOK(ctx, "bootout", l.service()); err != nil {
			return err
		}
		if err := l.env.waitDown(ctx, port); err != nil {
			return err
		}
	} else if msg := l.env.foreignDaemonMessage(ctx, port); msg != "" {
		// Not loaded and something else answers: bootstrap would crash-loop.
		return fmt.Errorf("not restarting: %s; stop it first", msg)
	}
	if err := l.ctlOK(ctx, "bootstrap", l.domain(), l.plistPath()); err != nil {
		return err
	}
	if err := l.env.waitHealthy(ctx, port, "launchd"); err != nil {
		return err
	}
	l.env.outf("restarted")
	return nil
}
