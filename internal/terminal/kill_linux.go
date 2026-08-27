//go:build linux

package terminal

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// killTree sends SIGKILL to rootPid and every one of its descendant
// processes, found by walking /proc -- not just rootPid's own process
// group. That distinction matters here specifically: pty.Start makes the
// shell an interactive session leader, and an interactive shell with job
// control enabled (which bash, zsh, etc. all turn on automatically for a
// PTY-backed session) puts each foreground or background command it runs
// into its *own* new process group, not the shell's. So SIGKILLing just
// -rootPid's original group would kill the shell but miss, say, a `sleep
// 100 &` background job it started -- exactly the orphaned-process
// failure mode this function exists to avoid. Walking /proc's real
// parent-child relationships catches those regardless of process-group
// assignment.
//
// Best-effort throughout: a process that's already exited by the time we
// get to it (a normal race on any live system, and the expected case for
// rootPid itself once its own SIGKILL above it in the descendants list --
// order within the list doesn't matter, see descendants -- has already
// landed) is silently skipped; "already gone" is exactly the end state
// this function is trying to reach anyway.
func killTree(rootPid int) {
	children := buildChildrenMap()
	for _, pid := range descendants(rootPid, children) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// descendants returns rootPid and every pid reachable from it via
// children, in BFS order (root first).
func descendants(rootPid int, children map[int][]int) []int {
	out := []int{rootPid}
	queue := []int{rootPid}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, c := range children[p] {
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}

// buildChildrenMap scans /proc for every process's PPID, returning a
// ppid -> child pids map. Best-effort: processes that disappear mid-scan
// (a normal race on any live system) are just skipped, and a failure to
// read /proc at all yields an empty map (killTree then falls back to
// just killing rootPid itself).
func buildChildrenMap() map[int][]int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	out := make(map[int][]int)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		ppid, ok := readPPID(pid)
		if !ok {
			continue
		}
		out[ppid] = append(out[ppid], pid)
	}
	return out
}

// readPPID reads pid's parent pid from /proc/<pid>/stat. That file's
// second field (the command name) is parenthesized and may itself
// contain spaces or parens, so parsing starts after that field's closing
// ')' rather than naively splitting the whole line on spaces.
func readPPID(pid int) (int, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	line := string(data)
	closeParen := strings.LastIndexByte(line, ')')
	if closeParen < 0 || closeParen+2 >= len(line) {
		return 0, false
	}
	// Fields after "pid (comm) ": state, ppid, ...
	fields := strings.Fields(line[closeParen+2:])
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}
