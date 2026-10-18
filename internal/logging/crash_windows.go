//go:build windows

package logging

import (
	"os"
	"syscall"
)

// openCrashFile opens path for append with FILE_SHARE_DELETE, so the crash
// handle (held by the Go runtime) never blocks a rotation's rename of
// daemon.log. os.OpenFile does not set that flag, so this needs its own
// CreateFile call, like openShared.
func openCrashFile(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.FILE_APPEND_DATA,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
