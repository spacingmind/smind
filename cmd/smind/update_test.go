package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/selfupdate"
	"github.com/spacingmind/smind/internal/selfupdate/selfupdatetest"
)

// fakeUpdateDaemon is a loopback daemon that speaks just enough of the real
// one: GET /healthz and a /ws that answers run.list with a canned result.
type fakeUpdateDaemon struct {
	srv *httptest.Server

	mu     sync.Mutex
	health string
	runs   string // JSON array returned by run.list
	token  string // token /ws accepts
	calls  []string
}

func newFakeUpdateDaemon(t *testing.T, home, runsJSON string) *fakeUpdateDaemon {
	t.Helper()
	token, err := auth.LoadOrCreateToken(home)
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeUpdateDaemon{health: `{"status":"ok","service":"smind","version":"0.11.1"}`, runs: runsJSON, token: token}
	up := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		_, _ = w.Write([]byte(d.health))
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		want := d.token
		d.mu.Unlock()
		if r.URL.Query().Get("token") != want {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		for {
			var env struct {
				ID     string          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := c.ReadJSON(&env); err != nil {
				return
			}
			d.mu.Lock()
			d.calls = append(d.calls, env.Method+" "+string(env.Params))
			runs := d.runs
			d.mu.Unlock()
			_ = c.WriteJSON(map[string]any{"id": env.ID, "result": json.RawMessage(runs)})
		}
	})
	d.srv = httptest.NewServer(mux)
	t.Cleanup(d.srv.Close)
	writeTestConfig(t, home, d.srv.URL)
	return d
}

func (d *fakeUpdateDaemon) rpcCalls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

// updateCLIEnv is a fake release server + an installed fake smind, wired into
// cmdUpdate through SMIND_UPDATE_BASE_URL and updaterHook.
type updateCLIEnv struct {
	home, exe string
	srv       *selfupdatetest.Server
	signer    *selfupdatetest.Signer
	// baseHook is updaterHook minus the verifier swap: cmdUpdate's own wiring.
	baseHook func(*selfupdate.Updater)
}

func newUpdateCLIEnv(t *testing.T, current, latest string) *updateCLIEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srv := selfupdatetest.New(t)
	srv.Latest = latest
	srv.Add(t, latest, runtime.GOOS, "amd64")
	signer := selfupdatetest.NewSigner(t)
	srv.Signer = signer
	t.Setenv(selfupdate.BaseURLEnv, srv.URL)

	exe := filepath.Join(t.TempDir(), selfupdate.BinaryName(runtime.GOOS))
	if err := os.WriteFile(exe, selfupdatetest.Binary(current), 0o755); err != nil {
		t.Fatal(err)
	}
	base := func(u *selfupdate.Updater) {
		u.Current, u.Exe = current, exe
		u.GOARCH = "amd64"
		u.RunVersion = selfupdatetest.RunVersion
		u.InContainer = func() bool { return false } // CI itself may be a container
	}
	updaterHook = func(u *selfupdate.Updater) {
		base(u)
		// The production wiring checks the embedded release key; the fake
		// release is signed with a test key, so swap the key (not the check).
		u.VerifyChecksums = selfupdate.SignatureVerifier(signer.PublicKey())
	}
	t.Cleanup(func() { updaterHook = nil })
	return &updateCLIEnv{home: home, exe: exe, srv: srv, signer: signer, baseHook: base}
}

func (e *updateCLIEnv) installed(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.exe)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(strings.TrimPrefix(string(b), "FAKE-SMIND "))
}

// runUpdate runs `smind update <args>` and returns its exit code and output.
func runUpdate(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	stderr = captureStderr(t, func() {
		stdout = captureStdout(t, func() { code = run(append([]string{"update"}, args...)) })
	})
	return code, stdout, stderr
}

func TestUpdateCheckReportsLatestAndExitsZero(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	code, out, errOut := runUpdate(t, "--check")
	if code != 0 {
		t.Fatalf("--check = %d, want 0; stderr: %s", code, errOut)
	}
	for _, want := range []string{"current: 0.11.1", "latest:  0.12.0", "update available"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want %q", out, want)
		}
	}
	if got := e.installed(t); got != "0.11.1" {
		t.Fatalf("--check changed the binary to %s", got)
	}

	// Same version: still exit 0.
	newUpdateCLIEnv(t, "0.12.0", "0.12.0")
	code, out, _ = runUpdate(t, "--check")
	if code != 0 || !strings.Contains(out, "up to date") {
		t.Fatalf("--check at latest = %d, %q; want 0 and \"up to date\"", code, out)
	}
}

func TestUpdateCheckJSON(t *testing.T) {
	newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	code, out, errOut := runUpdate(t, "--check", "--json")
	if code != 0 {
		t.Fatalf("code = %d; %s", code, errOut)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if got["current"] != "0.11.1" || got["latest"] != "0.12.0" || got["updateAvailable"] != true || len(got) != 3 {
		t.Fatalf("--check --json = %v, want exactly {current, latest, updateAvailable}", got)
	}
}

func TestUpdateCheckFailsLoudlyWhenTheReleaseServerIsDown(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	e.srv.Latest = ""
	code, _, errOut := runUpdate(t, "--check")
	if code == 0 || !strings.Contains(errOut, "smind update:") {
		t.Fatalf("--check with no release = %d, %q; want a non-zero exit with a reason", code, errOut)
	}
}

func TestUpdateInstallsAndReportsJSON(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	port := freePort(t)
	writePortConfig(t, e.home, port) // no daemon on it
	code, out, errOut := runUpdate(t, "--json")
	if code != 0 {
		t.Fatalf("update = %d; stderr: %s", code, errOut)
	}
	if got := e.installed(t); got != "0.12.0" {
		t.Fatalf("installed = %s, want 0.12.0", got)
	}
	var res selfupdate.UpdateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("stdout is not one JSON document (progress belongs on stderr): %v\n%s", err, out)
	}
	if !res.Updated || res.Target != "0.12.0" || res.DaemonRestarted {
		t.Fatalf("result = %+v", res)
	}
}

func TestUpdateNoDaemonRunningIsFine(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	writePortConfig(t, e.home, freePort(t))
	code, out, errOut := runUpdate(t)
	if code != 0 {
		t.Fatalf("update with no daemon = %d; stderr: %s", code, errOut)
	}
	if strings.Contains(out, "smind stop") {
		t.Fatalf("stdout = %q: no daemon is running, there is nothing to restart", out)
	}
	if got := e.installed(t); got != "0.12.0" {
		t.Fatalf("installed = %s", got)
	}
}

func TestUpdateRefusesWhileRunsActive(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	d := newFakeUpdateDaemon(t, e.home, `[{"ID":"r1","Status":"running"},{"ID":"r2","Status":"running"}]`)
	code, _, errOut := runUpdate(t)
	if code == 0 || !strings.Contains(errOut, "refused") || !strings.Contains(errOut, "2 run(s) are running") {
		t.Fatalf("update = %d, stderr %q; want a refusal naming the 2 runs", code, errOut)
	}
	if got := e.installed(t); got != "0.11.1" {
		t.Fatalf("a refused update changed the binary to %s", got)
	}
	calls := d.rpcCalls()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "run.list ") || !strings.Contains(calls[0], `"status":"running"`) {
		t.Fatalf("daemon calls = %v, want one run.list restricted to running runs", calls)
	}
}

// A run blocked on a permission prompt is a running run (permissions pend
// inside a run; run.list has no separate state for them), so the same query
// protects it: the fake reports the run the way the daemon does while it
// waits on the user.
func TestUpdateRefusesWhilePermissionPending(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	newFakeUpdateDaemon(t, e.home, `[{"ID":"r-awaiting-permission","Status":"running","Provider":"claude-native","PermissionMode":"default"}]`)
	code, _, errOut := runUpdate(t)
	if code == 0 || !strings.Contains(errOut, "1 run(s) are running (a run waiting on a permission counts)") {
		t.Fatalf("update = %d, stderr %q", code, errOut)
	}
	if got := e.installed(t); got != "0.11.1" {
		t.Fatalf("binary = %s", got)
	}
}

func TestUpdateIgnoresFinishedRuns(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	newFakeUpdateDaemon(t, e.home, `[{"ID":"r1","Status":"done"},{"ID":"r2","Status":"stopped"}]`)
	code, out, errOut := runUpdate(t)
	if code != 0 {
		t.Fatalf("update = %d; stderr: %s", code, errOut)
	}
	// The daemon is up and hand-started (no managedBy): never respawned, instructions printed.
	if !strings.Contains(out, "smind stop && smind serve") || !strings.Contains(out, "still smind 0.11.1") {
		t.Fatalf("stdout = %q, want restart instructions for the manual daemon", out)
	}
	if got := e.installed(t); got != "0.12.0" {
		t.Fatalf("installed = %s", got)
	}
}

func TestUpdateForceOverridesActiveRuns(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	d := newFakeUpdateDaemon(t, e.home, `[{"ID":"r1","Status":"running"}]`)
	code, _, errOut := runUpdate(t, "--force")
	if code != 0 {
		t.Fatalf("update --force = %d; stderr: %s", code, errOut)
	}
	if got := e.installed(t); got != "0.12.0" {
		t.Fatalf("installed = %s", got)
	}
	if calls := d.rpcCalls(); len(calls) != 0 {
		t.Fatalf("--force must not ask the daemon about runs, saw %v", calls)
	}
}

func TestUpdateRefusesWhenTheDaemonCannotBeAsked(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	d := newFakeUpdateDaemon(t, e.home, `[]`)
	d.mu.Lock()
	d.token = "not-the-token-in-SMIND_HOME" // something answers /healthz but rejects our credentials
	d.mu.Unlock()
	code, _, errOut := runUpdate(t)
	if code == 0 || !strings.Contains(errOut, "refused") || !strings.Contains(errOut, "--force") {
		t.Fatalf("update = %d, stderr %q; want a refusal pointing at --force", code, errOut)
	}
	if got := e.installed(t); got != "0.11.1" {
		t.Fatalf("binary = %s", got)
	}
	if code, _, errOut := runUpdate(t, "--force"); code != 0 {
		t.Fatalf("--force = %d; stderr: %s", code, errOut)
	}
}

func TestUpdateRefusesDowngradeWithoutForceCLI(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.12.0", "0.12.0")
	e.srv.Add(t, "0.10.0", runtime.GOOS, "amd64")
	writePortConfig(t, e.home, freePort(t))
	code, _, errOut := runUpdate(t, "--version", "0.10.0")
	if code == 0 || !strings.Contains(errOut, "downgrade") {
		t.Fatalf("downgrade = %d, %q", code, errOut)
	}
	if code, _, errOut = runUpdate(t, "--version=0.10.0", "--force"); code != 0 {
		t.Fatalf("forced downgrade = %d; %s", code, errOut)
	}
	if got := e.installed(t); got != "0.10.0" {
		t.Fatalf("installed = %s", got)
	}
}

func TestUpdateDryRunChangesNothingCLI(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	writePortConfig(t, e.home, freePort(t))
	code, out, errOut := runUpdate(t, "--dry-run")
	if code != 0 || !strings.Contains(out, "dry run") {
		t.Fatalf("--dry-run = %d, %q, %q", code, out, errOut)
	}
	if got := e.installed(t); got != "0.11.1" {
		t.Fatalf("--dry-run changed the binary to %s", got)
	}
}

func TestUpdateRefusesDevBuildCLI(t *testing.T) {
	newUpdateCLIEnv(t, "dev", "0.12.0")
	code, _, errOut := runUpdate(t)
	if code == 0 || !strings.Contains(errOut, "not a release") {
		t.Fatalf("dev build = %d, %q", code, errOut)
	}
}

func TestUpdateUsageErrors(t *testing.T) {
	newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	for _, args := range [][]string{{"--bogus"}, {"extra"}, {"--check", "--force"}, {"--check", "--version", "1.0.0"}, {"--version"}} {
		if code, _, errOut := runUpdate(t, args...); code != 2 || !strings.Contains(errOut, "usage: smind update") {
			t.Errorf("update %v = %d, %q; want exit 2 with usage", args, code, errOut)
		}
	}
}

func TestUpdateRunsTheRealVersionProbe(t *testing.T) {
	// No RunVersion override: executes the staged binary's `version` for real.
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell-script fake binary")
	}
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	writePortConfig(t, e.home, freePort(t))
	script := []byte("#!/bin/sh\n[ \"$1\" = version ] && echo 'smind 0.12.0 (test)'\n")
	e.srv.SetChecksums("0.12.0", "") // drop the fixture's entry; AddArchive records the script's digest
	e.srv.AddArchive(t, "0.12.0", runtime.GOOS, "amd64", selfupdatetest.Archive(t, runtime.GOOS, "smind", script))
	prev := updaterHook
	updaterHook = func(u *selfupdate.Updater) { prev(u); u.RunVersion = nil }
	t.Cleanup(func() { updaterHook = prev })
	code, _, errOut := runUpdate(t)
	if code != 0 {
		t.Fatalf("update = %d; stderr: %s", code, errOut)
	}
	if b, _ := os.ReadFile(e.exe); string(b) != string(script) {
		t.Fatalf("installed binary = %q, want the downloaded script", b)
	}
}

// buildSmindVersion compiles this package's binary with a stamped version.
func buildSmindVersion(t *testing.T, ver string, extraLdflags ...string) []byte {
	t.Helper()
	bin := filepath.Join(t.TempDir(), exeName("smind-"+ver))
	ldflags := strings.Join(append([]string{"-X github.com/spacingmind/smind/internal/version.Version=" + ver}, extraLdflags...), " ")
	out, err := exec.Command("go", "build", "-o", bin, "-ldflags", ldflags, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build (%s): %v\n%s", ver, err, out)
	}
	b, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestUpdateEndToEndReplacesTheRunningBinary builds two real smind binaries,
// publishes the newer one on the fake release server, and runs the OLDER
// binary's own `update`: with a valid signature it replaces itself while
// running (rename-aside on Windows, rename-over elsewhere), the real `version`
// probe passes, and the installed file then reports the new version. With an
// unsigned release, or one signed by another key, the real binary refuses and
// stays byte-identical, which also proves the production wiring in cmdUpdate.
func TestUpdateEndToEndReplacesTheRunningBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two binaries")
	}
	if selfupdate.InContainer(runtime.GOOS, os.Getenv, func(p string) bool { _, err := os.Stat(p); return err == nil }) {
		t.Skip("smind update refuses to run inside a container")
	}
	// The only way to point a binary at another release key is to build it so
	// (there is deliberately no run-time override), so stamp a throwaway key.
	signer := selfupdatetest.NewSigner(t)
	oldBin := buildSmindVersion(t, "0.1.0", "-X github.com/spacingmind/smind/internal/selfupdate.UpdaterPublicKey="+signer.PublicKey())
	newBin := buildSmindVersion(t, "0.2.0")

	for _, tc := range []struct {
		name    string
		signer  *selfupdatetest.Signer
		updated bool
		stderr  string
	}{
		{"signed by the release key", signer, true, ""},
		{"unsigned release", nil, false, "release has no signature"},
		{"signed by another key", selfupdatetest.NewSigner(t), false, "signed by key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := selfupdatetest.New(t)
			srv.Signer = tc.signer
			srv.Latest = "0.2.0"
			srv.AddArchive(t, "0.2.0", runtime.GOOS, runtime.GOARCH,
				selfupdatetest.Archive(t, runtime.GOOS, selfupdate.BinaryName(runtime.GOOS), newBin))

			installed := filepath.Join(t.TempDir(), selfupdate.BinaryName(runtime.GOOS))
			if err := os.WriteFile(installed, oldBin, 0o755); err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			writePortConfig(t, home, freePort(t))
			cmd := exec.Command(installed, "update", "--force") // --force must not matter to signatures
			cmd.Env = append(os.Environ(), "SMIND_HOME="+home, selfupdate.BaseURLEnv+"="+srv.URL)
			out, err := cmd.CombinedOutput()
			if tc.updated {
				if err != nil {
					t.Fatalf("smind update: %v\n%s", err, out)
				}
				got, err := exec.Command(installed, "version").Output()
				if err != nil || !strings.HasPrefix(string(got), "smind 0.2.0") {
					t.Fatalf("installed binary reports %q (%v), want smind 0.2.0", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(string(out), tc.stderr) {
				t.Fatalf("smind update = %v\n%s\nwant a failure mentioning %q", err, out, tc.stderr)
			}
			if got, _ := os.ReadFile(installed); string(got) != string(oldBin) {
				t.Fatal("a refused update changed the installed binary")
			}
		})
	}
}

func TestUpdateRefusesReleaseWithoutSignature(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	writePortConfig(t, e.home, freePort(t))
	e.srv.Signer = nil // published before signing existed
	code, _, errOut := runUpdate(t)
	if code == 0 || !strings.Contains(errOut, "release has no signature") {
		t.Fatalf("update = %d, stderr %q; want a refusal naming the missing signature", code, errOut)
	}
	if got := e.installed(t); got != "0.11.1" {
		t.Fatalf("binary = %s", got)
	}
}

func TestUpdateRefusesBadSignature(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	writePortConfig(t, e.home, freePort(t))
	e.srv.Signer = selfupdatetest.NewSigner(t)
	code, _, errOut := runUpdate(t, "--force") // --force must not bypass the signature
	if code == 0 || !strings.Contains(errOut, "signature does not verify") {
		t.Fatalf("update --force = %d, stderr %q", code, errOut)
	}
	if got := e.installed(t); got != "0.11.1" {
		t.Fatalf("binary = %s", got)
	}
}

// Production wiring: the hook here leaves cmdUpdate's own VerifyChecksums
// alone, so the embedded release key is what checks the fake release (signed by
// a test key) and must refuse it. Deleting the wiring line in cmdUpdate makes
// Update refuse for want of a verifier, which also fails this test's wording.
func TestUpdateUsesTheEmbeddedReleaseKeyByDefault(t *testing.T) {
	e := newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	writePortConfig(t, e.home, freePort(t))
	updaterHook = func(u *selfupdate.Updater) {
		if u.VerifyChecksums == nil {
			t.Error("cmdUpdate did not wire a checksum verifier")
		}
		e.baseHook(u)
	}
	code, _, errOut := runUpdate(t)
	if code == 0 || !strings.Contains(errOut, "smind release key") {
		t.Fatalf("update = %d, stderr %q; want a refusal because the test key is not the release key", code, errOut)
	}
	if got := e.installed(t); got != "0.11.1" {
		t.Fatalf("binary = %s", got)
	}
}

func TestUpdateHasNoSignatureBypassFlag(t *testing.T) {
	newUpdateCLIEnv(t, "0.11.1", "0.12.0")
	for _, flag := range []string{"--insecure-skip-signature", "--skip-signature", "--no-verify"} {
		if code, _, errOut := runUpdate(t, flag); code != 2 || !strings.Contains(errOut, "flag provided but not defined") {
			t.Errorf("update %s = %d, %q; want an unknown-flag usage error", flag, code, errOut)
		}
	}
}
