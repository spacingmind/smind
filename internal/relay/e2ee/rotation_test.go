package e2ee

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// Key rotation means a brand new session, never an in-session rekey
// (ADR-0007 (e), ADR-0035 §4): a second hello that differs from the first is
// a protocol violation that closes the channel.

func TestDifferentM1AfterEstablishedIsKeyRotation(t *testing.T) {
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
	if bytes.Equal(p.daemon.ClientEphemeralPublicKey(), rotated.Public()) {
		t.Error("channel silently adopted the rotated key")
	}
	if err := p.daemon.Send([]byte("after rotation")); !errors.Is(err, ErrClosed) {
		t.Errorf("Send after rejected rotation = %v, want ErrClosed", err)
	}
}

// TestDuplicateM1ResendsStoredM2AndReady: a byte-identical M1 on an
// established daemon re-sends the stored M2 and R_d (same d_eph, no AEAD
// frame) and the session carries on.
func TestDuplicateM1ResendsStoredM2AndReady(t *testing.T) {
	p := establishedPair(t)
	before := p.daemonConn.written()
	if len(before) != 2 || before[0][4] != frameHello || before[1][4] != frameReady {
		t.Fatalf("daemon handshake wrote %d frames, want M2 then R_d", len(before))
	}

	m1 := rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, p.daemon.ClientEphemeralPublicKey()))
	p.daemonConn.inject(m1)

	msg := []byte("still the same session")
	if err := p.mobile.Send(msg); err != nil {
		t.Fatalf("mobile Send: %v", err)
	}
	got, err := p.daemon.Receive()
	if err != nil {
		t.Fatalf("daemon Receive: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Errorf("daemon received %q, want %q", got, msg)
	}
	after := p.daemonConn.written()
	if len(after) != 4 {
		t.Fatalf("daemon wrote %d frames in total, want 4 (M2, R_d, M2, R_d)", len(after))
	}
	if !bytes.Equal(after[2], before[0]) || !bytes.Equal(after[3], before[1]) {
		t.Error("the re-sent M2/R_d are not byte-identical to the stored ones")
	}
	if p.daemon.Closed() {
		t.Error("a duplicate M1 must not close the channel")
	}
	// The client tolerates the repeated M2 and R_d and keeps working.
	back := []byte("and the reverse direction")
	if err := p.daemon.Send(back); err != nil {
		t.Fatalf("daemon Send: %v", err)
	}
	got, err = p.mobile.Receive()
	if err != nil {
		t.Fatalf("mobile Receive across repeated M2/R_d: %v", err)
	}
	if !bytes.Equal(got, back) {
		t.Errorf("mobile received %q, want %q", got, back)
	}
}

func TestClientToleratesRepeatedM2AndRejectsDifferentM2(t *testing.T) {
	p := establishedPair(t)
	m2 := append([]byte{ProtocolVersion, byte(RoleDaemon)}, p.mobile.DaemonPublicKey()...)
	m2 = append(m2, p.mobile.DaemonEphemeralPublicKey()...)

	p.mobileConn.inject(rawFrame(frameHello, m2))
	p.mobileConn.inject(rawFrame(frameReady, make([]byte, readyLen))) // wrong MAC
	if _, err := p.mobile.Receive(); !errors.Is(err, ErrHandshakeConfirm) {
		t.Fatalf("Receive(repeated ready with another MAC) = %v, want ErrHandshakeConfirm", err)
	}

	p = establishedPair(t)
	other := append([]byte{ProtocolVersion, byte(RoleDaemon)}, p.mobile.DaemonPublicKey()...)
	other = append(other, seedFrom(0x10)...)
	p.mobileConn.inject(rawFrame(frameHello, other))
	if _, err := p.mobile.Receive(); !errors.Is(err, ErrKeyRotation) {
		t.Fatalf("Receive(different M2) = %v, want ErrKeyRotation", err)
	}
	if !p.mobile.Closed() {
		t.Error("channel should be closed after a different M2")
	}
}

func TestReHelloMalformedOnEstablishedSession(t *testing.T) {
	p := establishedPair(t)

	p.daemonConn.inject(rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, p.daemon.ClientEphemeralPublicKey()[:8])))

	if _, err := p.daemon.Receive(); !errors.Is(err, ErrProtocol) {
		t.Errorf("Receive(malformed re-hello) = %v, want ErrProtocol", err)
	}
	if !p.daemon.Closed() {
		t.Error("channel should be closed after a malformed re-hello")
	}
}

func TestHandshakeKeyChangeBeforeReadyIsRejected(t *testing.T) {
	kp, _ := GenerateKeyPair()
	first, _ := GenerateKeyPair()
	second, _ := GenerateKeyPair()

	conn, peer := newMemPipe()
	daemon := newDaemonChannel(t, conn, kp)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- daemon.Handshake(ctx) }()

	for _, k := range []*KeyPair{first, second} {
		if _, err := peer.Write(rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, k.Public()))); err != nil {
			t.Fatalf("write hello: %v", err)
		}
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

// TestHandshakeDuplicateHelloBeforeReadyIsTolerated: a repeated identical M1
// before R_c re-sends M2/R_d and the handshake still completes.
func TestHandshakeDuplicateHelloBeforeReadyIsTolerated(t *testing.T) {
	staticSeed, ephSeed, clientSeed := seedFrom(0x01), seedFrom(0x40), seedFrom(0xa0)
	conn, peer := newMemPipe()
	daemon := newDaemonChannel(t, conn, keyPairFromSeed(t, staticSeed))
	seedEphemeral(t, daemon, ephSeed)

	clientPub := privFromSeed(t, clientSeed).PublicKey().Bytes()
	_, _, rc, _ := newScriptedDaemon(t, staticSeed, ephSeed).reply(t, clientPub)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- daemon.Handshake(ctx) }()

	m1 := rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, clientPub))
	for range 2 {
		if _, err := peer.Write(m1); err != nil {
			t.Fatalf("write hello: %v", err)
		}
	}
	if _, err := peer.Write(rawFrame(frameReady, rc)); err != nil {
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
		if n := len(conn.written()); n != 4 {
			t.Errorf("daemon wrote %d frames, want 4 (M2, R_d, M2, R_d)", n)
		}
	case <-time.After(testTimeout):
		t.Fatal("Handshake hung on a duplicate hello")
	}
}
