//go:build !windows

package arm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startFakeCLI starts a stand-in CLI whose command line mentions armDir (as
// the real --settings value does), writes its pid to pidFile like the
// launcher does, and spawns grandchild processes that outlive it. It
// returns the pids of the grandchildren.
func startFakeCLI(t *testing.T, armDir, pidFile string) (cli *exec.Cmd, grand []int) {
	t.Helper()
	childPids := filepath.Join(t.TempDir(), "children")
	// The "CLI": starts two background sleeps (one detached via a subshell),
	// records their pids, then sleeps itself. argv[1] carries the arm dir.
	script := fmt.Sprintf(`echo $$ > %q; (sleep 300 & echo $! >> %q); sleep 301 & echo $! >> %q; sleep 302`, pidFile, childPids, childPids)
	cli = exec.Command("sh", "-c", script, "fake-claude", "--settings", armDir)
	cli.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // what the launcher's perl setpgrp does
	if err := cli.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Process.Kill(); _, _ = cli.Process.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(childPids)
		if f := strings.Fields(string(b)); len(f) == 2 {
			for _, s := range f {
				p, _ := strconv.Atoi(s)
				grand = append(grand, p)
			}
			return cli, grand
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("fake CLI did not start its children")
	return nil, nil
}

func TestArmEnd_NoSurvivingChildProcess(t *testing.T) {
	armDir := filepath.Join(t.TempDir(), "arm-1")
	pidFile := filepath.Join(t.TempDir(), "cli.pid")
	cli, grand := startFakeCLI(t, armDir, pidFile)
	for _, p := range grand {
		if !alive(p) {
			t.Fatalf("setup: grandchild %d not alive", p)
		}
	}
	go func() { _ = cli.Wait() }() // reap the CLI like the SDK does
	if err := KillTree(context.Background(), pidFile, armDir, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for _, p := range grand {
		if alive(p) {
			t.Errorf("grandchild %d survived KillTree", p)
		}
	}
	if alive(cli.Process.Pid) {
		t.Error("the CLI itself survived")
	}
}

func TestArmEnd_KillTreeIgnoresReusedPid(t *testing.T) {
	armDir := filepath.Join(t.TempDir(), "arm-1")
	pidFile := filepath.Join(t.TempDir(), "cli.pid")
	// An unrelated live process (command line does not mention the arm).
	other := exec.Command("sleep", "300")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Process.Kill(); _, _ = other.Process.Wait() })
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(other.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := KillTree(context.Background(), pidFile, armDir, 50*time.Millisecond)
	if !errors.Is(err, ErrNotArmProcess) {
		t.Fatalf("err = %v, want ErrNotArmProcess", err)
	}
	if !alive(other.Process.Pid) {
		t.Fatal("an unrelated process was killed")
	}
}

func TestArmEnd_KillTreeMissingOrDeadIsNil(t *testing.T) {
	d := t.TempDir()
	if err := KillTree(context.Background(), filepath.Join(d, "none.pid"), "/x/arm-1", 10*time.Millisecond); err != nil {
		t.Errorf("missing pidfile: %v", err)
	}
	dead := exec.Command("true")
	_ = dead.Run()
	pf := filepath.Join(d, "dead.pid")
	_ = os.WriteFile(pf, []byte(strconv.Itoa(dead.Process.Pid)), 0o600)
	if err := KillTree(context.Background(), pf, "/x/arm-1", 10*time.Millisecond); err != nil {
		t.Errorf("dead pid: %v", err)
	}
	for _, bad := range []string{"", "abc", "1", "-5"} {
		_ = os.WriteFile(pf, []byte(bad), 0o600)
		if err := KillTree(context.Background(), pf, "/x/arm-1", 10*time.Millisecond); err == nil {
			t.Errorf("pidfile %q accepted", bad)
		}
	}
}
