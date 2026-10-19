package e2ee

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// TestHandshakeFrameCapRejectsBeforeAllocation: during a handshake a frame
// header above 128 bytes is a protocol error right away, for both roles. The
// header below announces exactly maxFrameLen, which the established-state
// reader would accept, and no body ever follows: reading it would hang until
// the deadline.
func TestHandshakeFrameCapRejectsBeforeAllocation(t *testing.T) {
	header := []byte{0x00, 0x10, 0x00, 0x00}
	for _, role := range []string{"daemon", "client"} {
		t.Run(role, func(t *testing.T) {
			conn, peer := newMemPipe()
			var ch *Channel
			if role == "daemon" {
				ch = newDaemonChannel(t, conn, keyPairFromSeed(t, seedFrom(0x01)))
			} else {
				ch = newClientChannel(t, conn)
			}
			if _, err := peer.Write(header); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()
			start := time.Now()
			if err := ch.Handshake(ctx); !errors.Is(err, ErrProtocol) {
				t.Fatalf("Handshake = %v, want ErrProtocol", err)
			}
			if time.Since(start) > testTimeout/2 {
				t.Fatal("the handshake waited for the oversized body")
			}
			if role == "daemon" && len(conn.written()) != 0 {
				t.Error("daemon wrote after an oversized header")
			}
		})
	}
}

// TestEstablishedReceiveKeepsTheLargeFrameLimit: the 128-byte cap is for the
// handshake only; a data frame well above it is received.
func TestEstablishedReceiveKeepsTheLargeFrameLimit(t *testing.T) {
	p := establishedPair(t)
	msg := bytes.Repeat([]byte("x"), 10_000)
	if err := p.daemon.Send(msg); err != nil {
		t.Fatal(err)
	}
	got, err := p.mobile.Receive()
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("Receive = %d bytes, %v", len(got), err)
	}
}
