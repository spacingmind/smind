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
)

var updateVector = flag.Bool("update-vector", false, "rewrite testdata/device_hello_vector.json")

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

func TestDeviceIDMustMatchPublicKeyHash(t *testing.T) {
	pub, priv := testKey(t)
	sum := sha256.Sum256(pub)
	want := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))[:26]
	if got := DeviceID(pub); got != want || len(got) != 26 {
		t.Fatalf("DeviceID = %q, want %q", got, want)
	}
	dpub, mpub := fixedBytes(1), fixedBytes(100)
	h := SignHello(priv, "laptop", dpub, mpub)

	otherPub, _ := testKey(t)
	if _, err := h.Verify(DeviceID(otherPub), dpub, mpub); !errors.Is(err, ErrIDMismatch) {
		t.Fatalf("frame id of another key: err = %v, want ErrIDMismatch", err)
	}
	if _, err := h.Verify(DeviceID(pub), dpub, mpub); err != nil {
		t.Fatalf("matching id: %v", err)
	}
}

func TestDeviceHelloValidSignatureAccepted(t *testing.T) {
	pub, priv := testKey(t)
	dpub, mpub := fixedBytes(1), fixedBytes(100)
	h := SignHello(priv, "Pixel 9", dpub, mpub)
	if h.V != HelloVersion {
		t.Fatalf("V = %d", h.V)
	}
	got, err := h.Verify(DeviceID(pub), dpub, mpub)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !bytes.Equal(got, pub) {
		t.Fatal("Verify returned a different key")
	}
}

func TestDeviceHelloBadSignatureDropped(t *testing.T) {
	pub, priv := testKey(t)
	dpub, mpub := fixedBytes(1), fixedBytes(100)
	id := DeviceID(pub)
	good := SignHello(priv, "ok", dpub, mpub)

	tests := []struct {
		name   string
		mutate func(h *Hello)
		want   error
	}{
		{"flipped signature bit", func(h *Hello) { h.Signature = append([]byte(nil), h.Signature...); h.Signature[0] ^= 1 }, ErrBadSignature},
		{"empty signature", func(h *Hello) { h.Signature = nil }, ErrBadSignature},
		{"truncated signature", func(h *Hello) { h.Signature = h.Signature[:63] }, ErrBadSignature},
		{"wrong version", func(h *Hello) { h.V = 2 }, ErrBadVersion},
		{"short key", func(h *Hello) { h.DevicePub = h.DevicePub[:31] }, ErrBadKey},
		{"long label", func(h *Hello) { h.Label = strings.Repeat("a", 65) }, ErrBadLabel},
		{"control char label", func(h *Hello) { h.Label = "a\x1b[2Jb" }, ErrBadLabel},
		{"newline label", func(h *Hello) { h.Label = "a\nb" }, ErrBadLabel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := good
			tt.mutate(&h)
			if _, err := h.Verify(id, dpub, mpub); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
	// A 64-byte label is the boundary and is fine.
	h := SignHello(priv, strings.Repeat("a", 64), dpub, mpub)
	if _, err := h.Verify(id, dpub, mpub); err != nil {
		t.Fatalf("64-byte label: %v", err)
	}
}

func TestDeviceHelloReplayInAnotherSessionRejected(t *testing.T) {
	pub, priv := testKey(t)
	dpub, mpub := fixedBytes(1), fixedBytes(100)
	h := SignHello(priv, "x", dpub, mpub)
	id := DeviceID(pub)
	if _, err := h.Verify(id, dpub, fixedBytes(101)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("other mobilePub: err = %v", err)
	}
	if _, err := h.Verify(id, fixedBytes(2), mpub); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("other daemonPub: err = %v", err)
	}
}

type vectorFile struct {
	Comment        string          `json:"comment"`
	SeedHex        string          `json:"seed_hex"`
	DevicePubHex   string          `json:"device_pub_hex"`
	DeviceID       string          `json:"device_id"`
	DaemonPubHex   string          `json:"daemon_pub_hex"`
	MobilePubHex   string          `json:"mobile_pub_hex"`
	Label          string          `json:"label"`
	SignedBytesHex string          `json:"signed_bytes_hex"`
	SignatureHex   string          `json:"signature_hex"`
	Hello          json.RawMessage `json:"hello"`
	Negative       vectorNegative  `json:"negative"`
}

type vectorNegative struct {
	Comment        string          `json:"comment"`
	MobilePubHex   string          `json:"mobile_pub_hex"`
	SignedBytesHex string          `json:"signed_bytes_hex"`
	Hello          json.RawMessage `json:"hello"`
	Error          string          `json:"error"`
}

func buildVector(t *testing.T) []byte {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(fixedBytes(0xA0))
	pub := priv.Public().(ed25519.PublicKey)
	dpub, mpub, otherM := fixedBytes(0x10), fixedBytes(0x50), fixedBytes(0x90)
	const label = "Pixel 9 (test vector)"

	h := SignHello(priv, label, dpub, mpub)
	hj, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	// Negative: signed for a different mobilePub, presented with mpub.
	neg := SignHello(priv, label, dpub, otherM)
	nj, err := json.Marshal(neg)
	if err != nil {
		t.Fatal(err)
	}
	v := vectorFile{
		Comment:        "ADR-0031 D9 device_hello. signature = Ed25519(seed, signed_bytes); signed_bytes = \"smind-device-hello\" || SHA256(\"smind relay e2ee v1\" || daemon_pub || mobile_pub) || daemon_pub. Verify with device_id from the frame.",
		SeedHex:        hex.EncodeToString(fixedBytes(0xA0)),
		DevicePubHex:   hex.EncodeToString(pub),
		DeviceID:       DeviceID(pub),
		DaemonPubHex:   hex.EncodeToString(dpub),
		MobilePubHex:   hex.EncodeToString(mpub),
		Label:          label,
		SignedBytesHex: hex.EncodeToString(HelloSignedBytes(dpub, mpub)),
		SignatureHex:   hex.EncodeToString(h.Signature),
		Hello:          hj,
		Negative: vectorNegative{
			Comment:        "Hello signed over a different mobile_pub; verifying it against mobile_pub (top level) must fail with bad signature.",
			MobilePubHex:   hex.EncodeToString(otherM),
			SignedBytesHex: hex.EncodeToString(HelloSignedBytes(dpub, otherM)),
			Hello:          nj,
			Error:          "bad_signature",
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

	// The file must also verify through the public API, positive and negative.
	var v vectorFile
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatal(err)
	}
	dpub, _ := hex.DecodeString(v.DaemonPubHex)
	mpub, _ := hex.DecodeString(v.MobilePubHex)
	var h Hello
	if err := json.Unmarshal(v.Hello, &h); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Verify(v.DeviceID, dpub, mpub); err != nil {
		t.Fatalf("positive vector: %v", err)
	}
	var nh Hello
	if err := json.Unmarshal(v.Negative.Hello, &nh); err != nil {
		t.Fatal(err)
	}
	if _, err := nh.Verify(v.DeviceID, dpub, mpub); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("negative vector: err = %v, want ErrBadSignature", err)
	}
}
