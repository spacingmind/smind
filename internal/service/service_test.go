package service

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var bg = context.Background()

// ---------------------------------------------------------------- unit text

func TestSystemdUserUnitGolden(t *testing.T) {
	golden(t, "systemd-user.service.golden", renderUnit(unitSpec{
		Exe: "/usr/local/bin/smind", Path: "/home/alice/.local/bin:/usr/local/bin:/usr/bin:/bin",
	}))
}

func TestSystemdUserUnitWithSMINDHOMEGolden(t *testing.T) {
	golden(t, "systemd-user-home.service.golden", renderUnit(unitSpec{
		Exe: "/usr/local/bin/smind", Path: "/usr/bin:/bin", Home: "/srv/smind-data",
	}))
}

func TestSystemdSystemUnitGoldenUsesSudoUser(t *testing.T) {
	te := newTestEnv(t, "linux")
	te.Euid = 0
	te.environ["SUDO_USER"] = "alice"
	te.Exe = "/usr/local/bin/smind"
	m, fs := te.systemd(t)

	if err := m.Install(bg, Options{System: true, NoStart: true}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(te.SystemUnitDir, UnitName))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "systemd-system.service.golden", string(got))
	if !strings.Contains(string(got), "User=alice\n") || strings.Contains(string(got), "User=root") {
		t.Errorf("system unit must run as the invoking user, never root:\n%s", got)
	}
	// System scope: no --user and no linger probing.
	if fs.called("systemctl --user") != 0 || fs.called("loginctl") != 0 {
		t.Errorf("system install used user-scope tools: %v", fs.calls)
	}
}

func TestSystemdSystemRefusesPlainRoot(t *testing.T) {
	for _, sudoUser := range []string{"", "root"} {
		te := newTestEnv(t, "linux")
		te.Euid = 0
		if sudoUser != "" {
			te.environ["SUDO_USER"] = sudoUser
		}
		m, fs := te.systemd(t)
		err := m.Install(bg, Options{System: true})
		if err == nil || !strings.Contains(err.Error(), "must not run the daemon as root") {
			t.Errorf("SUDO_USER=%q: err = %v, want a refusal", sudoUser, err)
		}
		if _, statErr := os.Stat(filepath.Join(te.SystemUnitDir, UnitName)); statErr == nil {
			t.Errorf("SUDO_USER=%q: a unit was written despite the refusal", sudoUser)
		}
		if len(fs.calls) != 0 {
			t.Errorf("SUDO_USER=%q: systemctl was called: %v", sudoUser, fs.calls)
		}
	}
}

func TestSystemdSystemNeedsRoot(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, _ := te.systemd(t)
	err := m.Install(bg, Options{System: true})
	if err == nil || !strings.Contains(err.Error(), "sudo") {
		t.Errorf("err = %v, want a sudo hint", err)
	}
}

func TestSystemdUnitOmitsSMINDHOMEWhenUnset(t *testing.T) {
	if strings.Contains(renderUnit(unitSpec{Exe: "/x/smind", Path: "/bin"}), "SMIND_HOME") {
		t.Error("SMIND_HOME must only be written when set")
	}

	te := newTestEnv(t, "linux")
	m, _ := te.systemd(t)
	if err := m.Install(bg, Options{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	unit, _ := os.ReadFile(te.userUnit())
	if strings.Contains(string(unit), "SMIND_HOME") {
		t.Errorf("unit has SMIND_HOME though the shell had none:\n%s", unit)
	}

	// Set in the shell, or via --home (which wins).
	// Host-absolute paths: "/from/shell" is not absolute on a Windows host, where
	// these tests also run.
	te2 := newTestEnv(t, "linux")
	shellHome := filepath.Join(te2.home, "from-shell")
	flagHome := filepath.Join(te2.home, "from-flag")
	te2.environ["SMIND_HOME"] = shellHome
	m2, _ := te2.systemd(t)
	_ = m2.Install(bg, Options{NoStart: true})
	if u, _ := os.ReadFile(te2.userUnit()); !strings.Contains(string(u), quoteEnv("SMIND_HOME", shellHome)) {
		t.Errorf("shell SMIND_HOME not captured:\n%s", u)
	}
	te3 := newTestEnv(t, "linux")
	te3.environ["SMIND_HOME"] = shellHome
	m3, _ := te3.systemd(t)
	_ = m3.Install(bg, Options{NoStart: true, Home: flagHome})
	if u, _ := os.ReadFile(te3.userUnit()); !strings.Contains(string(u), quoteEnv("SMIND_HOME", flagHome)) || strings.Contains(string(u), "from-shell") {
		t.Errorf("--home must win over the shell's SMIND_HOME:\n%s", u)
	}
}

func TestSystemdUnitCapturesPATH(t *testing.T) {
	te := newTestEnv(t, "linux")
	te.environ["PATH"] = "/home/alice/.nvm/bin:/usr/bin"
	m, _ := te.systemd(t)
	if err := m.Install(bg, Options{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	if u, _ := os.ReadFile(te.userUnit()); !strings.Contains(string(u), `Environment="PATH=/home/alice/.nvm/bin:/usr/bin"`) {
		t.Errorf("installing shell's PATH not captured:\n%s", u)
	}
	// --path overrides it.
	te2 := newTestEnv(t, "linux")
	m2, _ := te2.systemd(t)
	_ = m2.Install(bg, Options{NoStart: true, Path: "/custom:/bin"})
	if u, _ := os.ReadFile(te2.userUnit()); !strings.Contains(string(u), `"PATH=/custom:/bin"`) || strings.Contains(string(u), "/opt/bin") {
		t.Errorf("--path must override:\n%s", u)
	}
}

func TestSystemdUnitDirectives(t *testing.T) {
	u := renderUnit(unitSpec{Exe: "/usr/local/bin/smind", Path: "/bin"})
	for _, want := range []string{
		"ExecStart=/usr/local/bin/smind serve\n", "Restart=on-failure\n", "RestartSec=5\n", "TimeoutStopSec=30\n",
		`Environment="SMIND_SUPERVISOR=systemd"`, "WantedBy=default.target\n",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit lacks %q:\n%s", want, u)
		}
	}
	for _, banned := range []string{"ProtectHome", "ProtectSystem", "PrivateTmp", "NoNewPrivileges", "ReadOnlyPaths", "User="} {
		if strings.Contains(u, banned) {
			t.Errorf("a user unit must not contain %q (agents write to the user's repos):\n%s", banned, u)
		}
	}
}

func TestSystemdUnitQuoting(t *testing.T) {
	u := renderUnit(unitSpec{Exe: `/opt/my apps/100%/smind`, Path: `/a b:/c"d`, Home: `/h/$x`})
	for _, want := range []string{
		`ExecStart="/opt/my apps/100%%/smind" serve`,
		`Environment="PATH=/a b:/c\"d"`,
		`Environment="SMIND_HOME=/h/$x"`,
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit lacks %q:\n%s", want, u)
		}
	}
	if got := quoteExec("/p/$HOME/smind"); got != `"/p/$$HOME/smind"` {
		t.Errorf("$ must be doubled in ExecStart: %s", got)
	}
}

func TestSystemdInstallRefusesNewlinesInPaths(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, _ := te.systemd(t)
	if err := m.Install(bg, Options{Path: "/a\nExecStartPre=/bin/evil"}); err == nil {
		t.Error("a newline in --path would inject unit directives and must be refused")
	}
	if _, err := os.Stat(te.userUnit()); err == nil {
		t.Error("nothing may be written")
	}
}

func TestLaunchdPlistGolden(t *testing.T) {
	got := renderPlist(plistSpec{Exe: "/usr/local/bin/smind", Path: "/opt/homebrew/bin:/usr/bin:/bin", Work: "/Users/alice"})
	golden(t, "launchd.plist.golden", got)
	golden(t, "launchd-home.plist.golden", renderPlist(plistSpec{Exe: "/usr/local/bin/smind", Path: "/usr/bin", Home: "/Volumes/data/smind", Work: "/Users/alice"}))
	for _, want := range []string{"<key>RunAtLoad</key>\n\t<true/>", "<key>SuccessfulExit</key>\n\t\t<false/>", "<string>launchd</string>", SupervisorEnv} {
		if !strings.Contains(got, want) {
			t.Errorf("plist lacks %q", want)
		}
	}
	if strings.Contains(got, "SMIND_HOME") {
		t.Error("SMIND_HOME must only be written when set")
	}
}

func TestLaunchdPlistIsWellFormedAndEscaped(t *testing.T) {
	got := renderPlist(plistSpec{Exe: `/Apps/R&D <x>/smind`, Path: `/a&b`, Home: `/h"i`, Work: "/w"})
	dec := xml.NewDecoder(strings.NewReader(got))
	for {
		if _, err := dec.Token(); err != nil {
			if err != io.EOF {
				t.Fatalf("plist is not well-formed XML: %v\n%s", err, got)
			}
			break
		}
	}
	if !strings.Contains(got, "/Apps/R&amp;D &lt;x&gt;/smind") {
		t.Errorf("exe path not escaped:\n%s", got)
	}
	// Apple's own linter, when available (macOS).
	if plutil, err := exec.LookPath("plutil"); err == nil {
		dir := t.TempDir()
		p := filepath.Join(dir, "x.plist")
		_ = os.WriteFile(p, []byte(renderPlist(plistSpec{Exe: "/usr/local/bin/smind", Path: "/usr/bin", Home: "/h", Work: "/w"})), 0o644)
		if out, err := exec.Command(plutil, "-lint", p).CombinedOutput(); err != nil {
			t.Errorf("plutil -lint: %v\n%s", err, out)
		}
	}
}

// ------------------------------------------------------ install / idempotency

func TestServiceInstallIdempotent(t *testing.T) {
	t.Run("systemd", func(t *testing.T) {
		te := newTestEnv(t, "linux")
		m, fs := te.systemd(t)
		if err := m.Install(bg, Options{}); err != nil {
			t.Fatalf("first install: %v", err)
		}
		first, _ := os.ReadFile(te.userUnit())
		st1, _ := os.Stat(te.userUnit())
		starts, reloads := fs.called("systemctl --user start"), fs.called("systemctl --user daemon-reload")
		if starts != 1 || reloads != 1 {
			t.Fatalf("first install: starts=%d reloads=%d, want 1 and 1: %v", starts, reloads, fs.calls)
		}

		te.resetOutput()
		if err := m.Install(bg, Options{}); err != nil {
			t.Fatalf("second install: %v", err)
		}
		second, _ := os.ReadFile(te.userUnit())
		st2, _ := os.Stat(te.userUnit())
		if string(first) != string(second) || !st1.ModTime().Equal(st2.ModTime()) {
			t.Error("a second identical install must not rewrite the unit")
		}
		if fs.called("systemctl --user start") != starts || fs.called("systemctl --user daemon-reload") != reloads {
			t.Errorf("a second install must not restart or reload: %v", fs.calls)
		}
		if !strings.Contains(te.out.String(), "already running") || !strings.Contains(te.out.String(), "up to date") {
			t.Errorf("output = %q", te.out.String())
		}
	})
	t.Run("launchd", func(t *testing.T) {
		te := newTestEnv(t, "darwin")
		m, fl := te.launchd(t)
		if err := m.Install(bg, Options{}); err != nil {
			t.Fatalf("first install: %v", err)
		}
		if fl.called("launchctl bootstrap") != 1 {
			t.Fatalf("calls: %v", fl.calls)
		}
		te.resetOutput()
		if err := m.Install(bg, Options{}); err != nil {
			t.Fatalf("second install: %v", err)
		}
		if fl.called("launchctl bootstrap") != 1 || fl.called("launchctl kickstart") != 0 || fl.called("launchctl bootout") != 0 {
			t.Errorf("a second install must not touch the running job: %v", fl.calls)
		}
		if !strings.Contains(te.out.String(), "already running") {
			t.Errorf("output = %q", te.out.String())
		}
	})
}

func TestServiceInstallDoesNotRestartARunningDaemonOnAChangedUnit(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	_ = m.Install(bg, Options{})
	te.resetOutput()
	te.environ["PATH"] = "/somewhere/else"
	if err := m.Install(bg, Options{}); err != nil {
		t.Fatal(err)
	}
	if fs.called("systemctl --user restart") != 0 || fs.called("systemctl --user start") != 1 {
		t.Errorf("install must never bounce a running daemon: %v", fs.calls)
	}
	if !strings.Contains(te.out.String(), "smind service restart") {
		t.Errorf("should tell the user to restart: %q", te.out.String())
	}
}

func TestServiceInstallNoStartStillEnables(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	if err := m.Install(bg, Options{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	if fs.called("systemctl --user enable") != 1 || fs.called("systemctl --user start") != 0 {
		t.Errorf("calls: %v", fs.calls)
	}
}

func TestServiceInstallRefusesToStartWhenForeignDaemonAnswers(t *testing.T) {
	cases := map[string]func(d *fakeDaemon){
		"hand-started smind":       func(d *fakeDaemon) { d.set(true, true, "0.9.0", "") },
		"desktop-managed or other": func(d *fakeDaemon) { d.set(true, true, "0.9.0", "app") },
		"not smind at all":         func(d *fakeDaemon) { d.set(true, false, "", "") },
	}
	for name, setup := range cases {
		t.Run("systemd/"+name, func(t *testing.T) {
			te := newTestEnv(t, "linux")
			setup(te.daemon)
			m, fs := te.systemd(t)
			if err := m.Install(bg, Options{}); err != nil {
				t.Fatalf("install must succeed (enabled, not started): %v", err)
			}
			if fs.called("systemctl --user enable") != 1 {
				t.Errorf("must still enable: %v", fs.calls)
			}
			if fs.called("systemctl --user start") != 0 || fs.called("systemctl --user restart") != 0 || fs.called("systemctl --user stop") != 0 {
				t.Errorf("must not start, restart or stop anything: %v", fs.calls)
			}
			if !strings.Contains(te.errb.String(), "NOT starting") {
				t.Errorf("stderr = %q", te.errb.String())
			}
			if h := te.daemon.probe(bg, 0); !h.Up {
				t.Error("the foreign daemon must be left alone")
			}
		})
		t.Run("launchd/"+name, func(t *testing.T) {
			te := newTestEnv(t, "darwin")
			setup(te.daemon)
			m, fl := te.launchd(t)
			if err := m.Install(bg, Options{}); err != nil {
				t.Fatalf("install must succeed: %v", err)
			}
			if fl.called("launchctl bootstrap") != 0 || fl.called("launchctl kickstart") != 0 || fl.called("launchctl bootout") != 0 {
				t.Errorf("must not load or kill anything: %v", fl.calls)
			}
			if _, err := os.Stat(te.plist()); err != nil {
				t.Error("the plist must still be installed")
			}
			if !strings.Contains(te.errb.String(), "NOT starting") {
				t.Errorf("stderr = %q", te.errb.String())
			}
		})
	}
}

func TestServiceStartRefusesWhenForeignDaemonAnswers(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.daemon.set(true, true, "0.9.0", "")
	err := m.Start(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "not starting") {
		t.Errorf("err = %v", err)
	}
	if fs.called("systemctl --user start") != 0 {
		t.Error("must not start")
	}
}

func TestServiceInstallLaunchdWithoutGUISessionExplains(t *testing.T) {
	te := newTestEnv(t, "darwin")
	m, fl := te.launchd(t)
	fl.noGUISession = true
	err := m.Install(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "GUI session") {
		t.Errorf("err = %v, want the GUI-session explanation", err)
	}
	if _, statErr := os.Stat(te.plist()); statErr != nil {
		t.Error("the plist stays installed for the next login")
	}
}

func TestServiceInstallNoSystemdRunning(t *testing.T) {
	te := newTestEnv(t, "linux")
	te.SystemdBooted = func() bool { return false }
	m, fs := te.systemd(t)
	err := m.Install(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "systemd is not running") {
		t.Errorf("err = %v", err)
	}
	if len(fs.calls) != 0 {
		t.Errorf("calls: %v", fs.calls)
	}
}

func TestServiceInstallNoUserSessionBus(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	fs.noBus = true
	err := m.Install(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "no systemd user session") {
		t.Errorf("err = %v", err)
	}
}

func TestServiceInstallFailsWhenTheDaemonNeverComesUp(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	fs.daemonAfter = ""
	// `start` succeeds but the daemon never answers: swallow the fake's side effect.
	te.Env.Probe = func(context.Context, int) Health { return Health{} }
	m2, _ := New(te.Env)
	_ = m
	err := m2.Install(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("err = %v", err)
	}
}

func TestServiceInstallWarnsAboutTemporaryExe(t *testing.T) {
	te := newTestEnv(t, "linux")
	te.Exe = "/tmp/go-build123/b001/exe/smind"
	m, _ := te.systemd(t)
	_ = m.Install(bg, Options{NoStart: true})
	if !strings.Contains(te.errb.String(), "looks temporary") {
		t.Errorf("stderr = %q", te.errb.String())
	}
}

// -------------------------------------------------------------- linger hint

func TestLingerHintWhenDisabled(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	fs.linger = "no"
	if err := m.Install(bg, Options{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(te.out.String(), "sudo loginctl enable-linger alice") {
		t.Errorf("output = %q", te.out.String())
	}

	te2 := newTestEnv(t, "linux")
	m2, fs2 := te2.systemd(t)
	fs2.linger = "yes"
	_ = m2.Install(bg, Options{NoStart: true})
	if strings.Contains(te2.out.String(), "linger") {
		t.Errorf("no hint expected when lingering is on: %q", te2.out.String())
	}

	// loginctl unavailable: stay quiet rather than guess.
	te3 := newTestEnv(t, "linux")
	m3, fs3 := te3.systemd(t)
	fs3.linger = ""
	_ = m3.Install(bg, Options{NoStart: true})
	if strings.Contains(te3.out.String(), "linger") {
		t.Errorf("no hint expected when loginctl fails: %q", te3.out.String())
	}
}

// ---------------------------------------------------------------- uninstall

func TestServiceUninstallKeepsSMINDHOME(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			te := newTestEnv(t, goos)
			data := filepath.Join(te.home, ".smind")
			if err := os.MkdirAll(data, 0o700); err != nil {
				t.Fatal(err)
			}
			keep := filepath.Join(data, "smind.db")
			if err := os.WriteFile(keep, []byte("precious"), 0o600); err != nil {
				t.Fatal(err)
			}
			te.environ["SMIND_HOME"] = data

			var m Manager
			var unit string
			if goos == "linux" {
				m, _ = te.systemd(t)
				unit = te.userUnit()
			} else {
				m, _ = te.launchd(t)
				unit = te.plist()
			}
			if err := m.Install(bg, Options{}); err != nil {
				t.Fatal(err)
			}
			if err := m.Uninstall(bg, Options{}); err != nil {
				t.Fatalf("uninstall: %v", err)
			}
			if _, err := os.Stat(unit); err == nil {
				t.Error("the unit must be removed")
			}
			if b, err := os.ReadFile(keep); err != nil || string(b) != "precious" {
				t.Errorf("SMIND_HOME content must survive: %v %q", err, b)
			}
			if h := te.daemon.probe(bg, 0); h.Up {
				t.Error("uninstall must stop the daemon")
			}
			// Idempotent.
			te.resetOutput()
			if err := m.Uninstall(bg, Options{}); err != nil || !strings.Contains(te.out.String(), "not installed") {
				t.Errorf("second uninstall: %v %q", err, te.out.String())
			}
		})
	}
}

// ------------------------------------------------------------------- status

func TestServiceStatusExitCodes(t *testing.T) {
	t.Run("systemd", func(t *testing.T) {
		te := newTestEnv(t, "linux")
		m, fs := te.systemd(t)

		st, err := m.Status(bg, Options{})
		if err != nil || st != NotInstalled || st.ExitCode() != 4 {
			t.Fatalf("not installed: %v %v (%d)", st, err, st.ExitCode())
		}
		_ = m.Install(bg, Options{NoStart: true})
		st, err = m.Status(bg, Options{})
		if err != nil || st != Stopped || st.ExitCode() != 3 {
			t.Fatalf("stopped: %v %v (%d)", st, err, st.ExitCode())
		}
		_ = m.Start(bg, Options{})
		fs.daemonAfter = "1.0.0"
		te.resetOutput()
		st, err = m.Status(bg, Options{})
		if err != nil || st != Running || st.ExitCode() != 0 {
			t.Fatalf("running: %v %v (%d)", st, err, st.ExitCode())
		}
		out := te.out.String()
		for _, want := range []string{"pid 4242", "enabled: enabled", "port:    4648", "smind 1.0.0", "managed by systemd"} {
			if !strings.Contains(out, want) {
				t.Errorf("status lacks %q:\n%s", want, out)
			}
		}
	})
	t.Run("launchd", func(t *testing.T) {
		te := newTestEnv(t, "darwin")
		m, fl := te.launchd(t)
		if st, err := m.Status(bg, Options{}); err != nil || st != NotInstalled {
			t.Fatalf("not installed: %v %v", st, err)
		}
		_ = m.Install(bg, Options{NoStart: true})
		if st, _ := m.Status(bg, Options{}); st != Stopped {
			t.Fatalf("installed but not loaded must be stopped, got %v", st)
		}
		_ = m.Start(bg, Options{})
		te.resetOutput()
		if st, _ := m.Status(bg, Options{}); st != Running {
			t.Fatalf("running: %v", st)
		}
		if !strings.Contains(te.out.String(), "pid 777") || !strings.Contains(te.out.String(), "managed by launchd") {
			t.Errorf("status:\n%s", te.out.String())
		}
		fl.exited() // `smind stop`: loaded, not running
		if st, _ := m.Status(bg, Options{}); st != Stopped {
			t.Fatalf("loaded but exited must be stopped, got %v", st)
		}
	})
}

func TestServiceStatusReportsDaemonVersusBinaryVersion(t *testing.T) {
	te := newTestEnv(t, "linux")
	te.Version = "1.1.0" // the binary was updated; the daemon still runs 1.0.0
	m, _ := te.systemd(t)
	_ = m.Install(bg, Options{})
	te.resetOutput()
	_, _ = m.Status(bg, Options{})
	if !strings.Contains(te.out.String(), "smind 1.0.0 (this binary is 1.1.0") {
		t.Errorf("status:\n%s", te.out.String())
	}
}

func TestServiceStatusReportsAForeignListener(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, _ := te.systemd(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.daemon.set(true, false, "", "")
	te.resetOutput()
	_, _ = m.Status(bg, Options{})
	if !strings.Contains(te.out.String(), "not smind") {
		t.Errorf("status:\n%s", te.out.String())
	}
}

func TestServiceStatusReadsThePortFromConfig(t *testing.T) {
	te := newTestEnv(t, "linux")
	data := filepath.Join(te.home, ".smind")
	_ = os.MkdirAll(data, 0o700)
	_ = os.WriteFile(filepath.Join(data, "config.yaml"), []byte("server:\n  port: 5151\n"), 0o600)
	m, _ := te.systemd(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.resetOutput()
	_, _ = m.Status(bg, Options{})
	if !strings.Contains(te.out.String(), "port:    5151") {
		t.Errorf("status:\n%s", te.out.String())
	}
}

// ------------------------------------------------------- start / stop / restart

func TestServiceStartStopRestart(t *testing.T) {
	t.Run("systemd", func(t *testing.T) {
		te := newTestEnv(t, "linux")
		m, fs := te.systemd(t)
		if err := m.Start(bg, Options{}); !errors.Is(err, ErrNotInstalled) {
			t.Fatalf("start before install: %v", err)
		}
		_ = m.Install(bg, Options{NoStart: true})
		if err := m.Start(bg, Options{}); err != nil {
			t.Fatal(err)
		}
		if err := m.Restart(bg, Options{}); err != nil {
			t.Fatal(err)
		}
		if fs.called("systemctl --user restart") != 1 {
			t.Errorf("calls: %v", fs.calls)
		}
		if err := m.Stop(bg, Options{}); err != nil {
			t.Fatal(err)
		}
		if te.daemon.probe(bg, 0).Up {
			t.Error("stop must stop the daemon")
		}
		if err := m.Stop(bg, Options{}); err != nil {
			t.Errorf("stopping a stopped service is fine: %v", err)
		}
	})
	t.Run("launchd", func(t *testing.T) {
		te := newTestEnv(t, "darwin")
		m, fl := te.launchd(t)
		if err := m.Restart(bg, Options{}); !errors.Is(err, ErrNotInstalled) {
			t.Fatalf("restart before install: %v", err)
		}
		_ = m.Install(bg, Options{NoStart: true})
		if err := m.Start(bg, Options{}); err != nil {
			t.Fatal(err)
		}
		fl.mu.Lock()
		fl.calls = nil
		fl.mu.Unlock()
		if err := m.Restart(bg, Options{}); err != nil {
			t.Fatal(err)
		}
		// Unload, wait for the port to free up, load again (re-reads the plist).
		got := strings.Join(fl.calls, "|")
		if !strings.Contains(got, "bootout") || strings.Index(got, "bootout") > strings.Index(got, "bootstrap") {
			t.Errorf("restart sequence: %v", fl.calls)
		}
		if err := m.Stop(bg, Options{}); err != nil || te.daemon.probe(bg, 0).Up {
			t.Errorf("stop: %v", err)
		}
		// A daemon that exited by itself (smind stop) stays loaded; start reloads it
		// (bootout + bootstrap, never kickstart: that would keep the OLD plist).
		_ = m.Start(bg, Options{})
		fl.exited()
		fl.mu.Lock()
		fl.calls = nil
		fl.mu.Unlock()
		if err := m.Start(bg, Options{}); err != nil {
			t.Fatal(err)
		}
		got = strings.Join(fl.calls, "|")
		if fl.called("launchctl kickstart") != 0 || !strings.Contains(got, "bootout") || strings.Index(got, "bootout") > strings.Index(got, "bootstrap") {
			t.Errorf("a loaded-but-exited job must be re-bootstrapped, not kickstarted: %v", fl.calls)
		}
	})
}

func TestSystemdScopeAutoDetectsASystemUnit(t *testing.T) {
	te := newTestEnv(t, "linux")
	te.Euid = 0
	te.environ["SUDO_USER"] = "alice"
	m, _ := te.systemd(t)
	if err := m.Install(bg, Options{System: true, NoStart: true}); err != nil {
		t.Fatal(err)
	}
	// Later, as a normal user (no flag): only the system unit exists.
	te.Euid = 1000
	te.environ["SUDO_USER"] = ""
	m2, _ := New(te.Env)
	te.resetOutput()
	st, err := m2.Status(bg, Options{})
	if err != nil || st != Stopped {
		t.Fatalf("status = %v, %v", st, err)
	}
	if !strings.Contains(te.out.String(), "systemd (system)") {
		t.Errorf("status:\n%s", te.out.String())
	}
}

func TestNewRejectsOtherOSes(t *testing.T) {
	if _, err := New(Env{GOOS: "plan9"}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v", err)
	}
}

func TestLaunchdRejectsSystemFlag(t *testing.T) {
	te := newTestEnv(t, "darwin")
	m, _ := te.launchd(t)
	if err := m.Install(bg, Options{System: true}); err == nil || !strings.Contains(err.Error(), "only available on Linux") {
		t.Errorf("err = %v", err)
	}
}

func TestExecRunnerReportsExitCodes(t *testing.T) {
	r := ExecRunner{}
	res, err := r.Run(bg, "sh", "-c", "echo out; echo err >&2; exit 3")
	if err != nil || res.ExitCode != 3 || strings.TrimSpace(res.Stdout) != "out" || strings.TrimSpace(res.Stderr) != "err" {
		t.Errorf("res = %+v, err = %v", res, err)
	}
	if _, err := r.Run(bg, "definitely-not-a-real-binary-smind"); err == nil {
		t.Error("a missing tool must be an error, not an exit code")
	}
}

func TestWriteIfChanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "f")
	if ch, err := writeIfChanged(p, []byte("x"), 0o644); err != nil || !ch {
		t.Fatalf("first write: %v %v", ch, err)
	}
	if ch, err := writeIfChanged(p, []byte("x"), 0o644); err != nil || ch {
		t.Fatalf("same content: %v %v", ch, err)
	}
	if ch, err := writeIfChanged(p, []byte("y"), 0o644); err != nil || !ch {
		t.Fatalf("new content: %v %v", ch, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestSystemdUserInstallUnderSudoIsRefused(t *testing.T) {
	te := newTestEnv(t, "linux")
	te.Euid = 0
	te.environ["SUDO_USER"] = "alice"
	m, fs := te.systemd(t)
	err := m.Install(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "--system") {
		t.Errorf("err = %v, want advice to run without sudo or use --system", err)
	}
	if len(fs.calls) != 0 {
		t.Errorf("calls: %v", fs.calls)
	}
	if _, statErr := os.Stat(te.userUnit()); statErr == nil {
		t.Error("nothing may be written")
	}
}

// ------------------------------------------------ review round: home, guards

// The HIGH finding: a service installed with a SMIND_HOME of its own (a flag, or
// the installing shell's) must be driven with THAT home's config afterwards, from
// any shell: the port, the daemon.log and the foreign-daemon check all follow it.
func TestStatusStartRestartUseTheInstalledUnitsHome(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			te := newTestEnv(t, goos)
			svcHome := filepath.Join(te.home, "srv-smind")
			if err := os.MkdirAll(svcHome, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(svcHome, "config.yaml"), []byte("server:\n  port: 5151\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var m Manager
			if goos == "linux" {
				m, _ = te.systemd(t)
			} else {
				m, _ = te.launchd(t)
			}
			if err := m.Install(bg, Options{Home: svcHome, NoStart: true}); err != nil {
				t.Fatal(err)
			}
			// A later command from a plain shell (no SMIND_HOME): same home, same port.
			delete(te.environ, "SMIND_HOME")
			if h, ok := m.InstalledHome(Options{}); !ok || h != svcHome {
				t.Fatalf("InstalledHome = %q, %v; want %q", h, ok, svcHome)
			}
			var ports []int
			te.Env.Probe = func(_ context.Context, port int) Health {
				ports = append(ports, port)
				return te.daemon.probe(bg, port)
			}
			m2, _ := New(te.Env)
			te.resetOutput()
			_, _ = m2.Status(bg, Options{})
			if !strings.Contains(te.out.String(), "port:    5151") || !strings.Contains(te.out.String(), "home:    "+svcHome) {
				t.Errorf("status:\n%s", te.out.String())
			}
			if err := m2.Start(bg, Options{}); err != nil {
				t.Fatalf("start: %v", err)
			}
			for _, p := range ports {
				if p != 5151 {
					t.Errorf("probed port %d, want 5151 (the installed service's)", p)
				}
			}
		})
	}
}

func TestInstalledHomeDefaultsWhenTheUnitHasNoSMINDHOME(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, _ := te.systemd(t)
	_ = m.Install(bg, Options{NoStart: true})
	// The shell now has a SMIND_HOME the daemon does not: the unit is the authority.
	te.environ["SMIND_HOME"] = "/some/other/place"
	h, ok := m.InstalledHome(Options{})
	if !ok || h != filepath.Join(te.home, ".smind") {
		t.Errorf("InstalledHome = %q, %v", h, ok)
	}
	if _, ok := mustManager(t, newTestEnv(t, "linux")).InstalledHome(Options{}); ok {
		t.Error("nothing installed must report false")
	}
}

func mustManager(t *testing.T, te *testEnv) Manager {
	t.Helper()
	m, _ := te.systemd(t)
	return m
}

func TestInstallRejectsARelativeHome(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		te := newTestEnv(t, goos)
		var m Manager
		if goos == "linux" {
			m, _ = te.systemd(t)
		} else {
			m, _ = te.launchd(t)
		}
		err := m.Install(bg, Options{Home: "relative/dir"})
		if err == nil || !strings.Contains(err.Error(), "absolute") {
			t.Errorf("%s: err = %v", goos, err)
		}
		if _, statErr := os.Stat(te.userUnit()); statErr == nil {
			t.Errorf("%s: nothing may be written", goos)
		}
		te.environ["SMIND_HOME"] = "also/relative"
		if err := m.Install(bg, Options{}); err == nil {
			t.Errorf("%s: a relative SMIND_HOME in the shell must be rejected too", goos)
		}
	}
}

func TestUnitHomeRoundTripsThroughQuoting(t *testing.T) {
	for _, home := range []string{`/srv/my smind`, `/srv/100%/smind`, `/srv/a"b`, `/srv/back\slash`} {
		unit := renderUnit(unitSpec{Exe: "/x/smind", Path: "/bin", Home: home})
		m := reUnitHome.FindStringSubmatch(unit)
		if m == nil || unquoteEnvValue(m[1]) != home {
			t.Errorf("home %q did not round-trip: %v", home, m)
		}
	}
	if got := quoteEnv("SMIND_HOME", "/a%b"); got != `"SMIND_HOME=/a%%b"` {
		t.Errorf("%% must be doubled in Environment=: %s", got)
	}
}

func TestLaunchdRefusesRootAndSudo(t *testing.T) {
	te := newTestEnv(t, "darwin")
	te.Euid = 0
	te.environ["SUDO_USER"] = "alice"
	m, fl := te.launchd(t)
	for name, op := range map[string]func() error{
		"install":   func() error { return m.Install(bg, Options{}) },
		"uninstall": func() error { return m.Uninstall(bg, Options{}) },
		"start":     func() error { return m.Start(bg, Options{}) },
		"stop":      func() error { return m.Stop(bg, Options{}) },
		"restart":   func() error { return m.Restart(bg, Options{}) },
	} {
		if err := op(); err == nil || !strings.Contains(err.Error(), "without sudo") {
			t.Errorf("%s as root: err = %v", name, err)
		}
	}
	if _, err := os.Stat(te.plist()); err == nil {
		t.Error("nothing may be written as root")
	}
	if len(fl.calls) != 0 {
		t.Errorf("launchctl was called: %v", fl.calls)
	}
}

func TestSystemdActingCommandsRefuseSudoOnAUserUnitAndNeedRootForASystemUnit(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	_ = m.Install(bg, Options{NoStart: true})

	te.Euid = 0
	te.environ["SUDO_USER"] = "alice"
	m2, _ := New(te.Env)
	for name, op := range map[string]func() error{
		"start":     func() error { return m2.Start(bg, Options{}) },
		"stop":      func() error { return m2.Stop(bg, Options{}) },
		"restart":   func() error { return m2.Restart(bg, Options{}) },
		"uninstall": func() error { return m2.Uninstall(bg, Options{}) },
	} {
		if err := op(); err == nil || !strings.Contains(err.Error(), "without sudo") {
			t.Errorf("%s under sudo on a user unit: err = %v", name, err)
		}
	}
	if _, err := os.Stat(te.userUnit()); err != nil {
		t.Error("the user unit must not be touched by a sudo run")
	}
	_ = fs

	// A system unit, acted on by a non-root user: a clear sudo hint, not a raw polkit error.
	te2 := newTestEnv(t, "linux")
	te2.Euid = 0
	te2.environ["SUDO_USER"] = "alice"
	ms, _ := te2.systemd(t)
	if err := ms.Install(bg, Options{System: true, NoStart: true}); err != nil {
		t.Fatal(err)
	}
	te2.Euid = 1000
	te2.environ["SUDO_USER"] = ""
	ms2, _ := New(te2.Env)
	for name, op := range map[string]func() error{
		"start":   func() error { return ms2.Start(bg, Options{System: true}) },
		"stop":    func() error { return ms2.Stop(bg, Options{System: true}) },
		"restart": func() error { return ms2.Restart(bg, Options{System: true}) },
	} {
		if err := op(); err == nil || !strings.Contains(err.Error(), "sudo") {
			t.Errorf("%s of a system unit as a normal user: err = %v", name, err)
		}
	}
}

func TestSystemInstallWarnsThatSudoPATHIsCaptured(t *testing.T) {
	te := newTestEnv(t, "linux")
	te.Euid = 0
	te.environ["SUDO_USER"] = "alice"
	m, _ := te.systemd(t)
	_ = m.Install(bg, Options{System: true, NoStart: true})
	if !strings.Contains(te.errb.String(), "sudo's, not yours") {
		t.Errorf("stderr = %q", te.errb.String())
	}
	te2 := newTestEnv(t, "linux")
	te2.Euid = 0
	te2.environ["SUDO_USER"] = "alice"
	m2, _ := te2.systemd(t)
	_ = m2.Install(bg, Options{System: true, NoStart: true, Path: "/home/alice/.local/bin:/usr/bin"})
	if strings.Contains(te2.errb.String(), "sudo's, not yours") {
		t.Errorf("no warning when --path is given: %q", te2.errb.String())
	}
}

func TestSystemdBusFailureWordingVariants(t *testing.T) {
	for _, msg := range []string{
		"Failed to connect to bus: No medium found",
		"Failed to connect to user scope bus via local transport: No such file or directory",
	} {
		te := newTestEnv(t, "linux")
		m, fs := te.systemd(t)
		fs.noBus = true
		fs.busMessage = msg
		if err := m.Install(bg, Options{}); err == nil || !strings.Contains(err.Error(), "no systemd user session") {
			t.Errorf("%q: err = %v", msg, err)
		}
	}
}

func TestSystemdStatusToolFailureIsAnErrorNotNotInstalled(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	_ = m.Install(bg, Options{NoStart: true})
	fs.showFails = true
	st, err := m.Status(bg, Options{})
	if err == nil {
		t.Errorf("status = %v with no error; a failing `systemctl show` must be reported", st)
	}
}

func TestSystemdUninstallStopsWhenSystemdCannotBeReached(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	_ = m.Install(bg, Options{})
	fs.noBus = true
	if err := m.Uninstall(bg, Options{}); err == nil {
		t.Fatal("uninstall must fail when it cannot stop the daemon")
	}
	if _, err := os.Stat(te.userUnit()); err != nil {
		t.Error("the unit must stay: removing it would orphan a running, enabled daemon")
	}
}

func TestLaunchdUninstallKeepsThePlistWhenBootoutFails(t *testing.T) {
	te := newTestEnv(t, "darwin")
	m, fl := te.launchd(t)
	_ = m.Install(bg, Options{})
	fl.bootoutFails = true
	if err := m.Uninstall(bg, Options{}); err == nil {
		t.Fatal("uninstall must fail when bootout fails")
	}
	if _, err := os.Stat(te.plist()); err != nil {
		t.Error("the plist must stay while the agent is still loaded")
	}
}

func TestRestartRefusesWhenAStoppedServiceFacesAForeignDaemon(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.daemon.set(true, true, "0.9.0", "")
	err := m.Restart(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "not restarting") {
		t.Errorf("systemd: err = %v", err)
	}
	if fs.called("systemctl --user restart") != 0 {
		t.Error("must not restart into a crash loop")
	}

	te2 := newTestEnv(t, "darwin")
	m2, fl := te2.launchd(t)
	_ = m2.Install(bg, Options{NoStart: true})
	te2.daemon.set(true, true, "0.9.0", "")
	if err := m2.Restart(bg, Options{}); err == nil || !strings.Contains(err.Error(), "not restarting") {
		t.Errorf("launchd: err = %v", err)
	}
	if fl.called("launchctl bootstrap") != 0 {
		t.Error("must not bootstrap into a crash loop")
	}
}

func TestStartWaitsForOurOwnDaemonNotAnyoneElses(t *testing.T) {
	// The port is free when `start` is issued, but the daemon that then answers is
	// not managed by this service (a foreign smind grabbed the port): not "started".
	te := newTestEnv(t, "linux")
	m, fs := te.systemd(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.Env.Probe = func(context.Context, int) Health {
		if fs.called("systemctl --user start") > 0 {
			return Health{Up: true, Smind: true, Version: "0.9.0"} // no managedBy
		}
		return Health{}
	}
	m2, _ := New(te.Env)
	err := m2.Start(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("a foreign smind answering must not count as started: %v", err)
	}
}

func TestLaunchdStartRefusesWhenForeignDaemonAnswers(t *testing.T) {
	te := newTestEnv(t, "darwin")
	m, fl := te.launchd(t)
	_ = m.Install(bg, Options{NoStart: true})
	te.daemon.set(true, true, "0.9.0", "")
	err := m.Start(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "not starting") {
		t.Errorf("err = %v", err)
	}
	if fl.called("launchctl bootstrap") != 0 || fl.called("launchctl kickstart") != 0 {
		t.Errorf("must not load: %v", fl.calls)
	}
}

func TestLaunchdRestartWaitsForThePortToBeFree(t *testing.T) {
	te := newTestEnv(t, "darwin")
	m, fl := te.launchd(t)
	_ = m.Install(bg, Options{})
	fl.bootoutKeepsDaemon = true // the old daemon is slow to release the port
	err := m.Restart(bg, Options{})
	if err == nil || !strings.Contains(err.Error(), "still answering") {
		t.Errorf("err = %v, want a wait-for-port failure", err)
	}
	if fl.called("launchctl bootstrap") > 1 { // only the install's
		t.Errorf("must not bootstrap while the old daemon holds the port: %v", fl.calls)
	}
}

func TestInstallSaysWhenTheRunningDaemonIsOlderThanThisBinary(t *testing.T) {
	te := newTestEnv(t, "linux")
	m, _ := te.systemd(t)
	_ = m.Install(bg, Options{})
	te.Version = "1.1.0" // the binary was replaced by an upgrade
	m2, _ := New(te.Env)
	te.resetOutput()
	_ = m2.Install(bg, Options{})
	if !strings.Contains(te.out.String(), "already running smind 1.0.0, but this binary is 1.1.0") {
		t.Errorf("output = %q", te.out.String())
	}
}

func TestLaunchdStateRegexMatchesNotRunning(t *testing.T) {
	if m := reState.FindStringSubmatch("x = {\n\tstate = not running\n}\n"); m == nil || m[1] != "not running" {
		t.Errorf("state = %v", m)
	}
}
