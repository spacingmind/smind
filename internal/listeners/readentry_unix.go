//go:build !windows

package listeners

import (
	"fmt"
	"os"
	"syscall"
)

// openEntryFile opens file without following symlinks and without ever
// blocking, then stats the open descriptor itself (not the path, which could
// be swapped in between) and returns it only if it is a regular file of at
// most maxFileSize bytes.
func openEntryFile(file string) (*os.File, error) {
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && info.Mode().IsRegular() && info.Size() <= maxFileSize {
		return f, nil
	}
	_ = f.Close()
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("listeners: %s is not a regular file of at most %d bytes", file, maxFileSize)
}
