//go:build unix

package host

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts cmd in its own process group, so the plugin and every
// grandchild it spawns can be signalled and killed together.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// processGroup is the Unix kill handle: the leader's pid, which is also the
// group id (see setProcessGroup).
type processGroup struct{ pid int }

func attachProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	return &processGroup{pid: cmd.Process.Pid}, nil
}

// term asks the whole group to stop (SIGTERM).
func (g *processGroup) term() {
	if g != nil {
		_ = syscall.Kill(-g.pid, syscall.SIGTERM)
	}
}

// kill force-kills the whole group (SIGKILL), falling back to the leader alone
// if signalling the group fails. A group with no member left is the desired
// end state, so ESRCH is not an error.
func (g *processGroup) kill() {
	if g == nil {
		return
	}
	if err := syscall.Kill(-g.pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		_ = syscall.Kill(g.pid, syscall.SIGKILL)
	}
}

func (g *processGroup) release() {}
