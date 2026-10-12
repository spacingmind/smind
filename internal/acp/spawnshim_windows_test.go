//go:build windows

package acp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// buildTestCmd compiles one of this package's test helper commands (see
// arghelper/, treeagent/) into dir and returns its path.
func buildTestCmd(t *testing.T, dir, pkg string) string {
	t.Helper()
	bin := filepath.Join(dir, exeName(pkg))
	_, thisFile, _, _ := runtime.Caller(0)
	build := exec.Command("go", "build", "-o", bin, "./"+pkg)
	build.Dir = filepath.Dir(thisFile)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v: %s", pkg, err, out)
	}
	return bin
}

// installShim puts a test-only npm-style .cmd shim in dir: shim.cmd
// forwards to target exactly the way npm's shims do
// (`@"%~dp0\<bin>" %*`), and dir is meant to be put on PATH so a bare
// command name resolves to the shim through PATHEXT.
func installShim(t *testing.T, dir, shim, target string) {
	t.Helper()
	content := "@\"%~dp0\\" + filepath.Base(target) + "\" %*\r\n"
	if err := os.WriteFile(filepath.Join(dir, shim+".cmd"), []byte(content), 0o666); err != nil {
		t.Fatalf("write %s.cmd: %v", shim, err)
	}
}

// TestSpawnShim_ArgvRoundTrip_Windows: spawning an npm-style .cmd shim
// through newConn delivers every safe argument byte-exact to the exe
// behind the shim. The argument set is the measured-safe subset (Go's
// CommandLineToArgvW quoting round-trips these through cmd.exe's
// re-parse): plain, spaces, embedded quotes, !, empty string, trailing
// backslash, non-ASCII.
func TestSpawnShim_ArgvRoundTrip_Windows(t *testing.T) {
	dir := t.TempDir()
	helper := buildTestCmd(t, dir, "arghelper")
	installShim(t, dir, "shim", helper)
	t.Setenv("PATH", dir)

	want := []string{"plain", "with spaces", `a"b`, "a!b", "", `C:\dir\`, "ünïcødé-arg"}
	out := filepath.Join(dir, "argv.json")
	t.Setenv("SMIND_ARGHELPER_OUT", out)

	c, err := newConn(append([]string{"shim"}, want...), nil)
	if err != nil {
		t.Fatalf("newConn() error = %v", err)
	}
	var got []string
	readJSONFileWithDeadline(t, out, &got)
	if err := c.close(5 * time.Second); err != nil {
		t.Fatalf("close() error = %v", err)
	}

	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv round-trip: got %q, want %q", got, want)
	}
}

// TestSpawnShim_UnsafeArgRejected_Windows: an argument carrying a cmd.exe
// metacharacter must be refused before anything is spawned, with the error
// naming the offending argument -- measured behavior is that Go's quoting
// lets cmd.exe expand, split, or drop these (e.g. %PATH% expands, a|b
// pipes), i.e. it could execute something other than what was asked.
func TestSpawnShim_UnsafeArgRejected_Windows(t *testing.T) {
	dir := t.TempDir()
	helper := buildTestCmd(t, dir, "arghelper")
	installShim(t, dir, "shim", helper)
	t.Setenv("PATH", dir)

	for _, arg := range []string{"a&b", "a|b", "a<b", "a>b", "a^b", "%PATH%", "a%b", "a\r\nb"} {
		if _, err := newConn([]string{"shim", arg}, nil); err == nil {
			t.Fatalf("newConn(shim, %q) succeeded, want refusal", arg)
		} else if !strings.Contains(err.Error(), strconv.Quote(arg)) {
			t.Fatalf("newConn(shim, %q) error %q does not name the argument", arg, err)
		}
	}
}

// readJSONFileWithDeadline polls for path to appear (the spawned helper
// writes it once ready) and unmarshals it into v, bounded so a broken
// spawn fails the test instead of hanging it. Accepts any Fatalf-capable
// reporter so the re-exec'd crash-helper subprocess can use it too.
func readJSONFileWithDeadline(t testFatal, path string, v any) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(data, v); err != nil {
				t.Fatalf("unmarshal %s: %v", path, err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

// testFatal is the subset of *testing.T readJSONFileWithDeadline needs.
type testFatal interface {
	Fatalf(format string, args ...any)
	Helper()
}
