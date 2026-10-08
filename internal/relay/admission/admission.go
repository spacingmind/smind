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
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// ProtocolVersion is the admission state machine version. Version 2
// (ADR-0011, 2026-10-08 amendment) is the SCRAM-style proof: the client
// proves knowledge of the raw workspace secret without the relay's stored
// hash being sufficient to forge it. Version 1 (HMAC keyed by the stored
// hash itself) made the relay's workspaces.json credential-equivalent and
// is rejected outright — never dual-accepted.
const ProtocolVersion uint32 = 2

// NonceSize is the length of the client and server nonces.
const NonceSize = 32

// challengeTTL bounds how long an issued server nonce stays valid. The
// daemon completes the two-RPC exchange in milliseconds; 30s is generous
// headroom while keeping the replay window small.
const challengeTTL = 30 * time.Second

// Default bounds on outstanding (issued, unconsumed, unexpired) challenges.
// Challenge is unauthenticated and, to avoid a workspace-existence oracle,
// is answered even for unknown workspace IDs, so without a bound a flood of
// AdmitChallenge calls grows the map without limit for one challengeTTL at
// a time. The per-workspace cap stops one workspace ID (known or made up)
// from consuming the global budget on its own; the global cap bounds the
// total. A legitimate daemon/device holds a challenge for milliseconds, so
// both defaults are far above any real load.
const (
	DefaultMaxChallenges             = 4096
	DefaultMaxChallengesPerWorkspace = 256
)

// ErrRejected is the single, generic admission failure returned for every
// rejection path (unknown workspace, wrong secret, bad transcript,
// unknown/expired/consumed nonce, version mismatch). Distinct causes would
// let an attacker probe which workspace IDs exist or how close a guessed
// secret is, so callers must not wrap or enrich it with the cause.
var ErrRejected = errors.New("admission: rejected")

// Workspace is the relay's persistent record of a workspace it serves:
// its ID and the hash of its 256-bit admission secret. The raw secret
// (SCRAM's ClientKey) exists only where it was generated (pairing time);
// the relay keeps only StoredKey = SHA-256(ClientKey), which is not enough
// to produce an accepted proof (see ComputeProof).
type Workspace struct {
	ID         string
	SecretHash []byte // StoredKey = SHA-256(Secret); see NewWorkspace and ComputeProof
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
	perWS      map[string]int // workspace ID -> outstanding challenges
	now        func() time.Time

	maxChallenges     int
	maxChallengesPerW int
}

// VerifierOption configures a Verifier.
type VerifierOption func(*Verifier)

// WithMaxChallenges overrides the global and per-workspace-ID caps on
// outstanding challenges (<=0 keeps the default for that cap).
func WithMaxChallenges(global, perWorkspace int) VerifierOption {
	return func(v *Verifier) {
		if global > 0 {
			v.maxChallenges = global
		}
		if perWorkspace > 0 {
			v.maxChallengesPerW = perWorkspace
		}
	}
}

// WithClock injects the time source (tests advance it instead of sleeping).
func WithClock(now func() time.Time) VerifierOption {
	return func(v *Verifier) { v.now = now }
}

type challenge struct {
	workspaceID string
	daemonKeyID string
	clientNonce []byte
	expiresAt   time.Time
}

// NewVerifier returns a Verifier with no workspaces registered.
func NewVerifier(opts ...VerifierOption) *Verifier {
	v := &Verifier{
		secrets:           make(map[string][]byte),
		challenges:        make(map[string]challenge),
		perWS:             make(map[string]int),
		maxChallenges:     DefaultMaxChallenges,
		maxChallengesPerW: DefaultMaxChallengesPerWorkspace,
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
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
// after challengeTTL. When the global or per-workspace cap on outstanding
// challenges is reached it fails with the same uniform ErrRejected as every
// other rejection (no new oracle); slots free as challenges are consumed or
// expire.
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
	if len(v.challenges) >= v.maxChallenges || v.perWS[req.GetWorkspaceId()] >= v.maxChallengesPerW {
		return nil, ErrRejected
	}
	v.challenges[string(serverNonce)] = challenge{
		workspaceID: req.GetWorkspaceId(),
		daemonKeyID: req.GetDaemonKeyId(),
		clientNonce: append([]byte(nil), req.GetClientNonce()...),
		expiresAt:   now.Add(challengeTTL),
	}
	v.perWS[req.GetWorkspaceId()]++
	return &relaypb.AdmitChallengeResponse{ServerNonce: serverNonce}, nil
}

// Admit implements the Admit handler: it verifies the SCRAM-style proof
// over the challenge transcript in constant time and, on success,
// atomically consumes the server nonce and returns a workspace-bound
// Session. Every failure is ErrRejected (see its doc for the no-oracle
// rule).
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

	storedKey, known := v.secrets[ch.workspaceID]
	if !transcriptInputsMatch || !known || !verifyProof(storedKey, req) {
		return Session{}, ErrRejected
	}

	// Single use: consume the nonce so a captured transcript can never be
	// replayed, even within its TTL.
	v.deleteChallengeLocked(string(req.GetServerNonce()), ch.workspaceID)

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

// evictLocked drops expired challenges so the caps in Challenge only count
// live ones.
func (v *Verifier) evictLocked(now time.Time) {
	for nonce, ch := range v.challenges {
		if now.After(ch.expiresAt) {
			v.deleteChallengeLocked(nonce, ch.workspaceID)
		}
	}
}

func (v *Verifier) deleteChallengeLocked(nonce, workspaceID string) {
	delete(v.challenges, nonce)
	if v.perWS[workspaceID]--; v.perWS[workspaceID] <= 0 {
		delete(v.perWS, workspaceID)
	}
}

// transcript is the canonical byte string both ends bind the proof to:
// protocol_version || workspace_id || client_nonce || server_nonce ||
// daemon_key_id, where the version is a fixed-width big-endian 4 bytes and
// workspace_id/daemon_key_id are 4-byte big-endian length-prefixed so the
// concatenation is unambiguous.
func transcript(req *relaypb.AdmitRequest) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], req.GetProtocolVersion())
	out := append([]byte(nil), buf[:]...)
	out = appendLPString(out, req.GetWorkspaceId())
	out = append(out, req.GetClientNonce()...)
	out = append(out, req.GetServerNonce()...)
	out = appendLPString(out, req.GetDaemonKeyId())
	return out
}

func appendLPString(dst []byte, s string) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(len(s)))
	dst = append(dst, buf[:]...)
	return append(dst, s...)
}

// ProofMask is HMAC-SHA256(StoredKey, transcript): the one-time pad both
// ends derive from what the relay stores, bound to this exchange's
// transcript. It is exported so the client packages and tests can build
// and check proofs; it is NOT itself an acceptable proof (that was the
// version-1 scheme).
func ProofMask(storedKey []byte, req *relaypb.AdmitRequest) []byte {
	mac := hmac.New(sha256.New, storedKey)
	mac.Write(transcript(req))
	return mac.Sum(nil)
}

// ComputeProof builds the client's admission proof, SCRAM-style (RFC 5802):
//
//	ClientKey = the raw 32-byte workspace secret
//	StoredKey = SHA-256(ClientKey)            (what the relay persists)
//	proof     = ClientKey XOR HMAC-SHA256(StoredKey, transcript)
//
// The relay recovers ClientKey' = proof XOR HMAC(StoredKey, transcript) and
// accepts iff SHA-256(ClientKey') == StoredKey. Knowing only StoredKey
// (e.g. by reading the relay's workspaces.json) gives an attacker the pad
// but not ClientKey, so it cannot forge a proof. secret must be the raw
// 32-byte workspace secret.
func ComputeProof(secret []byte, req *relaypb.AdmitRequest) []byte {
	mask := ProofMask(HashSecret(secret), req)
	proof := make([]byte, len(mask))
	for i := range proof {
		var k byte
		if i < len(secret) {
			k = secret[i]
		}
		proof[i] = k ^ mask[i]
	}
	return proof
}

// verifyProof is the relay-side check: recover the candidate ClientKey from
// the proof and compare its hash to the stored key in constant time.
func verifyProof(storedKey []byte, req *relaypb.AdmitRequest) bool {
	proof := req.GetHmac()
	if len(proof) != sha256.Size {
		return false
	}
	mask := ProofMask(storedKey, req)
	recovered := make([]byte, sha256.Size)
	for i := range recovered {
		recovered[i] = proof[i] ^ mask[i]
	}
	return subtle.ConstantTimeCompare(HashSecret(recovered), storedKey) == 1
}
