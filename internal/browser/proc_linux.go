//go:build linux

package browser

import "syscall"

// hardenProc makes the kernel kill the browser if the daemon thread that
// started it dies (on top of the pipe: the browser also exits on EOF).
func hardenProc(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGKILL }
