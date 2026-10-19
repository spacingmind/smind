//go:build windows

package store

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func networkFS(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	if strings.HasPrefix(abs, `\\`) && !strings.HasPrefix(abs, `\\?\`) && !strings.HasPrefix(abs, `\\.\`) {
		return true // a UNC path: \\server\share\...
	}
	p, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return false
	}
	var root [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumePathName(p, &root[0], uint32(len(root))); err != nil {
		return false
	}
	return windows.GetDriveType(&root[0]) == windows.DRIVE_REMOTE
}
