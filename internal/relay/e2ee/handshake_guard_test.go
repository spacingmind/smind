package e2ee

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestConcurrentHandshakeIsRejected: a second Handshake call while the first
// still runs fails with ErrHandshakeInProgress and does not disturb it; the
// guard clears when the first one ends.
func TestConcurrentHandshakeIsRejected(t *testing.T) {
	conn, peer := newMemPipe()
	client := newClientChannel(t, conn)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	first := make(chan error, 1)
	go func() { first <- client.Handshake(ctx) }()
	deadline := time.Now().Add(testTimeout)
	for len(conn.written()) == 0 { // M1 is out: the first call is in flight
		if time.Now().After(deadline) {
			t.Fatal("first handshake never started")
		}
		time.Sleep(time.Millisecond)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := client.Handshake(ctx); !errors.Is(err, ErrHandshakeInProgress) {
				t.Errorf("concurrent Handshake = %v, want ErrHandshakeInProgress", err)
			}
		}()
	}
	wg.Wait()
	if n := len(conn.written()); n != 1 {
		t.Errorf("client wrote %d frames, a concurrent call must not send a second M1", n)
	}
	_ = peer.Close()
	if err := <-first; err == nil {
		t.Fatal("first handshake succeeded against a hung-up peer")
	}
	if client.handshakingNow() {
		t.Fatal("the in-progress flag was not cleared")
	}
}

func (c *Channel) handshakingNow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.handshaking
}
