//go:build windows

package codex

import (
	"os/exec"

	"github.com/spacingmind/smind/internal/winjob"
)

// setProcessGroup makes cmd start suspended so attachProcessGroup can put
// it in a kill-on-close Job Object before it (or anything it spawns) runs
// a single instruction -- see internal/winjob.
func setProcessGroup(cmd *exec.Cmd) {
	winjob.Prepare(cmd, 0)
}

// processGroup is the Windows flavor of the per-conn kill bookkeeping: a
// kill-on-close Job Object holding the Codex CLI and every process it spawns
// (stdio MCP servers included). A daemon crash closes the handle too, taking
// the tree down with it.
type processGroup struct {
	job *winjob.Job
}

// attachProcessGroup must be called exactly once, immediately after a
// successful cmd.Start() of a cmd prepared by setProcessGroup. On failure
// the child has already been killed and reaped.
func attachProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	job, err := winjob.Attach(cmd)
	if err != nil {
		return nil, err
	}
	return &processGroup{job: job}, nil
}

// kill terminates the Codex CLI and every process it spawned. Termination is
// asynchronous; the caller still reaps via cmd.Wait. A tree that already
// exited is the desired end state, so nothing here errors.
func (g *processGroup) kill() {
	if g != nil {
		g.job.Close()
	}
}

// release closes the job after the process was naturally reaped; any
// descendants left behind are taken down by the kill-on-close semantics,
// matching the Unix process-group contract as closely as the OS allows.
func (g *processGroup) release() {
	g.kill()
}
