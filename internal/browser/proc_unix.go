//go:build !windows

package browser

import (
	"io"
	"os"
	"os/exec"
	"syscall"
)

type pipeConn struct{ r, w *os.File }

func (p *pipeConn) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeConn) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeConn) Close() error {
	e1, e2 := p.r.Close(), p.w.Close()
	if e1 != nil {
		return e1
	}
	return e2
}

// startBrowser starts cmd with the DevTools pipe pair on fds 3 (the browser
// reads) and 4 (the browser writes), in its own process group, and returns our
// ends plus a function that kills the whole tree.
func startBrowser(cmd *exec.Cmd) (io.ReadWriteCloser, func(), error) {
	toR, toW, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	fromR, fromW, err := os.Pipe()
	if err != nil {
		_ = toR.Close()
		_ = toW.Close()
		return nil, nil, err
	}
	cmd.ExtraFiles = []*os.File{toR, fromW}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own group: the whole tree can be killed
	hardenProc(cmd.SysProcAttr)
	err = cmd.Start()
	_ = toR.Close() // the child's ends
	_ = fromW.Close()
	if err != nil {
		_ = toW.Close()
		_ = fromR.Close()
		return nil, nil, err
	}
	kill := func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // ESRCH once everything is gone is harmless
		_ = cmd.Process.Kill()
	}
	return &pipeConn{r: fromR, w: toW}, kill, nil
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
