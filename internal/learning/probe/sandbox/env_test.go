//go:build !windows

package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/testutil"
)

func newPrivDir(t *testing.T, mode os.FileMode) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "d")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(d, mode); err != nil {
		t.Fatal(err)
	}
	return d
}

func writeStub(t *testing.T, dir, body string) string {
	t.Helper()
	stub := filepath.Join(dir, "stub-cli")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return stub
}

func TestSandboxEnv_AllowlistOnly(t *testing.T) {
	have := map[string]string{"PATH": "/usr/bin", "HOME": "/h", "USER": "u", "SMIND_CANARY_SECRET": "CANARY-x", "GITHUB_TOKEN": "t", "LOGNAME": "u", "SHELL": "/bin/sh"}
	got := AllowlistEnv(func(n string) (string, bool) { v, ok := have[n]; return v, ok })
	names := map[string]string{}
	for _, kv := range got {
		n, v, _ := strings.Cut(kv, "=")
		names[n] = v
	}
	for _, want := range []string{"PATH", "HOME", "USER", "ENABLE_CLAUDEAI_MCP_SERVERS", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM"} {
		if _, ok := names[want]; !ok {
			t.Errorf("%s missing from the allowlist env", want)
		}
	}
	if names["ENABLE_CLAUDEAI_MCP_SERVERS"] != "false" || names["GIT_CONFIG_GLOBAL"] != "/dev/null" || names["GIT_CONFIG_NOSYSTEM"] != "1" {
		t.Errorf("fixed extras wrong: %v", names)
	}
	for _, bad := range []string{"SMIND_CANARY_SECRET", "GITHUB_TOKEN", "LOGNAME", "SHELL", "LANG"} {
		if _, ok := names[bad]; ok {
			t.Errorf("%s must not be in the allowlist env (absent or not allowlisted)", bad)
		}
	}
}

func TestSandboxEnv_NoDaemonEnvLeak(t *testing.T) {
	fakes := map[string]string{
		"FAKE_API_KEY": "fake-key-1", "FAKE_AUTH_TOKEN": "fake-token-2", "FAKE_SECRET": "fake-secret-3",
		"FAKE_PASSWORD": "fake-pw-4", "ANTHROPIC_API_KEY": "fake-anthropic-5",
	}
	for k, v := range fakes {
		t.Setenv(k, v)
	}
	dir := newPrivDir(t, 0o700)
	stub := writeStub(t, dir, "/usr/bin/env\n")
	pid := filepath.Join(dir, "cli.pid")
	lookup := func(n string) (string, bool) { return os.LookupEnv(n) }
	env := AllowlistEnv(lookup)
	launcher, err := WriteLauncher(dir, stub, env, pid)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(launcher).Output()
	if err != nil {
		t.Fatalf("launcher failed: %v", err)
	}
	for k, v := range fakes {
		if strings.Contains(string(out), v) || strings.Contains(string(out), k+"=") {
			t.Errorf("%s leaked through the launcher", k)
		}
	}
	want := map[string]bool{"PWD": true, "SHLVL": true, "_": true}
	for _, kv := range env {
		n, _, _ := strings.Cut(kv, "=")
		want[n] = true
	}
	got := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		k, _, _ := strings.Cut(line, "=")
		got[k] = true
		if !want[k] {
			t.Errorf("unexpected env var %s reached the CLI", k)
		}
	}
	for _, kv := range env {
		n, _, _ := strings.Cut(kv, "=")
		if !got[n] {
			t.Errorf("%s did not reach the CLI", n)
		}
	}
}

func TestSandboxEnv_ValuesVerbatimAndArgs(t *testing.T) {
	dir := newPrivDir(t, 0o700)
	stub := writeStub(t, dir, "/usr/bin/env\necho \"ARGS:$*\"\n")
	env := []string{"PATH=/usr/bin:/bin", "HOME=/tmp/private home", "TRICKY=it's $(touch " + filepath.Join(dir, "pwned") + ")"}
	launcher, err := WriteLauncher(dir, stub, env, filepath.Join(dir, "cli.pid"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(launcher, "--flag", "a b").Output()
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "HOME=/tmp/private home\n") || !strings.Contains(text, "TRICKY=it's $(touch "+filepath.Join(dir, "pwned")+")\n") {
		t.Errorf("values not passed verbatim:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Fatal("an env value was interpreted by the shell")
	}
	if !strings.Contains(text, "ARGS:--flag a b") {
		t.Errorf("argv not forwarded:\n%s", text)
	}
}

func TestSandboxEnv_PidFileHoldsCLIPid(t *testing.T) {
	dir := newPrivDir(t, 0o700)
	stub := writeStub(t, dir, "echo $$ > "+shQuote(filepath.Join(dir, "self.pid"))+"\nsleep 30 &\necho $! > "+shQuote(filepath.Join(dir, "child.pid"))+"\nsleep 2\n")
	pidFile := filepath.Join(dir, "cli.pid")
	launcher, err := WriteLauncher(dir, stub, []string{"PATH=/usr/bin:/bin"}, pidFile)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(launcher)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var childPid int
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if childPid > 0 {
			_ = syscall.Kill(childPid, syscall.SIGKILL)
		}
	})
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return strings.TrimSpace(string(b))
	}
	var got, self, child string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		got, self, child = read("cli.pid"), read("self.pid"), read("child.pid")
		if got != "" && self != "" && child != "" {
			break
		}
	}
	if got != strconv.Itoa(cmd.Process.Pid) || self != got {
		t.Fatalf("pidfile=%q stub-pid=%q launcher-pid=%d: exec must keep one pid", got, self, cmd.Process.Pid)
	}
	childPid, _ = strconv.Atoi(child)
	pid := cmd.Process.Pid
	if pgid, err := syscall.Getpgid(pid); err != nil || pgid != pid {
		t.Errorf("getpgid(%d) = %d, %v: the CLI must lead its own group", pid, pgid, err)
	}
	if pgid, err := syscall.Getpgid(childPid); err != nil || pgid != pid {
		t.Errorf("background child %d is in group %d (%v), want %d", childPid, pgid, err, pid)
	}
	// kill(-pgid) takes the whole group down, which is what the arm cleanup does.
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill(-pgid): %v", err)
	}
	_ = cmd.Wait()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(childPid, 0) != nil {
			return
		}
	}
	t.Errorf("background child %d survived kill(-pgid)", childPid)
}

// Not parallel: it swaps the package-level perlPath.
func TestSandboxEnv_LauncherNeedsPerl(t *testing.T) {
	defer func(old string) { perlPath = old }(perlPath)
	dir := newPrivDir(t, 0o700)
	notExec := filepath.Join(dir, "perl-not-exec")
	if err := os.WriteFile(notExec, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{"missing": filepath.Join(dir, "nope"), "not executable": notExec, "directory": dir} {
		perlPath = p
		pid := filepath.Join(dir, "p")
		if _, err := WriteLauncher(dir, "/bin/true", nil, pid); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: want ErrUnsupported, got %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "claude-launcher.sh")); err == nil {
			t.Errorf("%s: a launcher was written without perl", name)
		}
	}
}

func TestSandboxEnv_LauncherRejectsBadInput(t *testing.T) {
	dir := newPrivDir(t, 0o700)
	pid := filepath.Join(dir, "p")
	for _, bad := range []string{"NOEQUALS", "1BAD=x", "A B=x", "=x"} {
		if _, err := WriteLauncher(dir, "/bin/true", []string{bad}, pid); err == nil {
			t.Errorf("entry %q was accepted", bad)
		}
	}
	if _, err := WriteLauncher(dir, "relative/claude", nil, pid); err == nil {
		t.Error("relative CLI path accepted")
	}
	if _, err := WriteLauncher(dir, "/bin/true", nil, "p"); err == nil {
		t.Error("relative pid file accepted")
	}
	if _, err := WriteLauncher(dir, "/bin/true", nil, filepath.Join(t.TempDir(), "p")); err == nil {
		t.Error("pid file outside the launcher dir accepted")
	}
	if _, err := WriteLauncher(dir, "/bin/true", nil, filepath.Join(dir, "sub", "p")); err == nil {
		t.Error("nested pid file accepted")
	}
}

func TestSandboxEnv_LauncherRefusesSymlinkOrWorldWritableDir(t *testing.T) {
	d := newPrivDir(t, 0o700)
	pid := filepath.Join(d, "p")
	path, err := WriteLauncher(d, "/bin/true", []string{"PATH=/usr/bin"}, pid)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o700 {
		t.Errorf("launcher mode = %v, want 0700", fi.Mode().Perm())
	}
	if _, err := WriteLauncher(d, "/bin/true", nil, pid); err == nil {
		t.Error("an existing launcher file was overwritten")
	}
	d = newPrivDir(t, 0o700)
	if err := os.WriteFile(filepath.Join(d, "claude-launcher.sh"), []byte("planted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteLauncher(d, "/bin/true", nil, filepath.Join(d, "p")); err == nil {
		t.Error("a pre-planted file was accepted")
	}
	d = newPrivDir(t, 0o700)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	testutil.SymlinkOrSkip(t, victim, filepath.Join(d, "claude-launcher.sh"))
	if _, err := WriteLauncher(d, "/bin/true", nil, filepath.Join(d, "p")); err == nil {
		t.Error("a pre-planted symlink was followed")
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("symlink target was overwritten: %q", b)
	}
	for _, mode := range []os.FileMode{0o770, 0o707, 0o777} {
		d := newPrivDir(t, mode)
		if _, err := WriteLauncher(d, "/bin/true", nil, filepath.Join(d, "p")); err == nil {
			t.Errorf("dir mode %v accepted", mode)
		}
	}
	real := newPrivDir(t, 0o700)
	linkDir := filepath.Join(t.TempDir(), "linkdir")
	testutil.Link(t, real, linkDir)
	if _, err := WriteLauncher(linkDir, "/bin/true", nil, filepath.Join(linkDir, "p")); err == nil {
		t.Error("a symlinked launcher dir was accepted")
	}
}
