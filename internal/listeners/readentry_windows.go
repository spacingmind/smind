//go:build windows

package listeners

import (
	"fmt"
	"os"
)

// openEntryFile opens file and stats the open descriptor itself (not the
// path, which could be swapped in between), returning it only if it is a
// regular file of at most maxFileSize bytes. Windows has no O_NOFOLLOW or
// O_NONBLOCK; a symlink opens its target and a device fails the regular-file
// check.
func openEntryFile(file string) (*os.File, error) {
	f, err := os.Open(file)
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
