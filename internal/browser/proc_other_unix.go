//go:build !linux && !windows

package browser

import "syscall"

// hardenProc: no Pdeathsig here; the browser exits when its pipe closes.
func hardenProc(a *syscall.SysProcAttr) {}
