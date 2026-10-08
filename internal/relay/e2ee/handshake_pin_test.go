package e2ee

import (
	"context"
	"errors"
	"testing"
)

// TestHandshakePinMismatchFails: a mobile end pinned to the daemon key from
// its pairing offer must refuse a handshake with a different daemon key —
// the same semantics as mobile/src/relay/e2ee.ts (daemonPublicKeyIfMobile)
// and desktop channel.rs (pin_peer_public_key -> PinMismatch).
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
	// Only the hello went out: no ready frame confirming the session.
	for _, w := range p.mobileConn.written() {
		if len(w) >= 5 && w[4] == frameReady {
			t.Fatal("mobile sent a ready frame to a peer whose key did not match the pin")
		}
	}
	// The substituted daemon never completes either.
	if err := <-daemonErr; err == nil {
		t.Fatal("daemon handshake succeeded against a mobile that rejected its key")
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
		t.Fatalf("pinned mobile Handshake with the right key: %v", err)
	}
	if !p.mobile.Established() {
		t.Fatal("mobile not established")
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

// TestHandshakeNoPinUnchanged: without the option the behavior is exactly
// as before (the existing happy-path tests cover this too; this one names
// the contract).
func TestHandshakeNoPinUnchanged(t *testing.T) {
	p := newPair(t)
	p.handshake(t)
	if !p.mobile.Established() || !p.daemon.Established() {
		t.Fatal("unpinned handshake did not establish")
	}
}
