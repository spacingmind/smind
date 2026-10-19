package e2ee

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func TestHandshakeHappyPath(t *testing.T) {
	p := establishedPair(t)

	if !p.daemon.Established() || !p.mobile.Established() {
		t.Fatal("both ends should be established after a successful handshake")
	}
	if !bytes.Equal(p.mobile.DaemonPublicKey(), p.daemonKey.Public()) {
		t.Error("client did not learn the daemon static key")
	}
	if !bytes.Equal(p.daemon.DaemonPublicKey(), p.daemonKey.Public()) {
		t.Error("daemon reports another static key")
	}
	for name, get := range map[string]func(*Channel) []byte{
		"transcript hash":      (*Channel).TranscriptHash,
		"daemon ephemeral key": (*Channel).DaemonEphemeralPublicKey,
		"client ephemeral key": (*Channel).ClientEphemeralPublicKey,
	} {
		if !bytes.Equal(get(p.daemon), get(p.mobile)) || len(get(p.daemon)) != 32 {
			t.Errorf("the two ends disagree on the %s", name)
		}
	}
	if !bytes.Equal(p.daemon.TranscriptHash(), TranscriptHash(p.daemonKey.Public(), p.daemon.DaemonEphemeralPublicKey(), p.daemon.ClientEphemeralPublicKey())) {
		t.Error("TranscriptHash() is not the package-level transcript hash")
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

	if p.mobile.TranscriptHash() != nil || p.daemon.DaemonEphemeralPublicKey() != nil {
		t.Error("handshake values must be nil before the handshake completes")
	}
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
		name        string
		raw         []byte
		closeAfter  bool
		wantErr     error // nil: any error
		wantNoReply bool  // the daemon must write nothing
	}{
		{name: "truncated hello", raw: truncatedHello, closeAfter: true, wantErr: ErrProtocol},
		{name: "empty stream", raw: nil, closeAfter: true},
		{name: "zero length frame", raw: []byte{0, 0, 0, 0}, wantErr: ErrProtocol},
		{name: "oversized length prefix", raw: oversized, wantErr: ErrProtocol},
		{
			name:        "other protocol version",
			raw:         rawFrame(frameHello, helloPayload(ProtocolVersion+1, RoleMobile, peerKey.Public())),
			wantErr:     ErrUnsupportedVersion,
			wantNoReply: true,
		},
		{
			name:        "client claims the daemon role",
			raw:         rawFrame(frameHello, helloPayload(ProtocolVersion, RoleDaemon, peerKey.Public())),
			wantErr:     ErrProtocol,
			wantNoReply: true,
		},
		{
			name:        "unknown role",
			raw:         rawFrame(frameHello, helloPayload(ProtocolVersion, Role(9), peerKey.Public())),
			wantErr:     ErrProtocol,
			wantNoReply: true,
		},
		{
			name:        "short public key",
			raw:         rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, peerKey.Public()[:16])),
			wantErr:     ErrProtocol,
			wantNoReply: true,
		},
		{
			name:        "long public key",
			raw:         rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, append(peerKey.Public(), 0))),
			wantErr:     ErrProtocol,
			wantNoReply: true,
		},
		{name: "empty hello payload", raw: rawFrame(frameHello, nil), wantErr: ErrProtocol, wantNoReply: true},
		{name: "unknown frame type", raw: rawFrame(0x7f, []byte("nope")), wantErr: ErrProtocol, wantNoReply: true},
		{name: "data frame before hello", raw: rawFrame(frameData, make([]byte, 32)), wantErr: ErrProtocol, wantNoReply: true},
		{name: "ready frame before hello", raw: rawFrame(frameReady, nil), wantErr: ErrProtocol, wantNoReply: true},
		{
			// The all-zero point parses, then fails key agreement: no M2.
			name:        "degenerate ephemeral key",
			raw:         rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, make([]byte, PublicKeySize))),
			wantErr:     ErrKeyAgreement,
			wantNoReply: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kp, err := GenerateKeyPair()
			if err != nil {
				t.Fatalf("GenerateKeyPair: %v", err)
			}
			daemonConn, peerConn := newMemPipe()
			daemon := newDaemonChannel(t, daemonConn, kp)

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
				if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
					t.Errorf("Handshake error = %v, want %v", err, tc.wantErr)
				}
				if !daemon.Closed() {
					t.Error("channel should be closed after a failed handshake")
				}
				if tc.wantNoReply && len(daemonConn.written()) != 0 {
					t.Errorf("daemon wrote %d frame(s) to an invalid hello, want none", len(daemonConn.written()))
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

	daemon := newDaemonChannel(t, conn, kp)

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

func TestNewChannelsValidateArguments(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	conn, _ := newMemPipe()

	if _, err := NewDaemonChannel(nil, kp); err == nil {
		t.Error("NewDaemonChannel(nil conn) = nil error, want error")
	}
	if _, err := NewDaemonChannel(conn, nil); err == nil {
		t.Error("NewDaemonChannel(nil keypair) = nil error, want error")
	}
	if _, err := NewClientChannel(nil); err == nil {
		t.Error("NewClientChannel(nil conn) = nil error, want error")
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
