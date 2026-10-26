//go:build windows

package host

import (
	"os/exec"

	"github.com/spacingmind/smind/internal/winjob"
)

// setProcessGroup makes cmd start suspended so attachProcessGroup can put it
// in a kill-on-close job object before it (or a child) runs an instruction.
func setProcessGroup(cmd *exec.Cmd) { winjob.Prepare(cmd, 0) }

// processGroup is a kill-on-close job object holding the plugin and everything
// it spawns; a daemon crash closes the handle and takes the tree down too.
type processGroup struct{ job *winjob.Job }

func attachProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	job, err := winjob.Attach(cmd)
	if err != nil {
		return nil, err
	}
	return &processGroup{job: job}, nil
}

// term has no signal to send on Windows: the graceful request is the
// plugin.shutdown notification and the closed stdin, both done by the caller.
func (g *processGroup) term() {}

func (g *processGroup) kill() {
	if g != nil {
		g.job.Close()
	}
}

func (g *processGroup) release() { g.kill() }
