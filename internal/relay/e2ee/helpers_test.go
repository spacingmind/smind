package e2ee

import (
	"context"
	"crypto/ecdh"
	"encoding/hex"
	"testing"
	"time"
)

const testTimeout = 5 * time.Second

// seedFrom returns the 32-byte seed start, start+1, …, start+31 (the ADR-0035
// §7 reference seeds). Not secret: purely for reproducible vectors.
func seedFrom(start byte) []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = start + byte(i)
	}
	return seed
}

func privFromSeed(t testing.TB, seed []byte) *ecdh.PrivateKey {
	t.Helper()
	priv, err := ecdh.X25519().NewPrivateKey(seed)
	if err != nil {
		t.Fatalf("new private key: %v", err)
	}
	return priv
}

func keyPairFromSeed(t testing.TB, seed []byte) *KeyPair {
	t.Helper()
	return &KeyPair{priv: privFromSeed(t, seed)}
}

// seedEphemeral makes the channel draw its (single) handshake ephemeral from
// seed. In-package only: no exported API can do this.
func seedEphemeral(t testing.TB, c *Channel, seed []byte) {
	t.Helper()
	priv := privFromSeed(t, seed)
	c.newEphemeral = func() (*ecdh.PrivateKey, error) { return priv, nil }
}

func mustHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func newDaemonChannel(t testing.TB, conn *memConn, kp *KeyPair) *Channel {
	t.Helper()
	c, err := NewDaemonChannel(conn, kp)
	if err != nil {
		t.Fatalf("NewDaemonChannel: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func newClientChannel(t testing.TB, conn *memConn) *Channel {
	t.Helper()
	c, err := NewClientChannel(conn)
	if err != nil {
		t.Fatalf("NewClientChannel: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// pair is a daemon end and a client end joined by an in-process pipe — the
// stand-in for "both connected through the relay".
type pair struct {
	daemon     *Channel
	mobile     *Channel
	daemonConn *memConn
	mobileConn *memConn
	daemonKey  *KeyPair
}

func newPair(t *testing.T) *pair {
	t.Helper()
	daemonKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("daemon GenerateKeyPair: %v", err)
	}
	return newPairWithKey(t, daemonKey)
}

func newPairWithKey(t *testing.T, daemonKey *KeyPair) *pair {
	t.Helper()
	daemonConn, mobileConn := newMemPipe()
	return &pair{
		daemon:     newDaemonChannel(t, daemonConn, daemonKey),
		mobile:     newClientChannel(t, mobileConn),
		daemonConn: daemonConn,
		mobileConn: mobileConn,
		daemonKey:  daemonKey,
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

// scriptedDaemon is a hand-driven responder with known keys, so tests can
// build the exact frames (and tampered variants) a client would receive.
type scriptedDaemon struct {
	static, eph *ecdh.PrivateKey
}

func newScriptedDaemon(t testing.TB, staticSeed, ephSeed []byte) *scriptedDaemon {
	return &scriptedDaemon{static: privFromSeed(t, staticSeed), eph: privFromSeed(t, ephSeed)}
}

// reply returns the M2 payload, the R_d payload and the R_c payload the
// client with ephemeral public key clientEph would produce, per ADR-0035 §2.
func (d *scriptedDaemon) reply(t testing.TB, clientEph []byte) (m2, rd, rc []byte, sched *schedule) {
	t.Helper()
	es, err := dh(d.static, clientEph)
	if err != nil {
		t.Fatal(err)
	}
	ee, err := dh(d.eph, clientEph)
	if err != nil {
		t.Fatal(err)
	}
	dpub, deph := d.static.PublicKey().Bytes(), d.eph.PublicKey().Bytes()
	sched, err = deriveSchedule(es, ee, dpub, deph, clientEph)
	if err != nil {
		t.Fatal(err)
	}
	m2 = append([]byte{ProtocolVersion, byte(RoleDaemon)}, dpub...)
	m2 = append(m2, deph...)
	return m2, confirmTag(sched.confirmD, sched.th), confirmTag(sched.confirmM, sched.th), sched
}
