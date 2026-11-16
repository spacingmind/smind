//go:build linux

package runsock

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// SystemPeerReader returns the platform's reader (defined per platform).
func SystemPeerReader() PeerReader { return linuxPeerReader{} }

type linuxPeerReader struct{}

func (linuxPeerReader) Peer(conn net.Conn) (PeerCred, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return PeerCred{}, fmt.Errorf("runsock: not a unix connection: %T", conn)
	}
	rc, err := uc.SyscallConn()
	if err != nil {
		return PeerCred{}, err
	}
	var ucred *unix.Ucred
	var sockErr error
	if err := rc.Control(func(fd uintptr) {
		ucred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return PeerCred{}, err
	}
	if sockErr != nil {
		return PeerCred{}, sockErr
	}
	if ucred.Pid <= 0 {
		return PeerCred{}, fmt.Errorf("runsock: the kernel reported no peer pid")
	}
	return PeerCred{UID: ucred.Uid, PID: int(ucred.Pid)}, nil
}
