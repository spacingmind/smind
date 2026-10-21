//go:build !windows

package arm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrNotArmProcess is returned when the pid in the pidfile does not belong
// to this arm any more (the CLI exited and the pid was reused): nothing is
// signalled.
var ErrNotArmProcess = errors.New("arm: pid is not this arm's CLI")

// KillTree ends the arm's CLI and every process of the arm (the sandboxed
// Bash commands the agent started), R4. The SDK only signals the CLI itself,
// so an orphaned `sleep` or build would otherwise outlive the arm. The
// launcher makes the CLI a process-group leader and writes its pid (= pgid)
// to pidFile, so the kill covers: every process in that group (including
// commands the agent detached with `&`, which reparent to init but keep the
// group), plus the process tree below the CLI (a command that moved itself
// to another group with setpgid still has its parent chain, unless it was
// also reparented; that residual case runs inside the command sandbox and is
// reported as a survivor). A live CLI must be a group leader, a child of this
// daemon and carry armDir as a whole path element in its command line (the
// --settings value carries it), which keeps a reused pid from being killed.
// A pidfile that is absent or names a process that is already gone and an
// empty group is not an error. This is cleanup, not a security boundary.
//
// Everything is collected before any signal is sent, signalled TERM then
// KILL after grace, and the call returns once none is alive or after a
// bounded wait, with an error naming the survivors.
func KillTree(ctx context.Context, pidFile, armDir string, grace time.Duration) error {
	root, err := readPid(pidFile)
	if err != nil || root == 0 {
		return err
	}
	t1, err := processTable(ctx)
	if err != nil {
		return err
	}
	if len(t1.members(root)) == 0 {
		return nil
	}
	// Identity. While the CLI itself is alive it must be a process-group
	// leader (the launcher's setpgrp), a child of this daemon, and carry the
	// arm dir as a whole path element in its command line. Once it is gone
	// only its group is left, and a group id is never reused while the group
	// has members, so kill(-root) cannot hit a stranger.
	if _, live := t1.ppid[root]; live {
		cmd, cerr := commandOf(ctx, root)
		switch {
		case t1.pgid[root] != root:
			return fmt.Errorf("%w: %d is not a process-group leader (pgid %d)", ErrNotArmProcess, root, t1.pgid[root])
		case t1.ppid[root] != os.Getpid():
			return fmt.Errorf("%w: %d is not a child of this daemon", ErrNotArmProcess, root)
		case cerr != nil || !cmdHasPath(cmd, armDir):
			return ErrNotArmProcess
		}
	}
	pids := t1.members(root)
	signal := func(sig syscall.Signal, targets []int) {
		_ = syscall.Kill(-root, sig) // the whole group
		for _, p := range targets {  // and escapees below the CLI
			_ = syscall.Kill(p, sig)
		}
	}
	signal(syscall.SIGTERM, pids)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) && anyAlive(pids) {
		time.Sleep(25 * time.Millisecond)
	}
	// Re-snapshot before the KILL: a pid may have died and been recycled
	// during the grace, and the TERM may have let a trap start something. Only
	// processes that are still in the arm's group/tree now, or that still have
	// the same parent and group as when collected, are signalled again.
	killSet := pids
	if t2, err := processTable(ctx); err == nil {
		killSet = stable(pids, t1, t2)
		killSet = append(killSet, t2.members(root)...)
	} else {
		killSet = nil // cannot prove identity: rely on the group kill alone
	}
	signal(syscall.SIGKILL, killSet)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && anyAlive(killSet) {
		time.Sleep(25 * time.Millisecond)
	}
	var left []int
	seen := map[int]bool{}
	for _, p := range killSet {
		if !seen[p] && alive(p) {
			seen[p] = true
			left = append(left, p)
		}
	}
	if t3, err := processTable(ctx); err == nil {
		for _, p := range t3.members(root) {
			if !seen[p] && alive(p) {
				seen[p] = true
				left = append(left, p)
			}
		}
	}
	if len(left) > 0 {
		return &SurvivorsError{PIDs: left}
	}
	return nil
}

// stable returns the pids of old that are still present in t2 with the same
// parent and process group as in t1: a recycled pid almost surely differs.
func stable(old []int, t1, t2 procTable) []int {
	var out []int
	for _, p := range old {
		pp2, ok := t2.ppid[p]
		if ok && pp2 == t1.ppid[p] && t2.pgid[p] == t1.pgid[p] {
			out = append(out, p)
		}
	}
	return out
}

// cmdHasPath reports whether cmd contains dir as a whole path: not preceded
// or followed by a path-name character (so /x/arm-1 does not match /x/arm-12
// nor /y/x/arm-1). The JSON-escaped form is accepted too, since the
// --settings value is json.Marshal output, which escapes & < > as \u00XX.
func cmdHasPath(cmd, dir string) bool {
	forms := []string{dir}
	if b, err := json.Marshal(dir); err == nil && len(b) > 2 {
		if esc := string(b[1 : len(b)-1]); esc != dir {
			forms = append(forms, esc)
		}
	}
	nameChar := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' || c == '/'
	}
	for _, f := range forms {
		for i := 0; ; {
			j := strings.Index(cmd[i:], f)
			if j < 0 {
				break
			}
			start, end := i+j, i+j+len(f)
			if (start == 0 || !nameChar(cmd[start-1])) && (end == len(cmd) || !nameChar(cmd[end])) {
				return true
			}
			i = start + 1
		}
	}
	return false
}

// readPid returns the pid in pidFile, 0 when the file is absent.
func readPid(pidFile string) (int, error) {
	raw, err := os.ReadFile(pidFile)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("arm: read pidfile: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 || pid == os.Getpid() {
		return 0, fmt.Errorf("arm: pidfile holds %q", clip(string(raw), 40))
	}
	return pid, nil
}

// alive reports whether pid is a live, non-zombie process.
func alive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	return !strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

func anyAlive(pids []int) bool {
	for _, p := range pids {
		if alive(p) {
			return true
		}
	}
	return false
}

// procTable is a snapshot of the process list.
type procTable struct {
	ppid map[int]int
	pgid map[int]int
}

// processTable returns pid -> ppid and pid -> pgid for every process.
func processTable(ctx context.Context) (procTable, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,pgid=").Output()
	if err != nil {
		return procTable{}, fmt.Errorf("arm: ps: %w", err)
	}
	t := procTable{ppid: map[int]int{}, pgid: map[int]int{}}
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		f := strings.Fields(string(line))
		if len(f) != 3 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		pgid, e3 := strconv.Atoi(f[2])
		if e1 == nil && e2 == nil && e3 == nil {
			t.ppid[pid], t.pgid[pid] = ppid, pgid
		}
	}
	return t, nil
}

func commandOf(ctx context.Context, pid int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	return string(out), err
}

// members returns the CLI (root), every process in its group and every
// process below it, deepest first, never the daemon itself. Empty when root is
// gone and its group is empty.
func (t procTable) members(root int) []int {
	kids := map[int][]int{}
	for pid, ppid := range t.ppid {
		kids[ppid] = append(kids[ppid], pid)
	}
	seen := map[int]bool{}
	var order []int
	var walk func(int)
	walk = func(p int) {
		if seen[p] || p == os.Getpid() {
			return
		}
		seen[p] = true
		for _, k := range kids[p] {
			walk(k)
		}
		order = append(order, p)
	}
	if _, live := t.ppid[root]; live {
		walk(root)
	}
	for pid, pg := range t.pgid {
		if pg == root && !seen[pid] {
			walk(pid)
		}
	}
	return order
}
