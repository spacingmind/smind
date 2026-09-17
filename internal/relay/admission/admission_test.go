package admission

import (
	"bytes"
	"crypto/hmac"
	"errors"
	"testing"
	"time"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// runAdmission performs the full daemon-side exchange against v: obtains a
// challenge, computes the HMAC the daemon would (key = SHA-256 of the raw
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
	req.Hmac = ComputeHMAC(HashSecret(secret), req)
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
	req.Hmac = ComputeHMAC(HashSecret(secret), req)

	if _, err := v.Admit(req); err != nil {
		t.Fatalf("first use: %v", err)
	}
	// Replay the identical transcript (same server nonce, same HMAC).
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
	req.Hmac = ComputeHMAC(HashSecret(secret), req)

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

	// Correct HMAC over a *different* transcript: the nonce was issued for
	// workspace ws-a, the request claims ws-b. A correct-for-ws-b tag must
	// not admit (cross-workspace binding, ADR-0011).
	other := &relaypb.AdmitRequest{
		ProtocolVersion: ProtocolVersion,
		WorkspaceId:     "ws-b",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "daemon-key-1",
		ServerNonce:     chal.GetServerNonce(),
	}
	other.Hmac = ComputeHMAC(HashSecret(secret), other) // even if ws-b had the same secret
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

	// Unknown workspace vs wrong secret vs tampered HMAC vs garbage nonce:
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
	generic("tampered hmac", err)

	_, err = v.Admit(&relaypb.AdmitRequest{ServerNonce: []byte("never-issued")})
	generic("unknown nonce", err)
}

func TestComputeHMACCanonicalForm(t *testing.T) {
	// The transcript encoding must be unambiguous: length prefixes mean
	// ("ab", "c") and ("a", "bc") style collisions are impossible, and
	// client vs server nonce positions cannot be swapped to produce the
	// same tag.
	req := &relaypb.AdmitRequest{
		ProtocolVersion: 1,
		WorkspaceId:     "ab",
		ClientNonce:     []byte{1, 2, 3},
		ServerNonce:     []byte{4, 5, 6},
		DaemonKeyId:     "c",
	}
	key := []byte("k")
	base := ComputeHMAC(key, req)

	swapped := &relaypb.AdmitRequest{
		ProtocolVersion: req.GetProtocolVersion(),
		WorkspaceId:     req.GetWorkspaceId(),
		ClientNonce:     req.GetServerNonce(),
		ServerNonce:     req.GetClientNonce(),
		DaemonKeyId:     req.GetDaemonKeyId(),
	}
	if hmac.Equal(base, ComputeHMAC(key, swapped)) {
		t.Fatalf("swapped nonces produced the same HMAC")
	}

	boundaries := &relaypb.AdmitRequest{
		ProtocolVersion: 1,
		WorkspaceId:     "a",
		ClientNonce:     []byte{1, 2, 3},
		ServerNonce:     []byte{4, 5, 6},
		DaemonKeyId:     "bc",
	}
	if hmac.Equal(base, ComputeHMAC(key, boundaries)) {
		t.Fatalf("different string split produced the same HMAC")
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
		{ProtocolVersion: 2, WorkspaceId: "ws-a", ClientNonce: make([]byte, NonceSize), DaemonKeyId: "k"},
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
