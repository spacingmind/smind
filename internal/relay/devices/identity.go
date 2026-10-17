// Package devices is the relay's device identity and registry layer
// (ADR-0031 D9). A device is a long-lived Ed25519 key; its id is a hash of
// the public key, so the id can never be claimed by anyone who does not hold
// the private key. The device proves possession with a signed hello bound to
// the one end-to-end handshake it is sent over, so a captured hello cannot
// be replayed into another session.
package devices

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/relay/e2ee"
)

const (
	// HelloMethod is the application-level method a client sends first,
	// inside the encrypted channel, to identify its device.
	HelloMethod = "relay.device_hello"
	// HelloVersion is the only hello format this package accepts.
	HelloVersion = 1

	deviceIDLen   = 26
	maxLabelBytes = 64
	helloDomain   = "smind-device-hello"
)

var (
	ErrBadVersion   = errors.New("devices: unsupported hello version")
	ErrBadKey       = errors.New("devices: device public key must be 32 bytes")
	ErrIDMismatch   = errors.New("devices: device id does not match public key")
	ErrBadSignature = errors.New("devices: bad hello signature")
	ErrBadLabel     = errors.New("devices: invalid device label")
)

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// DeviceID derives the stable device id: lowercase unpadded base32 of
// SHA-256(pub), truncated to 26 characters (130 bits). Deriving it from the
// key rather than letting devices pick one makes the id self-authenticating.
func DeviceID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return strings.ToLower(idEncoding.EncodeToString(sum[:]))[:deviceIDLen]
}

// Hello is the device's self-introduction. Password is only meaningful for
// the optional password-based admission path and is not covered by the
// signature (it is a separate credential, not part of the key proof).
type Hello struct {
	V         int    `json:"v"`
	DevicePub []byte `json:"device_pub"`
	Label     string `json:"label"`
	Signature []byte `json:"signature"`
	Password  string `json:"password,omitempty"`
}

// HelloSignedBytes is the message a device signs:
// "smind-device-hello" ‖ TranscriptHash(daemonPub, mobilePub) ‖ daemonPub.
// mobilePub is the client's ephemeral X25519 handshake key, so the signature
// is valid for exactly one session; daemonPub is repeated outside the hash so
// the signed bytes name the daemon explicitly.
func HelloSignedBytes(daemonPub, mobilePub []byte) []byte {
	th := e2ee.TranscriptHash(daemonPub, mobilePub)
	out := make([]byte, 0, len(helloDomain)+len(th)+len(daemonPub))
	out = append(out, helloDomain...)
	out = append(out, th...)
	out = append(out, daemonPub...)
	return out
}

// SignHello builds a signed hello for the session identified by the two
// handshake public keys.
func SignHello(priv ed25519.PrivateKey, label string, daemonPub, mobilePub []byte) Hello {
	pub := priv.Public().(ed25519.PublicKey)
	return Hello{
		V:         HelloVersion,
		DevicePub: append([]byte(nil), pub...),
		Label:     label,
		Signature: ed25519.Sign(priv, HelloSignedBytes(daemonPub, mobilePub)),
	}
}

// Verify checks the hello against the session and the device id the frame
// claimed, returning the device's public key. The label is checked too:
// it is shown to humans approving devices, so control characters must not
// get through.
func (h Hello) Verify(frameDeviceID string, daemonPub, mobilePub []byte) (ed25519.PublicKey, error) {
	if h.V != HelloVersion {
		return nil, fmt.Errorf("%w: %d", ErrBadVersion, h.V)
	}
	if len(h.DevicePub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrBadKey, len(h.DevicePub))
	}
	pub := ed25519.PublicKey(h.DevicePub)
	if DeviceID(pub) != frameDeviceID {
		return nil, ErrIDMismatch
	}
	if err := ValidateLabel(h.Label); err != nil {
		return nil, err
	}
	if !ed25519.Verify(pub, HelloSignedBytes(daemonPub, mobilePub), h.Signature) {
		return nil, ErrBadSignature
	}
	return append(ed25519.PublicKey(nil), pub...), nil
}

// ValidateLabel rejects labels longer than 64 bytes, containing control
// characters, or not valid UTF-8. Nothing is trimmed or rewritten: a label
// is either accepted as sent or refused.
func ValidateLabel(label string) error {
	if len(label) > maxLabelBytes {
		return fmt.Errorf("%w: longer than %d bytes", ErrBadLabel, maxLabelBytes)
	}
	if !utf8.ValidString(label) {
		return fmt.Errorf("%w: not valid UTF-8", ErrBadLabel)
	}
	for _, r := range label {
		// Controls and the invisible/format/separator/private-use characters
		// (bidi overrides, zero-width, line separators) that could spoof a
		// terminal listing or another device's name.
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp, unicode.Co) {
			return fmt.Errorf("%w: contains control or invisible character", ErrBadLabel)
		}
	}
	return nil
}

// GenerateKey creates a new device identity key.
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}
