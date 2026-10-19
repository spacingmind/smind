package devices

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/relay/e2ee"
)

var updateVector = flag.Bool("update-vector", false, "rewrite testdata/device_hello_vector.json (v2)")

func fixedBytes(start byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = start + byte(i)
	}
	return b
}

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// session is a fixed route session id and challenge for the tests: a hello is
// only valid for the (keys, session id, challenge) it was signed over.
var (
	testSession   = []byte("sess-0123456789abcdef")
	testChallenge = fixedBytes(0xC0)
)

func TestDeviceIDMustMatchPublicKeyHash(t *testing.T) {
	pub, priv := testKey(t)
	sum := sha256.Sum256(pub)
	want := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))[:26]
	if got := DeviceID(pub); got != want || len(got) != 26 {
		t.Fatalf("DeviceID = %q, want %q", got, want)
	}
	dpub, th := fixedBytes(1), fixedBytes(100)
	h := SignHello(priv, "laptop", dpub, th, testSession, testChallenge)

	otherPub, _ := testKey(t)
	if _, err := h.Verify(DeviceID(otherPub), dpub, th, testSession, testChallenge); !errors.Is(err, ErrIDMismatch) {
		t.Fatalf("frame id of another key: err = %v, want ErrIDMismatch", err)
	}
	if _, err := h.Verify(DeviceID(pub), dpub, th, testSession, testChallenge); err != nil {
		t.Fatalf("matching id: %v", err)
	}
}

func TestDeviceHelloValidSignatureAccepted(t *testing.T) {
	pub, priv := testKey(t)
	dpub, th := fixedBytes(1), fixedBytes(100)
	h := SignHello(priv, "Pixel 9", dpub, th, testSession, testChallenge)
	if h.V != HelloVersion || HelloVersion != 2 {
		t.Fatalf("V = %d", h.V)
	}
	got, err := h.Verify(DeviceID(pub), dpub, th, testSession, testChallenge)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !bytes.Equal(got, pub) {
		t.Fatal("Verify returned a different key")
	}
}

func TestDeviceHelloBadSignatureDropped(t *testing.T) {
	pub, priv := testKey(t)
	dpub, th := fixedBytes(1), fixedBytes(100)
	id := DeviceID(pub)
	good := SignHello(priv, "ok", dpub, th, testSession, testChallenge)

	tests := []struct {
		name   string
		mutate func(h *Hello)
		want   error
	}{
		{"flipped signature bit", func(h *Hello) { h.Signature = append([]byte(nil), h.Signature...); h.Signature[0] ^= 1 }, ErrBadSignature},
		{"empty signature", func(h *Hello) { h.Signature = nil }, ErrBadSignature},
		{"truncated signature", func(h *Hello) { h.Signature = h.Signature[:63] }, ErrBadSignature},
		{"wrong version", func(h *Hello) { h.V = 3 }, ErrBadVersion},
		{"short key", func(h *Hello) { h.DevicePub = h.DevicePub[:31] }, ErrBadKey},
		{"long label", func(h *Hello) { h.Label = strings.Repeat("a", 65) }, ErrBadLabel},
		{"control char label", func(h *Hello) { h.Label = "a\x1b[2Jb" }, ErrBadLabel},
		{"newline label", func(h *Hello) { h.Label = "a\nb" }, ErrBadLabel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := good
			tt.mutate(&h)
			if _, err := h.Verify(id, dpub, th, testSession, testChallenge); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
	// A 64-byte label is the boundary and is fine.
	h := SignHello(priv, strings.Repeat("a", 64), dpub, th, testSession, testChallenge)
	if _, err := h.Verify(id, dpub, th, testSession, testChallenge); err != nil {
		t.Fatalf("64-byte label: %v", err)
	}
}

// TestDeviceHelloReplayInAnotherSessionRejected: a captured hello is useless
// anywhere but the session it was made for — another transcript hash (a
// different client or daemon ephemeral key), daemon, route session id, or
// daemon challenge each break the signature.
func TestDeviceHelloReplayInAnotherSessionRejected(t *testing.T) {
	pub, priv := testKey(t)
	dpub, th := fixedBytes(1), fixedBytes(100)
	h := SignHello(priv, "x", dpub, th, testSession, testChallenge)
	id := DeviceID(pub)
	for name, c := range map[string]struct{ d, m, s, c []byte }{
		"other transcript hash":  {dpub, fixedBytes(101), testSession, testChallenge},
		"other daemon key":       {fixedBytes(2), th, testSession, testChallenge},
		"other route session id": {dpub, th, []byte("sess-another-route"), testChallenge},
		"other daemon challenge": {dpub, th, testSession, fixedBytes(0xC1)},
	} {
		if _, err := h.Verify(id, c.d, c.m, c.s, c.c); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: err = %v, want ErrBadSignature", name, err)
		}
	}
	if _, err := h.Verify(id, dpub, th[:31], testSession, testChallenge); !errors.Is(err, ErrBadTranscript) {
		t.Errorf("short transcript hash: err = %v, want ErrBadTranscript", err)
	}
	// And the one thing the replaying party lacks is a challenge it has not
	// already seen: the challenge must be the full 32 bytes.
	if _, err := h.Verify(id, dpub, th, testSession, testChallenge[:31]); !errors.Is(err, ErrBadChallenge) {
		t.Errorf("short challenge: err = %v, want ErrBadChallenge", err)
	}
}

// TestV1HelloIsRefused: the retired v1 format (it signed nothing the daemon
// contributed) is not accepted, whatever its signature.
func TestV1HelloIsRefused(t *testing.T) {
	pub, priv := testKey(t)
	dpub, th := fixedBytes(1), fixedBytes(100)
	// A well-formed v1 hello: domain "smind-device-hello", v1 transcript, daemon key.
	v1th := e2eeV1Transcript(dpub, fixedBytes(0x50))
	signed := append(append([]byte("smind-device-hello"), v1th...), dpub...)
	h := Hello{V: HelloVersionV1, DevicePub: pub, Label: "old", Signature: ed25519.Sign(priv, signed)}
	if _, err := h.Verify(DeviceID(pub), dpub, th, testSession, testChallenge); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("v1 hello: err = %v, want ErrBadVersion", err)
	}
	// Relabelled as v2 it still fails: the v2 signed bytes are different.
	h.V = HelloVersion
	if _, err := h.Verify(DeviceID(pub), dpub, th, testSession, testChallenge); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("v1 signature under v2: err = %v, want ErrBadSignature", err)
	}
}

func TestChallengeMessageRoundTrip(t *testing.T) {
	c, err := NewChallenge()
	if err != nil || len(c) != ChallengeLen {
		t.Fatalf("NewChallenge = %x, %v", c, err)
	}
	c2, _ := NewChallenge()
	if bytes.Equal(c, c2) {
		t.Fatal("two challenges are identical")
	}
	got, ok, err := ParseChallengeMessage(ChallengeMessage(c))
	if err != nil || !ok || !bytes.Equal(got, c) {
		t.Fatalf("round trip = %x, %v, %v", got, ok, err)
	}
	if _, ok, _ := ParseChallengeMessage([]byte(`{"id":"1","result":{}}`)); ok {
		t.Fatal("an unrelated message must not parse as a challenge")
	}
	if _, ok, err := ParseChallengeMessage([]byte(`{"event":"relay.device_challenge","params":{"v":1,"challenge":"AAAA"}}`)); !ok || !errors.Is(err, ErrBadVersion) {
		t.Fatalf("wrong challenge version: ok=%v err=%v", ok, err)
	}
	if _, ok, err := ParseChallengeMessage([]byte(`{"event":"relay.device_challenge","params":{"v":2,"challenge":"AAAA"}}`)); !ok || !errors.Is(err, ErrBadChallenge) {
		t.Fatalf("short challenge: ok=%v err=%v", ok, err)
	}
}

// e2eeV1Transcript is the retired E2EE v1 transcript, only used to build a
// well-formed v1 hello that must be refused.
func e2eeV1Transcript(daemonPub, mobilePub []byte) []byte {
	// SHA-256("smind relay e2ee v1" || daemon_pub || mobile_pub): the v1 HKDF salt.
	h := sha256.New()
	h.Write([]byte("smind relay e2ee v1"))
	h.Write(daemonPub)
	h.Write(mobilePub)
	return h.Sum(nil)
}

type vectorFile struct {
	Comment           string           `json:"comment"`
	Version           int              `json:"version"`
	SeedHex           string           `json:"seed_hex"`
	DevicePubHex      string           `json:"device_pub_hex"`
	DeviceID          string           `json:"device_id"`
	DaemonPubHex      string           `json:"daemon_pub_hex"`
	DaemonEphPubHex   string           `json:"daemon_eph_pub_hex"`
	MobilePubHex      string           `json:"mobile_pub_hex"`
	TranscriptHashHex string           `json:"transcript_hash_hex"`
	SessionID         string           `json:"session_id"`
	ChallengeHex      string           `json:"challenge_hex"`
	ChallengeMessage  json.RawMessage  `json:"challenge_message"`
	Label             string           `json:"label"`
	SignedBytesHex    string           `json:"signed_bytes_hex"`
	SignatureHex      string           `json:"signature_hex"`
	Hello             json.RawMessage  `json:"hello"`
	Negative          []vectorNegative `json:"negative"`
}

// vectorNegative is a hello signed over ONE different input, presented against
// the top-level values: verification must fail with `error`.
type vectorNegative struct {
	Name           string          `json:"name"`
	Changed        string          `json:"changed"`
	SignedBytesHex string          `json:"signed_bytes_hex"`
	Hello          json.RawMessage `json:"hello"`
	Error          string          `json:"error"`
}

func buildVector(t *testing.T) []byte {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(fixedBytes(0xA0))
	pub := priv.Public().(ed25519.PublicKey)
	dpub, deph, mpub := fixedBytes(0x10), fixedBytes(0x30), fixedBytes(0x50)
	th := e2ee.TranscriptHash(dpub, deph, mpub)
	session, challenge := []byte("sess-0123456789abcdef"), fixedBytes(0xC0)
	const label = "Pixel 9 (test vector)"
	marshal := func(h Hello) json.RawMessage {
		b, err := json.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	h := SignHello(priv, label, dpub, th, session, challenge)
	// Each negative is signed over a hello that differs in ONE input of the
	// session (for a key, over the transcript hash that key produces), and is
	// presented against the top-level values.
	neg := func(name, changed string, d, th, s, c []byte) vectorNegative {
		return vectorNegative{
			Name: name, Changed: changed,
			SignedBytesHex: hex.EncodeToString(HelloSignedBytes(d, th, s, c)),
			Hello:          marshal(SignHello(priv, label, d, th, s, c)),
			Error:          "bad_signature",
		}
	}
	otherDaemon, otherEph, otherMobile := fixedBytes(0x20), fixedBytes(0x70), fixedBytes(0x90)
	v := vectorFile{
		Comment: "ADR-0031 D9 device_hello VERSION 2 over the E2EE v2 transcript (ADR-0035). signature = Ed25519(seed, signed_bytes); " +
			"signed_bytes = \"smind-device-hello-v2\" || th || daemon_pub || uint16BE(len(session_id)) || session_id || challenge; " +
			"th = transcript_hash_hex = SHA256(\"smind relay e2ee v2\" || daemon_pub || daemon_eph_pub || mobile_pub), " +
			"taken from the established E2EE channel (mobile_pub is the client's ephemeral handshake key). " +
			"session_id is the exact bytes of the relay route's Frame.session_id (the vector's value is ASCII); " +
			"challenge is the 32 bytes the daemon sent in challenge_message (standard base64 WITH padding) right after the E2EE handshake. " +
			"Verify with device_id from the frame. Each negative is a hello signed over one different input (a different key changes th too) and must fail with bad_signature. " +
			"Version 1 (device_hello_vector_v1.json) is retired and refused.",
		Version:           HelloVersion,
		SeedHex:           hex.EncodeToString(fixedBytes(0xA0)),
		DevicePubHex:      hex.EncodeToString(pub),
		DeviceID:          DeviceID(pub),
		DaemonPubHex:      hex.EncodeToString(dpub),
		DaemonEphPubHex:   hex.EncodeToString(deph),
		MobilePubHex:      hex.EncodeToString(mpub),
		TranscriptHashHex: hex.EncodeToString(th),
		SessionID:         string(session),
		ChallengeHex:      hex.EncodeToString(challenge),
		ChallengeMessage:  ChallengeMessage(challenge),
		Label:             label,
		SignedBytesHex:    hex.EncodeToString(HelloSignedBytes(dpub, th, session, challenge)),
		SignatureHex:      hex.EncodeToString(h.Signature),
		Hello:             marshal(h),
		Negative: []vectorNegative{
			neg("other_daemon_pub", "daemon_pub", otherDaemon, e2ee.TranscriptHash(otherDaemon, deph, mpub), session, challenge),
			neg("other_mobile_pub", "mobile_pub", dpub, e2ee.TranscriptHash(dpub, deph, otherMobile), session, challenge),
			neg("other_daemon_eph", "daemon_eph_pub", dpub, e2ee.TranscriptHash(dpub, otherEph, mpub), session, challenge),
			neg("other_session_id", "session_id", dpub, th, []byte("sess-another-route"), challenge),
			neg("other_challenge", "challenge", dpub, th, session, fixedBytes(0xC1)),
		},
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(out, '\n')
}

func TestHelloVectorMatchesFile(t *testing.T) {
	const path = "testdata/device_hello_vector.json"
	want := buildVector(t)
	if *updateVector {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from regenerated vector; run with -update-vector if the change is intended", path)
	}

	// The file must also verify through the public API, positive and negative,
	// and its challenge message must parse back to its challenge.
	var v vectorFile
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatal(err)
	}
	if v.Version != HelloVersion {
		t.Fatalf("vector version %d, want %d", v.Version, HelloVersion)
	}
	dpub, _ := hex.DecodeString(v.DaemonPubHex)
	deph, _ := hex.DecodeString(v.DaemonEphPubHex)
	mpub, _ := hex.DecodeString(v.MobilePubHex)
	th, _ := hex.DecodeString(v.TranscriptHashHex)
	challenge, _ := hex.DecodeString(v.ChallengeHex)
	session := []byte(v.SessionID)
	if c, ok, err := ParseChallengeMessage(v.ChallengeMessage); err != nil || !ok || !bytes.Equal(c, challenge) {
		t.Fatalf("challenge_message does not parse to challenge_hex: %x %v %v", c, ok, err)
	}
	var h Hello
	if err := json.Unmarshal(v.Hello, &h); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Verify(v.DeviceID, dpub, th, session, challenge); err != nil {
		t.Fatalf("positive vector: %v", err)
	}
	if len(v.Negative) != 5 {
		t.Fatalf("expected the five negative vectors, got %d", len(v.Negative))
	}
	// th is independently the documented SHA-256 over label and the three keys.
	wantTh := sha256.Sum256(append(append(append([]byte("smind relay e2ee v2"), dpub...), deph...), mpub...))
	if !bytes.Equal(th, wantTh[:]) {
		t.Fatal("transcript_hash_hex is not SHA256(label || daemon_pub || daemon_eph_pub || mobile_pub)")
	}
	// The golden signed_bytes_hex is also assembled here from the written
	// layout, independently of HelloSignedBytes, so a bug in that function
	// cannot be baked into the vector unnoticed.
	hand := []byte("smind-device-hello-v2")
	hand = append(hand, th...)
	hand = append(hand, dpub...)
	hand = append(hand, byte(len(session)>>8), byte(len(session)))
	hand = append(hand, session...)
	hand = append(hand, challenge...)
	if hex.EncodeToString(hand) != v.SignedBytesHex {
		t.Fatalf("signed_bytes_hex does not match the documented layout:\n got  %s\n want %s", v.SignedBytesHex, hex.EncodeToString(hand))
	}
	for _, n := range v.Negative {
		var nh Hello
		if err := json.Unmarshal(n.Hello, &nh); err != nil {
			t.Fatal(err)
		}
		if _, err := nh.Verify(v.DeviceID, dpub, th, session, challenge); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("negative vector %s (changed %s): err = %v, want ErrBadSignature", n.Name, n.Changed, err)
		}
	}
}

// TestV1VectorIsHistorical: the v1 vector stays in the tree as the record of
// the retired format; the v2 verifier refuses its hello.
func TestV1VectorIsHistorical(t *testing.T) {
	raw, err := os.ReadFile("testdata/device_hello_vector_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		DeviceID     string          `json:"device_id"`
		DaemonPubHex string          `json:"daemon_pub_hex"`
		MobilePubHex string          `json:"mobile_pub_hex"`
		Hello        json.RawMessage `json:"hello"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var h Hello
	if err := json.Unmarshal(v.Hello, &h); err != nil {
		t.Fatal(err)
	}
	if h.V != HelloVersionV1 {
		t.Fatalf("the historical vector is version %d, want 1", h.V)
	}
	dpub, _ := hex.DecodeString(v.DaemonPubHex)
	mpub, _ := hex.DecodeString(v.MobilePubHex)
	if _, err := h.Verify(v.DeviceID, dpub, e2eeV1Transcript(dpub, mpub), testSession, testChallenge); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("a v1 hello must be refused as a bad version: %v", err)
	}
}

func loadVector(t *testing.T) vectorFile {
	t.Helper()
	raw, err := os.ReadFile("testdata/device_hello_vector.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestHelloVectorV2OverNewTranscript: the committed vector verifies with the
// th that the E2EE v2 package computes from the three public keys.
func TestHelloVectorV2OverNewTranscript(t *testing.T) {
	v := loadVector(t)
	dpub, _ := hex.DecodeString(v.DaemonPubHex)
	deph, _ := hex.DecodeString(v.DaemonEphPubHex)
	mpub, _ := hex.DecodeString(v.MobilePubHex)
	challenge, _ := hex.DecodeString(v.ChallengeHex)
	th := e2ee.TranscriptHash(dpub, deph, mpub)
	if hex.EncodeToString(th) != v.TranscriptHashHex {
		t.Fatalf("transcript_hash_hex = %s, e2ee.TranscriptHash = %x", v.TranscriptHashHex, th)
	}
	var h Hello
	if err := json.Unmarshal(v.Hello, &h); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Verify(v.DeviceID, dpub, th, []byte(v.SessionID), challenge); err != nil {
		t.Fatalf("vector over the v2 transcript: %v", err)
	}
}

// TestHelloNegativeOtherDaemonEphemeral: a hello signed for the same client
// and daemon but another daemon ephemeral (a replayed client handshake) fails.
func TestHelloNegativeOtherDaemonEphemeral(t *testing.T) {
	v := loadVector(t)
	dpub, _ := hex.DecodeString(v.DaemonPubHex)
	th, _ := hex.DecodeString(v.TranscriptHashHex)
	challenge, _ := hex.DecodeString(v.ChallengeHex)
	for _, n := range v.Negative {
		if n.Name != "other_daemon_eph" {
			continue
		}
		var h Hello
		if err := json.Unmarshal(n.Hello, &h); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Verify(v.DeviceID, dpub, th, []byte(v.SessionID), challenge); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("other_daemon_eph: err = %v, want ErrBadSignature", err)
		}
		return
	}
	t.Fatal("vector has no other_daemon_eph negative")
}
