package selfupdatetest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// Signer is a throwaway minisign keypair for tests. It is independent of the
// verifier under test (own encoding code), so a format misreading shows up as a
// failing test rather than cancelling out.
type Signer struct {
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
	keyID [8]byte
}

// NewSigner generates a keypair.
func NewSigner(t testing.TB) *Signer {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &Signer{pub: pub, priv: priv}
	if _, err := rand.Read(s.keyID[:]); err != nil {
		t.Fatal(err)
	}
	return s
}

// WithKeyIDOf returns a signer with fresh key material but other's key id: what
// an attacker forging "the release key" can do (key ids are not secret).
func (s *Signer) WithKeyIDOf(other *Signer) *Signer {
	c := *s
	c.keyID = other.keyID
	return &c
}

// PublicKey is the key in Tauri's stored form (base64 of the public key file).
func (s *Signer) PublicKey() string {
	body := append(append([]byte("Ed"), s.keyID[:]...), s.pub...)
	file := fmt.Sprintf("untrusted comment: minisign public key: %X\n%s\n", s.keyID, base64.StdEncoding.EncodeToString(body))
	return base64.StdEncoding.EncodeToString([]byte(file))
}

// Sign returns a minisign signature file for data using the prehashed ("ED")
// algorithm, like minisign and `tauri signer sign`.
func (s *Signer) Sign(data []byte, trustedComment string) []byte {
	return s.sign(data, trustedComment, "ED")
}

// SignLegacy signs with the non-prehashed "Ed" algorithm.
func (s *Signer) SignLegacy(data []byte, trustedComment string) []byte {
	return s.sign(data, trustedComment, "Ed")
}

func (s *Signer) sign(data []byte, trustedComment, alg string) []byte {
	msg := data
	if alg == "ED" {
		sum := blake2b.Sum512(data)
		msg = sum[:]
	}
	sig := ed25519.Sign(s.priv, msg)
	global := ed25519.Sign(s.priv, append(append([]byte(nil), sig...), trustedComment...))
	body := append(append([]byte(alg), s.keyID[:]...), sig...)
	var b strings.Builder
	fmt.Fprintf(&b, "untrusted comment: signature from a test key\n%s\ntrusted comment: %s\n%s\n",
		base64.StdEncoding.EncodeToString(body), trustedComment, base64.StdEncoding.EncodeToString(global))
	return []byte(b.String())
}

// TrustedComment is what `tauri signer sign --app-version <v>` records for
// checksums.txt.
func TrustedComment(version string) string {
	return "timestamp:1791994870\tfile:checksums.txt\tversion:" + version
}
