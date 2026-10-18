//go:build windows

package logging

import (
	"os"
	"syscall"
)

// openShared opens path for reading with FILE_SHARE_DELETE, so a reader that
// is following daemon.log (`smind service logs -f`) never blocks the daemon
// from renaming it during rotation. os.Open does not set that flag, and a
// rename of a file with an open handle that lacks it fails on Windows.
func openShared(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
