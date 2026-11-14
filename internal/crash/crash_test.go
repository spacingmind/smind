package crash

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// testConfig returns a recorder config that never touches the real home:
// the redaction paths point at a fake user, and the rate limit is high.
func testConfig(dir string) Config {
	return Config{
		Dir:          dir,
		Version:      "test-1",
		Source:       "daemon",
		Home:         "/home/tester",
		SmindHome:    "/home/tester/.smind",
		MaxPerMinute: 1000,
	}
}

// setDefault installs cfg as the process default for the test and restores
// the previous recorder afterwards.
func setDefault(t *testing.T, cfg Config) {
	t.Helper()
	old := defaultRecorder.Load()
	Init(cfg)
	t.Cleanup(func() { defaultRecorder.Store(old) })
}

// clock returns a fake Now over the returned settable time.
func clock() (func() time.Time, *time.Time) {
	now := time.Date(2025, 11, 15, 10, 0, 0, 0, time.UTC)
	return func() time.Time { return now }, &now
}

func crashFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if isCrashName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func waitForReports(t *testing.T, dir string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if names := crashFiles(t, dir); len(names) == want {
			return names
		} else if time.Now().After(deadline) {
			t.Fatalf("expected %d crash files in %s, got %d", want, dir, len(names))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func readReport(t *testing.T, path string) report {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rep report
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return rep
}

// TestCrashGoPanicProducesReport: crash-go-panic-produces-report — a panic
// in a goroutine started through Go produces exactly one report file and
// the process keeps running.
func TestCrashGoPanicProducesReport(t *testing.T) {
	dir := t.TempDir()
	setDefault(t, testConfig(dir))
	done := make(chan struct{})
	Go("go.panic", func() {
		defer close(done)
		panic("boom")
	})
	<-done
	names := waitForReports(t, dir, 1)
	rep := readReport(t, filepath.Join(dir, names[0]))
	if rep.Site != "go.panic" || rep.Source != "daemon" {
		t.Errorf("report site/source = %q/%q, want go.panic/daemon", rep.Site, rep.Source)
	}
	if rep.Message != "" || !rep.MessageWithheld {
		t.Errorf("panic(%q) message = %q withheld=%v, want withheld", "boom", rep.Message, rep.MessageWithheld)
	}
}

// TestCrashGoSurvivesAndRunsDefers: crash-go-survives-and-runs-defers — the
// panicking goroutine's own defers run before the wrapper recovers, and the
// process survives.
func TestCrashGoSurvivesAndRunsDefers(t *testing.T) {
	dir := t.TempDir()
	setDefault(t, testConfig(dir))
	var unlocked, finalised bool
	done := make(chan struct{})
	Go("go.defers", func() {
		defer func() { finalised = true; close(done) }()
		defer func() { unlocked = true }()
		panic("x")
	})
	<-done
	if !unlocked || !finalised {
		t.Fatalf("defers did not run: unlocked=%v finalised=%v", unlocked, finalised)
	}
	waitForReports(t, dir, 1)
}

// TestCrashRecoverHelperRecords: crash-recover-helper-records — a deferred
// method Recover records the panic and swallows it.
func TestCrashRecoverHelperRecords(t *testing.T) {
	dir := t.TempDir()
	r := New(testConfig(dir))
	func() {
		defer r.Recover("helper.site")
		panic("helper boom")
	}()
	names := crashFiles(t, dir)
	if len(names) != 1 {
		t.Fatalf("expected 1 report, got %d", len(names))
	}
	rep := readReport(t, filepath.Join(dir, names[0]))
	if rep.Site != "helper.site" || rep.PanicType != "string" {
		t.Errorf("report site/type = %q/%q, want helper.site/string", rep.Site, rep.PanicType)
	}
	if !strings.Contains(rep.Stack, "TestCrashRecoverHelperRecords") {
		t.Errorf("stack does not identify the site:\n%s", rep.Stack)
	}
}

// TestCrashWithholdsNonRuntimeMessage: crash-withholds-non-runtime-message —
// a panic value that is not a runtime.Error is recorded by type only.
func TestCrashWithholdsNonRuntimeMessage(t *testing.T) {
	dir := t.TempDir()
	r := New(testConfig(dir))
	stack := []byte("goroutine 1 [running]:\ntest.Fn()\n\t/t.go:1 +0x1\n")
	for _, tc := range []struct {
		name string
		v    any
		typ  string
	}{
		{"string", "the token is sk-abcdefghijklmnop", "string"},
		{"error", errors.New("boom at /home/tester/secret"), "*errors.errorString"},
		{"nil", nil, "<nil>"},
	} {
		if got := r.Record("w."+tc.name, tc.v, stack); got == "" {
			t.Fatalf("%s: no report written", tc.name)
		} else {
			rep := readReport(t, got)
			if rep.Message != "" || !rep.MessageWithheld {
				t.Errorf("%s: message = %q withheld=%v, want \"\"/true", tc.name, rep.Message, rep.MessageWithheld)
			}
			if rep.PanicType != tc.typ {
				t.Errorf("%s: panicType = %q, want %q", tc.name, rep.PanicType, tc.typ)
			}
		}
	}
}

// TestCrashKeepsRuntimeErrorMessage: crash-keeps-runtime-error-message — a
// runtime.Error (nil map write) keeps its message.
func TestCrashKeepsRuntimeErrorMessage(t *testing.T) {
	dir := t.TempDir()
	r := New(testConfig(dir))
	func() {
		defer r.Recover("rt.nilmap")
		var m map[string]int
		m["k"] = 1
	}()
	names := crashFiles(t, dir)
	if len(names) != 1 {
		t.Fatalf("expected 1 report, got %d", len(names))
	}
	rep := readReport(t, filepath.Join(dir, names[0]))
	if rep.MessageWithheld || rep.Message == "" {
		t.Errorf("runtime error message withheld: %+v", rep)
	}
	if !strings.Contains(rep.Message, "nil map") {
		t.Errorf("message %q does not mention the nil map", rep.Message)
	}
}

// TestCrashReportFieldsAndSchema: crash-report-fields-and-schema — exactly
// the twelve schema-1 fields, with the right shapes.
func TestCrashReportFieldsAndSchema(t *testing.T) {
	dir := t.TempDir()
	nowFn, _ := clock()
	cfg := testConfig(filepath.Join(dir, "crashes"))
	cfg.Now = nowFn
	r := New(cfg)
	func() {
		defer r.Recover("fields.site")
		panic("fields")
	}()
	names := crashFiles(t, filepath.Join(dir, "crashes"))
	if len(names) != 1 {
		t.Fatalf("expected 1 report, got %d", len(names))
	}
	data, err := os.ReadFile(filepath.Join(dir, "crashes", names[0]))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	want := []string{"schema", "source", "version", "os", "arch", "time", "site", "panicType", "message", "messageWithheld", "stack", "suppressedSinceLast"}
	if len(m) != len(want) {
		t.Errorf("report has %d fields, want %d: %v", len(m), len(want), m)
	}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("report is missing field %q", k)
		}
	}
	if m["schema"] != float64(1) || m["source"] != "daemon" || m["version"] != "test-1" {
		t.Errorf("schema/source/version = %v/%v/%v", m["schema"], m["source"], m["version"])
	}
	if m["os"] != runtime.GOOS || m["arch"] != runtime.GOARCH {
		t.Errorf("os/arch = %v/%v, want %s/%s", m["os"], m["arch"], runtime.GOOS, runtime.GOARCH)
	}
	ts, ok := m["time"].(string)
	if !ok || !strings.HasSuffix(ts, "Z") {
		t.Errorf("time = %v, want UTC RFC3339Nano", m["time"])
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Errorf("time %q does not parse: %v", ts, err)
	}
	if m["site"] != "fields.site" || m["messageWithheld"] != true || m["suppressedSinceLast"] != float64(0) {
		t.Errorf("site/messageWithheld/suppressed = %v/%v/%v", m["site"], m["messageWithheld"], m["suppressedSinceLast"])
	}
	if !strings.Contains(m["stack"].(string), "TestCrashReportFieldsAndSchema") {
		t.Errorf("stack does not contain the panicking function:\n%v", m["stack"])
	}
}

// TestCrashCapsReportSize: crash-caps-report-size — a huge stack is cut so
// the file fits MaxFileBytes.
func TestCrashCapsReportSize(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.MaxFileBytes = 2048
	r := New(cfg)
	big := strings.Repeat("pkg.example.com/Fn(i int)\n\t/usr/local/src/pkg/f.go:12 +0x10\n", 2000)
	path := r.Record("big.stack", "boom", []byte(big))
	if path == "" {
		t.Fatal("no report written")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() > 2048 {
		t.Errorf("report is %d bytes, want <= 2048", fi.Size())
	}
	rep := readReport(t, path)
	if !strings.HasSuffix(rep.Stack, stackTruncated) {
		t.Errorf("stack does not carry the truncation marker:\n%q", rep.Stack)
	}
	if len(rep.Stack) >= len(big) {
		t.Errorf("stack was not cut: %d bytes", len(rep.Stack))
	}
}

// TestCrashKeepsNewest20: crash-keeps-newest-20 — only the newest MaxFiles
// reports are kept, oldest deleted by name order.
func TestCrashKeepsNewest20(t *testing.T) {
	dir := t.TempDir()
	nowFn, now := clock()
	cfg := testConfig(dir)
	cfg.Now = nowFn
	r := New(cfg)
	stack := []byte("goroutine 1 [running]:\ntest.Fn()\n\t/t.go:1 +0x1\n")
	for i := 0; i < 25; i++ {
		*now = now.Add(2 * time.Second)
		if got := r.Record(fmt.Sprintf("n%02d", i), "boom", stack); got == "" {
			t.Fatalf("write %d dropped", i)
		}
	}
	names := crashFiles(t, dir)
	if len(names) != 20 {
		t.Fatalf("expected 20 reports kept, got %d", len(names))
	}
	joined := strings.Join(names, " ")
	if strings.Contains(joined, "n00.") || strings.Contains(joined, "n04.") {
		t.Errorf("oldest reports were not deleted: %v", names)
	}
	if !strings.Contains(joined, "n24.json") {
		t.Errorf("newest report missing: %v", names)
	}
}

// TestCrashCapsDirSize: crash-caps-dir-size — the directory is capped at
// MaxDirBytes by deleting oldest first.
func TestCrashCapsDirSize(t *testing.T) {
	dir := t.TempDir()
	nowFn, now := clock()
	cfg := testConfig(dir)
	cfg.Now = nowFn
	cfg.MaxFiles = 100
	cfg.MaxDirBytes = 2048
	r := New(cfg)
	stack := []byte("goroutine 1 [running]:\ntest.Fn()\n\t/t.go:1 +0x1\n")
	for i := 0; i < 12; i++ {
		*now = now.Add(2 * time.Second)
		if got := r.Record(fmt.Sprintf("d%02d", i), "boom", stack); got == "" {
			t.Fatalf("write %d dropped", i)
		}
	}
	names := crashFiles(t, dir)
	if len(names) == 12 {
		t.Fatal("dir cap deleted nothing")
	}
	var total int64
	for _, n := range names {
		fi, err := os.Stat(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		total += fi.Size()
	}
	if total > 2048 {
		t.Errorf("dir total %d bytes exceeds the 2048 cap", total)
	}
	if !strings.Contains(names[len(names)-1], "d11.json") {
		t.Errorf("newest report was deleted: %v", names)
	}
}

// TestCrashRateLimit: crash-rate-limit — at most MaxPerMinute writes in a
// rolling minute; dropped reports are counted into the next written one.
func TestCrashRateLimit(t *testing.T) {
	dir := t.TempDir()
	nowFn, now := clock()
	cfg := testConfig(dir)
	cfg.Now = nowFn
	cfg.MaxPerMinute = 3
	r := New(cfg)
	stack := []byte("goroutine 1 [running]:\ntest.Fn()\n\t/t.go:1 +0x1\n")
	for i := 0; i < 3; i++ {
		if got := r.Record(fmt.Sprintf("a%d", i), "boom", stack); got == "" {
			t.Fatalf("write %d dropped", i)
		}
	}
	*now = now.Add(10 * time.Second)
	for i := 3; i < 5; i++ {
		if got := r.Record(fmt.Sprintf("a%d", i), "boom", stack); got != "" {
			t.Fatalf("write %d above the rate limit was recorded", i)
		}
	}
	if got := crashFiles(t, dir); len(got) != 3 {
		t.Fatalf("expected 3 files after the rate limit, got %d", len(got))
	}
	*now = now.Add(51 * time.Second) // 61s after the first burst
	if got := r.Record("after", "boom", stack); got == "" {
		t.Fatal("write after the rolling minute was dropped")
	}
	names := crashFiles(t, dir)
	if len(names) != 4 {
		t.Fatalf("expected 4 files, got %d", len(names))
	}
	if rep := readReport(t, filepath.Join(dir, names[len(names)-1])); rep.SuppressedSinceLast != 2 {
		t.Errorf("suppressedSinceLast = %d, want 2", rep.SuppressedSinceLast)
	}
}

// TestCrashDedupesIdenticalPanics: crash-dedupes-identical-panics — the
// same site and stack frame inside the window is not written again but
// counted.
func TestCrashDedupesIdenticalPanics(t *testing.T) {
	dir := t.TempDir()
	nowFn, now := clock()
	cfg := testConfig(dir)
	cfg.Now = nowFn
	r := New(cfg)
	first := []byte("goroutine 1 [running]:\ntest.SameFrame()\n\t/t.go:1 +0x1\n")
	if got := r.Record("dd", "boom", first); got == "" {
		t.Fatal("first report dropped")
	}
	*now = now.Add(10 * time.Second)
	if got := r.Record("dd", "boom", first); got != "" {
		t.Fatalf("identical panic inside the window was written to %s", got)
	}
	*now = now.Add(10 * time.Second)
	other := []byte("goroutine 1 [running]:\ntest.OtherFrame()\n\t/t.go:2 +0x1\n")
	if got := r.Record("dd", "boom", other); got == "" {
		t.Fatal("new panic dropped")
	}
	names := crashFiles(t, dir)
	if len(names) != 2 {
		t.Fatalf("expected 2 files, got %d", len(names))
	}
	if rep := readReport(t, filepath.Join(dir, names[1])); rep.SuppressedSinceLast != 1 {
		t.Errorf("suppressedSinceLast = %d, want 1", rep.SuppressedSinceLast)
	}
}

// TestCrashPerms07000600: crash-perms-0700-0600 — the directory is 0700 and
// the report file 0600 on Unix.
func TestCrashPerms07000600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows keeps the platform default ACL")
	}
	dir := filepath.Join(t.TempDir(), "crashes")
	r := New(testConfig(dir))
	path := r.Record("perm", "boom", []byte("test.Fn()\n\t/t.go:1 +0x1\n"))
	if path == "" {
		t.Fatal("no report written")
	}
	di, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %o, want 700", di.Mode().Perm())
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 600", fi.Mode().Perm())
	}
}

// TestCrashRefusesLinkDir: crash-refuses-link-dir — a crash directory that
// is a link is not used and not followed.
func TestCrashRefusesLinkDir(t *testing.T) {
	real := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "crashes-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	r := New(testConfig(link))
	if got := r.Record("link", "boom", []byte("test.Fn()\n")); got != "" {
		t.Fatalf("report written through the link to %s", got)
	}
	func() {
		defer r.Recover("link")
		panic("boom")
	}()
	if got := crashFiles(t, real); len(got) != 0 {
		t.Fatalf("link target holds %d reports, want 0", len(got))
	}
	if _, err := os.ReadDir(link); err != nil {
		t.Fatalf("recorder should leave the link alone: %v", err)
	}
}

// TestCrashWriteFailureIsSilent: crash-write-failure-is-silent — a bad
// directory never panics and never blocks.
func TestCrashWriteFailureIsSilent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := New(testConfig(filepath.Join(file, "crashes")))
	if got := r.Record("bad", "boom", []byte("test.Fn()\n")); got != "" {
		t.Fatalf("report written to %s", got)
	}
	func() {
		defer r.Recover("bad")
		panic("boom")
	}()
}

// TestCrashUninitialisedIsNoopAndRecovers: crash-uninitialised-is-noop-and-
// recovers — with no default recorder, Recover still swallows a panic and
// Record on a nil recorder is a no-op.
func TestCrashUninitialisedIsNoopAndRecovers(t *testing.T) {
	old := defaultRecorder.Load()
	defaultRecorder.Store(nil)
	t.Cleanup(func() { defaultRecorder.Store(old) })
	if Default() != nil {
		t.Fatal("Default() is not nil")
	}
	func() {
		defer Recover("uninit")
		panic("boom")
	}()
	var r *Recorder
	if got := r.Record("uninit", "boom", nil); got != "" {
		t.Fatalf("nil recorder returned %q", got)
	}
	func() {
		defer r.Recover("uninit")
		panic("boom")
	}()
}

// TestCrashSiteNameSanitised: crash-site-name-sanitised — a hostile site
// label cannot escape the crash directory.
func TestCrashSiteNameSanitised(t *testing.T) {
	dir := t.TempDir()
	r := New(testConfig(dir))
	path := r.Record("../../etc/x y", "boom", []byte("test.Fn()\n"))
	if path == "" {
		t.Fatal("no report written")
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("report landed in %s, want %s", filepath.Dir(path), dir)
	}
	name := filepath.Base(path)
	want := regexp.MustCompile(`^crash-\d{8}T\d{6}\.\d{3}-daemon-[a-z0-9._-]{1,40}\.json$`)
	if !want.MatchString(name) {
		t.Errorf("file name %q does not match the crash name shape", name)
	}
	if !strings.HasSuffix(name, "-..-..-etc-x-y.json") {
		t.Errorf("file name %q does not carry the sanitised site", name)
	}
	if got := crashFiles(t, dir); len(got) != 1 {
		t.Fatalf("expected 1 file in the crash dir, got %d", len(got))
	}
}

// TestListNone: List on a missing directory reports nothing, no error, and
// does not create it.
func TestListNone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	s := List(dir)
	if s.Count != 0 || s.Err != "" || s.Refused || !s.Newest.IsZero() {
		t.Errorf("List(missing) = %+v, want Count 0 and no error", s)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("List created the directory: %v", err)
	}
}

// TestListSome: List counts reports and reads the newest one's fields.
func TestListSome(t *testing.T) {
	dir := t.TempDir()
	nowFn, _ := clock()
	cfg := testConfig(dir)
	cfg.Now = nowFn
	r := New(cfg)
	if got := r.Record("list.some", "boom", []byte("test.Fn()\n")); got == "" {
		t.Fatal("no report written")
	}
	s := List(dir)
	if s.Count != 1 || s.Err != "" || s.Refused {
		t.Errorf("List = %+v, want Count 1", s)
	}
	if s.NewestSite != "list.some" || s.NewestSource != "daemon" {
		t.Errorf("newest site/source = %q/%q", s.NewestSite, s.NewestSource)
	}
	if s.Newest.IsZero() {
		t.Error("newest time is zero")
	}
}

// TestListNewestWins: the newest report is picked by file name timestamp.
func TestListNewestWins(t *testing.T) {
	dir := t.TempDir()
	nowFn, now := clock()
	cfg := testConfig(dir)
	cfg.Now = nowFn
	r := New(cfg)
	stack := []byte("test.Fn()\n")
	for _, site := range []string{"list.old", "list.mid", "list.new"} {
		*now = now.Add(3 * time.Second)
		if got := r.Record(site, "boom", stack); got == "" {
			t.Fatalf("report for %s dropped", site)
		}
	}
	s := List(dir)
	if s.Count != 3 {
		t.Fatalf("count = %d, want 3", s.Count)
	}
	if s.NewestSite != "list.new" {
		t.Errorf("newest site = %q, want list.new", s.NewestSite)
	}
}

// TestListLinkRefused: a linked crash directory is refused, not followed.
func TestListLinkRefused(t *testing.T) {
	real := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "crashes-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	s := List(link)
	if !s.Refused {
		t.Errorf("List(link) = %+v, want Refused", s)
	}
	if s.Count != 0 {
		t.Errorf("count = %d, want 0", s.Count)
	}
}

// TestDesktopDir: DesktopDir points into the platform's app-data location.
func TestDesktopDir(t *testing.T) {
	var want string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("no home: %v", err)
		}
		want = filepath.Join(home, "Library", "Application Support", "dev.spacingmind.desktop", "crashes")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			t.Skip("no APPDATA")
		}
		want = filepath.Join(appData, "dev.spacingmind.desktop", "crashes")
	default:
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				t.Skipf("no home: %v", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
		want = filepath.Join(base, "dev.spacingmind.desktop", "crashes")
	}
	if got := DesktopDir(); got != want {
		t.Errorf("DesktopDir() = %q, want %q", got, want)
	}
}
