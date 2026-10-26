//go:build !unix && !windows

package host

import "os/exec"

func setProcessGroup(*exec.Cmd) {}

// processGroup can only reach the leader where process groups do not exist.
type processGroup struct{ cmd *exec.Cmd }

func attachProcessGroup(cmd *exec.Cmd) (*processGroup, error) { return &processGroup{cmd: cmd}, nil }

func (g *processGroup) term() {}

func (g *processGroup) kill() {
	if g != nil && g.cmd.Process != nil {
		_ = g.cmd.Process.Kill()
	}
}

func (g *processGroup) release() {}
