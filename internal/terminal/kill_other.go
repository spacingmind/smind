//go:build !linux

package terminal

import "syscall"

// killTree is a best-effort fallback for platforms without a /proc
// filesystem to walk (see kill_linux.go's version, which is what actually
// runs on this project's target platform): it kills rootPid directly plus
// rootPid's own process group (pty.Start makes the shell a session/
// process-group leader via Setsid, so its pgid equals its pid). This
// covers rootPid itself and any descendant that happens to still share
// its process group, but -- unlike the Linux implementation -- won't
// reach a descendant that an interactive shell's job control gave its own
// process group (e.g. a background job), since there's no portable way to
// enumerate a process's descendants without /proc.
func killTree(rootPid int) {
	_ = syscall.Kill(-rootPid, syscall.SIGKILL)
	_ = syscall.Kill(rootPid, syscall.SIGKILL)
}
