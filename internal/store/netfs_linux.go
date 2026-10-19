//go:build linux

package store

import "golang.org/x/sys/unix"

// statfs f_type magic numbers (linux/magic.h) of network filesystems.
// 9p is what a WSL2 distro sees for /mnt/c.
var networkFSMagic = map[int64]bool{
	0x6969:     true, // NFS_SUPER_MAGIC
	0x517B:     true, // SMB_SUPER_MAGIC
	0xFF534D42: true, // CIFS_MAGIC_NUMBER
	0xFE534D42: true, // SMB2_MAGIC_NUMBER
	0x5346414F: true, // AFS_SUPER_MAGIC
	0x00C36400: true, // CEPH_SUPER_MAGIC
	0x01021997: true, // V9FS_MAGIC
}

func networkFS(dir string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return false
	}
	return networkFSMagic[int64(st.Type)]
}
