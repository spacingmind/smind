package e2ee

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Key rotation in v1 means a brand new session, never an in-session rekey
// (ADR-0007 (e)): a second hello carrying a different key is a protocol
// violation that closes the channel.

func TestReHelloWithDifferentKeyClosesChannel(t *testing.T) {
	p := establishedPair(t)

	rotated, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	p.daemonConn.inject(rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, rotated.Public())))

	if _, err := p.daemon.Receive(); !errors.Is(err, ErrKeyRotation) {
		t.Fatalf("Receive(re-hello with a new key) = %v, want ErrKeyRotation", err)
	}
	if !p.daemon.Closed() {
		t.Error("channel should be closed after a rejected re-handshake")
	}
	// The session must not have been re-keyed behind the caller's back.
	if got := p.daemon.PeerPublicKey(); string(got) == string(rotated.Public()) {
		t.Error("channel silently adopted the rotated key")
	}
	if err := p.daemon.Send([]byte("after rotation")); !errors.Is(err, ErrClosed) {
		t.Errorf("Send after rejected rotation = %v, want ErrClosed", err)
	}
}

func TestReHelloWithSameKeyIsTolerated(t *testing.T) {
	p := establishedPair(t)

	// A peer that never saw our ready frame retries its hello; same key, so
	// this is a harmless retry rather than an attack.
	p.daemonConn.inject(rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, p.mobileKey.Public())))

	msg := []byte("still the same session")
	if err := p.mobile.Send(msg); err != nil {
		t.Fatalf("mobile Send: %v", err)
	}
	got, err := p.daemon.Receive()
	if err != nil {
		t.Fatalf("daemon Receive: %v", err)
	}
	if string(got) != string(msg) {
		t.Errorf("daemon received %q, want %q", got, msg)
	}
	if p.daemon.Closed() {
		t.Error("a duplicate hello with the same key must not close the channel")
	}
}

func TestReHelloMalformedOnEstablishedSession(t *testing.T) {
	p := establishedPair(t)

	p.daemonConn.inject(rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, p.mobileKey.Public()[:8])))

	if _, err := p.daemon.Receive(); !errors.Is(err, ErrProtocol) {
		t.Errorf("Receive(malformed re-hello) = %v, want ErrProtocol", err)
	}
	if !p.daemon.Closed() {
		t.Error("channel should be closed after a malformed re-hello")
	}
}

func TestHandshakeKeyChangeBeforeReadyIsRejected(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	first, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	second, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	conn, peer := newMemPipe()
	daemon, err := NewChannel(conn, kp, RoleDaemon)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	t.Cleanup(func() { _ = daemon.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- daemon.Handshake(ctx) }()

	if _, err := peer.Write(rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, first.Public()))); err != nil {
		t.Fatalf("write first hello: %v", err)
	}
	if _, err := peer.Write(rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, second.Public()))); err != nil {
		t.Fatalf("write second hello: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrKeyRotation) {
			t.Errorf("Handshake = %v, want ErrKeyRotation", err)
		}
		if !daemon.Closed() {
			t.Error("channel should be closed after a mid-handshake key change")
		}
	case <-time.After(testTimeout):
		t.Fatal("Handshake hung on a mid-handshake key change")
	}
}

func TestHandshakeDuplicateHelloBeforeReadyIsTolerated(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	peerKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	conn, peer := newMemPipe()
	daemon, err := NewChannel(conn, kp, RoleDaemon)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	t.Cleanup(func() { _ = daemon.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- daemon.Handshake(ctx) }()

	hello := rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, peerKey.Public()))
	for range 2 {
		if _, err := peer.Write(hello); err != nil {
			t.Fatalf("write hello: %v", err)
		}
	}
	if _, err := peer.Write(rawFrame(frameReady, nil)); err != nil {
		t.Fatalf("write ready: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Handshake with a duplicate hello: %v", err)
		}
		if !daemon.Established() {
			t.Error("channel should be established")
		}
	case <-time.After(testTimeout):
		t.Fatal("Handshake hung on a duplicate hello")
	}
}
