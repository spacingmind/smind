// Package admission implements relay admission (ADR-0011): the
// daemon↔relay auth layer that decides which workspace a connection is
// bound to, independent of the E2EE handshake the relay blindly forwards.
// It is pure logic with no network or gRPC wiring; the future Relay
// service implementation calls these methods from its handlers.
package admission

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"sync"
	"time"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// ProtocolVersion is the admission state machine version (ADR-0011 v1:
// workspace-secret HMAC challenge-response only).
const ProtocolVersion uint32 = 1

// NonceSize is the length of the client and server nonces.
const NonceSize = 32

// challengeTTL bounds how long an issued server nonce stays valid. The
// daemon completes the two-RPC exchange in milliseconds; 30s is generous
// headroom while keeping the replay window small.
const challengeTTL = 30 * time.Second

// ErrRejected is the single, generic admission failure returned for every
// rejection path (unknown workspace, wrong secret, bad transcript,
// unknown/expired/consumed nonce, version mismatch). Distinct causes would
// let an attacker probe which workspace IDs exist or how close a guessed
// secret is, so callers must not wrap or enrich it with the cause.
var ErrRejected = errors.New("admission: rejected")

// Workspace is the relay's persistent record of a workspace it serves:
// its ID and the hash of its 256-bit admission secret. The raw secret
// exists only where it was generated (pairing time); the relay keeps only
// the hash.
type Workspace struct {
	ID         string
	SecretHash []byte // SHA-256(Secret); see NewWorkspace and ComputeHMAC
}

// NewWorkspace generates a fresh workspace record with a random 256-bit
// secret and returns the record plus the raw secret exactly once, for the
// caller to hand to the daemon's pairing offer flow. The raw secret is
// never persisted by this package.
func NewWorkspace(id string) (rec Workspace, secret []byte, err error) {
	secret = make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return Workspace{}, nil, fmt.Errorf("admission: generate secret: %w", err)
	}
	return Workspace{ID: id, SecretHash: HashSecret(secret)}, secret, nil
}

// HashSecret maps a raw admission secret to the stored form. Plain SHA-256
// (not a password KDF) is deliberate: the secret is 256 random bits, not
// user-chosen, so brute-forcing the hash is infeasible and a slow KDF would
// only add latency to every relay-side verification.
func HashSecret(secret []byte) []byte {
	h := sha256.Sum256(secret)
	return h[:]
}

// Session is a successfully admitted, workspace-bound connection context.
// The future gRPC server attaches one of these to each connection after a
// successful Admit and enforces that all later frames on that connection
// match WorkspaceID.
type Session struct {
	WorkspaceID string
	AdmissionID []byte
}

// Verifier implements the server side of the admission exchange. It owns
// the issued-but-unconsumed server nonces (in memory — a relay restart
// simply invalidates in-flight challenges, clients retry) and verifies
// Admit requests against the registered workspaces.
type Verifier struct {
	mu         sync.Mutex
	secrets    map[string][]byte // workspace ID -> secret hash
	challenges map[string]challenge
	now        func() time.Time
}

type challenge struct {
	workspaceID string
	daemonKeyID string
	clientNonce []byte
	expiresAt   time.Time
}

// NewVerifier returns a Verifier with no workspaces registered.
func NewVerifier() *Verifier {
	return &Verifier{
		secrets:    make(map[string][]byte),
		challenges: make(map[string]challenge),
	}
}

// Register adds (or replaces) a workspace record. Called at pairing /
// enrollment time, not per connection.
func (v *Verifier) Register(ws Workspace) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.secrets[ws.ID] = ws.SecretHash
}

// Challenge implements the AdmitChallenge handler: it issues a fresh
// single-use server nonce keyed to the requested workspace and transcript
// inputs, which Admit later requires to match exactly. The nonce expires
// after challengeTTL.
func (v *Verifier) Challenge(req *relaypb.AdmitChallengeRequest) (*relaypb.AdmitChallengeResponse, error) {
	if req.GetProtocolVersion() != ProtocolVersion ||
		len(req.GetClientNonce()) != NonceSize ||
		req.GetWorkspaceId() == "" || req.GetDaemonKeyId() == "" {
		return nil, ErrRejected
	}

	serverNonce := make([]byte, NonceSize)
	if _, err := rand.Read(serverNonce); err != nil {
		return nil, fmt.Errorf("admission: generate nonce: %w", err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.nowLocked()
	v.evictLocked(now)
	v.challenges[string(serverNonce)] = challenge{
		workspaceID: req.GetWorkspaceId(),
		daemonKeyID: req.GetDaemonKeyId(),
		clientNonce: append([]byte(nil), req.GetClientNonce()...),
		expiresAt:   now.Add(challengeTTL),
	}
	return &relaypb.AdmitChallengeResponse{ServerNonce: serverNonce}, nil
}

// Admit implements the Admit handler: it verifies the HMAC over the
// challenge transcript in constant time and, on success, atomically
// consumes the server nonce and returns a workspace-bound Session.
// Every failure is ErrRejected (see its doc for the no-oracle rule).
func (v *Verifier) Admit(req *relaypb.AdmitRequest) (Session, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.nowLocked()
	v.evictLocked(now)

	ch, ok := v.challenges[string(req.GetServerNonce())]
	if !ok || now.After(ch.expiresAt) {
		return Session{}, ErrRejected
	}

	// Transcript inputs must match the challenge exactly, so a nonce
	// issued for one workspace/key/nonce triple cannot admit another.
	transcriptInputsMatch := req.GetProtocolVersion() == ProtocolVersion &&
		req.GetWorkspaceId() == ch.workspaceID &&
		req.GetDaemonKeyId() == ch.daemonKeyID &&
		hmac.Equal(req.GetClientNonce(), ch.clientNonce)

	want, known := v.secrets[ch.workspaceID]
	if !transcriptInputsMatch || !known ||
		!hmac.Equal(req.GetHmac(), ComputeHMAC(want, req)) {
		return Session{}, ErrRejected
	}

	// Single use: consume the nonce so a captured transcript can never be
	// replayed, even within its TTL.
	delete(v.challenges, string(req.GetServerNonce()))

	admissionID := make([]byte, 16)
	if _, err := rand.Read(admissionID); err != nil {
		return Session{}, fmt.Errorf("admission: generate admission id: %w", err)
	}
	return Session{WorkspaceID: ch.workspaceID, AdmissionID: admissionID}, nil
}

func (v *Verifier) nowLocked() time.Time {
	if v.now != nil {
		return v.now()
	}
	return time.Now()
}

// evictLocked drops expired challenges. It does not bound the map against
// a flood of never-completed Challenge calls; that is rate limiting's job
// (transport step), not this package's.
func (v *Verifier) evictLocked(now time.Time) {
	for nonce, ch := range v.challenges {
		if now.After(ch.expiresAt) {
			delete(v.challenges, nonce)
		}
	}
}

// ComputeHMAC is the canonical transcript construction shared by both
// ends: HMAC-SHA256(key, protocol_version || workspace_id || client_nonce
// || server_nonce || daemon_key_id) where the version is a fixed-width
// big-endian 4 bytes and workspace_id/daemon_key_id are 4-byte
// big-endian length-prefixed so concatenation is unambiguous.
//
// The key is HashSecret(rawSecret) — SHA-256 of the workspace secret —
// not the raw secret itself: the relay persists only the hash (ADR-0011),
// so both ends must derive the same HMAC key from what they hold (the
// daemon hashes its raw secret; the relay uses its stored hash).
func ComputeHMAC(key []byte, req *relaypb.AdmitRequest) []byte {
	mac := hmac.New(sha256.New, key)
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], req.GetProtocolVersion())
	mac.Write(buf[:])
	writeLPString(mac, req.GetWorkspaceId())
	mac.Write(req.GetClientNonce())
	mac.Write(req.GetServerNonce())
	writeLPString(mac, req.GetDaemonKeyId())
	return mac.Sum(nil)
}

func writeLPString(mac hash.Hash, s string) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(len(s)))
	mac.Write(buf[:])
	mac.Write([]byte(s))
}
