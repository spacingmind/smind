package e2ee

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

const testTimeout = 5 * time.Second

// pair is a daemon end and a mobile end joined by an in-process pipe — the
// stand-in for "both sides connected through the relay".
type pair struct {
	daemon     *Channel
	mobile     *Channel
	daemonConn *memConn
	mobileConn *memConn
	daemonKey  *KeyPair
	mobileKey  *KeyPair
}

func newPair(t *testing.T) *pair {
	t.Helper()

	daemonKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("daemon GenerateKeyPair: %v", err)
	}
	mobileKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("mobile GenerateKeyPair: %v", err)
	}

	daemonConn, mobileConn := newMemPipe()
	daemon, err := NewChannel(daemonConn, daemonKey, RoleDaemon)
	if err != nil {
		t.Fatalf("NewChannel(daemon): %v", err)
	}
	mobile, err := NewChannel(mobileConn, mobileKey, RoleMobile)
	if err != nil {
		t.Fatalf("NewChannel(mobile): %v", err)
	}
	t.Cleanup(func() {
		_ = daemon.Close()
		_ = mobile.Close()
	})

	return &pair{
		daemon:     daemon,
		mobile:     mobile,
		daemonConn: daemonConn,
		mobileConn: mobileConn,
		daemonKey:  daemonKey,
		mobileKey:  mobileKey,
	}
}

// handshake runs both ends' handshakes concurrently and fails the test if
// either errors.
func (p *pair) handshake(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	mobileErr := make(chan error, 1)
	go func() { mobileErr <- p.mobile.Handshake(ctx) }()

	if err := p.daemon.Handshake(ctx); err != nil {
		t.Fatalf("daemon Handshake: %v", err)
	}
	if err := <-mobileErr; err != nil {
		t.Fatalf("mobile Handshake: %v", err)
	}
}

func establishedPair(t *testing.T) *pair {
	t.Helper()
	p := newPair(t)
	p.handshake(t)
	return p
}

func TestHandshakeHappyPath(t *testing.T) {
	p := establishedPair(t)

	if !p.daemon.Established() || !p.mobile.Established() {
		t.Fatal("both ends should be established after a successful handshake")
	}
	if !bytes.Equal(p.daemon.PeerPublicKey(), p.mobileKey.Public()) {
		t.Error("daemon did not learn the mobile public key")
	}
	if !bytes.Equal(p.mobile.PeerPublicKey(), p.daemonKey.Public()) {
		t.Error("mobile did not learn the daemon public key")
	}

	// Both directions encrypt and decrypt under the same agreed key.
	toMobile := []byte(`{"kind":"task.update","id":"t-1"}`)
	if err := p.daemon.Send(toMobile); err != nil {
		t.Fatalf("daemon Send: %v", err)
	}
	got, err := p.mobile.Receive()
	if err != nil {
		t.Fatalf("mobile Receive: %v", err)
	}
	if !bytes.Equal(got, toMobile) {
		t.Errorf("mobile received %q, want %q", got, toMobile)
	}

	toDaemon := []byte(`{"kind":"prompt","text":"ship it"}`)
	if err := p.mobile.Send(toDaemon); err != nil {
		t.Fatalf("mobile Send: %v", err)
	}
	got, err = p.daemon.Receive()
	if err != nil {
		t.Fatalf("daemon Receive: %v", err)
	}
	if !bytes.Equal(got, toDaemon) {
		t.Errorf("daemon received %q, want %q", got, toDaemon)
	}
}

func TestWireCarriesOnlyCiphertext(t *testing.T) {
	p := establishedPair(t)

	plaintext := []byte("the relay must never see this")
	if err := p.daemon.Send(plaintext); err != nil {
		t.Fatalf("daemon Send: %v", err)
	}
	if _, err := p.mobile.Receive(); err != nil {
		t.Fatalf("mobile Receive: %v", err)
	}

	for i, frame := range p.daemonConn.written() {
		if bytes.Contains(frame, plaintext) {
			t.Fatalf("frame %d written to the wire contains plaintext", i)
		}
	}
}

func TestSendAndReceiveBeforeHandshake(t *testing.T) {
	p := newPair(t)

	if err := p.daemon.Send([]byte("early")); !errors.Is(err, ErrNotEstablished) {
		t.Errorf("Send before handshake = %v, want ErrNotEstablished", err)
	}
	if _, err := p.daemon.Receive(); !errors.Is(err, ErrNotEstablished) {
		t.Errorf("Receive before handshake = %v, want ErrNotEstablished", err)
	}
}

func TestHandshakeTwiceIsRejected(t *testing.T) {
	p := establishedPair(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := p.daemon.Handshake(ctx); !errors.Is(err, ErrAlreadyEstablished) {
		t.Errorf("second Handshake = %v, want ErrAlreadyEstablished", err)
	}
}

func TestHandshakeRejectsMalformedHello(t *testing.T) {
	peerKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	oversized := make([]byte, 4)
	binary.BigEndian.PutUint32(oversized, 1<<30)

	truncatedHello := rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, peerKey.Public()))
	truncatedHello = truncatedHello[:10] // header promises more than follows

	tests := []struct {
		name         string
		raw          []byte
		closeAfter   bool
		wantProtocol bool
	}{
		{name: "truncated hello", raw: truncatedHello, closeAfter: true, wantProtocol: true},
		{name: "empty stream", raw: nil, closeAfter: true},
		{name: "zero length frame", raw: []byte{0, 0, 0, 0}, wantProtocol: true},
		{name: "oversized length prefix", raw: oversized, wantProtocol: true},
		{
			name:         "wrong protocol version",
			raw:          rawFrame(frameHello, helloPayload(ProtocolVersion+1, RoleMobile, peerKey.Public())),
			wantProtocol: true,
		},
		{
			name:         "peer claims the same role",
			raw:          rawFrame(frameHello, helloPayload(ProtocolVersion, RoleDaemon, peerKey.Public())),
			wantProtocol: true,
		},
		{
			name:         "unknown role",
			raw:          rawFrame(frameHello, helloPayload(ProtocolVersion, Role(9), peerKey.Public())),
			wantProtocol: true,
		},
		{
			name:         "short public key",
			raw:          rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, peerKey.Public()[:16])),
			wantProtocol: true,
		},
		{
			name:         "long public key",
			raw:          rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, append(peerKey.Public(), 0))),
			wantProtocol: true,
		},
		{
			name:         "empty hello payload",
			raw:          rawFrame(frameHello, nil),
			wantProtocol: true,
		},
		{
			name:         "unknown frame type",
			raw:          rawFrame(0x7f, []byte("nope")),
			wantProtocol: true,
		},
		{
			name:         "data frame before hello",
			raw:          rawFrame(frameData, make([]byte, 32)),
			wantProtocol: true,
		},
		{
			name:         "ready frame before hello",
			raw:          rawFrame(frameReady, nil),
			wantProtocol: true,
		},
		{
			// A valid-looking hello whose key is the all-zero point: rejected
			// at key agreement rather than parsing, but still rejected.
			name: "degenerate public key",
			raw:  rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, make([]byte, PublicKeySize))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kp, err := GenerateKeyPair()
			if err != nil {
				t.Fatalf("GenerateKeyPair: %v", err)
			}
			daemonConn, peerConn := newMemPipe()
			daemon, err := NewChannel(daemonConn, kp, RoleDaemon)
			if err != nil {
				t.Fatalf("NewChannel: %v", err)
			}
			t.Cleanup(func() { _ = daemon.Close() })

			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()
			errCh := make(chan error, 1)
			go func() { errCh <- daemon.Handshake(ctx) }()

			if len(tc.raw) > 0 {
				if _, err := peerConn.Write(tc.raw); err != nil {
					t.Fatalf("write hostile frame: %v", err)
				}
			}
			if tc.closeAfter || len(tc.raw) == 0 {
				_ = peerConn.Close()
			}

			select {
			case err := <-errCh:
				if err == nil {
					t.Fatal("Handshake accepted a malformed hello")
				}
				if tc.wantProtocol && !errors.Is(err, ErrProtocol) {
					t.Errorf("Handshake error = %v, want ErrProtocol", err)
				}
				if !daemon.Closed() {
					t.Error("channel should be closed after a failed handshake")
				}
			case <-time.After(testTimeout):
				t.Fatal("Handshake hung on a malformed hello")
			}
		})
	}
}

func TestHandshakeDoesNotHangOnSilentPeer(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	conn, peer := newMemPipe()
	t.Cleanup(func() { _ = peer.Close() })

	daemon, err := NewChannel(conn, kp, RoleDaemon)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}

	// The peer connects and then says nothing at all: the context deadline,
	// not the peer, has to end the handshake.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- daemon.Handshake(ctx) }()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Handshake completed against a silent peer")
		}
		if !daemon.Closed() {
			t.Error("channel should be closed after the handshake deadline")
		}
	case <-time.After(testTimeout):
		t.Fatal("Handshake hung past its context deadline")
	}
}

func TestNewChannelValidatesArguments(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	conn, _ := newMemPipe()

	if _, err := NewChannel(nil, kp, RoleDaemon); err == nil {
		t.Error("NewChannel(nil conn) = nil error, want error")
	}
	if _, err := NewChannel(conn, nil, RoleDaemon); err == nil {
		t.Error("NewChannel(nil keypair) = nil error, want error")
	}
	if _, err := NewChannel(conn, kp, Role(7)); err == nil {
		t.Error("NewChannel(unknown role) = nil error, want error")
	}
}

func TestRoleString(t *testing.T) {
	if got := RoleDaemon.String(); got != "daemon" {
		t.Errorf("RoleDaemon.String() = %q", got)
	}
	if got := RoleMobile.String(); got != "mobile" {
		t.Errorf("RoleMobile.String() = %q", got)
	}
	if got := Role(9).String(); got != "role(9)" {
		t.Errorf("Role(9).String() = %q", got)
	}
}
