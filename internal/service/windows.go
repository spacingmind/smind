package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/selfupdate"
)

// SupervisorTask is the SMIND_SUPERVISOR / /healthz managedBy value of a
// daemon started by the Windows scheduled task.
const SupervisorTask = "taskscheduler"

const (
	// TaskName is the scheduled task's name (it lives in the root folder).
	TaskName = "smind"
	// taskDefVersion is bumped when renderTaskXML changes in a way an existing
	// task must pick up even though its action did not change.
	taskDefVersion = "1"
)

// taskSched is the Windows backend: one per-user Scheduled Task that runs at
// logon, as the user, in their interactive session (ADR-0031 decision 2). It
// needs no elevation (verified on a standard, non-admin user in CI). The task
// starts at logon, not at boot. Not a Windows Service: that would run as another
// identity without the user's PATH, agent logins and profile.
type taskSched struct{ env Env }

func (t *taskSched) Name(Options) string { return "Task Scheduler (per-user logon task)" }

// taskSpec is the input of renderTaskXML.
type taskSpec struct {
	Exe  string // smind.exe, absolute
	Home string // SMIND_HOME passed as `service run --home`; "" omits it
	User string // DOMAIN\user the task belongs to and triggers for
	// Conhost is the absolute path of conhost.exe, used as `conhost.exe
	// --headless smind.exe ...` so the daemon has a console (children such as
	// git and the agent CLIs need one) but no window. "" launches smind.exe
	// directly, which shows a console window.
	Conhost string
}

// command and arguments are the task's single Exec action.
func (sp taskSpec) command() string {
	if sp.Conhost != "" {
		return sp.Conhost
	}
	return sp.Exe
}

func (sp taskSpec) arguments() string {
	var parts []string
	if sp.Conhost != "" {
		parts = append(parts, "--headless", winQuote(sp.Exe))
	}
	parts = append(parts, "service", "run")
	if sp.Home != "" {
		parts = append(parts, "--home", winQuote(sp.Home))
	}
	return strings.Join(parts, " ")
}

// defHash identifies the definition: an installed task whose description holds
// the same hash is up to date and is not re-registered. (schtasks normalises the
// XML it stores, so the stored text can not be compared with ours.)
func (sp taskSpec) defHash() string {
	h := sha256.Sum256([]byte(strings.Join([]string{sp.command(), sp.arguments(), sp.User, taskDefVersion}, "\x00")))
	return fmt.Sprintf("%x", h[:6])
}

// renderTaskXML is the Task Scheduler 1.4 definition. LogonTrigger for the user
// only, interactive token at the least privilege level, no execution time
// limit, one instance at a time, and restart-on-failure 3x at 1-minute intervals
// (the same policy `smind service run` applies itself when conhost hides the
// exit code; see cmd/smind/service_run.go).
func renderTaskXML(sp taskSpec) string {
	esc := func(s string) string { return html.EscapeString(s) }
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-16"?>` + "\n")
	b.WriteString(`<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">` + "\n")
	b.WriteString("  <RegistrationInfo>\n")
	fmt.Fprintf(&b, "    <Description>sMind daemon, started at logon by `smind service install` [def:%s]</Description>\n", sp.defHash())
	b.WriteString("  </RegistrationInfo>\n")
	b.WriteString("  <Triggers>\n    <LogonTrigger>\n      <Enabled>true</Enabled>\n")
	fmt.Fprintf(&b, "      <UserId>%s</UserId>\n", esc(sp.User))
	b.WriteString("    </LogonTrigger>\n  </Triggers>\n")
	b.WriteString(`  <Principals>` + "\n" + `    <Principal id="Author">` + "\n")
	fmt.Fprintf(&b, "      <UserId>%s</UserId>\n", esc(sp.User))
	b.WriteString("      <LogonType>InteractiveToken</LogonType>\n      <RunLevel>LeastPrivilege</RunLevel>\n")
	b.WriteString("    </Principal>\n  </Principals>\n")
	b.WriteString("  <Settings>\n")
	for _, line := range []string{
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
		"<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>",
		"<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>",
		"<AllowHardTerminate>true</AllowHardTerminate>",
		"<StartWhenAvailable>true</StartWhenAvailable>",
		"<AllowStartOnDemand>true</AllowStartOnDemand>",
		"<Enabled>true</Enabled>",
		"<Hidden>false</Hidden>",
		"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"<RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure>",
	} {
		b.WriteString("    " + line + "\n")
	}
	b.WriteString("  </Settings>\n")
	b.WriteString(`  <Actions Context="Author">` + "\n    <Exec>\n")
	fmt.Fprintf(&b, "      <Command>%s</Command>\n", esc(sp.command()))
	fmt.Fprintf(&b, "      <Arguments>%s</Arguments>\n", esc(sp.arguments()))
	b.WriteString("    </Exec>\n  </Actions>\n</Task>\n")
	return b.String()
}

// encodeTaskXML is the file schtasks /Create /XML reads: UTF-16 LE with a byte
// order mark. (A UTF-8 file is rejected: "unable to switch the encoding".)
func encodeTaskXML(xml string) []byte {
	units := utf16.Encode([]rune(xml))
	out := make([]byte, 0, 2+2*len(units))
	out = append(out, 0xFF, 0xFE)
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// winQuote quotes one argument for a Windows command line.
func winQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range s {
		switch r {
		case '\\':
			backslashes++
			b.WriteRune(r)
		case '"':
			b.WriteString(strings.Repeat(`\`, backslashes+1))
			b.WriteRune(r)
			backslashes = 0
		default:
			backslashes = 0
			b.WriteRune(r)
		}
	}
	b.WriteString(strings.Repeat(`\`, backslashes)) // double the backslashes before the closing quote
	b.WriteByte('"')
	return b.String()
}

// splitWinArgs is the inverse for the simple cases winQuote produces.
func splitWinArgs(s string) []string {
	var args []string
	var cur strings.Builder
	in, have := false, false
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(rs) && rs[i] == '\\' {
				n++
				i++
			}
			if i < len(rs) && rs[i] == '"' {
				cur.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					cur.WriteRune('"')
				} else {
					in = !in
				}
			} else {
				cur.WriteString(strings.Repeat(`\`, n))
				i--
			}
			have = true
		case c == '"':
			in = !in
			have = true
		case (c == ' ' || c == '\t') && !in:
			if have {
				args = append(args, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(c)
			have = true
		}
	}
	if have {
		args = append(args, cur.String())
	}
	return args
}

// task is what `schtasks /Query /XML` said about the installed task.
type task struct {
	home    string // the --home of its action; "" = the user's default
	enabled bool
	defHash string
	exe     string
	userID  string // the principal the task runs as (the stored XML holds the SID)
}

var (
	reTaskArgs    = regexp.MustCompile(`(?s)<Arguments>(.*?)</Arguments>`)
	reTaskCommand = regexp.MustCompile(`(?s)<Command>(.*?)</Command>`)
	reTaskDef     = regexp.MustCompile(`\[def:([0-9a-f]+)\]`)
	reTaskSetting = regexp.MustCompile(`(?s)<Settings>(.*?)</Settings>`)
	reEnabled     = regexp.MustCompile(`<Enabled>(\w+)</Enabled>`)
	rePrincipal   = regexp.MustCompile(`(?s)<Principal[^>]*>.*?<UserId>(.*?)</UserId>`)
)

func parseTask(xml string) task {
	var t task
	t.enabled = true
	if m := reTaskSetting.FindStringSubmatch(xml); m != nil {
		if e := reEnabled.FindStringSubmatch(m[1]); e != nil {
			t.enabled = strings.EqualFold(e[1], "true")
		}
	}
	if m := reTaskDef.FindStringSubmatch(xml); m != nil {
		t.defHash = m[1]
	}
	if m := rePrincipal.FindStringSubmatch(xml); m != nil {
		t.userID = html.UnescapeString(strings.TrimSpace(m[1]))
	}
	var args []string
	if m := reTaskArgs.FindStringSubmatch(xml); m != nil {
		args = splitWinArgs(html.UnescapeString(strings.TrimSpace(m[1])))
	}
	for i, a := range args {
		if a == "--home" && i+1 < len(args) {
			t.home = args[i+1]
		}
	}
	if m := reTaskCommand.FindStringSubmatch(xml); m != nil {
		cmd := html.UnescapeString(strings.TrimSpace(m[1]))
		if strings.EqualFold(filepath.Base(strings.ReplaceAll(cmd, `\`, "/")), "conhost.exe") && len(args) >= 2 {
			t.exe = args[1] // --headless <exe> service run ...
		} else {
			t.exe = cmd
		}
	}
	return t
}

// query runs `schtasks /Query /TN smind /XML`; installed is false when the task
// does not exist. (schtasks words that error in the user's language, so any
// non-zero exit is read as "no such task".)
func (t *taskSched) query(ctx context.Context) (tk task, installed bool, err error) {
	// Through cmd with code page 65001: schtasks writes to a pipe in the console's
	// OEM code page, which would garble a non-ASCII user name or --home path, and
	// the exe and home recovered from the XML drive stop, status and logs.
	res, err := t.env.Runner.Run(ctx, "cmd.exe", "/c", "chcp 65001 >nul & schtasks /Query /TN "+TaskName+" /XML")
	if err != nil {
		return task{}, false, fmt.Errorf("run schtasks: %w", err)
	}
	if res.ExitCode != 0 {
		return task{}, false, nil
	}
	return parseTask(res.Stdout), true, nil
}

func (t *taskSched) schtasksOK(ctx context.Context, args ...string) error {
	res, err := t.env.Runner.Run(ctx, "schtasks", args...)
	if err != nil {
		return fmt.Errorf("run schtasks: %w", err)
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return fmt.Errorf("schtasks %s: exit %d: %s", strings.Join(args, " "), res.ExitCode, msg)
	}
	return nil
}

func (t *taskSched) guard(o Options) error {
	if o.System {
		return errors.New("--system is only available on Linux (Windows uses a per-user scheduled task)")
	}
	return nil
}

// ours reports whether the daemon answering on the port was started by this
// service's task (it says so in /healthz managedBy).
func ours(h Health) bool { return h.Smind && h.ManagedBy == SupervisorTask }

func (t *taskSched) Install(ctx context.Context, o Options) error {
	env := t.env
	if err := t.guard(o); err != nil {
		return err
	}
	if o.Path != "" {
		return errors.New("--path does nothing on Windows: the task runs with your own logon environment, PATH included")
	}
	if why, owned := selfupdate.DesktopOwned(env.Exe, "windows", env.Getenv, func(p string) bool { _, err := os.Stat(p); return err == nil }); owned {
		return fmt.Errorf("not installing a service for this binary: %s; install smind with install.ps1 and run `smind service install` from there", why)
	}
	if strings.ContainsAny(env.Exe, "\"\n\r") || strings.ContainsAny(o.Home, "\"\n\r") {
		return errors.New("refusing a path containing a quote or newline")
	}
	smindHome, err := env.smindHome(o, env.Home)
	if err != nil {
		return err
	}
	env.warnTemporaryExe()

	sp := taskSpec{Exe: env.Exe, User: env.Username}
	if sp.User == "" {
		return errors.New("could not determine your user name for the logon trigger")
	}
	if o.Home != "" || env.Getenv("SMIND_HOME") != "" {
		sp.Home = smindHome
	}
	if c := t.conhostPath(); c != "" {
		sp.Conhost = c
	} else {
		env.warnf("conhost.exe was not found, so the daemon will show a console window; it is otherwise started with no window")
	}

	old, installed, err := t.query(ctx)
	if err != nil {
		return err
	}
	if installed {
		if err := t.checkOwner(old); err != nil {
			return err
		}
	}
	// A disabled task is re-registered too (which enables it): otherwise "up to
	// date" would hide a task that never runs.
	changed := !installed || old.defHash != sp.defHash() || !old.enabled
	if changed {
		if err := t.register(ctx, sp); err != nil {
			return err
		}
		env.outf("registered the scheduled task \\%s for %s (runs at logon, not at boot)", TaskName, sp.User)
	} else {
		env.outf("the scheduled task \\%s is up to date", TaskName)
	}

	port := env.port(smindHome)
	h := env.Probe(ctx, port)
	switch {
	case o.NoStart:
		env.outf("not starting (--no-start); start it with `smind service start`")
	case ours(h):
		switch {
		case changed && installed:
			env.outf("already running; run `smind service restart` to apply the new task")
		case env.Version != "" && h.Version != "" && h.Version != env.Version:
			env.outf("already running smind %s, but this binary is %s: run `smind service restart` to switch", h.Version, env.Version)
		default:
			env.outf("already running")
		}
	default:
		if msg := env.foreignDaemonMessage(ctx, port); msg != "" {
			env.warnf("NOT starting the service: %s. The task is installed; stop that daemon, then run `smind service start`.", msg)
			return nil
		}
		t.sweepStale(ctx, env.Exe, port)
		if err := t.schtasksOK(ctx, "/Run", "/TN", TaskName); err != nil {
			return err
		}
		if err := env.waitHealthy(ctx, port, SupervisorTask); err != nil {
			return t.sessionHint(err)
		}
		env.outf("started; smind is listening on http://127.0.0.1:%d", port)
	}
	return nil
}

// checkOwner refuses a task that belongs to another user: the name \smind is
// machine-wide, and this service is per-user.
func (t *taskSched) checkOwner(tk task) error {
	if tk.userID == "" || (t.env.UserSID == "" && t.env.Username == "") {
		return nil
	}
	for _, me := range []string{t.env.UserSID, t.env.Username} {
		if me != "" && strings.EqualFold(tk.userID, me) {
			return nil
		}
	}
	return fmt.Errorf("the scheduled task \\%s belongs to another user (%s): this service is per-user, so remove it from that account first", TaskName, tk.userID)
}

// sessionHint explains the usual reason a started task never answers.
func (t *taskSched) sessionHint(err error) error {
	return fmt.Errorf("%w; the task runs only in your own interactive Windows session (signed in at the console or over RDP), not over ssh or from a service", err)
}

// sweepStale ends supervisors left over from an earlier run (one waiting out its
// restart delay after a crash would otherwise keep the new task instance from
// starting: the task runs one instance at a time). Best effort.
func (t *taskSched) sweepStale(ctx context.Context, exe string, port int) {
	if t.env.ForceStop == nil {
		return
	}
	if _, err := t.env.ForceStop(ctx, exe, port, KillNone, 0); err != nil {
		t.env.warnf("could not check for a stale supervisor (%v); continuing", err)
	}
}

// register writes the task XML to a temp file and imports it, replacing any
// previous definition. Registering a logon task for your own account needs no
// elevation; when policy denies it the error says which step was refused.
func (t *taskSched) register(ctx context.Context, sp taskSpec) error {
	f, err := os.CreateTemp("", "smind-task-*.xml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(encodeTaskXML(renderTaskXML(sp))); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	res, err := t.env.Runner.Run(ctx, "schtasks", "/Create", "/TN", TaskName, "/XML", f.Name(), "/F")
	if err != nil {
		return fmt.Errorf("run schtasks: %w", err)
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr + " " + res.Stdout)
		if strings.Contains(strings.ToLower(msg), "access is denied") || strings.Contains(msg, "0x80070005") {
			return fmt.Errorf("Windows refused `schtasks /Create` (creating the logon task for your own account): %s. This normally needs no administrator rights, so a policy on this machine is restricting scheduled tasks; ask an administrator, or run `smind serve` yourself", msg)
		}
		return fmt.Errorf("schtasks /Create: exit %d: %s", res.ExitCode, msg)
	}
	return nil
}

func (t *taskSched) conhostPath() string {
	root := t.env.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	p := filepath.Join(root, "System32", "conhost.exe")
	if t.env.FileExists != nil && !t.env.FileExists(p) {
		return ""
	}
	return p
}

func (t *taskSched) Uninstall(ctx context.Context, o Options) error {
	env := t.env
	if err := t.guard(o); err != nil {
		return err
	}
	tk, installed, err := t.query(ctx)
	if err != nil {
		return err
	}
	if !installed {
		env.outf("not installed (no scheduled task \\%s); nothing to do", TaskName)
		return nil
	}
	if err := t.checkOwner(tk); err != nil {
		return err
	}
	// strict=false: a daemon on the port that this task did not start is left
	// alone (with a note) rather than blocking the removal of the task. One this
	// task did start, and cannot stop, aborts: nothing would manage it afterwards.
	if err := t.stop(ctx, serviceHome(tk.home, env.Home), tk.exe, false); err != nil {
		return err
	}
	if err := t.schtasksOK(ctx, "/Delete", "/TN", TaskName, "/F"); err != nil {
		return err
	}
	env.outf("stopped and removed the scheduled task \\%s; your data in the smind home directory is untouched", TaskName)
	return nil
}

// InstalledHome implements Manager: the --home in the task's action.
func (t *taskSched) InstalledHome(Options) (string, bool) {
	tk, installed, err := t.query(context.Background())
	if err != nil || !installed {
		return "", false
	}
	return serviceHome(tk.home, t.env.Home), true
}

func (t *taskSched) Status(ctx context.Context, o Options) (State, error) {
	env := t.env
	if err := t.guard(o); err != nil {
		return NotInstalled, err
	}
	tk, installed, err := t.query(ctx)
	if err != nil {
		return NotInstalled, err
	}
	if !installed {
		env.outf("smind service: not installed (%s)", t.Name(o))
		return NotInstalled, nil
	}
	smindHome := serviceHome(tk.home, env.Home)
	port := env.port(smindHome)
	h := env.Probe(ctx, port)
	state := Stopped
	if ours(h) {
		state = Running
	}

	env.outf("smind service (%s)", t.Name(o))
	enabled := "enabled"
	if !tk.enabled {
		enabled = "DISABLED"
	}
	env.outf("  task:    \\%s (%s); runs at logon of %s, not at boot", TaskName, enabled, env.Username)
	run := "stopped"
	if state == Running {
		run = "running"
	}
	env.outf("  state:   %s", run)
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

func (t *taskSched) requireInstalled(ctx context.Context, o Options) (task, error) {
	if err := t.guard(o); err != nil {
		return task{}, err
	}
	tk, installed, err := t.query(ctx)
	if err != nil {
		return task{}, err
	}
	if !installed {
		return task{}, ErrNotInstalled
	}
	if err := t.checkOwner(tk); err != nil {
		return task{}, err
	}
	return tk, nil
}

func (t *taskSched) Start(ctx context.Context, o Options) error {
	tk, err := t.requireInstalled(ctx, o)
	if err != nil {
		return err
	}
	port := t.env.port(serviceHome(tk.home, t.env.Home))
	h := t.env.Probe(ctx, port)
	if ours(h) {
		t.env.outf("already running")
		return nil
	}
	if msg := t.env.foreignDaemonMessage(ctx, port); msg != "" {
		return fmt.Errorf("not starting: %s; stop it first", msg)
	}
	t.sweepStale(ctx, tk.exe, port)
	if err := t.schtasksOK(ctx, "/Run", "/TN", TaskName); err != nil {
		return err
	}
	if err := t.env.waitHealthy(ctx, port, SupervisorTask); err != nil {
		return t.sessionHint(err)
	}
	t.env.outf("started")
	return nil
}

func (t *taskSched) Stop(ctx context.Context, o Options) error {
	tk, err := t.requireInstalled(ctx, o)
	if err != nil {
		return err
	}
	if err := t.stop(ctx, serviceHome(tk.home, t.env.Home), tk.exe, true); err != nil {
		return err
	}
	t.env.outf("stopped")
	return nil
}

func (t *taskSched) Restart(ctx context.Context, o Options) error {
	tk, err := t.requireInstalled(ctx, o)
	if err != nil {
		return err
	}
	smindHome := serviceHome(tk.home, t.env.Home)
	if err := t.stop(ctx, smindHome, tk.exe, true); err != nil {
		return err
	}
	port := t.env.port(smindHome)
	if msg := t.env.foreignDaemonMessage(ctx, port); msg != "" {
		return fmt.Errorf("not restarting: %s; stop it first", msg)
	}
	if err := t.schtasksOK(ctx, "/Run", "/TN", TaskName); err != nil {
		return err
	}
	if err := t.env.waitHealthy(ctx, port, SupervisorTask); err != nil {
		return t.sessionHint(err)
	}
	t.env.outf("restarted")
	return nil
}

// KillMode says which process listening on the daemon's port ForceStop may kill.
type KillMode string

// The kill modes of Env.ForceStop.
const (
	// KillNone kills no listener: only this install's supervisors.
	KillNone KillMode = "none"
	// KillOurs kills the listener if it is this install's smind.exe (the daemon
	// answered as managed by this task and ignored the shutdown request).
	KillOurs KillMode = "ours"
	// KillOwned kills the listener only if it is this install's smind.exe AND a
	// child of one of its supervisors (a daemon of ours that hangs and answers
	// nothing, as opposed to a foreign one on the same port).
	KillOwned KillMode = "owned"
)

// stop stops the daemon this task started, gracefully first: POST
// /api/shutdown (ADR-0029) with the home's token, then wait for the port to
// free. Only if it does not go down in time is the task ended and the leftover
// processes killed (`schtasks /End` ends only the task's own process, conhost,
// and leaves the daemon running). A daemon on the port that this task did not
// start is never touched: with strict (stop, restart) that is an error, without
// it (uninstall) a note, so the task can still be removed.
func (t *taskSched) stop(ctx context.Context, smindHome, exe string, strict bool) error {
	env := t.env
	port := env.port(smindHome)
	h := env.Probe(ctx, port)
	mode := KillNone
	graceful := false
	switch {
	case ours(h):
		if err := env.shutdown(ctx, port, smindHome); err != nil {
			env.warnf("could not ask the daemon to shut down (%v); ending the task instead", err)
		} else if err := env.waitDown(ctx, port); err == nil {
			graceful = true
		} else {
			env.warnf("the daemon did not stop within %s; ending the task and killing what is left", env.StopTimeout)
		}
		if !graceful {
			mode = KillOurs
		}
	case h.Up:
		// Something else answers (or our own daemon is hung and answers nothing
		// useful): only a listener that is a child of this install's supervisor
		// may be killed; a foreign daemon never is.
		mode = KillOwned
	}
	// Whatever the outcome, no instance of the task or its supervisor may stay
	// behind (a daemon that crashed leaves `service run` waiting to restart it).
	_ = t.schtasksOK(ctx, "/End", "/TN", TaskName)
	if env.ForceStop != nil {
		// After a graceful stop the daemon is still finishing its shutdown (killing
		// setups and terminals, closing the store): give the supervisor time to
		// exit by itself before sweeping it.
		wait := time.Duration(0)
		if graceful {
			wait = 10 * time.Second
		}
		if _, err := env.ForceStop(ctx, exe, port, mode, wait); err != nil {
			if !graceful {
				return fmt.Errorf("stop the leftover processes: %w", err)
			}
			env.warnf("the daemon stopped, but sweeping leftover processes failed (%v)", err)
		}
	}
	after := env.Probe(ctx, port)
	switch {
	case ours(after):
		return fmt.Errorf("the daemon is still answering on port %d", port)
	case after.Up && strict:
		return fmt.Errorf("not stopping: %s. It was not started by this service, so it is left alone", env.foreignDaemonMessage(ctx, port))
	case after.Up:
		env.warnf("%s. It was not started by this service, so it is left alone", env.foreignDaemonMessage(ctx, port))
	}
	return nil
}

// shutdown asks the daemon to stop via POST /api/shutdown.
func (e Env) shutdown(ctx context.Context, port int, home string) error {
	if e.Shutdown != nil {
		return e.Shutdown(ctx, port, home)
	}
	return httpShutdown(ctx, port, home)
}

// httpShutdown is the real POST /api/shutdown with the home's bearer token.
func httpShutdown(ctx context.Context, port int, home string) error {
	raw, err := os.ReadFile(auth.TokenPath(home))
	if err != nil {
		return fmt.Errorf("read the daemon token: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+strconv.Itoa(port)+"/api/shutdown", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(raw)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("the daemon answered %s", resp.Status)
	}
	return nil
}

// forceStopScript kills the leftover processes of this install: every
// `smind.exe service run` supervisor whose command line names exe and that
// belongs to the current user (killing it takes its job, the daemon, with it),
// after waiting up to $wait seconds for them to exit on their own; then, by $mode,
// the process listening on the port. It matches on the command line, not the image
// path, so it still finds a supervisor whose exe was renamed aside by an update. It
// never touches another smind.exe: the desktop app's daemon is `smind.exe serve`
// from another path, and a hand-started daemon has no supervisor as its parent.
const forceStopScript = `$ErrorActionPreference = 'SilentlyContinue'
$exe = '%s'
$port = %d
$mode = '%s'
$wait = %d
$me = $env:USERNAME
$pat = [regex]::Escape($exe)
function Get-Supervisors {
  Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -and $_.CommandLine -match $pat -and $_.CommandLine -match ' service run( |$)' } |
    Where-Object { (Invoke-CimMethod -InputObject $_ -MethodName GetOwner).User -eq $me }
}
$deadline = (Get-Date).AddSeconds($wait)
while ($wait -gt 0 -and @(Get-Supervisors).Count -gt 0 -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 300 }
$sup = @(Get-Supervisors)
$supPids = @($sup | ForEach-Object { $_.ProcessId })
$killed = @()
foreach ($s in $sup) { & taskkill.exe /T /F /PID $s.ProcessId | Out-Null; $killed += $s.ProcessId }
if ($mode -ne 'none') {
  $c = Get-NetTCPConnection -LocalPort $port -State Listen | Select-Object -First 1
  if ($c) {
    $p = Get-CimInstance Win32_Process -Filter "ProcessId=$($c.OwningProcess)"
    if ($p) {
      $mine = ($p.CommandLine -and $p.CommandLine -match $pat) -or ($p.ExecutablePath -eq $exe)
      $ok = $false
      if ($mode -eq 'ours') { $ok = $mine }
      if ($mode -eq 'owned') { $ok = $mine -and ($supPids -contains $p.ParentProcessId) }
      if ($ok) { & taskkill.exe /T /F /PID $p.ProcessId | Out-Null; $killed += $p.ProcessId }
    }
  }
}
$killed -join ','
`

// psQuote escapes s for a PowerShell single-quoted string: PowerShell treats the
// typographic single quotes (U+2018 to U+201B) as delimiters too.
func psQuote(s string) string {
	for _, q := range []string{"'", "\u2018", "\u2019", "\u201A", "\u201B"} {
		s = strings.ReplaceAll(s, q, q+q)
	}
	return s
}

// powershellForceStop is the default Env.ForceStop.
func powershellForceStop(r Runner) func(ctx context.Context, exe string, port int, mode KillMode, wait time.Duration) ([]int, error) {
	return func(ctx context.Context, exe string, port int, mode KillMode, wait time.Duration) ([]int, error) {
		script := fmt.Sprintf(forceStopScript, psQuote(exe), port, string(mode), int(wait.Seconds()))
		res, err := r.Run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
		if err != nil {
			return nil, err
		}
		if res.ExitCode != 0 {
			return nil, fmt.Errorf("powershell exit %d: %s", res.ExitCode, strings.TrimSpace(res.Stderr))
		}
		var pids []int
		for _, f := range strings.Split(strings.TrimSpace(res.Stdout), ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil {
				pids = append(pids, n)
			}
		}
		return pids, nil
	}
}
