package e2ee

import (
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
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

// hkdfLabel namespaces this protocol's key derivation. It carries the
// protocol version, so v2 keys can never be accepted by a v1 implementation.
const hkdfLabel = "smind relay e2ee v2"

// Derivation labels bind each derived key to one purpose and direction, so a
// frame can never be reflected back at its sender and still authenticate, and
// a confirmation key is never a traffic key.
const (
	dirDaemonToMobile = hkdfLabel + " daemon->mobile"
	dirMobileToDaemon = hkdfLabel + " mobile->daemon"
	confirmDaemonInfo = hkdfLabel + " confirm daemon"
	confirmMobileInfo = hkdfLabel + " confirm mobile"
)

// Session holds the two directional ChaCha20-Poly1305 keys derived from an
// X25519 exchange, plus the per-direction counters that both supply nonces
// and provide replay protection (ADR-0007 (c)/(d)).
//
// Nonces are never random: each direction's 12-byte nonce is a 4-byte zero
// prefix followed by the big-endian frame counter, so no nonce is ever reused
// under a key as long as the counter is monotonic — which is the same
// property that makes replayed frames detectable. Keys are unique per
// handshake (ADR-0035: both ends contribute a fresh ephemeral), so the
// counters restarting at zero in every session never repeats a (key, nonce).
type Session struct {
	mu          sync.Mutex
	send        cipher.AEAD
	recv        cipher.AEAD
	sendCounter uint64
	recvCounter uint64 // the next counter the peer is expected to use
}

// schedule is everything the v2 key schedule (ADR-0035 §2) derives from one
// handshake. Only th is public.
type schedule struct {
	th       []byte
	d2m, m2d []byte
	confirmD []byte
	confirmM []byte
}

// zero wipes the secret outputs (best effort: Go gives no stronger guarantee).
func (s *schedule) zero() {
	for _, b := range [][]byte{s.d2m, s.m2d, s.confirmD, s.confirmM} {
		clear(b)
	}
}

// TranscriptHash is the handshake transcript digest the session keys are
// salted with: SHA-256(label ‖ daemonPub ‖ daemonEphPub ‖ clientEphPub). It is
// public data both ends can compute, so an application layer above the
// channel (the relay's device_hello, ADR-0031 D9) can bind a signature to
// exactly this session. All three arguments are 32-byte X25519 public keys.
// Established channels expose it as Channel.TranscriptHash.
func TranscriptHash(daemonPub, daemonEphPub, clientEphPub []byte) []byte {
	transcript := sha256.New()
	transcript.Write([]byte(hkdfLabel))
	transcript.Write(daemonPub)
	transcript.Write(daemonEphPub)
	transcript.Write(clientEphPub)
	return transcript.Sum(nil)
}

// deriveSchedule runs the ADR-0035 §2 key schedule from the two DH outputs
// (es = DH(client eph, daemon static), ee = DH(client eph, daemon eph)).
// Both ends call it with the same inputs, so they arrive at the same keys.
func deriveSchedule(es, ee, daemonPub, daemonEphPub, clientEphPub []byte) (*schedule, error) {
	th := TranscriptHash(daemonPub, daemonEphPub, clientEphPub)
	ikm := make([]byte, 0, len(es)+len(ee))
	ikm = append(ikm, es...)
	ikm = append(ikm, ee...)
	defer clear(ikm)

	out := &schedule{th: th}
	for _, d := range []struct {
		dst  *[]byte
		info string
	}{
		{&out.d2m, dirDaemonToMobile},
		{&out.m2d, dirMobileToDaemon},
		{&out.confirmD, confirmDaemonInfo},
		{&out.confirmM, confirmMobileInfo},
	} {
		key, err := hkdf.Key(sha256.New, ikm, th, d.info, chacha20poly1305.KeySize)
		if err != nil {
			return nil, fmt.Errorf("e2ee: derive %q: %w", d.info, err)
		}
		*d.dst = key
	}
	return out, nil
}

// dh is X25519 with the RFC 7748 §6.1 all-zero-output check (crypto/ecdh
// already refuses a low-order peer point; the wrap gives it a typed error).
func dh(priv *ecdh.PrivateKey, peer []byte) ([]byte, error) {
	pub, err := ParsePublicKey(peer)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrProtocol, err)
	}
	shared, err := priv.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrKeyAgreement, err)
	}
	return shared, nil
}

// confirmTag is the key-confirmation MAC: HMAC-SHA256(confirmKey, th).
func confirmTag(confirmKey, th []byte) []byte {
	mac := hmac.New(sha256.New, confirmKey)
	mac.Write(th)
	return mac.Sum(nil)
}

// newSession builds the directional AEADs from a schedule.
func newSession(role Role, sched *schedule) (*Session, error) {
	sendKey, recvKey := sched.d2m, sched.m2d
	if role == RoleMobile {
		sendKey, recvKey = sched.m2d, sched.d2m
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
