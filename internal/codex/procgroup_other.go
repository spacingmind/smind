//go:build !unix && !windows

package codex

import "os/exec"

// setProcessGroup is a no-op where process groups aren't available.
func setProcessGroup(*exec.Cmd) {}

// processGroup is the fallback flavor where process groups aren't
// available: it can only kill the CLI process itself.
type processGroup struct {
	cmd *exec.Cmd
}

// attachProcessGroup is the post-Start counterpart of setProcessGroup; on
// platforms without process groups it only records cmd.
func attachProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	return &processGroup{cmd: cmd}, nil
}

// kill kills just the CLI process where process groups aren't available.
func (g *processGroup) kill() {
	if g == nil || g.cmd.Process == nil {
		return
	}
	_ = g.cmd.Process.Kill()
}

// release is a no-op: there is no kernel resource to release.
func (g *processGroup) release() {}
