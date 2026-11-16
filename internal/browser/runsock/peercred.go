// Package runsock: a per-run local socket for the browser child, and the checks
// that decide who may talk to it.
package runsock

import (
	"errors"
	"net"
)

// PeerCred is who is on the other end of a local connection.
type PeerCred struct {
	UID uint32
	PID int
}

// PeerReader reads the credentials the kernel recorded for the peer of conn.
type PeerReader interface {
	Peer(conn net.Conn) (PeerCred, error)
}

// ErrPeerUnsupported: this platform has no peer-credential reader.
var ErrPeerUnsupported = errors.New("runsock: peer credentials are not available on this platform")

// FuncPeerReader adapts a function to PeerReader (the fake used in tests, here and in other packages).
type FuncPeerReader func(net.Conn) (PeerCred, error)

func (f FuncPeerReader) Peer(c net.Conn) (PeerCred, error) { return f(c) }

// SystemPeerReader returns the platform's reader (defined per platform).
