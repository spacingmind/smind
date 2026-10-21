//go:build !windows

package arm

import (
	"bytes"
	"context"
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
// reported as a survivor). armDir must appear in the CLI's command line (the
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
	table, err := processTable(ctx)
	if err != nil {
		return err
	}
	if len(table.members(root)) == 0 {
		return nil
	}
	if cmd, cerr := commandOf(ctx, root); cerr == nil && !strings.Contains(cmd, armDir) {
		return ErrNotArmProcess
	} else if cerr != nil && alive(root) {
		return ErrNotArmProcess
	}
	// A pgid that is not the CLI's own pid means setpgrp did not run: do not
	// signal a group we do not own.
	if pg, ok := table.pgid[root]; ok && pg != root {
		return fmt.Errorf("arm: cli %d is not a process-group leader (pgid %d)", root, pg)
	}
	pids := table.members(root)
	signalAll := func(sig syscall.Signal) {
		_ = syscall.Kill(-root, sig) // the whole group
		for _, p := range pids {     // and escapees below the CLI
			_ = syscall.Kill(p, sig)
		}
	}
	signalAll(syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) && anyAlive(pids) {
		time.Sleep(25 * time.Millisecond)
	}
	// Re-collect: the TERM may have let a trap start something.
	if t2, err := processTable(ctx); err == nil {
		pids = append(pids, t2.members(root)...)
	}
	signalAll(syscall.SIGKILL)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && anyAlive(pids) {
		time.Sleep(25 * time.Millisecond)
	}
	var left []int
	seen := map[int]bool{}
	for _, p := range pids {
		if !seen[p] && alive(p) {
			seen[p] = true
			left = append(left, p)
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("arm: processes survived kill: %v", left)
	}
	return nil
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
