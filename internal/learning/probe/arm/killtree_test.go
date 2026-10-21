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

func TestArmEnd_KillTreeIdentityIsWholePathElement(t *testing.T) {
	root := t.TempDir()
	pidFile := filepath.Join(root, "cli.pid")
	cli, grand := startFakeCLI(t, filepath.Join(root, "arm-12"), pidFile)
	go func() { _ = cli.Wait() }()
	err := KillTree(context.Background(), pidFile, filepath.Join(root, "arm-1"), 50*time.Millisecond)
	if !errors.Is(err, ErrNotArmProcess) {
		t.Fatalf("arm-1 matched a process of arm-12: %v", err)
	}
	if !alive(cli.Process.Pid) || !alive(grand[0]) {
		t.Fatal("a process of another arm was killed")
	}
	if err := KillTree(context.Background(), pidFile, filepath.Join(root, "arm-12"), 100*time.Millisecond); err != nil {
		t.Fatalf("the real arm dir was refused: %v", err)
	}
}

func TestArmEnd_KillTreeRefusesLeaderThatIsNotOurChild(t *testing.T) {
	armDir := filepath.Join(t.TempDir(), "arm-1")
	pidFile := filepath.Join(t.TempDir(), "cli.pid")
	// A group leader whose command line names the arm but whose parent is not
	// this daemon: the shell exits right after backgrounding it, so it is
	// reparented to init.
	script := fmt.Sprintf(`/usr/bin/perl -e 'setpgrp(0,0); exec @ARGV' sh -c 'sleep 300; :' fake-claude --settings %q & echo $! > %q`, armDir, pidFile)
	if err := exec.Command("sh", "-c", script).Run(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL); _ = syscall.Kill(pid, syscall.SIGKILL) })
	time.Sleep(200 * time.Millisecond) // let the reparenting settle
	if err := KillTree(context.Background(), pidFile, armDir, 50*time.Millisecond); !errors.Is(err, ErrNotArmProcess) {
		t.Fatalf("err = %v, want ErrNotArmProcess for a leader that is not our child", err)
	}
	if !alive(pid) {
		t.Fatal("a process that is not our child was killed")
	}
}

func TestArmEnd_CmdHasPath(t *testing.T) {
	cases := []struct {
		cmd, dir string
		want     bool
	}{
		{"claude --settings /x/arm-1 --tools", "/x/arm-1", true},
		{`claude --settings {"allowWrite":["/x/arm-1"]}`, "/x/arm-1", true},
		{"claude /x/arm-12", "/x/arm-1", false},
		{"claude /x/arm-1/sub", "/x/arm-1", false}, // only a whole element, never a parent of something else
		{"claude /y/x/arm-1", "/x/arm-1", false},
		{"claude /x/arm-12 /x/arm-1", "/x/arm-1", true},
		{"claude {\"w\":[\"/x/a" + `\` + "u0026b\"]}", "/x/a&b", true}, // json.Marshal escapes &
		{"claude nothing", "/x/arm-1", false},
	}
	for _, tc := range cases {
		if got := cmdHasPath(tc.cmd, tc.dir); got != tc.want {
			t.Errorf("cmdHasPath(%q, %q) = %v, want %v", tc.cmd, tc.dir, got, tc.want)
		}
	}
}

func TestArmEnd_StableDropsRecycledPids(t *testing.T) {
	t1 := procTable{ppid: map[int]int{10: 1, 11: 10, 12: 10}, pgid: map[int]int{10: 10, 11: 10, 12: 10}}
	// 11 died and its pid was recycled by an unrelated process under another
	// parent and group; 12 is unchanged; 10 is gone.
	t2 := procTable{ppid: map[int]int{11: 500, 12: 10}, pgid: map[int]int{11: 500, 12: 10}}
	got := stable([]int{10, 11, 12}, t1, t2)
	if len(got) != 1 || got[0] != 12 {
		t.Errorf("stable = %v, want [12]", got)
	}
}

func TestArmEnd_SurvivorsErrorIsTyped(t *testing.T) {
	var err error = &SurvivorsError{PIDs: []int{4, 5}}
	var se *SurvivorsError
	if !errors.As(fmt.Errorf("wrapped: %w", err), &se) || len(se.PIDs) != 2 || !strings.Contains(se.Error(), "survived") {
		t.Errorf("SurvivorsError = %+v", se)
	}
}
