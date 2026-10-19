// Package devices is the relay's device identity and registry layer
// (ADR-0031 D9). A device is a long-lived Ed25519 key; its id is a hash of
// the public key, so the id can never be claimed by anyone who does not hold
// the private key. The device proves possession with a signed hello bound to
// the one end-to-end session it is sent over — the E2EE v2 transcript hash
// (which covers the daemon's fresh per-handshake ephemeral key), the relay
// route's session id and a fresh challenge the daemon sends first — so a
// captured hello is useless on any other session.
package devices

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// HelloMethod is the application-level method a client sends first,
	// inside the encrypted channel, to identify its device.
	HelloMethod = "relay.device_hello"
	// HelloVersion is the only hello format this package accepts. Version 1
	// signed only (daemon key, client ephemeral key); both are fixed by the
	// client's captured handshake, so anyone able to open a route could replay
	// a captured session (handshake frames plus the hello ciphertext) and be
	// admitted as that device. Version 2 adds the route's session id and a
	// per-session challenge from the daemon, and (ADR-0035) signs the E2EE v2
	// transcript hash, which now also covers the daemon's fresh ephemeral key.
	// No released client ever spoke v1 (the first clients ship after this
	// change), so v1 is not accepted.
	HelloVersion = 2
	// HelloVersionV1 is the retired format, kept so a v1 hello is recognised
	// and refused with a clear reason.
	HelloVersionV1 = 1

	// ChallengeMethod is the event the daemon sends first, inside the
	// encrypted channel, on a device route (flow: handshake → challenge →
	// hello): {"event":"relay.device_challenge","params":{"v":2,"challenge":b64}}.
	ChallengeMethod = "relay.device_challenge"
	// ChallengeLen is the challenge size in bytes.
	ChallengeLen = 32

	deviceIDLen   = 26
	maxLabelBytes = 64
	helloDomain   = "smind-device-hello-v2"
)

var (
	ErrBadVersion    = errors.New("devices: unsupported hello version")
	ErrBadKey        = errors.New("devices: device public key must be 32 bytes")
	ErrIDMismatch    = errors.New("devices: device id does not match public key")
	ErrBadSignature  = errors.New("devices: bad hello signature")
	ErrBadLabel      = errors.New("devices: invalid device label")
	ErrBadChallenge  = errors.New("devices: bad hello challenge")
	ErrBadTranscript = errors.New("devices: transcript hash must be 32 bytes")
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

// NewChallenge returns a fresh random challenge for one session.
func NewChallenge() ([]byte, error) {
	c := make([]byte, ChallengeLen)
	if _, err := rand.Read(c); err != nil {
		return nil, err
	}
	return c, nil
}

// ChallengeMessage is the encrypted application message that carries the
// daemon's challenge to the device.
func ChallengeMessage(challenge []byte) []byte {
	b, err := json.Marshal(map[string]any{
		"event":  ChallengeMethod,
		"params": map[string]any{"v": HelloVersion, "challenge": challenge},
	})
	if err != nil {
		panic(err) // a map of a string, an int and a []byte always marshals
	}
	return b
}

// ParseChallengeMessage extracts the challenge from a message, reporting
// whether the message is the daemon's challenge event at all.
func ParseChallengeMessage(msg []byte) (challenge []byte, ok bool, err error) {
	var env struct {
		Event  string `json:"event"`
		Params struct {
			V         int    `json:"v"`
			Challenge []byte `json:"challenge"`
		} `json:"params"`
	}
	if json.Unmarshal(msg, &env) != nil || env.Event != ChallengeMethod {
		return nil, false, nil
	}
	if env.Params.V != HelloVersion {
		return nil, true, fmt.Errorf("%w: challenge version %d", ErrBadVersion, env.Params.V)
	}
	if len(env.Params.Challenge) != ChallengeLen {
		return nil, true, fmt.Errorf("%w: %d bytes", ErrBadChallenge, len(env.Params.Challenge))
	}
	return env.Params.Challenge, true, nil
}

// transcriptHashLen is the size of the E2EE transcript hash (SHA-256).
const transcriptHashLen = sha256.Size

// HelloSignedBytes is the message a device signs (version 2):
//
//	"smind-device-hello-v2"
//	‖ th                                      32 bytes, e2ee Channel.TranscriptHash()
//	‖ daemonPub                               32 bytes
//	‖ uint16BE(len(sessionID)) ‖ sessionID    the exact bytes of the route's Frame.session_id
//	‖ challenge                               32 bytes, fresh from the daemon
//
// th is the E2EE v2 transcript hash, SHA-256(label ‖ daemon static key ‖
// daemon ephemeral key ‖ client ephemeral key), which is also the HKDF salt of
// the session (ADR-0035): taking it from the established channel binds the
// signature to this handshake's fresh daemon ephemeral, so a replayed client
// handshake yields a different th and a captured hello never verifies. The
// session id ties the hello to one route, and the daemon's fresh challenge to
// one live session. daemonPub is repeated outside the hash so the signed
// bytes name the daemon explicitly.
func HelloSignedBytes(daemonPub, th, sessionID, challenge []byte) []byte {
	if len(sessionID) > 0xFFFF {
		panic("devices: session id too long to sign") // the relay caps ids at 128 bytes
	}
	out := make([]byte, 0, len(helloDomain)+len(th)+len(daemonPub)+2+len(sessionID)+len(challenge))
	out = append(out, helloDomain...)
	out = append(out, th...)
	out = append(out, daemonPub...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(sessionID)))
	out = append(out, sessionID...)
	out = append(out, challenge...)
	return out
}

// SignHello builds a signed hello for the session identified by the daemon's
// static key, the channel's transcript hash th, the relay route's session id
// and the daemon's challenge.
func SignHello(priv ed25519.PrivateKey, label string, daemonPub, th, sessionID, challenge []byte) Hello {
	pub := priv.Public().(ed25519.PublicKey)
	return Hello{
		V:         HelloVersion,
		DevicePub: append([]byte(nil), pub...),
		Label:     label,
		Signature: ed25519.Sign(priv, HelloSignedBytes(daemonPub, th, sessionID, challenge)),
	}
}

// Verify checks the hello against the session and the device id the frame
// claimed, returning the device's public key. th is the verifying channel's
// own TranscriptHash(). The label is checked too: it is shown to humans
// approving devices, so control characters must not get through.
func (h Hello) Verify(frameDeviceID string, daemonPub, th, sessionID, challenge []byte) (ed25519.PublicKey, error) {
	if h.V != HelloVersion {
		return nil, fmt.Errorf("%w: %d", ErrBadVersion, h.V)
	}
	if len(challenge) != ChallengeLen {
		return nil, ErrBadChallenge
	}
	if len(th) != transcriptHashLen {
		return nil, ErrBadTranscript
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
	if !ed25519.Verify(pub, HelloSignedBytes(daemonPub, th, sessionID, challenge), h.Signature) {
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
