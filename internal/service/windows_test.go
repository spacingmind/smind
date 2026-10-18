package service

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ------------------------------------------------------------- definition

func TestWindowsTaskXMLGolden(t *testing.T) {
	sp := taskSpec{
		Exe: `C:\Users\alice\AppData\Local\Programs\smind\smind.exe`, User: `WORKGROUP\alice`,
		Conhost: `C:\Windows\System32\conhost.exe`,
	}
	got := renderTaskXML(sp)
	golden(t, "windows-task.xml.golden", got)
	for _, want := range []string{
		"<LogonTrigger>", `<UserId>WORKGROUP\alice</UserId>`, "<LogonType>InteractiveToken</LogonType>", "<RunLevel>LeastPrivilege</RunLevel>",
		"<RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure>", "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
		`<Command>C:\Windows\System32\conhost.exe</Command>`,
		`<Arguments>--headless C:\Users\alice\AppData\Local\Programs\smind\smind.exe service run</Arguments>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("definition lacks %q", want)
		}
	}
	for _, banned := range []string{"RunLevel>HighestAvailable", "BootTrigger", "S4U", "<LogonType>Password"} {
		if strings.Contains(got, banned) {
			t.Errorf("definition must not contain %q (per-user, interactive, no elevation)", banned)
		}
	}
	golden(t, "windows-task-home.xml.golden", renderTaskXML(taskSpec{
		Exe: `C:\Program Files\smind\smind.exe`, User: `CORP\a.b`, Home: `D:\data\my smind`, Conhost: `C:\Windows\System32\conhost.exe`,
	}))
	golden(t, "windows-task-noconhost.xml.golden", renderTaskXML(taskSpec{Exe: `C:\smind\smind.exe`, User: `PC\u`}))
}

func TestWindowsTaskXMLIsWellFormedAndEscaped(t *testing.T) {
	x := renderTaskXML(taskSpec{Exe: `C:\R&D <x>\smind.exe`, User: `PC\o'brien`, Home: `D:\a&b`, Conhost: `C:\Windows\System32\conhost.exe`})
	if err := xml.Unmarshal([]byte(strings.Replace(x, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)), new(struct{})); err != nil {
		t.Fatalf("not well-formed: %v\n%s", err, x)
	}
	if !strings.Contains(x, `R&amp;D &lt;x&gt;`) {
		t.Errorf("path not escaped:\n%s", x)
	}
	// What schtasks reads back round-trips through our parser.
	tk := parseTask(x)
	if tk.exe != `C:\R&D <x>\smind.exe` || tk.home != `D:\a&b` {
		t.Errorf("parseTask = %+v", tk)
	}
}

func TestWindowsTaskFileIsUTF16WithBOM(t *testing.T) {
	// schtasks /Create /XML rejects UTF-8 ("unable to switch the encoding"): measured on windows-latest.
	b := encodeTaskXML("<a>é€</a>")
	if b[0] != 0xFF || b[1] != 0xFE {
		t.Fatalf("no BOM: % x", b[:4])
	}
	got, ok := decodeUTF16(b)
	if !ok || got != "<a>é€</a>" {
		t.Errorf("round trip = %q %v", got, ok)
	}
}

func TestWindowsDefinitionHashChangesWithTheAction(t *testing.T) {
	base := taskSpec{Exe: `C:\a\smind.exe`, User: `PC\u`, Conhost: `C:\Windows\System32\conhost.exe`}
	h := base.defHash()
	for name, sp := range map[string]taskSpec{
		"exe":     {Exe: `C:\b\smind.exe`, User: base.User, Conhost: base.Conhost},
		"home":    {Exe: base.Exe, User: base.User, Conhost: base.Conhost, Home: `D:\x`},
		"user":    {Exe: base.Exe, User: `PC\v`, Conhost: base.Conhost},
		"conhost": {Exe: base.Exe, User: base.User},
	} {
		if sp.defHash() == h {
			t.Errorf("changing the %s must change the definition hash", name)
		}
	}
	if base.defHash() != h {
		t.Error("hash must be stable")
	}
}

func TestWinQuoteRoundTrip(t *testing.T) {
	for _, s := range []string{`C:\a\smind.exe`, `C:\Program Files\smind\smind.exe`, `D:\dir with space\`, `a"b`, `C:\x\"y`, ``, `\\server\share\smind`} {
		got := splitWinArgs(winQuote(s) + " next")
		if len(got) != 2 || got[0] != s || got[1] != "next" {
			t.Errorf("%q -> %q -> %q", s, winQuote(s), got)
		}
	}
	if got := splitWinArgs(`--headless "C:\Program Files\x.exe" service run --home "D:\my smind"`); strings.Join(got, "|") != `--headless|C:\Program Files\x.exe|service|run|--home|D:\my smind` {
		t.Errorf("split = %q", got)
	}
}

// ---------------------------------------------------------------- install

func TestWindowsInstallRegistersTheTaskAndStarts(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	if err := m.Install(bg, Options{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if fs.called("schtasks /Create /TN smind /XML") != 1 || !strings.HasSuffix(fs.calls[1], "/F") {
		t.Errorf("calls: %v", fs.calls)
	}
	if fs.called("schtasks /Run /TN smind") != 1 {
		t.Errorf("calls: %v", fs.calls)
	}
	if !strings.Contains(fs.xml, "service run") || !strings.Contains(fs.xml, `WORKGROUP\alice`) {
		t.Errorf("registered definition:\n%s", fs.xml)
	}
	out := te.out.String()
	if !strings.Contains(out, "runs at logon, not at boot") || !strings.Contains(out, "started; smind is listening") {
		t.Errorf("output = %q", out)
	}
	if h := te.daemon.probe(bg, 0); h.ManagedBy != SupervisorTask {
		t.Errorf("the daemon must report managedBy=%s, got %q", SupervisorTask, h.ManagedBy)
	}
}

func TestWindowsInstallNeedsNoElevationPrompt(t *testing.T) {
	// Registration is an unprivileged schtasks /Create of a per-user task: no
	// runas, no "highest" run level, no system-wide trigger.
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	for _, c := range fs.calls {
		if strings.Contains(c, "/RU") || strings.Contains(c, "/RL") || strings.Contains(strings.ToLower(c), "system") {
			t.Errorf("schtasks was asked for an elevated identity: %s", c)
		}
	}
}

func TestServiceInstallIdempotentWindows(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	if err := m.Install(bg, Options{}); err != nil {
		t.Fatal(err)
	}
	te.resetOutput()
	if err := m.Install(bg, Options{}); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if fs.called("schtasks /Create") != 1 || fs.called("schtasks /Run") != 1 {
		t.Errorf("a second identical install must not re-register or start: %v", fs.calls)
	}
	if !strings.Contains(te.out.String(), "up to date") || !strings.Contains(te.out.String(), "already running") {
		t.Errorf("output = %q", te.out.String())
	}
}

func TestWindowsInstallChangedDefinitionReRegistersWithoutBouncingTheDaemon(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	_ = m.Install(bg, Options{})
	te.resetOutput()
	if err := m.Install(bg, Options{Home: `D:\srv\smind`}); err != nil {
		t.Fatal(err)
	}
	if fs.called("schtasks /Create") != 2 {
		t.Errorf("a changed definition must be re-registered: %v", fs.calls)
	}
	if fs.called("schtasks /Run") != 1 || fs.called("schtasks /End") != 0 || fs.order.index("shutdown") != -1 {
		t.Errorf("install must never stop or restart a running daemon: %v %v", fs.calls, fs.order.items)
	}
	for _, m := range hooks.modes {
		if m != KillNone {
			t.Errorf("install must never kill a listener: %v", hooks.modes)
		}
	}
	if !strings.Contains(te.out.String(), "smind service restart") {
		t.Errorf("output = %q", te.out.String())
	}
}

func TestWindowsInstallRefusesToStartWhenForeignDaemonAnswers(t *testing.T) {
	cases := map[string]func(d *fakeDaemon){
		"hand-started smind": func(d *fakeDaemon) { d.set(true, true, "0.9.0", "") },
		"desktop app daemon": func(d *fakeDaemon) { d.set(true, true, "0.9.0", "") },
		"another service":    func(d *fakeDaemon) { d.set(true, true, "0.9.0", "systemd") },
		"not smind at all":   func(d *fakeDaemon) { d.set(true, false, "", "") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			te := newTestEnv(t, "windows")
			setup(te.daemon)
			m, fs, hooks := te.windows(t)
			if err := m.Install(bg, Options{}); err != nil {
				t.Fatalf("install must succeed (registered, not started): %v", err)
			}
			if fs.called("schtasks /Create") != 1 {
				t.Errorf("the task must still be registered: %v", fs.calls)
			}
			if fs.called("schtasks /Run") != 0 || fs.called("schtasks /End") != 0 || len(hooks.order) != 0 {
				t.Errorf("must not start, end or kill anything: %v %v", fs.calls, hooks.order)
			}
			if !strings.Contains(te.errb.String(), "NOT starting") {
				t.Errorf("stderr = %q", te.errb.String())
			}
			if !te.daemon.probe(bg, 0).Up {
				t.Error("the foreign daemon must be left alone")
			}
		})
	}
}

func TestWindowsInstallDeniedNamesTheRefusedStep(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	fs.createDenied = true
	err := m.Install(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "schtasks /Create") || !strings.Contains(err.Error(), "policy") {
		t.Errorf("err = %v, want the denied step named", err)
	}
}

func TestWindowsInstallRefusesTheDesktopAppsDaemon(t *testing.T) {
	for _, exe := range []string{
		`C:\Users\alice\AppData\Local\smind\bin\smind.exe`,
		`C:\Users\alice\AppData\Local\managed-daemon\bin\smind.exe`,
	} {
		te := newTestEnv(t, "windows")
		m, fs, _ := te.windows(t)
		te.Exe = exe
		m, _ = New(te.Env)
		_ = fs
		err := m.Install(bg, Options{})
		if err == nil || !strings.Contains(err.Error(), "install.ps1") {
			t.Errorf("%s: err = %v", exe, err)
		}
		if fs.called("schtasks /Create") != 0 {
			t.Errorf("%s: a task pointing into the app's directory would break on its next update", exe)
		}
	}
}

func TestWindowsInstallRejectsLinuxOnlyFlagsAndBadPaths(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	for name, o := range map[string]Options{
		"--system": {System: true},
		"--path":   {Path: `C:\x`},
		"relative": {Home: `rel\dir`},
		"quote":    {Home: `D:\a"b`},
	} {
		if err := m.Install(bg, o); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
	if len(fs.calls) != 0 {
		t.Errorf("nothing may be run: %v", fs.calls)
	}
}

func TestWindowsInstallWithoutConhostWarnsAndStillWorks(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	te.Env.FileExists = func(string) bool { return false }
	m, _ = New(te.Env)
	if err := m.Install(bg, Options{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(te.errb.String(), "console window") || strings.Contains(fs.xml, "conhost") {
		t.Errorf("stderr = %q, xml:\n%s", te.errb.String(), fs.xml)
	}
}

func TestWindowsInstallToolMissingIsAnError(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	fs.queryErr = true
	if err := m.Install(bg, Options{}); err == nil || !strings.Contains(err.Error(), "schtasks") {
		t.Errorf("err = %v", err)
	}
}

// -------------------------------------------------------- stop / start

func TestWindowsTaskStopUsesShutdownEndpointFirst(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	_ = m.Install(bg, Options{})
	fs.order.mu.Lock()
	fs.order.items = nil
	fs.order.mu.Unlock()
	hooks.modes, hooks.waits = nil, nil
	if err := m.Stop(bg, Options{}); err != nil {
		t.Fatal(err)
	}
	// One chronological log: shutdown request, then end the task, then sweep.
	sd, end, sweep := fs.order.index("shutdown"), fs.order.index("schtasks /End"), fs.order.index("forcestop:")
	if sd < 0 || end < 0 || sweep < 0 || !(sd < end && end < sweep) {
		t.Fatalf("the graceful shutdown must come first, then /End, then the sweep: %v", fs.order.items)
	}
	// The daemon exited on its own: nothing is force-killed (only leftover supervisors are swept),
	// after giving the supervisor time to finish by itself.
	if len(hooks.modes) != 1 || hooks.modes[0] != KillNone || hooks.waits[0] <= 0 {
		t.Errorf("modes %v waits %v: a graceful stop must not kill the daemon and must let the supervisor exit first", hooks.modes, hooks.waits)
	}
	if te.daemon.probe(bg, 0).Up {
		t.Error("the daemon must be down")
	}
}

func TestWindowsTaskStopFallsBackToEnd(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	_ = m.Install(bg, Options{})
	hooks.shutdownStops = false // the daemon ignores the request
	if err := m.Stop(bg, Options{}); err != nil {
		t.Fatal(err)
	}
	if fs.called("schtasks /End /TN smind") != 1 {
		t.Errorf("the task must be ended: %v", fs.calls)
	}
	if len(hooks.modes) == 0 || hooks.modes[len(hooks.modes)-1] != KillOurs {
		t.Errorf("a daemon that ignores the shutdown must be killed (schtasks /End only ends conhost): %v", hooks.modes)
	}
	if !strings.Contains(te.errb.String(), "did not stop") {
		t.Errorf("stderr = %q", te.errb.String())
	}
}

func TestWindowsTaskStopFallsBackWhenTheRequestFails(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	_ = m.Install(bg, Options{})
	hooks.shutdownErr = errors.New("token rejected")
	if err := m.Stop(bg, Options{}); err != nil {
		t.Fatal(err)
	}
	if fs.called("schtasks /End") != 1 || len(hooks.modes) == 0 || hooks.modes[len(hooks.modes)-1] != KillOurs {
		t.Errorf("%v %v", fs.calls, hooks.modes)
	}
}

func TestWindowsStopNeverTouchesAForeignDaemon(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.daemon.set(true, true, "0.9.0", "") // the desktop app's / a hand-started daemon
	err := m.Stop(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "left alone") {
		t.Errorf("err = %v", err)
	}
	if fs.order.index("shutdown") != -1 {
		t.Errorf("a daemon this task did not start must not be asked to shut down: %v", fs.order.items)
	}
	// The sweep runs, but may only kill a listener that is a child of OUR supervisor.
	for _, m := range hooks.modes {
		if m == KillOurs {
			t.Errorf("a foreign daemon must never be killed as ours: %v", hooks.modes)
		}
	}
	if !te.daemon.probe(bg, 0).Up {
		t.Error("the foreign daemon must still be up")
	}
}

func TestWindowsStopKillsAHungDaemonOfOurs(t *testing.T) {
	// Our own daemon hangs: it accepts connections but never answers /healthz, so
	// it looks like "another program". Only because its parent is our supervisor
	// may it be killed (KillOwned); stop must not refuse to deal with it.
	te := newTestEnv(t, "windows")
	m, _, hooks := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.daemon.set(true, false, "", "") // answers the port, not healthz
	hooks.hungOwned = true
	if err := m.Stop(bg, Options{}); err != nil {
		t.Fatalf("stop of a hung daemon of ours: %v", err)
	}
	if len(hooks.modes) != 1 || hooks.modes[0] != KillOwned || te.daemon.probe(bg, 0).Up {
		t.Errorf("modes %v, daemon up %v", hooks.modes, te.daemon.probe(bg, 0).Up)
	}
}

func TestWindowsUninstallWithAForeignDaemonStillRemovesTheTask(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.daemon.set(true, true, "0.9.0", "") // the desktop app's daemon holds the port
	te.resetOutput()
	if err := m.Uninstall(bg, Options{}); err != nil {
		t.Fatalf("uninstall must not be blocked by a daemon that is not ours: %v", err)
	}
	if fs.xml != "" || fs.called("schtasks /Delete") != 1 {
		t.Errorf("the task must be deleted: %v", fs.calls)
	}
	if !te.daemon.probe(bg, 0).Up || fs.order.index("shutdown") != -1 {
		t.Error("the foreign daemon must be left alone")
	}
	for _, m := range hooks.modes {
		if m == KillOurs {
			t.Errorf("modes %v", hooks.modes)
		}
	}
	if !strings.Contains(te.errb.String(), "left alone") {
		t.Errorf("stderr = %q", te.errb.String())
	}
}

func TestWindowsFailedSweepAfterAGracefulStopIsOnlyAWarning(t *testing.T) {
	// PowerShell blocked (AppLocker, constrained language): the daemon is down, so
	// stop, restart and uninstall must still work.
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	_ = m.Install(bg, Options{})
	hooks.forceStopErr = errors.New("powershell is blocked")
	if err := m.Stop(bg, Options{}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !strings.Contains(te.errb.String(), "sweeping leftover processes failed") {
		t.Errorf("stderr = %q", te.errb.String())
	}
	_ = m.Start(bg, Options{})
	if err := m.Uninstall(bg, Options{}); err != nil || fs.xml != "" {
		t.Errorf("uninstall: %v", err)
	}
	// ...but without a graceful stop the failed kill is an error.
	te2 := newTestEnv(t, "windows")
	m2, _, h2 := te2.windows(t)
	_ = m2.Install(bg, Options{})
	h2.shutdownStops = false
	h2.forceStopErr = errors.New("powershell is blocked")
	if err := m2.Stop(bg, Options{}); err == nil {
		t.Error("a daemon that could not be stopped must be reported")
	}
}

func TestWindowsStopWhenNothingRunsStillSweepsALeftoverSupervisor(t *testing.T) {
	// A crashed daemon leaves `service run` waiting to restart it: stop must end that too.
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	if err := m.Stop(bg, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(hooks.modes) != 1 || hooks.modes[0] != KillNone || fs.called("schtasks /End") != 1 {
		t.Errorf("%v %v", hooks.modes, fs.calls)
	}
}

func TestWindowsStartAndRestart(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	if err := m.Start(bg, Options{}); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("start before install: %v", err)
	}
	_ = m.Install(bg, Options{NoStart: true})
	if err := m.Start(bg, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(bg, Options{}); err != nil || fs.called("schtasks /Run") != 1 {
		t.Errorf("starting a running service is a no-op: %v %v", err, fs.calls)
	}
	if err := m.Restart(bg, Options{}); err != nil {
		t.Fatal(err)
	}
	if fs.called("schtasks /Run") != 2 || len(hooks.order) < 2 {
		t.Errorf("restart = graceful stop then run: %v %v", fs.calls, hooks.order)
	}
	if !strings.Contains(te.out.String(), "restarted") {
		t.Errorf("output = %q", te.out.String())
	}
}

func TestWindowsStartRefusesWhenForeignDaemonAnswers(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.daemon.set(true, true, "0.9.0", "")
	if err := m.Start(bg, Options{}); err == nil || !strings.Contains(err.Error(), "not starting") {
		t.Errorf("err = %v", err)
	}
	if fs.called("schtasks /Run") != 0 {
		t.Error("must not run the task")
	}
}

func TestWindowsStartWaitsForOurOwnDaemon(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.Env.Probe = func(context.Context, int) Health {
		if fs.called("schtasks /Run") > 0 {
			return Health{Up: true, Smind: true, Version: "0.9.0"} // a foreign smind, no managedBy
		}
		return Health{}
	}
	m2, _ := New(te.Env)
	if err := m2.Start(bg, Options{}); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("a foreign smind answering must not count as started: %v", err)
	}
}

// ---------------------------------------------------- uninstall / status

func TestWindowsUninstallStopsThenDeletesAndKeepsSMINDHOME(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	_ = m.Install(bg, Options{})
	if err := m.Uninstall(bg, Options{}); err != nil {
		t.Fatal(err)
	}
	if fs.xml != "" || fs.called("schtasks /Delete /TN smind /F") != 1 {
		t.Errorf("the task must be deleted: %v", fs.calls)
	}
	if fs.order.index("shutdown") < 0 || fs.order.index("shutdown") > fs.order.index("schtasks /Delete") {
		t.Errorf("uninstall must stop the daemon gracefully before deleting the task: %v", fs.order.items)
	}
	if !strings.Contains(te.out.String(), "untouched") {
		t.Errorf("output = %q", te.out.String())
	}
	te.resetOutput()
	if err := m.Uninstall(bg, Options{}); err != nil || !strings.Contains(te.out.String(), "not installed") {
		t.Errorf("second uninstall: %v %q", err, te.out.String())
	}
}

func TestWindowsUninstallKeepsTheTaskWhenTheDaemonCannotBeStopped(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	_ = m.Install(bg, Options{})
	hooks.shutdownStops = false
	hooks.forceStopErr = errors.New("access denied")
	if err := m.Uninstall(bg, Options{}); err == nil {
		t.Fatal("uninstall must fail when the daemon cannot be stopped")
	}
	if fs.xml == "" || fs.called("schtasks /Delete") != 0 {
		t.Error("the task must stay: deleting it would orphan a running daemon")
	}
}

func TestWindowsServiceStatusExitCodes(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, _, _ := te.windows(t)
	if st, err := m.Status(bg, Options{}); err != nil || st != NotInstalled || st.ExitCode() != 4 {
		t.Fatalf("not installed: %v %v", st, err)
	}
	_ = m.Install(bg, Options{NoStart: true})
	if st, _ := m.Status(bg, Options{}); st != Stopped || st.ExitCode() != 3 {
		t.Fatalf("stopped: %v", st)
	}
	_ = m.Start(bg, Options{})
	te.resetOutput()
	st, err := m.Status(bg, Options{})
	if err != nil || st != Running || st.ExitCode() != 0 {
		t.Fatalf("running: %v %v", st, err)
	}
	for _, want := range []string{"enabled", "not at boot", "managed by taskscheduler", "port:    4648", "smind 1.0.0"} {
		if !strings.Contains(te.out.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, te.out.String())
		}
	}
	// A foreign daemon on the port is not "running" for this service.
	te.daemon.set(true, true, "0.9.0", "")
	if st, _ := m.Status(bg, Options{}); st != Stopped {
		t.Errorf("a daemon this task did not start must not read as running: %v", st)
	}
}

func TestWindowsStatusReadsTheInstalledTasksHome(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, _, _ := te.windows(t)
	if err := m.Install(bg, Options{Home: `D:\srv\my smind`, NoStart: true}); err != nil {
		t.Fatal(err)
	}
	// From a shell with another SMIND_HOME the task's own home still wins.
	te.environ["SMIND_HOME"] = `E:\elsewhere`
	h, ok := m.InstalledHome(Options{})
	if !ok || h != `D:\srv\my smind` {
		t.Errorf("InstalledHome = %q, %v", h, ok)
	}
	te.resetOutput()
	_, _ = m.Status(bg, Options{})
	if !strings.Contains(te.out.String(), `home:    D:\srv\my smind`) {
		t.Errorf("status:\n%s", te.out.String())
	}
	// No task: false.
	te2 := newTestEnv(t, "windows")
	m2, _, _ := te2.windows(t)
	if _, ok := m2.InstalledHome(Options{}); ok {
		t.Error("nothing installed must report false")
	}
}

func TestWindowsStatusReportsADisabledTask(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	fs.xml = strings.Replace(fs.xml, "<Enabled>true</Enabled>\n    <Hidden>", "<Enabled>false</Enabled>\n    <Hidden>", 1)
	te.resetOutput()
	_, _ = m.Status(bg, Options{})
	if !strings.Contains(te.out.String(), "DISABLED") {
		t.Errorf("status:\n%s", te.out.String())
	}
}

func TestWindowsQueryGoesThroughUTF8AndRoundTripsNonASCII(t *testing.T) {
	// schtasks writes to a pipe in the console's OEM code page; the query asks for
	// code page 65001 so a non-ASCII user or path comes back intact.
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	home := `D:\Dữ liệu\Nguyễn smind`
	if err := m.Install(bg, Options{Home: home, NoStart: true}); err != nil {
		t.Fatal(err)
	}
	h, ok := m.InstalledHome(Options{})
	if !fs.chcp {
		t.Error("the query must run through `chcp 65001`")
	}
	if !ok || h != home {
		t.Errorf("InstalledHome = %q, %v; want %q", h, ok, home)
	}
}

func TestWindowsDisabledTaskIsReRegisteredByInstall(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	fs.xml = strings.Replace(fs.xml, "<Enabled>true</Enabled>\n    <Hidden>", "<Enabled>false</Enabled>\n    <Hidden>", 1)
	te.resetOutput()
	if err := m.Install(bg, Options{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	if fs.called("schtasks /Create") != 2 || strings.Contains(te.out.String(), "up to date") {
		t.Errorf("a disabled task must be re-registered (enabled): %v %q", fs.calls, te.out.String())
	}
}

func TestWindowsRefusesATaskThatBelongsToAnotherUser(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, fs, hooks := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	// The stored XML names another principal (Task Scheduler stores the SID).
	fs.xml = strings.ReplaceAll(fs.xml, `<UserId>WORKGROUP\alice</UserId>`, `<UserId>S-1-5-21-111-222-333-1001</UserId>`)
	te.UserSID = "S-1-5-21-111-222-333-500"
	m, _ = New(te.Env)
	for name, op := range map[string]func() error{
		"install":   func() error { return m.Install(bg, Options{}) },
		"uninstall": func() error { return m.Uninstall(bg, Options{}) },
		"start":     func() error { return m.Start(bg, Options{}) },
		"stop":      func() error { return m.Stop(bg, Options{}) },
		"restart":   func() error { return m.Restart(bg, Options{}) },
	} {
		if err := op(); err == nil || !strings.Contains(err.Error(), "another user") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if fs.called("schtasks /Delete") != 0 || fs.called("schtasks /End") != 0 || len(hooks.modes) != 0 {
		t.Errorf("another user's task must not be touched: %v", fs.calls)
	}
	// The same SID is fine.
	te.UserSID = "S-1-5-21-111-222-333-1001"
	m, _ = New(te.Env)
	if err := m.Uninstall(bg, Options{}); err != nil {
		t.Errorf("own task by SID: %v", err)
	}
}

func TestWindowsStartAndInstallSweepAStaleSupervisorBeforeRunning(t *testing.T) {
	// After a crash the supervisor waits out its restart delay with the task still
	// "running" (one instance at a time): /Run would do nothing. Sweep first.
	te := newTestEnv(t, "windows")
	m, fs, _ := te.windows(t)
	_ = m.Install(bg, Options{})
	if i, r := fs.order.index("forcestop:none"), fs.order.index("schtasks /Run"); i < 0 || r < 0 || i > r {
		t.Errorf("install must sweep before /Run: %v", fs.order.items)
	}
	_ = m.Stop(bg, Options{})
	fs.order.mu.Lock()
	fs.order.items = nil
	fs.order.mu.Unlock()
	_ = m.Start(bg, Options{})
	if i, r := fs.order.index("forcestop:none"), fs.order.index("schtasks /Run"); i < 0 || r < 0 || i > r {
		t.Errorf("start must sweep before /Run: %v", fs.order.items)
	}
}

func TestWindowsStartTimeoutExplainsTheInteractiveSession(t *testing.T) {
	te := newTestEnv(t, "windows")
	m, _, _ := te.windows(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.Env.Probe = func(context.Context, int) Health { return Health{} } // the task never brings the daemon up
	m2, _ := New(te.Env)
	err := m2.Start(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "interactive Windows session") {
		t.Errorf("err = %v", err)
	}
}

func TestPsQuoteDoublesEveryKindOfSingleQuote(t *testing.T) {
	// PowerShell also treats U+2018..U+201B as single-quote delimiters.
	for _, q := range []string{"'", "\u2018", "\u2019", "\u201A", "\u201B"} {
		if got := psQuote("O" + q + "Neil"); got != "O"+q+q+"Neil" {
			t.Errorf("psQuote(%q) = %q", q, got)
		}
	}
	if psQuote(`C:\a b\smind.exe`) != `C:\a b\smind.exe` {
		t.Error("plain paths are unchanged")
	}
}

func TestPowershellForceStopScriptNamesOnlyThisInstall(t *testing.T) {
	var got string
	r := runnerFunc(func(name string, args ...string) (Result, error) {
		got = strings.Join(args, " ")
		return Result{Stdout: "12,34\n"}, nil
	})
	pids, err := powershellForceStop(r)(bg, `C:\Users\o'neil\smind.exe`, 4648, KillOurs, 10*time.Second)
	if err != nil || fmt.Sprint(pids) != "[12 34]" {
		t.Fatalf("pids = %v, %v", pids, err)
	}
	for _, want := range []string{
		`$exe = 'C:\Users\o''neil\smind.exe'`, "$port = 4648", "$mode = 'ours'", "$wait = 10",
		// supervisors: this install's exe in the command line (survives a rename-aside after an update), this user's
		`$_.CommandLine -match $pat`, `' service run( |$)'`, "GetOwner", "$me = $env:USERNAME",
		// the listener: never a foreign process
		`$mine -and ($supPids -contains $p.ParentProcessId)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("script lacks %q:\n%s", want, got)
		}
	}
	// The supervisor filter must pin THIS exe: a decoy smind.exe elsewhere with
	// `service run` must not match.
	line := ""
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "service run(") {
			line = l
		}
	}
	if !strings.Contains(line, "$pat") {
		t.Errorf("the supervisor filter does not pin the exe: %q", line)
	}
	_, _ = powershellForceStop(r)(bg, `C:\smind.exe`, 1, KillNone, 0)
	if !strings.Contains(got, "$mode = 'none'") || !strings.Contains(got, "$wait = 0") {
		t.Error("mode and wait not passed")
	}
}

type runnerFunc func(name string, args ...string) (Result, error)

func (f runnerFunc) Run(_ context.Context, name string, args ...string) (Result, error) {
	return f(name, args...)
}
