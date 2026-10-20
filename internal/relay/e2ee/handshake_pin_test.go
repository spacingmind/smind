package e2ee

import (
	"context"
	"errors"
	"testing"
)

// TestHandshakePinMismatchFails: a client pinned to the daemon key from its
// pairing offer must refuse a handshake with a different daemon key — the same
// semantics as mobile/src/relay/e2ee.ts (daemonPublicKeyIfMobile) and desktop
// channel.rs (pin_peer_public_key -> PinMismatch).
func TestHandshakePinMismatchFails(t *testing.T) {
	p := newPair(t)
	offerKey, err := GenerateKeyPair() // the key the pairing offer named
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	daemonErr := make(chan error, 1)
	go func() { daemonErr <- p.daemon.Handshake(ctx) }() // substituted daemon, other key

	err = p.mobile.Handshake(ctx, WithExpectedPeerKey(offerKey.Public()))
	if !errors.Is(err, ErrPeerKeyMismatch) {
		t.Fatalf("pinned Handshake with substituted daemon key = %v, want ErrPeerKeyMismatch", err)
	}
	if !p.mobile.Closed() || p.mobile.Established() {
		t.Fatalf("mismatch must close the channel without establishing (closed=%v established=%v)", p.mobile.Closed(), p.mobile.Established())
	}
	if p.mobile.Session() != nil {
		t.Fatal("no session may be derived for a mismatched peer")
	}
	// Only M1 went out: no ready frame confirming the session.
	if w := p.mobileConn.written(); len(w) != 1 || w[0][4] != frameHello {
		t.Fatalf("client wrote %d frame(s) after a pin mismatch, want only M1", len(w))
	}
	// The substituted daemon never completes either.
	if err := <-daemonErr; err == nil {
		t.Fatal("daemon handshake succeeded against a client that rejected its key")
	}
}

// TestPinMismatchBeforeAnyDH: with a pin that does not match, the client
// rejects on the static key alone. A daemon_eph_pub that would make the key
// agreement fail (the all-zero point) must therefore still surface as
// ErrPeerKeyMismatch, not ErrKeyAgreement.
func TestPinMismatchBeforeAnyDH(t *testing.T) {
	conn, peer := newMemPipe()
	client := newClientChannel(t, conn)
	daemon := newScriptedDaemon(t, seedFrom(0x01), seedFrom(0x40))

	m2 := append([]byte{ProtocolVersion, byte(RoleDaemon)}, daemon.static.PublicKey().Bytes()...)
	m2 = append(m2, make([]byte, PublicKeySize)...) // low-order daemon ephemeral
	if _, err := peer.Write(rawFrame(frameHello, m2)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	other := keyPairFromSeed(t, seedFrom(0x80))
	err := client.Handshake(ctx, WithExpectedPeerKey(other.Public()))
	if !errors.Is(err, ErrPeerKeyMismatch) {
		t.Fatalf("Handshake = %v, want ErrPeerKeyMismatch (pin is checked before any DH)", err)
	}
}

func TestHandshakePinMatchSucceeds(t *testing.T) {
	p := newPair(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	mobileErr := make(chan error, 1)
	go func() { mobileErr <- p.mobile.Handshake(ctx, WithExpectedPeerKey(p.daemonKey.Public())) }()
	if err := p.daemon.Handshake(ctx); err != nil {
		t.Fatalf("daemon Handshake: %v", err)
	}
	if err := <-mobileErr; err != nil {
		t.Fatalf("pinned client Handshake with the right key: %v", err)
	}
	if !p.mobile.Established() {
		t.Fatal("client not established")
	}
}

// TestHandshakePinFailsClosed: pinning an empty/malformed key can never
// match, so a missing offer key cannot degrade into "no pin".
func TestHandshakePinFailsClosed(t *testing.T) {
	for name, pin := range map[string][]byte{"nil": nil, "empty": {}, "short": {1, 2, 3}} {
		t.Run(name, func(t *testing.T) {
			p := newPair(t)
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()
			go func() { _ = p.daemon.Handshake(ctx) }()
			if err := p.mobile.Handshake(ctx, WithExpectedPeerKey(pin)); !errors.Is(err, ErrPeerKeyMismatch) {
				t.Fatalf("Handshake with %s pin = %v, want ErrPeerKeyMismatch", name, err)
			}
		})
	}
}

// TestHandshakeNoPinUnchanged: without the option the handshake runs unpinned
// (the happy-path tests cover this too; this one names the contract).
func TestHandshakeNoPinUnchanged(t *testing.T) {
	p := newPair(t)
	p.handshake(t)
	if !p.mobile.Established() || !p.daemon.Established() {
		t.Fatal("unpinned handshake did not establish")
	}
}
