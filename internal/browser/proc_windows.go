//go:build windows

package browser

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/spacingmind/smind/internal/winjob"
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

// startBrowser starts cmd inside a kill-on-close Job Object with the DevTools
// pipe handles inherited (--remote-debugging-io-pipes). Not verified on a real
// Windows machine.
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
	cmd.Args = append(cmd.Args, fmt.Sprintf("--remote-debugging-io-pipes=%d,%d", toR.Fd(), fromW.Fd()))
	cmd.SysProcAttr = &syscall.SysProcAttr{AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(toR.Fd()), syscall.Handle(fromW.Fd())}}
	job, err := winjob.Start(cmd, 0)
	_ = toR.Close()
	_ = fromW.Close()
	if err != nil {
		_ = toW.Close()
		_ = fromR.Close()
		return nil, nil, err
	}
	kill := func() {
		job.Close()
		_ = cmd.Process.Kill()
	}
	return &pipeConn{r: fromR, w: toW}, kill, nil
}

func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}
