//go:build darwin

package store

import (
	"bytes"

	"golang.org/x/sys/unix"
)

func networkFS(dir string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return false
	}
	name := st.Fstypename[:]
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	switch string(name) {
	case "nfs", "smbfs", "afpfs", "webdav", "ftp":
		return true
	}
	return false
}
