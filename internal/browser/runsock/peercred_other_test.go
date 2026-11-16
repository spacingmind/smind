//go:build !linux && !darwin

package runsock

import (
	"errors"
	"net"
	"testing"
)

func TestSystemPeerReaderUnsupported(t *testing.T) {
	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		c1.Close()
		c2.Close()
	})

	_, err := SystemPeerReader().Peer(c1)
	if !errors.Is(err, ErrPeerUnsupported) {
		t.Errorf("Peer() error = %v, want ErrPeerUnsupported", err)
	}
}
