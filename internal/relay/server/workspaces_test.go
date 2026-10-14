package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/relay/admission"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// TestEnrolledHostFromOldStoreAdmitsV2 proves the SCRAM-style proof
// (admission protocol v2) kept the stored value unchanged: a
// workspaces.json written before the change — just {"secret_hash":
// base64(SHA-256(secret))} — still verifies a v2 client holding the raw
// secret, with no migration.
func TestEnrolledHostFromOldStoreAdmitsV2(t *testing.T) {
	secret := bytes.Repeat([]byte{0x7e}, 32)
	sum := sha256.Sum256(secret)

	dir := t.TempDir()
	old, err := json.Marshal(map[string]map[string]string{
		"ws-old": {"secret_hash": base64.StdEncoding.EncodeToString(sum[:])},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspaces.json"), old, 0o600); err != nil {
		t.Fatal(err)
	}

	v, err := newHostStore(dir).verifier()
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	clientNonce := bytes.Repeat([]byte{0x09}, admission.NonceSize)
	chal, err := v.Challenge(&relaypb.AdmitChallengeRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     "ws-old",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "k",
	})
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	req := &relaypb.AdmitRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     "ws-old",
		ClientNonce:     clientNonce,
		DaemonKeyId:     "k",
		ServerNonce:     chal.GetServerNonce(),
	}
	req.Hmac = admission.ComputeProof(secret, req)
	if _, err := v.Admit(req); err != nil {
		t.Fatalf("v2 admit against pre-existing store: %v", err)
	}
}

// TestHostStoreRotateAdmitsNewSecretOnly proves rotate replaces the stored
// hash: a verifier seeded afterwards admits the new secret and rejects the
// old one, and rm drops the host entirely.
func TestHostStoreRotateAdmitsNewSecretOnly(t *testing.T) {
	store := newHostStore(t.TempDir())
	oldSecret, err := store.enroll("h")
	if err != nil {
		t.Fatal(err)
	}
	newSecret, err := store.rotate("h")
	if err != nil {
		t.Fatal(err)
	}

	admit := func(id string, secret []byte) error {
		v, err := store.verifier()
		if err != nil {
			t.Fatal(err)
		}
		nonce := bytes.Repeat([]byte{0x05}, admission.NonceSize)
		chal, err := v.Challenge(&relaypb.AdmitChallengeRequest{
			ProtocolVersion: admission.ProtocolVersion, WorkspaceId: id, ClientNonce: nonce, DaemonKeyId: "k",
		})
		if err != nil {
			return err
		}
		req := &relaypb.AdmitRequest{
			ProtocolVersion: admission.ProtocolVersion, WorkspaceId: id, ClientNonce: nonce,
			DaemonKeyId: "k", ServerNonce: chal.GetServerNonce(),
		}
		req.Hmac = admission.ComputeProof(secret, req)
		_, err = v.Admit(req)
		return err
	}
	if err := admit("h", newSecret); err != nil {
		t.Fatalf("new secret must admit: %v", err)
	}
	if err := admit("h", oldSecret); err == nil {
		t.Fatal("old secret must not admit after rotate")
	}
	if err := store.remove("h"); err != nil {
		t.Fatal(err)
	}
	if err := admit("h", newSecret); err == nil {
		t.Fatal("removed relay host must not admit")
	}
}
