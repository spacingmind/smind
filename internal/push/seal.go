// Package push seals notification payloads for mobile push delivery
// (docs/decisions/0040-mobile-push-notifications.md §1): the daemon encrypts
// the notification text with a per-device 32-byte key using ChaCha20-Poly1305,
// AAD "smind-push-v1"+deviceID, so Expo, Apple and Google only ever see an
// opaque, length-padded blob; the phone decrypts it locally and falls back to
// a generic notification when it cannot.
package push

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	Version       = 1
	KeySize       = 32
	bucketSize    = 256
	MaxPlainSize  = 1024 // padded plaintext, including the 2-byte length prefix
	MaxTitleRunes = 80
	MaxBodyRunes  = 160
)

var (
	ErrKey       = errors.New("push: key must be 32 bytes")
	ErrVersion   = errors.New("push: unsupported blob version")
	ErrMalformed = errors.New("push: malformed blob")
	ErrAuth      = errors.New("push: authentication failed")
	ErrTooLarge  = errors.New("push: event too large")
)

// Event is the plaintext of one notification.
type Event struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // permission | needs_continue | done | error | test
	TaskID int64  `json:"taskId"`
	RunID  string `json:"runId,omitempty"`
	ChatID int64  `json:"chatId,omitempty"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	At     int64  `json:"at"` // unix seconds
}

// Blob is the only thing sent as the push "data". N and C are base64url WITHOUT padding.
type Blob struct {
	V int    `json:"v"`
	N string `json:"n"` // 12-byte nonce
	C string `json:"c"` // ciphertext || 16-byte tag
}

// Seal encrypts ev for the device holding key. The nonce is drawn from
// crypto/rand; random 96-bit nonces are safe at push volumes under a
// per-device key (ADR-0040 §1).
func Seal(key []byte, deviceID string, ev Event) (Blob, error) {
	if len(key) != KeySize {
		return Blob{}, ErrKey
	}
	nonce := make([]byte, chacha20poly1305.NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return Blob{}, err
	}
	return sealWithNonce(key, deviceID, ev, nonce)
}

// sealWithNonce is Seal with a caller-supplied nonce (test hook).
func sealWithNonce(key []byte, deviceID string, ev Event, nonce []byte) (Blob, error) {
	if len(key) != KeySize {
		return Blob{}, ErrKey
	}
	ev.Title = truncateRunes(ev.Title, MaxTitleRunes)
	ev.Body = truncateRunes(ev.Body, MaxBodyRunes)
	framed, err := frame(ev)
	if err != nil {
		return Blob{}, err
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return Blob{}, err
	}
	ct := aead.Seal(nil, nonce, framed, aad(deviceID))
	return Blob{
		V: Version,
		N: base64.RawURLEncoding.EncodeToString(nonce),
		C: base64.RawURLEncoding.EncodeToString(ct),
	}, nil
}

// Open decrypts and validates b. It returns the zero Event and no partial
// plaintext on any failure.
func Open(key []byte, deviceID string, b Blob) (Event, error) {
	if len(key) != KeySize {
		return Event{}, ErrKey
	}
	if b.V != Version {
		return Event{}, ErrVersion
	}
	nonce, err := base64.RawURLEncoding.DecodeString(b.N)
	if err != nil || len(nonce) != chacha20poly1305.NonceSize {
		return Event{}, ErrMalformed
	}
	ct, err := base64.RawURLEncoding.DecodeString(b.C)
	if err != nil || len(ct) < 16+2 {
		return Event{}, ErrMalformed
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return Event{}, err
	}
	framed, err := aead.Open(nil, nonce, ct, aad(deviceID))
	if err != nil {
		return Event{}, ErrAuth
	}
	n := int(binary.BigEndian.Uint16(framed))
	if n+2 > len(framed) {
		return Event{}, ErrMalformed
	}
	for i := n + 2; i < len(framed); i++ {
		if framed[i] != 0 {
			return Event{}, ErrMalformed
		}
	}
	var ev Event
	if err := json.Unmarshal(framed[2:n+2], &ev); err != nil {
		return Event{}, ErrMalformed
	}
	return ev, nil
}

// aad binds a blob to one device id: the ciphertext cannot be replayed to a
// different device's key.
func aad(deviceID string) []byte {
	return []byte("smind-push-v1" + deviceID)
}

// frame marshals ev as u16BE(len) || JSON, then zero-pads to a multiple of
// bucketSize so the push provider cannot read even the title length.
func frame(ev Event) ([]byte, error) {
	plain, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	// MaxPlainSize is far below 2^16, so this check also keeps the length
	// prefix from wrapping.
	if len(plain)+2 > MaxPlainSize {
		return nil, ErrTooLarge
	}
	framed := binary.BigEndian.AppendUint16(nil, uint16(len(plain)))
	framed = append(framed, plain...)
	if r := len(framed) % bucketSize; r != 0 {
		framed = append(framed, make([]byte, bucketSize-r)...)
	}
	return framed, nil
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
