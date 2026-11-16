package runsock

import (
	"errors"
	"net"
	"testing"
)

func TestFuncPeerReader(t *testing.T) {
	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		c1.Close()
		c2.Close()
	})

	want := PeerCred{UID: 1000, PID: 42}
	sentinel := errors.New("boom")
	calls := 0
	var r PeerReader = FuncPeerReader(func(c net.Conn) (PeerCred, error) {
		calls++
		if c != c1 {
			t.Errorf("Peer passed conn %p, want %p", c, c1)
		}
		return want, sentinel
	})

	got, err := r.Peer(c1)
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
	if got != want {
		t.Errorf("Peer() = %+v, want %+v", got, want)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("Peer() error = %v, want %v", err, sentinel)
	}
}
