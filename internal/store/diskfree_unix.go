//go:build !windows

package store

import "golang.org/x/sys/unix"

// freeDiskBytes is the space available to an unprivileged writer under dir.
func freeDiskBytes(dir string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
