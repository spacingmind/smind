package e2ee

import (
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

// Session errors.
var (
	// ErrReplay marks a frame whose counter was already used or skipped
	// ahead — a replayed or reordered frame (ADR-0007 (d)).
	ErrReplay = errors.New("e2ee: frame counter replayed or out of order")
	// ErrDecrypt marks a frame that failed authentication/decryption.
	ErrDecrypt = errors.New("e2ee: frame failed to decrypt")
	// ErrCounterExhausted marks a session that has sent the maximum number of
	// frames its nonce space allows; the peers must start a new session.
	ErrCounterExhausted = errors.New("e2ee: frame counter exhausted")
)

// hkdfLabel namespaces this protocol's key derivation.
const hkdfLabel = "smind relay e2ee v1"

// Direction labels bind each derived key to one direction of travel, so a
// frame can never be reflected back at its sender and still authenticate.
const (
	dirDaemonToMobile = hkdfLabel + " daemon->mobile"
	dirMobileToDaemon = hkdfLabel + " mobile->daemon"
)

// Session holds the two directional ChaCha20-Poly1305 keys derived from an
// X25519 exchange, plus the per-direction counters that both supply nonces
// and provide replay protection (ADR-0007 (c)/(d)).
//
// Nonces are never random: each direction's 12-byte nonce is a 4-byte zero
// prefix followed by the big-endian frame counter, so no nonce is ever reused
// under a key as long as the counter is monotonic — which is the same
// property that makes replayed frames detectable.
type Session struct {
	mu          sync.Mutex
	send        cipher.AEAD
	recv        cipher.AEAD
	sendCounter uint64
	recvCounter uint64 // the next counter the peer is expected to use
}

// newSession derives both directional keys from an X25519 exchange.
//
// The salt binds the derived keys to the full handshake transcript (protocol
// label plus both public keys, always in daemon-then-mobile order so the two
// ends agree), so a key can only be used with the exact pair of identities it
// was negotiated for.
func newSession(priv *ecdh.PrivateKey, peerPub *ecdh.PublicKey, role Role, daemonPub, mobilePub []byte) (*Session, error) {
	d2m, m2d, err := deriveKeys(priv, peerPub, daemonPub, mobilePub)
	if err != nil {
		return nil, err
	}

	sendKey, recvKey := d2m, m2d
	if role == RoleMobile {
		sendKey, recvKey = m2d, d2m
	}
	sendAEAD, err := chacha20poly1305.New(sendKey)
	if err != nil {
		return nil, fmt.Errorf("e2ee: send cipher: %w", err)
	}
	recvAEAD, err := chacha20poly1305.New(recvKey)
	if err != nil {
		return nil, fmt.Errorf("e2ee: receive cipher: %w", err)
	}
	return &Session{send: sendAEAD, recv: recvAEAD}, nil
}

// deriveKeys performs the X25519 exchange and expands it into the two
// directional keys. Both ends run this with the same inputs — the public keys
// are always hashed daemon-first, regardless of which side is deriving — so
// both arrive at the same pair of keys.
func deriveKeys(priv *ecdh.PrivateKey, peerPub *ecdh.PublicKey, daemonPub, mobilePub []byte) (daemonToMobile, mobileToDaemon []byte, err error) {
	shared, err := priv.ECDH(peerPub)
	if err != nil {
		return nil, nil, fmt.Errorf("e2ee: X25519 key agreement: %w", err)
	}

	transcript := sha256.New()
	transcript.Write([]byte(hkdfLabel))
	transcript.Write(daemonPub)
	transcript.Write(mobilePub)
	salt := transcript.Sum(nil)

	daemonToMobile, err = hkdf.Key(sha256.New, shared, salt, dirDaemonToMobile, chacha20poly1305.KeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("e2ee: derive daemon->mobile key: %w", err)
	}
	mobileToDaemon, err = hkdf.Key(sha256.New, shared, salt, dirMobileToDaemon, chacha20poly1305.KeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("e2ee: derive mobile->daemon key: %w", err)
	}
	return daemonToMobile, mobileToDaemon, nil
}

// Seal encrypts plaintext with the next outbound counter, returning that
// counter and the ciphertext.
func (s *Session) Seal(plaintext []byte) (uint64, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sendCounter == math.MaxUint64 {
		return 0, nil, ErrCounterExhausted
	}
	counter := s.sendCounter
	ciphertext := s.send.Seal(nil, counterNonce(counter), plaintext, nil)
	s.sendCounter++
	return counter, ciphertext, nil
}

// Open decrypts a frame, rejecting any counter that is not exactly the next
// one expected from the peer: a repeat (replay) and a skip (reordered or
// dropped frame) are both refused rather than decrypted.
func (s *Session) Open(counter uint64, ciphertext []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Seal never emits MaxUint64, so the receive counter can be incremented
	// without wrapping.
	if s.recvCounter == math.MaxUint64 {
		return nil, ErrCounterExhausted
	}
	if counter != s.recvCounter {
		return nil, fmt.Errorf("%w: got counter %d, want %d", ErrReplay, counter, s.recvCounter)
	}
	plaintext, err := s.recv.Open(nil, counterNonce(counter), ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: counter %d: %s", ErrDecrypt, counter, err)
	}
	s.recvCounter++
	return plaintext, nil
}

// SendCounter reports how many frames this side has sealed.
func (s *Session) SendCounter() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendCounter
}

// ExpectedRecvCounter reports the counter the next inbound frame must carry.
func (s *Session) ExpectedRecvCounter() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recvCounter
}

// counterNonce renders a frame counter as a 12-byte ChaCha20-Poly1305 nonce.
// The leading 4 bytes stay zero: they are reserved so a future protocol
// version can add a sub-stream/channel identifier without changing the
// nonce's width.
func counterNonce(counter uint64) []byte {
	var nonce [chacha20poly1305.NonceSize]byte
	binary.BigEndian.PutUint64(nonce[4:], counter)
	return nonce[:]
}
