package admission

import (
	"bytes"
	"crypto/hmac"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// runAdmission performs the full daemon-side exchange against v: obtains a
// challenge, computes the SCRAM-style proof the daemon would (from the raw
// secret), and calls Admit.
func runAdmission(v *Verifier, workspaceID, daemonKeyID string, secret []byte) (Session, error) {
	clientNonce := bytes.Repeat([]byte{0xa5}, NonceSize)
	chal, err := v.Challenge(&relaypb.AdmitChallengeRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     workspaceID,
		ClientNonce:     clientNonce,
		DaemonKeyId:     daemonKeyID,
	})
	if err != nil {
		return Session{}, err
	}
	req := &relaypb.AdmitRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     workspaceID,
		ClientNonce:     clientNonce,
		DaemonKeyId:     daemonKeyID,
		ServerNonce:     chal.GetServerNonce(),
	}
	req.Hmac = ComputeProof(secret, req)
	return v.Admit(req)
}

func newTestVerifier(t *testing.T, id string) (*Verifier, []byte) {
	t.Helper()
	ws, secret, err := NewWorkspace(id)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	if len(secret) != 32 {
		t.Fatalf("secret is %d bytes, want 32", len(secret))
	}
	if HashSecret(secret) == nil || bytes.Equal(HashSecret(secret), secret) {
		t.Fatalf("stored form must be a hash of, not equal to, the secret")
	}
	v := NewVerifier()
	v.Register(ws)
	return v, secret
}

func TestAdmitValidBindsWorkspace(t *testing.T) {
	v, secret := newTestVerifier(t, "ws-a")
	s, err := runAdmission(v, "ws-a", "daemon-key-1", secret)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if s.WorkspaceID != "ws-a" {
		t.Fatalf("session bound to %q, want %q", s.WorkspaceID, "ws-a")
	}
	if len(s.AdmissionID) == 0 {
		t.Fatalf("session has no admission ID")
	}
}

func TestAdmitWrongSecretRejected(t *testing.T) {
	v, _ := newTestVerifier(t, "ws-a")
	s, err := runAdmission(v, "ws-a", "daemon-key-1", bytes.Repeat([]byte{0x00}, 32))
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
	if s.WorkspaceID != "" {
		t.Fatalf("rejected admission must not bind a workspace, got %q", s.WorkspaceID)
	}
}

func TestAdmitUnknownWorkspaceRejected(t *testing.T) {
	v, secret := newTestVerifier(t, "ws-a")
	if _, err := runAdmission(v, "ws-nope", "daemon-key-1", secret); !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
}

func TestAdmitReplayRejected(t *testing.T) {
	v, secret := newTestVerifier(t, "ws-a")

	clientNonce := bytes.Repeat([]byte{0x5a}, NonceSize)
	chal, err := v.Challenge(&relaypb.AdmitChallengeRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-a",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
	})
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	req := &relaypb.AdmitRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-a",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
		ServerNonce:     chal.GetServerNonce(),
	}
	req.Hmac = ComputeProof(secret, req)

	if _, err := v.Admit(req); err != nil {
		t.Fatalf("first use: %v", err)
	}
	// Replay the identical transcript (same server nonce, same proof).
	if _, err := v.Admit(req); !errors.Is(err, ErrRejected) {
		t.Fatalf("replay: err = %v, want ErrRejected", err)
	}
}

func TestAdmitExpiredNonceRejected(t *testing.T) {
	base := time.Now()
	v, secret := newTestVerifier(t, "ws-a")
	v.now = func() time.Time { return base }

	clientNonce := bytes.Repeat([]byte{0x77}, NonceSize)
	chal, err := v.Challenge(&relaypb.AdmitChallengeRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-a",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
	})
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	req := &relaypb.AdmitRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-a",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
		ServerNonce:     chal.GetServerNonce(),
	}
	req.Hmac = ComputeProof(secret, req)

	v.now = func() time.Time { return base.Add(challengeTTL + time.Second) }
	if _, err := v.Admit(req); !errors.Is(err, ErrRejected) {
		t.Fatalf("expired: err = %v, want ErrRejected", err)
	}
}

func TestAdmitTranscriptMismatchRejected(t *testing.T) {
	v, secret := newTestVerifier(t, "ws-a")

	clientNonce := bytes.Repeat([]byte{0x11}, NonceSize)
	chal, err := v.Challenge(&relaypb.AdmitChallengeRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-a",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
	})
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}

	// Correct proof over a *different* transcript: the nonce was issued for
	// workspace ws-a, the request claims ws-b. A correct-for-ws-b proof must
	// not admit (cross-workspace binding, ADR-0011).
	other := &relaypb.AdmitRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-b",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
		ServerNonce:     chal.GetServerNonce(),
	}
	other.Hmac = ComputeProof(secret, other) // even if ws-b had the same secret
	if _, err := v.Admit(other); !errors.Is(err, ErrRejected) {
		t.Fatalf("cross-workspace transcript: err = %v, want ErrRejected", err)
	}
}

// TestRejectionShapeIsUniform asserts structurally that every rejection
// path surfaces as the same error value, so no caller-visible signal
// distinguishes unknown-workspace from wrong-secret from bad-nonce.
func TestRejectionShapeIsUniform(t *testing.T) {
	v, secret := newTestVerifier(t, "ws-a")

	generic := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrRejected) {
			t.Fatalf("%s: err = %v, want ErrRejected", name, err)
		}
		if _, ok := err.(interface{ Unwrap() error }); ok && err != ErrRejected {
			t.Fatalf("%s: rejection wraps another error — leaks cause", name)
		}
	}

	// Unknown workspace vs wrong secret vs tampered proof vs garbage nonce:
	// all must be indistinguishable ErrRejected.
	_, err := runAdmission(v, "ws-nope", "daemon-key-1", secret)
	generic("unknown workspace", err)
	_, err = runAdmission(v, "ws-a", "daemon-key-1", bytes.Repeat([]byte{0x00}, 32))
	generic("wrong secret", err)

	clientNonce := bytes.Repeat([]byte{0x33}, NonceSize)
	chal, err := v.Challenge(&relaypb.AdmitChallengeRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-a",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
	})
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	req := &relaypb.AdmitRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-a",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
		ServerNonce:     chal.GetServerNonce(),
		Hmac:            bytes.Repeat([]byte{0x00}, 32),
	}
	_, err = v.Admit(req)
	generic("tampered proof", err)

	_, err = v.Admit(&relaypb.AdmitRequest{ServerNonce: []byte("never-issued")})
	generic("unknown nonce", err)
}

func TestProofMaskCanonicalForm(t *testing.T) {
	// The transcript encoding must be unambiguous: length prefixes mean
	// ("ab", "c") and ("a", "bc") style collisions are impossible, and
	// client vs server nonce positions cannot be swapped to produce the
	// same mask.
	req := &relaypb.AdmitRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ab",
		ClientNonce:     []byte{1, 2, 3},
		ServerNonce:     []byte{4, 5, 6},
		DaemonKeyId:     "c",
	}
	key := []byte("k")
	base := ProofMask(key, req)

	swapped := &relaypb.AdmitRequest{
		ProtocolVersion: req.GetProtocolVersion(),
		WorkspaceId:     req.GetWorkspaceId(),
		ClientNonce:     req.GetServerNonce(),
		ServerNonce:     req.GetClientNonce(),
		DaemonKeyId:     req.GetDaemonKeyId(),
	}
	if hmac.Equal(base, ProofMask(key, swapped)) {
		t.Fatalf("swapped nonces produced the same mask")
	}

	boundaries := &relaypb.AdmitRequest{
		ProtocolVersion: req.GetProtocolVersion(),
		WorkspaceId:     "a",
		ClientNonce:     []byte{1, 2, 3},
		ServerNonce:     []byte{4, 5, 6},
		DaemonKeyId:     "bc",
	}
	if hmac.Equal(base, ProofMask(key, boundaries)) {
		t.Fatalf("different string split produced the same mask")
	}
}

// TestProofKnownVector pins the exact proof bytes for a fixed input. The
// identical vector is asserted by mobile/src/relay/__tests__/admission.test.ts
// and desktop/daemon-client/src/relay/admission.rs, so a drift in any one
// implementation's transcript/XOR construction fails that implementation's
// own suite.
func TestProofKnownVector(t *testing.T) {
	secret := bytes.Repeat([]byte{0x42}, 32)
	req := &relaypb.AdmitRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-vector",
		ClientNonce:     bytes.Repeat([]byte{0x01}, NonceSize),
		ServerNonce:     bytes.Repeat([]byte{0x02}, NonceSize),
		DaemonKeyId:     "key-vector",
	}
	got := hex.EncodeToString(ComputeProof(secret, req))
	if got != knownVectorProofHex {
		t.Fatalf("proof = %s, want %s", got, knownVectorProofHex)
	}
	if !verifyProof(HashSecret(secret), withProof(req, ComputeProof(secret, req))) {
		t.Fatalf("known-vector proof does not verify against its own stored key")
	}
}

const knownVectorProofHex = "b007e940ba3c295ffd33e247373d4d150370b3702d90d9a4757d435227b37cfb"

func withProof(req *relaypb.AdmitRequest, proof []byte) *relaypb.AdmitRequest {
	cp := &relaypb.AdmitRequest{
		ProtocolVersion: req.GetProtocolVersion(),
		WorkspaceId:     req.GetWorkspaceId(),
		ClientNonce:     req.GetClientNonce(),
		ServerNonce:     req.GetServerNonce(),
		DaemonKeyId:     req.GetDaemonKeyId(),
		Hmac:            proof,
	}
	return cp
}

// TestAdmitStoredKeyAloneCannotAdmit is the regression test for the
// version-1 flaw: the relay's workspaces.json holds StoredKey, and under
// v1 that was the HMAC key, so reading the file was enough to admit as the
// workspace. Under the SCRAM-style proof an attacker holding only
// StoredKey must be rejected for every proof they can construct from it.
func TestAdmitStoredKeyAloneCannotAdmit(t *testing.T) {
	v, secret := newTestVerifier(t, "ws-a")
	storedKey := HashSecret(secret) // exactly what the relay persists

	attacks := map[string]func(req *relaypb.AdmitRequest) []byte{
		"v1 style: HMAC(StoredKey, transcript)": func(req *relaypb.AdmitRequest) []byte {
			return ProofMask(storedKey, req)
		},
		"StoredKey passed off as ClientKey": func(req *relaypb.AdmitRequest) []byte {
			// ComputeProof hashes its input to get StoredKey, so feeding it
			// StoredKey yields a mask keyed by SHA-256(StoredKey) — wrong —
			// and the direct construction below uses the right mask but the
			// wrong ClientKey. Both must fail.
			return ComputeProof(storedKey, req)
		},
		"XOR of mask with StoredKey": func(req *relaypb.AdmitRequest) []byte {
			mask := ProofMask(storedKey, req)
			out := make([]byte, len(mask))
			for i := range out {
				out[i] = mask[i] ^ storedKey[i]
			}
			return out
		},
		"all zero":  func(*relaypb.AdmitRequest) []byte { return make([]byte, 32) },
		"too short": func(*relaypb.AdmitRequest) []byte { return make([]byte, 31) },
		"too long":  func(*relaypb.AdmitRequest) []byte { return make([]byte, 33) },
		"empty":     func(*relaypb.AdmitRequest) []byte { return nil },
		"StoredKey": func(*relaypb.AdmitRequest) []byte { return append([]byte(nil), storedKey...) },
	}
	for name, forge := range attacks {
		t.Run(name, func(t *testing.T) {
			clientNonce := bytes.Repeat([]byte{0x6b}, NonceSize)
			chal, err := v.Challenge(&relaypb.AdmitChallengeRequest{
				ProtocolVersion: ProtocolVersion,
				WorkspaceId:     "ws-a",
				ClientNonce:     clientNonce,
				DaemonKeyId:     "daemon-key-1",
			})
			if err != nil {
				t.Fatalf("challenge: %v", err)
			}
			req := &relaypb.AdmitRequest{
				ProtocolVersion: ProtocolVersion,
				WorkspaceId:     "ws-a",
				ClientNonce:     clientNonce,
				DaemonKeyId:     "daemon-key-1",
				ServerNonce:     chal.GetServerNonce(),
			}
			req.Hmac = forge(req)
			if s, err := v.Admit(req); !errors.Is(err, ErrRejected) || s.WorkspaceID != "" {
				t.Fatalf("forged proof admitted: session=%+v err=%v", s, err)
			}
		})
	}

	// Sanity: the genuine secret holder still gets in on the same verifier.
	if _, err := runAdmission(v, "ws-a", "daemon-key-1", secret); err != nil {
		t.Fatalf("genuine admission failed: %v", err)
	}
}

func TestAdmitV1Rejected(t *testing.T) {
	v, secret := newTestVerifier(t, "ws-a")
	clientNonce := bytes.Repeat([]byte{0x21}, NonceSize)

	if _, err := v.Challenge(&relaypb.AdmitChallengeRequest{
		ProtocolVersion: 1,
		WorkspaceId:     "ws-a",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
	}); !errors.Is(err, ErrRejected) {
		t.Fatalf("v1 challenge: err = %v, want ErrRejected", err)
	}

	// A v2 challenge followed by a v1-labelled Admit (with the proof valid
	// for that v1 transcript, and the old v1 HMAC) must not admit.
	chal, err := v.Challenge(&relaypb.AdmitChallengeRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-a",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
	})
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	req := &relaypb.AdmitRequest{
		ProtocolVersion: 1,
		WorkspaceId:     "ws-a",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
		ServerNonce:     chal.GetServerNonce(),
	}
	req.Hmac = ComputeProof(secret, req)
	if _, err := v.Admit(req); !errors.Is(err, ErrRejected) {
		t.Fatalf("v1 admit: err = %v, want ErrRejected", err)
	}
}

// TestAdmitProofBoundToChallenge: a proof valid for one challenge cannot
// be moved onto another challenge's server nonce.
func TestAdmitProofBoundToChallenge(t *testing.T) {
	v, secret := newTestVerifier(t, "ws-a")
	clientNonce := bytes.Repeat([]byte{0x31}, NonceSize)
	issue := func() []byte {
		chal, err := v.Challenge(&relaypb.AdmitChallengeRequest{
			ProtocolVersion: ProtocolVersion,
			WorkspaceId:     "ws-a",
			ClientNonce:     clientNonce,
			DaemonKeyId:     "daemon-key-1",
		})
		if err != nil {
			t.Fatalf("challenge: %v", err)
		}
		return chal.GetServerNonce()
	}
	n1, n2 := issue(), issue()
	mk := func(nonce []byte) *relaypb.AdmitRequest {
		return &relaypb.AdmitRequest{
			ProtocolVersion: ProtocolVersion,
			WorkspaceId:     "ws-a",
			ClientNonce:     clientNonce,
			DaemonKeyId:     "daemon-key-1",
			ServerNonce:     nonce,
		}
	}
	proof1 := ComputeProof(secret, mk(n1))
	moved := mk(n2)
	moved.Hmac = proof1
	if _, err := v.Admit(moved); !errors.Is(err, ErrRejected) {
		t.Fatalf("proof moved to another challenge: err = %v, want ErrRejected", err)
	}
	ok := mk(n1)
	ok.Hmac = proof1
	if _, err := v.Admit(ok); err != nil {
		t.Fatalf("original pairing: %v", err)
	}
}

func TestChallengeValidation(t *testing.T) {
	v, _ := newTestVerifier(t, "ws-a")
	valid := &relaypb.AdmitChallengeRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-a",
		ClientNonce:     make([]byte, NonceSize),
		DaemonKeyId:     "k",
	}
	if _, err := v.Challenge(valid); err != nil {
		t.Fatalf("valid challenge: %v", err)
	}

	bad := []*relaypb.AdmitChallengeRequest{
		{ProtocolVersion: 1, WorkspaceId: "ws-a", ClientNonce: make([]byte, NonceSize), DaemonKeyId: "k"},
		{ProtocolVersion: ProtocolVersion, WorkspaceId: "", ClientNonce: make([]byte, NonceSize), DaemonKeyId: "k"},
		{ProtocolVersion: ProtocolVersion, WorkspaceId: "ws-a", ClientNonce: []byte{1}, DaemonKeyId: "k"},
		{ProtocolVersion: ProtocolVersion, WorkspaceId: "ws-a", ClientNonce: make([]byte, NonceSize), DaemonKeyId: ""},
	}
	for i, req := range bad {
		if _, err := v.Challenge(req); !errors.Is(err, ErrRejected) {
			t.Fatalf("bad[%d]: err = %v, want ErrRejected", i, err)
		}
	}
}

func TestNewWorkspaceSecretIsRandom(t *testing.T) {
	_, s1, err := NewWorkspace("ws")
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	_, s2, err := NewWorkspace("ws")
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	if bytes.Equal(s1, s2) {
		t.Fatalf("two generated secrets are identical")
	}
}
