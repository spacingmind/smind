package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/crash"
)

func stubDoctor(t *testing.T, result any, dialErr error) {
	t.Helper()
	old := doctorDial
	doctorDial = func(context.Context) (wsCaller, func(), error) {
		if dialErr != nil {
			return nil, nil, dialErr
		}
		return func(_ context.Context, method string, _ any, out any) error {
			if method != "doctor.sessions" {
				t.Errorf("method %q", method)
			}
			raw, _ := json.Marshal(result)
			return json.Unmarshal(raw, out)
		}, func() {}, nil
	}
	t.Cleanup(func() { doctorDial = old })
}

// doctorHome points HOME and SMIND_HOME at fresh temp dirs with a ~/.claude.
func doctorHome(t *testing.T) (home, smindHome string) {
	t.Helper()
	home, smindHome = t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SMIND_HOME", smindHome)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return
}

func runDoctorCmd(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = runDoctor(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func TestDoctorPrintsWarnAndFix(t *testing.T) {
	doctorHome(t)
	stubDoctor(t, nil, errors.New("down"))
	code, out, _ := runDoctorCmd()
	if code != 0 || !strings.Contains(out, "[warn]") || !strings.Contains(out, "cleanupPeriodDays") || !strings.Contains(out, "  fix: ") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func snapshot(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			sum := ""
			if !d.IsDir() {
				b, _ := os.ReadFile(p)
				sum = fmt.Sprintf("%x", sha256.Sum256(b))
			}
			snap[p] = fmt.Sprintf("%s|%d|%s", info.ModTime(), info.Size(), sum)
			return nil
		})
	}
	return snap
}

func TestDoctorNeverWritesAnything(t *testing.T) {
	home, smindHome := doctorHome(t)
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"cleanupPeriodDays": 7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(smindHome, "providers", "x", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubDoctor(t, doctorSessions{}, nil)
	before := snapshot(t, home, smindHome)
	runDoctorCmd()
	runDoctorCmd("--json")
	after := snapshot(t, home, smindHome)
	if len(before) != len(after) {
		t.Fatalf("file set changed: %d -> %d", len(before), len(after))
	}
	for p, v := range before {
		if after[p] != v {
			t.Fatalf("%s changed", p)
		}
	}
}

func TestDoctorJSON(t *testing.T) {
	doctorHome(t)
	stubDoctor(t, doctorSessions{Runs: 3, WithRef: 2, Readable: 1, Gone: 1}, nil)
	code, out, _ := runDoctorCmd("--json")
	var got struct {
		Findings []map[string]any `json:"findings"`
		Sessions *doctorSessions  `json:"sessions"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("code %d out %q", code, out)
	}
	if len(got.Findings) == 0 || got.Sessions == nil || got.Sessions.Readable != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestDoctorSessionsLineAndDaemonDown(t *testing.T) {
	doctorHome(t)
	stubDoctor(t, doctorSessions{Runs: 5, WithRef: 4, Readable: 3, Gone: 1}, nil)
	code, out, _ := runDoctorCmd()
	if code != 0 || !strings.Contains(out, "sessions: 3 of 4 runs with a provider session can still be rebuilt (1 already gone)") ||
		!strings.Contains(out, `Run "smind doctor" again after changing the setting`) {
		t.Fatalf("code %d out %q", code, out)
	}
	stubDoctor(t, doctorSessions{Runs: 5, WithRef: 5, Readable: 3, TooLarge: 1, Gone: 1}, nil)
	if _, out, _ := runDoctorCmd(); !strings.Contains(out, "(1 already gone, 1 over the 64 MiB rebuild limit)") {
		t.Fatalf("too-large count missing: %q", out)
	}
	stubDoctor(t, nil, errors.New("no daemon"))
	code, out, _ = runDoctorCmd()
	if code != 0 || !strings.Contains(out, "sessions: the daemon is not running") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestDoctorUsage(t *testing.T) {
	doctorHome(t)
	code, _, errOut := runDoctorCmd("--bogus")
	if code != 2 || !strings.Contains(errOut, "smind doctor") {
		t.Fatalf("code %d err %q", code, errOut)
	}
}

// desktopHome isolates the desktop app-data location so DesktopDir points
// inside the test's temp dirs on every platform.
func desktopHome(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("XDG_DATA_HOME", d)
	t.Setenv("APPDATA", d)
	return d
}

// recordCrashes writes one real report per site, one second apart, so the
// newest report is always the last site.
func recordCrashes(t *testing.T, dir string, sites ...string) {
	t.Helper()
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	rec := crash.New(crash.Config{
		Dir: dir, Source: "daemon", Home: "/home/x", SmindHome: "/home/x/.smind",
		Now: func() time.Time { now = now.Add(time.Second); return now },
	})
	for _, site := range sites {
		if rec.Record(site, "boom", []byte("x.Fn()\n")) == "" {
			t.Fatalf("record %q wrote nothing", site)
		}
	}
}

func TestDoctorCrashesNone(t *testing.T) {
	_, smindHome := doctorHome(t)
	desktopHome(t)
	stubDoctor(t, nil, errors.New("down"))
	code, out, _ := runDoctorCmd()
	want := "crashes: none recorded (daemon: " + filepath.Join(smindHome, "crashes") + ")"
	if code != 0 || !strings.Contains(out, want) {
		t.Fatalf("code %d out %q", code, out)
	}
	if strings.Contains(out, "desktop crashes:") {
		t.Fatalf("desktop line printed with no reports: %q", out)
	}
}

func TestDoctorCrashesSome(t *testing.T) {
	_, smindHome := doctorHome(t)
	desktopHome(t)
	dir := filepath.Join(smindHome, "crashes")
	recordCrashes(t, dir, "wsapi.dispatch", "runs.drive")
	stubDoctor(t, nil, errors.New("down"))
	code, out, _ := runDoctorCmd()
	want := "crashes: 2 recorded in " + dir + "; newest 2026-11-14T10:00:02Z (daemon, runs.drive). Reports stay on this machine and are never uploaded."
	if code != 0 || !strings.Contains(out, want) {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestDoctorCrashesJSON(t *testing.T) {
	_, smindHome := doctorHome(t)
	desktopHome(t)
	recordCrashes(t, filepath.Join(smindHome, "crashes"), "wsapi.dispatch", "runs.drive")
	stubDoctor(t, doctorSessions{Runs: 3, WithRef: 2, Readable: 1, Gone: 1}, nil)
	code, out, _ := runDoctorCmd("--json")
	var top map[string]json.RawMessage
	if code != 0 || json.Unmarshal([]byte(out), &top) != nil {
		t.Fatalf("code %d out %q", code, out)
	}
	for _, key := range []string{"findings", "sessions", "crashes"} {
		if _, ok := top[key]; !ok {
			t.Fatalf("key %q missing in %s", key, out)
		}
	}
	var crashes struct {
		Daemon struct {
			Dir        string `json:"dir"`
			Count      int    `json:"count"`
			Newest     string `json:"newest"`
			NewestSite string `json:"newestSite"`
		} `json:"daemon"`
	}
	if err := json.Unmarshal(top["crashes"], &crashes); err != nil {
		t.Fatal(err)
	}
	if crashes.Daemon.Count != 2 || crashes.Daemon.NewestSite != "runs.drive" ||
		crashes.Daemon.Newest != "2026-11-14T10:00:02Z" ||
		crashes.Daemon.Dir != filepath.Join(smindHome, "crashes") {
		t.Fatalf("crashes %+v", crashes)
	}
}

func TestDoctorCrashesLinkRefused(t *testing.T) {
	_, smindHome := doctorHome(t)
	desktopHome(t)
	target := t.TempDir()
	recordCrashes(t, target, "wsapi.dispatch")
	link := filepath.Join(smindHome, "crashes")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	stubDoctor(t, nil, errors.New("down"))
	code, out, _ := runDoctorCmd()
	if code != 0 || !strings.Contains(out, "crashes: "+link+" is a link, so it was not read") {
		t.Fatalf("code %d out %q", code, out)
	}
	if strings.Contains(out, "recorded in") {
		t.Fatalf("count shown for a linked directory: %q", out)
	}
}

func TestDoctorCrashesDoesNotWrite(t *testing.T) {
	_, smindHome := doctorHome(t)
	desktopHome(t)
	stubDoctor(t, nil, errors.New("down"))
	runDoctorCmd()
	runDoctorCmd("--json")
	if _, err := os.Lstat(filepath.Join(smindHome, "crashes")); !os.IsNotExist(err) {
		t.Fatalf("doctor created the crashes directory: %v", err)
	}
}

func TestDoctorCrashesPrintableSite(t *testing.T) {
	_, smindHome := doctorHome(t)
	desktopHome(t)
	recordCrashes(t, filepath.Join(smindHome, "crashes"), "wsapi.\ndispatch\x1b[31m")
	stubDoctor(t, nil, errors.New("down"))
	_, out, _ := runDoctorCmd()
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\ndispatch") {
		t.Fatalf("control byte from the site reached the output: %q", out)
	}
	if !strings.Contains(out, "wsapi.?dispatch") {
		t.Fatalf("site not passed through printable: %q", out)
	}
}

func TestDoctorWarnsAboutDisabledAutoModeAndStripsControlBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a control byte cannot be part of a Windows file name")
	}
	doctorHome(t)
	dir := filepath.Join(t.TempDir(), "cfg\x1b[31mred")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"cleanupPeriodDays":90,"permissions":{"disableAutoMode":"disable"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stubDoctor(t, nil, errors.New("down"))
	code, out, _ := runDoctorCmd()
	if code != 0 || !strings.Contains(out, "permissions.disableAutoMode") {
		t.Fatalf("code %d out %q", code, out)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("doctor output carries an escape byte: %q", out)
	}
}

func TestClaudeManagedSettingsPathsAreAbsolute(t *testing.T) {
	got := claudeManagedSettingsPaths()
	if len(got) == 0 {
		t.Fatal("no managed settings path")
	}
	for _, p := range got {
		if !strings.HasSuffix(p, "managed-settings.json") || !(strings.HasPrefix(p, "/") || strings.Contains(p, `:\`)) {
			t.Fatalf("path %q is not an absolute managed-settings.json path", p)
		}
	}
}
