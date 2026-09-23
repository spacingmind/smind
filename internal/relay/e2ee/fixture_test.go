package e2ee

// fixture_test.go generates the golden test vectors Item 3's TypeScript
// E2EE client is checked against byte-for-byte
// (mobile/src/relay/__tests__/e2ee.test.ts), per
// docs/plans/active/mobile-app-milestone-1.md's Item 3 Test Scenarios:
// "the single most important test in this plan, since a subtly wrong HKDF
// info string or nonce construction would fail silently as 'handshake
// hangs' rather than a clear error."
//
// It calls the real, unexported newSession (not a reimplementation), so a
// change to deriveKeys/newSession's actual behavior changes this test's
// expected output too -- the fixture can't silently drift from the real
// wire format.

import (
	"bytes"
	"crypto/ecdh"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// fixtureSeed returns a 32-byte X25519 seed with every byte set to b --
// not secret, purely for a reproducible cross-language fixture.
func fixtureSeed(b byte) []byte {
	return bytes.Repeat([]byte{b}, 32)
}

func fixtureKeyPair(t *testing.T, seed []byte) (*KeyPair, *ecdh.PrivateKey) {
	t.Helper()
	priv, err := ecdh.X25519().NewPrivateKey(seed)
	if err != nil {
		t.Fatalf("new private key: %v", err)
	}
	return &KeyPair{priv: priv}, priv
}

// TestFixtureVectorsForTypeScriptPort prints (and pins, via t.Log plus
// hardcoded expectations) the exact byte sequences the TypeScript port
// must reproduce: both public keys, the daemon->mobile and mobile->daemon
// keys, and one sealed frame in each direction at counter 0. Run with
// `go test -run TestFixtureVectorsForTypeScriptPort -v` to see them
// printed; mobile/src/relay/__tests__/e2ee.test.ts hardcodes the same hex
// literals asserted here.
// These are the exact literals mobile/src/relay/__tests__/e2ee.test.ts
// hardcodes and checks byte-for-byte -- computed once by running this test
// with -v and copied from its own output, not independently derived, so
// both sides are checked against the same numbers per the plan's Test
// Scenario.
const (
	fixtureDaemonPubHex          = "7b4e909bbe7ffe44c465a220037d608ee35897d31ef972f07f74892cb0f73f13"
	fixtureMobilePubHex          = "0faa684ed28867b97f4a6a2dee5df8ce974e76b7018e3f22a1c4cf2678570f20"
	fixtureDaemonToMobileCipher0 = "d6a248cac0f4b322f162664fc9c16b2ef390e3809baa230e1d23499e9d8e64c16442705fa096806590f5bc4b37"
	fixtureMobileToDaemonCipher0 = "233f25b1c411311a8ea4dd9f9034e63a44f9fb4808b83c89ee9de79cca7b2398c27a50e891de"
	fixturePlaintextD2M          = "hello from smind e2ee fixture"
	fixturePlaintextM2D          = "hello back from mobile"
)

func TestFixtureVectorsForTypeScriptPort(t *testing.T) {
	daemonKP, daemonPriv := fixtureKeyPair(t, fixtureSeed(0x11))
	mobileKP, mobilePriv := fixtureKeyPair(t, fixtureSeed(0x22))

	daemonPub := daemonKP.Public()
	mobilePub := mobileKP.Public()
	if got := hex.EncodeToString(daemonPub); got != fixtureDaemonPubHex {
		t.Fatalf("daemon public key = %s, want %s", got, fixtureDaemonPubHex)
	}
	if got := hex.EncodeToString(mobilePub); got != fixtureMobilePubHex {
		t.Fatalf("mobile public key = %s, want %s", got, fixtureMobilePubHex)
	}

	daemonSession, err := newSession(daemonPriv, mobilePriv.PublicKey(), RoleDaemon, daemonPub, mobilePub)
	if err != nil {
		t.Fatalf("daemon newSession: %v", err)
	}
	mobileSession, err := newSession(mobilePriv, daemonPriv.PublicKey(), RoleMobile, daemonPub, mobilePub)
	if err != nil {
		t.Fatalf("mobile newSession: %v", err)
	}

	counter, ciphertext, err := daemonSession.Seal([]byte(fixturePlaintextD2M))
	if err != nil {
		t.Fatalf("daemon seal: %v", err)
	}
	if counter != 0 {
		t.Fatalf("counter = %d, want 0", counter)
	}
	if got := hex.EncodeToString(ciphertext); got != fixtureDaemonToMobileCipher0 {
		t.Fatalf("daemon->mobile ciphertext = %s, want %s", got, fixtureDaemonToMobileCipher0)
	}

	got, err := mobileSession.Open(0, ciphertext)
	if err != nil {
		t.Fatalf("mobile open: %v", err)
	}
	if string(got) != fixturePlaintextD2M {
		t.Fatalf("round trip mismatch: got %q", got)
	}

	replyCounter, replyCiphertext, err := mobileSession.Seal([]byte(fixturePlaintextM2D))
	if err != nil {
		t.Fatalf("mobile seal: %v", err)
	}
	if replyCounter != 0 {
		t.Fatalf("reply counter = %d, want 0", replyCounter)
	}
	if got := hex.EncodeToString(replyCiphertext); got != fixtureMobileToDaemonCipher0 {
		t.Fatalf("mobile->daemon ciphertext = %s, want %s", got, fixtureMobileToDaemonCipher0)
	}

	if _, err := daemonSession.Open(0, replyCiphertext); err != nil {
		t.Fatalf("daemon open reply: %v", err)
	}
}

// fixtureDaemonHelloFrame is the exact wire bytes writeHello/writeFrame
// (handshake.go) produce for the daemon's hello, using the fixture
// keypair -- pinned so the TypeScript port's own frame encoder can be
// checked against the identical bytes, not just the crypto in isolation.
const fixtureDaemonHelloFrame = "000000230101017b4e909bbe7ffe44c465a220037d608ee35897d31ef972f07f74892cb0f73f13"

// TestFixtureHelloFrameForTypeScriptPort pins the exact byte layout of a
// hello frame: 4-byte big-endian length (of type+payload), 1-byte frame
// type (0x01 = hello), then the payload itself (protocol version, role,
// public key) -- see handshake.go's writeFrame/writeHello.
func TestFixtureHelloFrameForTypeScriptPort(t *testing.T) {
	daemonKP, _ := fixtureKeyPair(t, fixtureSeed(0x11))
	payload := make([]byte, 0, helloPayloadLen)
	payload = append(payload, ProtocolVersion, byte(RoleDaemon))
	payload = append(payload, daemonKP.Public()...)

	frame := make([]byte, 4+1+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(1+len(payload)))
	frame[4] = frameHello
	copy(frame[5:], payload)

	if got := hex.EncodeToString(frame); got != fixtureDaemonHelloFrame {
		t.Fatalf("hello frame = %s, want %s", got, fixtureDaemonHelloFrame)
	}
}
