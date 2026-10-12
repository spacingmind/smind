//go:build unix

package acp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts cmd in its own process group, so the agent and
// every grandchild it spawns (e.g. an MCP server run via npx -> node ->
// chromium) can be killed together by processGroup.kill.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// processGroup is the Unix flavor of the per-conn kill bookkeeping: the
// agent's pid, which is also its process-group id (see setProcessGroup).
// Windows carries a Job Object handle here instead.
type processGroup struct {
	pid int
}

// attachProcessGroup is the post-Start counterpart of setProcessGroup.
// Unix needs no post-Start step -- the process group existed from the
// first instruction -- so it just records the pid, which is safe to read
// because the process has not been waited on yet.
func attachProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	return &processGroup{pid: cmd.Process.Pid}, nil
}

// kill force-kills the whole process group, falling back to the agent
// process alone if signalling the group fails. Only call it while the
// process has not yet been waited on, so its pid (the group id) can't
// have been reused.
func (g *processGroup) kill() {
	if g == nil {
		return
	}
	if err := syscall.Kill(-g.pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(g.pid, syscall.SIGKILL)
	}
}

// release is a no-op on Unix: a naturally-reaped process group leaves no
// kernel resource the way an open Job Object handle does.
func (g *processGroup) release() {}
