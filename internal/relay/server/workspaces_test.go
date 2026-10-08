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

// TestEnrolledWorkspaceFromOldStoreAdmitsV2 proves the SCRAM-style proof
// (admission protocol v2) kept the stored value unchanged: a
// workspaces.json written before the change — just {"secret_hash":
// base64(SHA-256(secret))} — still verifies a v2 client holding the raw
// secret, with no migration.
func TestEnrolledWorkspaceFromOldStoreAdmitsV2(t *testing.T) {
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

	v, err := newWorkspaceStore(dir).verifier()
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
