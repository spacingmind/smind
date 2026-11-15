//go:build !linux && !darwin

package runsock

import "net"

// SystemPeerReader returns the platform's reader (defined per platform).
func SystemPeerReader() PeerReader { return otherPeerReader{} }

type otherPeerReader struct{}

func (otherPeerReader) Peer(net.Conn) (PeerCred, error) {
	return PeerCred{}, ErrPeerUnsupported
}
