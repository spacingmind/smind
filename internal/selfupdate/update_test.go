package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/selfupdate/selfupdatetest"
)

const testArch = "amd64"

// fakeDaemon scripts the daemon as the updater sees it.
type fakeDaemon struct {
	mu         sync.Mutex
	status     DaemonStatus      // current answer
	onRestart  func(*fakeDaemon) // mutates status when Restart is called
	restarts   []string          // exe paths Restart was called with
	restartErr error
}

func (d *fakeDaemon) Status(context.Context) (DaemonStatus, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status, nil
}

func (d *fakeDaemon) Restart(_ context.Context, exe string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.restarts = append(d.restarts, exe)
	if d.onRestart != nil {
		d.onRestart(d)
	}
	return d.restartErr
}

func (d *fakeDaemon) restartCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.restarts)
}

type testEnv struct {
	t      *testing.T
	srv    *selfupdatetest.Server
	dir    string
	exe    string
	log    *bytes.Buffer
	daemon *fakeDaemon
	u      *Updater
}

// newEnv installs a fake smind `current` in a temp dir and wires an Updater
// at a fresh fake release server (latest = latest, which is published).
func newEnv(t *testing.T, current, latest string) *testEnv {
	t.Helper()
	srv := selfupdatetest.New(t)
	if latest != "" {
		srv.Latest = latest
		srv.Add(t, latest, runtime.GOOS, testArch)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, BinaryName(runtime.GOOS))
	if err := os.WriteFile(exe, selfupdatetest.Binary(current), 0o755); err != nil {
		t.Fatal(err)
	}
	log := &bytes.Buffer{}
	d := &fakeDaemon{}
	return &testEnv{t: t, srv: srv, dir: dir, exe: exe, log: log, daemon: d, u: &Updater{
		Source:     &Source{BaseURL: srv.URL, Client: newHTTPClient()},
		Current:    current,
		Exe:        exe,
		GOOS:       runtime.GOOS,
		GOARCH:     testArch,
		Daemon:     d,
		ActiveWork: func(context.Context) (Activity, error) { return Activity{}, nil },
		RunVersion: selfupdatetest.RunVersion,
		// These tests are about swapping and guards; signatures are verify_test.go's.
		VerifyChecksums: func(context.Context, string, []byte, func(context.Context, string) ([]byte, error)) error { return nil },
		Getenv:          func(string) string { return "" },
		Exists:          func(string) bool { return false },
		InContainer:     func() bool { return false },
		HealthTimeout:   400 * time.Millisecond,
		HealthPoll:      10 * time.Millisecond,
		Log:             log,
	}}
}

func (e *testEnv) exeContent() string {
	e.t.Helper()
	b, err := os.ReadFile(e.exe)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *testEnv) wantInstalled(version string) {
	e.t.Helper()
	if got := e.exeContent(); got != string(selfupdatetest.Binary(version)) {
		e.t.Fatalf("installed binary = %q, want smind %s", got, version)
	}
}

// wantUntouched asserts the install dir holds exactly the original binary:
// byte-identical, with no staged file, .old or temp leftovers.
func (e *testEnv) wantUntouched(current string) {
	e.t.Helper()
	e.wantInstalled(current)
	entries, err := os.ReadDir(e.dir)
	if err != nil {
		e.t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, en := range entries {
			names = append(names, en.Name())
		}
		e.t.Fatalf("install dir = %v, want only %s", names, filepath.Base(e.exe))
	}
}

func wantRefusal(t *testing.T, err error, substr string) {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("err = %v (%T), want a *Refusal", err, err)
	}
	if !strings.Contains(r.Reason, substr) {
		t.Fatalf("refusal = %q, want it to mention %q", r.Reason, substr)
	}
}

func TestUpdateCheckReportsLatest(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	res, err := e.u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res != (CheckResult{Current: "0.11.1", Latest: "0.12.0", UpdateAvailable: true}) {
		t.Fatalf("Check = %+v", res)
	}
	e.u.Current = "0.12.0"
	if res, _ = e.u.Check(context.Background()); res.UpdateAvailable {
		t.Fatalf("Check at latest = %+v, want updateAvailable=false", res)
	}
	e.u.Current = "dev"
	if res, _ = e.u.Check(context.Background()); res.UpdateAvailable || res.Latest != "0.12.0" {
		t.Fatalf("Check on a dev build = %+v", res)
	}
}

func TestUpdateLatestUsesRedirectNotAPI(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	if _, err := e.u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	paths := e.srv.Paths()
	if len(paths) != 1 || paths[0] != "/releases/latest" {
		t.Fatalf("requests = %v, want exactly one GET /releases/latest (no API, no tag page)", paths)
	}
}

func TestUpdateLatestFailsWithoutARelease(t *testing.T) {
	e := newEnv(t, "0.11.1", "")
	if _, err := e.u.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "published release") {
		t.Fatalf("Check with no release: err = %v", err)
	}
}

func TestUpdateVerifiesChecksumAndInstalls(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	res, err := e.u.Update(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Update: %v\n%s", err, e.log)
	}
	if !res.Updated || res.Target != "0.12.0" || res.Current != "0.11.1" {
		t.Fatalf("result = %+v", res)
	}
	e.wantInstalled("0.12.0")
	if !strings.Contains(e.log.String(), "checksum verified") {
		t.Fatalf("log = %q, want a checksum line", e.log)
	}
	if _, err := os.Stat(OldPath(e.exe)); !os.IsNotExist(err) && !RenameAside {
		t.Fatalf("smind.old should be discarded after a successful update, stat err = %v", err)
	}
	entries, _ := os.ReadDir(e.dir)
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), ".smind.") {
			t.Fatalf("leftover staging file %s", en.Name())
		}
	}
}

func TestUpdateFollowsDownloadRedirects(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.srv.Redirect = true
	if _, err := e.u.Update(context.Background(), Options{}); err != nil {
		t.Fatalf("Update through redirects: %v", err)
	}
	e.wantInstalled("0.12.0")
}

func TestUpdateInstallsExplicitVersion(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.srv.Add(t, "0.11.5", runtime.GOOS, testArch)
	if _, err := e.u.Update(context.Background(), Options{Version: "v0.11.5"}); err != nil {
		t.Fatal(err)
	}
	e.wantInstalled("0.11.5")
}

func TestUpdateAlreadyUpToDateChangesNothing(t *testing.T) {
	e := newEnv(t, "0.12.0", "0.12.0")
	res, err := e.u.Update(context.Background(), Options{})
	if err != nil || res.Updated {
		t.Fatalf("Update at latest = %+v, %v; want a no-op", res, err)
	}
	e.wantUntouched("0.12.0")
	if !strings.Contains(e.log.String(), "already up to date") {
		t.Fatalf("log = %q", e.log)
	}
}

func TestUpdateChecksumMismatchLeavesBinaryIdentical(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.srv.SetChecksums("0.12.0", strings.Repeat("0", 64)+"  "+AssetName("0.12.0", runtime.GOOS, testArch)+"\n")
	_, err := e.u.Update(context.Background(), Options{})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
	e.wantUntouched("0.11.1")
}

func TestUpdateMissingAssetLeavesBinaryIdentical(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	// checksums.txt lists only another platform.
	e.srv.SetChecksums("0.12.0", strings.Repeat("a", 64)+"  smind_0.12.0_plan9_amd64.tar.gz\n")
	_, err := e.u.Update(context.Background(), Options{})
	if !errors.Is(err, ErrNoChecksum) || !strings.Contains(err.Error(), runtime.GOOS+"/"+testArch) {
		t.Fatalf("err = %v, want ErrNoChecksum naming the platform", err)
	}
	e.wantUntouched("0.11.1")
}

func TestUpdateAssetListedButNotServed(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	delete(e.srv.Assets, "0.12.0/"+AssetName("0.12.0", runtime.GOOS, testArch))
	_, err := e.u.Update(context.Background(), Options{})
	if !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("err = %v, want ErrAssetNotFound", err)
	}
	e.wantUntouched("0.11.1")
}

func TestUpdateUnknownReleaseIs404(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	_, err := e.u.Update(context.Background(), Options{Version: "9.9.9"})
	if !errors.Is(err, ErrAssetNotFound) || !strings.Contains(err.Error(), "v9.9.9") {
		t.Fatalf("err = %v, want a 404 naming the release", err)
	}
	e.wantUntouched("0.11.1")
}

func TestUpdateRejectsArchiveWithPathTraversal(t *testing.T) {
	e := newEnv(t, "0.11.1", "")
	e.srv.Latest = "0.12.0"
	e.srv.AddArchive(t, "0.12.0", runtime.GOOS, testArch, selfupdatetest.ArchiveOf(t, runtime.GOOS,
		selfupdatetest.File{Name: BinaryName(runtime.GOOS), Body: selfupdatetest.Binary("0.12.0")},
		selfupdatetest.File{Name: "../../evil", Body: []byte("x")}))
	_, err := e.u.Update(context.Background(), Options{})
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("err = %v, want ErrUnsafePath", err)
	}
	e.wantUntouched("0.11.1")
}

func TestUpdateRejectsMissingBinaryMember(t *testing.T) {
	e := newEnv(t, "0.11.1", "")
	e.srv.Latest = "0.12.0"
	e.srv.AddArchive(t, "0.12.0", runtime.GOOS, testArch, selfupdatetest.ArchiveOf(t, runtime.GOOS,
		selfupdatetest.File{Name: "LICENSE", Body: []byte("MIT")},
		selfupdatetest.File{Name: "nested/" + BinaryName(runtime.GOOS), Body: selfupdatetest.Binary("0.12.0")}))
	_, err := e.u.Update(context.Background(), Options{})
	if !errors.Is(err, ErrMissingBinary) {
		t.Fatalf("err = %v, want ErrMissingBinary (a nested member does not count)", err)
	}
	e.wantUntouched("0.11.1")
}

func TestUpdateRejectsOversizeMember(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			dir := t.TempDir()
			name := "a.tar.gz"
			if goos == "windows" {
				name = "a.zip"
			}
			archive := filepath.Join(dir, name)
			if err := os.WriteFile(archive, selfupdatetest.Archive(t, goos, BinaryName(goos), bytes.Repeat([]byte("x"), 4096)), 0o600); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(dir, "out")
			err := ExtractBinary(archive, BinaryName(goos), dst, 1024)
			if !errors.Is(err, ErrOversize) {
				t.Fatalf("ExtractBinary(limit 1024) = %v, want ErrOversize", err)
			}
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatalf("oversize member left %s behind (stat err = %v)", dst, err)
			}
			if err := ExtractBinary(archive, BinaryName(goos), dst, 4096); err != nil {
				t.Fatalf("ExtractBinary at exactly the limit: %v", err)
			}
		})
	}
}

func TestExtractZipAndTarRejectUnsafeNames(t *testing.T) {
	for _, bad := range []string{"../x", "a/../../x", "/abs", `..\x`, "C:/x", "a/../../../etc/passwd"} {
		for _, goos := range []string{"linux", "windows"} {
			dir := t.TempDir()
			archive := filepath.Join(dir, "a.tar.gz")
			if goos == "windows" {
				archive = filepath.Join(dir, "a.zip")
			}
			data := selfupdatetest.ArchiveOf(t, goos,
				selfupdatetest.File{Name: BinaryName(goos), Body: []byte("b")}, selfupdatetest.File{Name: bad, Body: []byte("x")})
			if err := os.WriteFile(archive, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := ExtractBinary(archive, BinaryName(goos), filepath.Join(dir, "out"), 1<<20); !errors.Is(err, ErrUnsafePath) {
				t.Errorf("%s archive with entry %q: err = %v, want ErrUnsafePath", goos, bad, err)
			}
		}
	}
}

func TestUpdateRefusesDevBuild(t *testing.T) {
	e := newEnv(t, "dev", "0.12.0")
	_, err := e.u.Update(context.Background(), Options{})
	wantRefusal(t, err, "not a release")
	e.wantUntouched("dev")
}

func TestUpdateRefusesDesktopManagedPath(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	managed := filepath.Join(e.dir, "dev.spacingmind.desktop", "managed-daemon", "bin")
	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	e.u.Exe = filepath.Join(managed, BinaryName(runtime.GOOS))
	_, err := e.u.Update(context.Background(), Options{})
	wantRefusal(t, err, "desktop app")
	if len(e.srv.Paths()) != 0 {
		t.Fatalf("a desktop-owned binary must be refused before any network call, saw %v", e.srv.Paths())
	}
}

func TestDesktopOwned(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	none := func(string) bool { return false }
	cases := []struct {
		name, exe, goos string
		getenv          func(string) string
		exists          func(string) bool
		want            bool
	}{
		{"mac app bundle sidecar", "/Applications/sMind.app/Contents/MacOS/smind", "darwin", env(nil), none, true},
		{"mac managed daemon", "/Users/me/Library/Application Support/dev.spacingmind.desktop/managed-daemon/bin/smind", "darwin", env(nil), none, true},
		{"mac user install", "/usr/local/bin/smind", "darwin", env(nil), none, false},
		{"linux user install", "/home/me/.local/bin/smind", "linux", env(nil), none, false},
		{"windows managed dir", `C:\Users\me\AppData\Local\smind\bin\smind.exe`, "windows", env(map[string]string{"LOCALAPPDATA": `C:\Users\me\AppData\Local`}), none, true},
		{"windows managed dir, other case", `c:\users\ME\appdata\local\SMIND\bin\smind.exe`, "windows", env(map[string]string{"LOCALAPPDATA": `C:\Users\me\AppData\Local`}), none, true},
		{"windows beside the app", `C:\Program Files\sMind\smind.exe`, "windows", env(nil), func(p string) bool { return p == `C:/Program Files/sMind/smind-desktop.exe` }, true},
		// A case-insensitive file system answers "exists" for sMind.exe whenever smind.exe is there:
		// that must not read as "the desktop app is next to this binary".
		{"windows standalone install on a case-insensitive fs", `C:\Users\me\AppData\Local\Programs\smind\smind.exe`, "windows", env(map[string]string{"LOCALAPPDATA": `C:\Users\me\AppData\Local`}), func(p string) bool {
			return strings.EqualFold(path.Base(p), "smind.exe") || strings.EqualFold(path.Base(p), "sMind.exe")
		}, false},
		{"windows install script dir", `C:\Users\me\AppData\Local\Programs\smind\smind.exe`, "windows", env(map[string]string{"LOCALAPPDATA": `C:\Users\me\AppData\Local`}), none, false},
	}
	for _, c := range cases {
		if reason, got := DesktopOwned(c.exe, c.goos, c.getenv, c.exists); got != c.want {
			t.Errorf("%s: DesktopOwned = %v (%q), want %v", c.name, got, reason, c.want)
		}
	}
}

func TestUpdateRefusesInsideContainer(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.u.InContainer = func() bool { return true }
	_, err := e.u.Update(context.Background(), Options{})
	wantRefusal(t, err, "pulling a new image")
	e.wantUntouched("0.11.1")
}

// Inside an image the answer is "pull a new image", even for a dev build.
func TestUpdateInsideAContainerSaysPullEvenForADevBuild(t *testing.T) {
	e := newEnv(t, "0.12.0-dev+abc1234", "0.12.0")
	e.u.InContainer = func() bool { return true }
	_, err := e.u.Update(context.Background(), Options{})
	wantRefusal(t, err, "pulling a new image")
}

func TestInContainer(t *testing.T) {
	exists := func(m ...string) func(string) bool {
		return func(p string) bool {
			for _, x := range m {
				if x == p {
					return true
				}
			}
			return false
		}
	}
	noenv := func(string) string { return "" }
	if !InContainer("linux", noenv, exists("/.dockerenv")) || !InContainer("linux", noenv, exists("/run/.containerenv")) {
		t.Error("docker/podman marker files should mean container")
	}
	if !InContainer("linux", func(k string) string {
		if k == "KUBERNETES_SERVICE_HOST" {
			return "10.0.0.1"
		}
		return ""
	}, exists()) {
		t.Error("KUBERNETES_SERVICE_HOST should mean container")
	}
	if InContainer("linux", noenv, exists()) || InContainer("darwin", noenv, exists("/.dockerenv")) {
		t.Error("a plain host is not a container")
	}
}

func TestUpdateRefusesDowngradeWithoutForce(t *testing.T) {
	e := newEnv(t, "0.12.0", "0.12.0")
	e.srv.Add(t, "0.10.0", runtime.GOOS, testArch)
	_, err := e.u.Update(context.Background(), Options{Version: "0.10.0"})
	wantRefusal(t, err, "--force")
	e.wantUntouched("0.12.0")

	if _, err := e.u.Update(context.Background(), Options{Version: "0.10.0", Force: true}); err != nil {
		t.Fatalf("forced downgrade: %v", err)
	}
	e.wantInstalled("0.10.0")
}

func TestUpdateForceReinstallsSameVersion(t *testing.T) {
	e := newEnv(t, "0.12.0", "0.12.0")
	res, err := e.u.Update(context.Background(), Options{Force: true})
	if err != nil || !res.Updated {
		t.Fatalf("Update --force at latest = %+v, %v", res, err)
	}
}

func TestUpdateRefusesUnwritableDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permission bits are not enforced here")
	}
	e := newEnv(t, "0.11.1", "0.12.0")
	if err := os.Chmod(e.dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.dir, 0o755) })
	_, err := e.u.Update(context.Background(), Options{})
	wantRefusal(t, err, "not writable")
	wantRefusal(t, err, "permission denied")
	e.wantInstalled("0.11.1")
}

func TestUpdateRefusesWhileRunsActive(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.u.ActiveWork = func(context.Context) (Activity, error) { return Activity{DaemonRunning: true, RunningRuns: 2}, nil }
	_, err := e.u.Update(context.Background(), Options{})
	wantRefusal(t, err, "2 run(s) are running")
	e.wantUntouched("0.11.1")
	for _, p := range e.srv.Paths() {
		if strings.Contains(p, "/download/") {
			t.Fatalf("an active-work refusal must come before any download, saw %v", e.srv.Paths())
		}
	}
}

func TestUpdateRefusesWhenDaemonCannotBeAsked(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.u.ActiveWork = func(context.Context) (Activity, error) {
		return Activity{DaemonRunning: true}, errors.New("token rejected")
	}
	_, err := e.u.Update(context.Background(), Options{})
	wantRefusal(t, err, "token rejected")
	e.wantUntouched("0.11.1")
	if _, err := e.u.Update(context.Background(), Options{Force: true}); err != nil {
		t.Fatalf("--force should skip the check: %v", err)
	}
}

func TestUpdateForceOverridesActiveRuns(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.u.ActiveWork = func(context.Context) (Activity, error) {
		t.Error("--force must not even ask the daemon")
		return Activity{RunningRuns: 5}, nil
	}
	if _, err := e.u.Update(context.Background(), Options{Force: true}); err != nil {
		t.Fatal(err)
	}
	e.wantInstalled("0.12.0")
}

func TestUpdateNoDaemonRunningIsFine(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	res, err := e.u.Update(context.Background(), Options{})
	if err != nil || !res.Updated || res.DaemonRestarted || res.DaemonNeedsRestart {
		t.Fatalf("Update with no daemon = %+v, %v", res, err)
	}
	if e.daemon.restartCount() != 0 {
		t.Fatal("must not restart a daemon that is not running")
	}
}

func TestUpdateAtomicSwapKeepsOldUntilVerified(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	var sawOld string
	calls := 0
	e.u.RunVersion = func(ctx context.Context, exe string) (string, error) {
		calls++
		if calls == 2 { // the post-swap check on the installed binary
			b, err := os.ReadFile(OldPath(e.exe))
			if err != nil {
				t.Errorf("the old binary must exist while the new one is being verified: %v", err)
			}
			sawOld = string(b)
		}
		return selfupdatetest.RunVersion(ctx, exe)
	}
	if _, err := e.u.Update(context.Background(), Options{}); err != nil {
		t.Fatal(err)
	}
	if sawOld != string(selfupdatetest.Binary("0.11.1")) {
		t.Fatalf("smind.old during verification = %q, want the previous binary", sawOld)
	}
	if calls != 2 {
		t.Fatalf("version probe ran %d times, want 2 (staged + installed)", calls)
	}
}

func TestUpdateStagedBinaryVersionMismatchIsNeverInstalled(t *testing.T) {
	e := newEnv(t, "0.11.1", "")
	e.srv.Latest = "0.12.0"
	// The archive claims 0.12.0 but its binary reports 0.9.9.
	e.srv.AddArchive(t, "0.12.0", runtime.GOOS, testArch, selfupdatetest.Archive(t, runtime.GOOS, BinaryName(runtime.GOOS), selfupdatetest.Binary("0.9.9")))
	_, err := e.u.Update(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "reports version") {
		t.Fatalf("err = %v", err)
	}
	e.wantUntouched("0.11.1")
}

func TestUpdateRollsBackWhenNewBinaryVersionMismatches(t *testing.T) {
	for _, aside := range []bool{false, true} {
		t.Run(map[bool]string{false: "atomic", true: "rename-aside"}[aside], func(t *testing.T) {
			e := newEnv(t, "0.11.1", "0.12.0")
			e.u.Aside = &aside
			calls := 0
			e.u.RunVersion = func(ctx context.Context, exe string) (string, error) {
				calls++
				if calls == 2 {
					return "0.0.1", nil // the installed binary misreports itself
				}
				return selfupdatetest.RunVersion(ctx, exe)
			}
			_, err := e.u.Update(context.Background(), Options{})
			if err == nil || !strings.Contains(err.Error(), "rolled back") {
				t.Fatalf("err = %v, want a rollback", err)
			}
			e.wantUntouched("0.11.1")
		})
	}
}

func TestUpdateDryRunChangesNothing(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.daemon.status = DaemonStatus{Running: true, Version: "0.11.1", Managed: true}
	res, err := e.u.Update(context.Background(), Options{DryRun: true})
	if err != nil || res.Updated || !res.DryRun {
		t.Fatalf("dry run = %+v, %v", res, err)
	}
	e.wantUntouched("0.11.1")
	if e.daemon.restartCount() != 0 {
		t.Fatal("a dry run must not restart anything")
	}
	if !strings.Contains(e.log.String(), "dry run") {
		t.Fatalf("log = %q", e.log)
	}
}

func TestUpdateDryRunStillVerifies(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.srv.SetChecksums("0.12.0", strings.Repeat("0", 64)+"  "+AssetName("0.12.0", runtime.GOOS, testArch)+"\n")
	if _, err := e.u.Update(context.Background(), Options{DryRun: true}); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("dry run err = %v, want the checksum failure", err)
	}
}

func TestUpdateRestartsServiceAndWaitsForHealthzVersion(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.daemon.status = DaemonStatus{Running: true, Version: "0.11.1", Managed: true}
	polls := 0
	e.daemon.onRestart = func(d *fakeDaemon) { d.status = DaemonStatus{} } // down, then...
	e.u.Daemon = &scriptedDaemon{fakeDaemon: e.daemon, after: func(d *fakeDaemon) {
		polls++
		switch {
		case polls == 3:
			d.status = DaemonStatus{Running: true, Version: "0.11.1", Managed: true} // old one still answering
		case polls >= 5:
			d.status = DaemonStatus{Running: true, Version: "0.12.0", Managed: true}
		}
	}}
	res, err := e.u.Update(context.Background(), Options{})
	if err != nil || !res.Updated || !res.DaemonRestarted {
		t.Fatalf("Update = %+v, %v\n%s", res, err, e.log)
	}
	if got := e.daemon.restarts; len(got) != 1 || got[0] != e.exe {
		t.Fatalf("restarts = %v, want one restart through the installed binary %s", got, e.exe)
	}
	e.wantInstalled("0.12.0")
}

// scriptedDaemon wraps fakeDaemon, running `after` on every Status call made
// after the first restart (the poll loop), so a test can script the sequence.
type scriptedDaemon struct {
	*fakeDaemon
	after func(*fakeDaemon)
}

func (s *scriptedDaemon) Status(ctx context.Context) (DaemonStatus, error) {
	if s.restartCount() > 0 {
		s.fakeDaemon.mu.Lock()
		s.after(s.fakeDaemon)
		s.fakeDaemon.mu.Unlock()
	}
	return s.fakeDaemon.Status(ctx)
}

func TestUpdateRollsBackWhenHealthzNeverReportsNewVersion(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.daemon.status = DaemonStatus{Running: true, Version: "0.11.1", Managed: true}
	// The restart "succeeds" but the daemon keeps reporting the old version.
	res, err := e.u.Update(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "rolled back") || !strings.Contains(err.Error(), "still reports version 0.11.1") {
		t.Fatalf("err = %v, want a healthz-version rollback", err)
	}
	if res.Updated {
		t.Fatalf("result claims Updated after a rollback: %+v", res)
	}
	e.wantUntouched("0.11.1")
	if e.daemon.restartCount() != 2 {
		t.Fatalf("restarts = %d, want 2 (onto the new binary, then back onto the old one)", e.daemon.restartCount())
	}
}

func TestUpdateRollsBackWhenServiceRestartFails(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.daemon.status = DaemonStatus{Running: true, Version: "0.11.1", Managed: true}
	e.daemon.restartErr = errors.New("systemctl: unit not found")
	_, err := e.u.Update(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "unit not found") || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("err = %v", err)
	}
	e.wantUntouched("0.11.1")
}

func TestUpdateDoesNotRespawnManualDaemon(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.daemon.status = DaemonStatus{Running: true, Version: "0.11.1", Managed: false}
	res, err := e.u.Update(context.Background(), Options{})
	if err != nil || !res.Updated || res.DaemonRestarted || !res.DaemonNeedsRestart {
		t.Fatalf("Update = %+v, %v", res, err)
	}
	if e.daemon.restartCount() != 0 {
		t.Fatal("a hand-started daemon must never be restarted by update")
	}
	if !strings.Contains(e.log.String(), "smind stop && smind serve") {
		t.Fatalf("log = %q, want restart instructions", e.log)
	}
}

func TestUpdateSignatureHookRunsBeforeAnyDigestIsTrusted(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	var gotChecksums string
	e.u.VerifyChecksums = func(_ context.Context, _ string, checksums []byte, fetch func(context.Context, string) ([]byte, error)) error {
		gotChecksums = string(checksums)
		return errors.New("bad signature")
	}
	_, err := e.u.Update(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "bad signature") {
		t.Fatalf("err = %v, want the verifier's error", err)
	}
	if !strings.Contains(gotChecksums, AssetName("0.12.0", runtime.GOOS, testArch)) {
		t.Fatalf("verifier saw %q", gotChecksums)
	}
	e.wantUntouched("0.11.1")
	for _, p := range e.srv.Paths() {
		if strings.HasSuffix(p, ".tar.gz") || strings.HasSuffix(p, ".zip") {
			t.Fatalf("the archive was downloaded despite a failed signature check: %v", e.srv.Paths())
		}
	}
}

func TestSwapAndRestoreBothStrategies(t *testing.T) {
	for _, aside := range []bool{false, true} {
		t.Run(map[bool]string{false: "atomic", true: "rename-aside"}[aside], func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "smind")
			staged := filepath.Join(dir, ".smind.new")
			must(t, os.WriteFile(exe, []byte("old"), 0o755))
			must(t, os.WriteFile(staged, []byte("new"), 0o755))

			must(t, Swap(exe, staged, aside))
			if read(t, exe) != "new" || read(t, OldPath(exe)) != "old" {
				t.Fatalf("after Swap: exe=%q old=%q", read(t, exe), read(t, OldPath(exe)))
			}
			if _, err := os.Stat(staged); !os.IsNotExist(err) {
				t.Fatalf("staged file should be consumed, stat err = %v", err)
			}

			must(t, Restore(exe, aside))
			if read(t, exe) != "old" {
				t.Fatalf("after Restore: exe=%q", read(t, exe))
			}
			if _, err := os.Stat(OldPath(exe)); !os.IsNotExist(err) {
				t.Fatalf(".old should be consumed by Restore, stat err = %v", err)
			}
			if _, err := os.Stat(failedPath(exe)); aside && err == nil {
				t.Fatalf("the rejected binary should be cleaned up when it is not locked")
			}
		})
	}
}

func TestSwapReplacesAStaleOldFile(t *testing.T) {
	for _, aside := range []bool{false, true} {
		dir := t.TempDir()
		exe, staged := filepath.Join(dir, "smind"), filepath.Join(dir, ".new")
		must(t, os.WriteFile(exe, []byte("v2"), 0o755))
		must(t, os.WriteFile(staged, []byte("v3"), 0o755))
		must(t, os.WriteFile(OldPath(exe), []byte("v1 from an earlier update"), 0o755))
		must(t, Swap(exe, staged, aside))
		if read(t, OldPath(exe)) != "v2" {
			t.Fatalf("aside=%v: .old = %q, want the binary just replaced", aside, read(t, OldPath(exe)))
		}
	}
}

func TestSwapFailureLeavesExeUntouched(t *testing.T) {
	for _, aside := range []bool{false, true} {
		dir := t.TempDir()
		exe := filepath.Join(dir, "smind")
		must(t, os.WriteFile(exe, []byte("old"), 0o755))
		err := Swap(exe, filepath.Join(dir, "missing-staged"), aside)
		if err == nil {
			t.Fatalf("aside=%v: Swap with a missing staged file succeeded", aside)
		}
		if read(t, exe) != "old" {
			t.Fatalf("aside=%v: exe = %q after a failed Swap", aside, read(t, exe))
		}
		if _, err := os.Stat(OldPath(exe)); !os.IsNotExist(err) {
			t.Fatalf("aside=%v: a failed Swap left %s behind", aside, OldPath(exe))
		}
	}
}

func TestCleanupStaleRemovesLeftovers(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "smind.exe")
	must(t, os.WriteFile(OldPath(exe), []byte("o"), 0o755))
	must(t, os.WriteFile(failedPath(exe), []byte("f"), 0o755))
	CleanupStale(exe)
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("leftovers remain: %v", entries)
	}
	CleanupStale(exe) // idempotent, no panic on missing files
}

func TestUpdateWindowsStyleUpdateReplacesRunningBinary(t *testing.T) {
	// Full flow through the rename-aside strategy (the Windows path) on any OS.
	e := newEnv(t, "0.11.1", "0.12.0")
	aside := true
	e.u.Aside = &aside
	res, err := e.u.Update(context.Background(), Options{})
	if err != nil || !res.Updated {
		t.Fatalf("Update = %+v, %v", res, err)
	}
	e.wantInstalled("0.12.0")
	// Discard is best effort on Windows; here the file is deletable, so both outcomes are fine,
	// but a surviving .old must be the previous binary.
	if b, err := os.ReadFile(OldPath(e.exe)); err == nil && string(b) != string(selfupdatetest.Binary("0.11.1")) {
		t.Fatalf(".old = %q", b)
	}
}

func TestExecVersionParsesRealBinaryOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "smind")
	must(t, os.WriteFile(script, []byte("#!/bin/sh\n[ \"$1\" = version ] && echo 'smind 1.2.3 (abc1234)'\n"), 0o755))
	got, err := execVersion(context.Background(), script)
	if err != nil || got != "1.2.3" {
		t.Fatalf("execVersion = %q, %v", got, err)
	}
	must(t, os.WriteFile(script, []byte("#!/bin/sh\necho 'something else'\n"), 0o755))
	if _, err := execVersion(context.Background(), script); err == nil {
		t.Fatal("execVersion accepted output that is not `smind <version>`")
	}
	must(t, os.WriteFile(script, []byte("#!/bin/sh\nexit 3\n"), 0o755))
	if _, err := execVersion(context.Background(), script); err == nil {
		t.Fatal("execVersion ignored a non-zero exit")
	}
}

func TestHealthzDaemonStatus(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    DaemonStatus
	}{
		{"plain smind", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"ok","service":"smind","version":"0.11.1"}`))
		}, DaemonStatus{Running: true, Version: "0.11.1"}},
		{"service-managed", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"ok","version":"0.12.0","managedBy":"systemd"}`))
		}, DaemonStatus{Running: true, Version: "0.12.0", Managed: true}},
		{"container-managed is not restartable by update", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"ok","version":"0.12.0","managedBy":"docker"}`))
		}, DaemonStatus{Running: true, Version: "0.12.0"}},
		{"something else owns the port", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
			DaemonStatus{Running: true}},
	}
	for _, c := range cases {
		srv := httptest.NewServer(c.handler)
		got, err := (&HealthzDaemon{BaseURL: srv.URL}).Status(context.Background())
		srv.Close()
		if err != nil || got != c.want {
			t.Errorf("%s: Status = %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}
	// Nothing listening.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if got, err := (&HealthzDaemon{BaseURL: url}).Status(context.Background()); err != nil || got.Running {
		t.Errorf("closed port: Status = %+v, %v; want not running", got, err)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.11.1", "0.12.0", -1}, {"0.12.0", "0.11.1", 1}, {"1.0.0", "1.0.0", 0}, {"v1.0.0", "1.0.0", 0},
		{"0.9.0", "0.10.0", -1}, {"1.0.0-rc.1", "1.0.0", -1}, {"1.0.0-rc.1", "1.0.0-rc.2", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1}, {"1.0.0-2", "1.0.0-alpha", -1}, {"1.0.0+b1", "1.0.0+b2", 0},
	}
	for _, c := range cases {
		got, err := CompareVersions(c.a, c.b)
		if err != nil || got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, %v; want %d", c.a, c.b, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "dev", "1.0", "1.0.0.0", "01.0.0", "1.0.0-", "../1.0.0", "1.0.0/x"} {
		if ValidVersion(bad) {
			t.Errorf("ValidVersion(%q) = true", bad)
		}
	}
}

func TestExpectedChecksum(t *testing.T) {
	good := strings.Repeat("ab", 32)
	other := strings.Repeat("cd", 32)
	sums := []byte(good + "  smind_1_linux_amd64.tar.gz\n" + other + " *smind_1_windows_amd64.zip\n\n")
	if got, err := ExpectedChecksum(sums, "smind_1_linux_amd64.tar.gz"); err != nil || got != good {
		t.Fatalf("text-mode line: %q, %v", got, err)
	}
	if got, err := ExpectedChecksum(sums, "smind_1_windows_amd64.zip"); err != nil || got != other {
		t.Fatalf("binary-mode (*) line: %q, %v", got, err)
	}
	if _, err := ExpectedChecksum(sums, "nope"); !errors.Is(err, ErrNoChecksum) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := ExpectedChecksum([]byte(good+"  a\n"+other+"  a\n"), "a"); err == nil {
		t.Fatal("a name listed twice with different digests must be rejected")
	}
	if _, err := ExpectedChecksum([]byte("short  a\n"), "a"); err == nil {
		t.Fatal("a malformed digest must be rejected")
	}
	if _, err := ExpectedChecksum([]byte(strings.ToUpper(good)+"  a\n"), "a"); err != nil {
		t.Fatalf("an upper-case digest is still a digest: %v", err)
	}
}

func TestUpdateHonoursHTTPSProxy(t *testing.T) {
	var mu sync.Mutex
	var connects []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connects = append(connects, r.Method+" "+r.Host)
		mu.Unlock()
		http.Error(w, "no route", http.StatusBadGateway)
	}))
	defer proxy.Close()

	// net/http reads the proxy environment once per process, so the child
	// process is what proves our client is wired to it.
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProxyProcess$")
	cmd.Env = append(filteredEnv(), "SMIND_PROXY_HELPER=1", "HTTPS_PROXY="+proxy.URL, "https_proxy="+proxy.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(connects) == 0 || connects[0] != "CONNECT releases.example.invalid:443" {
		t.Fatalf("proxy saw %v, want a CONNECT to releases.example.invalid:443", connects)
	}
}

// TestHelperProxyProcess is the child of TestUpdateHonoursHTTPSProxy; it does
// nothing in a normal test run.
func TestHelperProxyProcess(t *testing.T) {
	if os.Getenv("SMIND_PROXY_HELPER") != "1" {
		t.Skip("helper process")
	}
	s := &Source{BaseURL: "https://releases.example.invalid/spacingmind/smind", Client: newHTTPClient()}
	_, _ = s.LatestVersion(context.Background()) // fails at the proxy; only the CONNECT matters
}

func filteredEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "ALL_PROXY", "REQUEST_METHOD":
			continue
		}
		env = append(env, kv)
	}
	return env
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestValidVersionRejectsPathTricksInBuildMetadata(t *testing.T) {
	for _, bad := range []string{"1.0.0+x/../../../tmp/pwn", "1.0.0+..", "1.0.0+", "1.0.0+a b", `1.0.0+a\b`, "1.0.0-rc/1"} {
		if ValidVersion(bad) {
			t.Errorf("ValidVersion(%q) = true", bad)
		}
	}
	for _, good := range []string{"1.0.0+build.5", "0.12.0-dev+abc1234.dirty", "1.2.3-rc.1"} {
		if !ValidVersion(good) {
			t.Errorf("ValidVersion(%q) = false", good)
		}
	}
	e := newEnv(t, "0.11.1", "0.12.0")
	_, err := e.u.Update(context.Background(), Options{Version: "1.0.0+x/../../../../tmp/pwn"})
	wantRefusal(t, err, "not a valid version")
	e.wantUntouched("0.11.1")
}

func TestIsDevBuildCoversWhatTaskBuildStamps(t *testing.T) {
	for _, dev := range []string{"dev", "0.12.0-dev+abc1234", "0.12.0-dev+abc1234.dirty", "0.12.0+abc", "0.12.0-dev.3", "garbage"} {
		if !IsDevBuild(dev) {
			t.Errorf("IsDevBuild(%q) = false", dev)
		}
	}
	for _, rel := range []string{"0.11.1", "v1.0.0", "1.0.0-rc.1"} {
		if IsDevBuild(rel) {
			t.Errorf("IsDevBuild(%q) = true", rel)
		}
	}
	e := newEnv(t, "0.11.0-dev+abc1234.dirty", "0.12.0")
	_, err := e.u.Update(context.Background(), Options{})
	wantRefusal(t, err, "not a release")
	e.wantUntouched("0.11.0-dev+abc1234.dirty")
	if res, err := e.u.Check(context.Background()); err != nil || res.UpdateAvailable {
		t.Fatalf("Check on a task-build binary = %+v, %v; want updateAvailable=false", res, err)
	}
}

func TestUpdateRechecksActiveRunsJustBeforeSwap(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	calls := 0
	e.u.ActiveWork = func(context.Context) (Activity, error) {
		calls++
		if calls == 1 {
			return Activity{DaemonRunning: true}, nil // idle when we started downloading...
		}
		return Activity{DaemonRunning: true, RunningRuns: 1}, nil // ...busy once the download is done
	}
	_, err := e.u.Update(context.Background(), Options{})
	wantRefusal(t, err, "1 run(s) are running")
	e.wantUntouched("0.11.1")
	if calls != 2 {
		t.Fatalf("ActiveWork called %d times, want 2 (before download, before swap)", calls)
	}
}

type errDaemon struct{ *fakeDaemon }

func (*errDaemon) Status(context.Context) (DaemonStatus, error) {
	return DaemonStatus{}, errors.New("healthz timed out")
}

func TestUpdateRefusesWhenDaemonProbeFailsUnlessForced(t *testing.T) {
	e := newEnv(t, "0.11.1", "0.12.0")
	e.u.Daemon = &errDaemon{e.daemon}
	_, err := e.u.Update(context.Background(), Options{})
	wantRefusal(t, err, "healthz timed out")
	e.wantUntouched("0.11.1")
	if _, err := e.u.Update(context.Background(), Options{Force: true}); err != nil {
		t.Fatalf("--force: %v", err)
	}
}

func TestHealthzDaemonHungDaemonIsNotNoDaemon(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	d := &HealthzDaemon{BaseURL: srv.URL, Client: &http.Client{Timeout: 100 * time.Millisecond}}
	if st, err := d.Status(context.Background()); err == nil || st.Running {
		t.Fatalf("hung daemon: Status = %+v, %v; want an error, not \"not running\"", st, err)
	}
}

func TestPreserveKeepsRollbackCopyOutOfCleanupsReach(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "smind.exe")
	must(t, os.WriteFile(OldPath(exe), []byte("previous"), 0o755))
	kept := Preserve(exe)
	CleanupStale(exe)
	if read(t, kept) != "previous" {
		t.Fatalf("preserved copy at %s lost", kept)
	}
	if _, err := os.Stat(OldPath(exe)); !os.IsNotExist(err) {
		t.Fatal(".old should have been moved")
	}
}

func TestNoHTTPSDowngradeRedirect(t *testing.T) {
	mk := func(u string) *http.Request { r, _ := http.NewRequest("GET", u, nil); return r }
	if err := noHTTPSDowngrade(mk("http://evil.example/x"), []*http.Request{mk("https://github.com/x")}); err == nil {
		t.Error("https -> http redirect was allowed")
	}
	if err := noHTTPSDowngrade(mk("https://objects.example/x"), []*http.Request{mk("https://github.com/x")}); err != nil {
		t.Errorf("https -> https refused: %v", err)
	}
	if err := noHTTPSDowngrade(mk("http://127.0.0.1/y"), []*http.Request{mk("http://127.0.0.1/x")}); err != nil {
		t.Errorf("http -> http (test seam) refused: %v", err)
	}
	via := make([]*http.Request, maxRedirects)
	for i := range via {
		via[i] = mk("https://a.example/")
	}
	if err := noHTTPSDowngrade(mk("https://a.example/"), via); err == nil {
		t.Error("redirect loop was not stopped")
	}
}

func TestExtractMemberEdgeCases(t *testing.T) {
	write := func(t *testing.T, data []byte) string {
		p := filepath.Join(t.TempDir(), "a.tar.gz")
		must(t, os.WriteFile(p, data, 0o600))
		return p
	}
	dst := func(a string) string { return filepath.Join(filepath.Dir(a), "out") }

	a := write(t, selfupdatetest.ArchiveOf(t, "linux", selfupdatetest.File{Name: "./smind", Body: []byte("bin")}))
	if err := ExtractBinary(a, "smind", dst(a), 1<<20); err != nil || read(t, dst(a)) != "bin" {
		t.Fatalf("./smind should be accepted: %v", err)
	}

	a = write(t, selfupdatetest.ArchiveOf(t, "linux",
		selfupdatetest.File{Name: "smind", Body: []byte("one")}, selfupdatetest.File{Name: "smind", Body: []byte("two")}))
	if err := ExtractBinary(a, "smind", dst(a), 1<<20); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("duplicate member: %v", err)
	}
	if _, err := os.Stat(dst(a)); !os.IsNotExist(err) {
		t.Fatal("a rejected archive left the destination behind")
	}

	a = write(t, selfupdatetest.ArchiveOf(t, "linux", selfupdatetest.File{Name: "SMIND", Body: []byte("x")}))
	if err := ExtractBinary(a, "smind", dst(a), 1<<20); !errors.Is(err, ErrMissingBinary) {
		t.Fatalf("case-different member: %v", err)
	}

	// A symlink named smind must not be treated as the binary.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	must(t, tw.WriteHeader(&tar.Header{Name: "smind", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}))
	must(t, tw.Close())
	must(t, gz.Close())
	a = write(t, buf.Bytes())
	if err := ExtractBinary(a, "smind", dst(a), 1<<20); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink member: %v", err)
	}
}
