//go:build darwin

package runsock

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// SystemPeerReader returns the platform's reader (defined per platform).
func SystemPeerReader() PeerReader { return darwinPeerReader{} }

type darwinPeerReader struct{}

func (darwinPeerReader) Peer(conn net.Conn) (PeerCred, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return PeerCred{}, fmt.Errorf("runsock: not a unix connection: %T", conn)
	}
	rc, err := uc.SyscallConn()
	if err != nil {
		return PeerCred{}, err
	}
	var uid uint32
	var pid int
	var sockErr error
	if err := rc.Control(func(fd uintptr) {
		x, e1 := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if e1 != nil {
			sockErr = e1
			return
		}
		p, e2 := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		if e2 != nil {
			sockErr = e2
			return
		}
		uid = x.Uid
		pid = p
	}); err != nil {
		return PeerCred{}, err
	}
	if sockErr != nil {
		return PeerCred{}, sockErr
	}
	if pid <= 0 {
		return PeerCred{}, fmt.Errorf("runsock: the kernel reported no peer pid")
	}
	return PeerCred{UID: uid, PID: pid}, nil
}
