package e2ee

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateKeyPairPersistsAcrossRestarts(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreateKeyPair(dir)
	if err != nil {
		t.Fatalf("first LoadOrCreateKeyPair: %v", err)
	}

	// "Restart": a fresh load from the same home directory must hand back the
	// same keypair, not a new one (ADR-0007 (g)).
	second, err := LoadOrCreateKeyPair(dir)
	if err != nil {
		t.Fatalf("second LoadOrCreateKeyPair: %v", err)
	}
	if !bytes.Equal(first.Public(), second.Public()) {
		t.Errorf("public key changed across restart: %x != %x", first.Public(), second.Public())
	}
	if !bytes.Equal(first.private().Bytes(), second.private().Bytes()) {
		t.Error("secret key changed across restart")
	}
	if len(first.Public()) != PublicKeySize {
		t.Errorf("public key length = %d, want %d", len(first.Public()), PublicKeySize)
	}
}

func TestLoadOrCreateKeyPairFileIs0600(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "home")

	if _, err := LoadOrCreateKeyPair(dir); err != nil {
		t.Fatalf("LoadOrCreateKeyPair: %v", err)
	}

	info, err := os.Stat(KeyPairPath(dir))
	if err != nil {
		t.Fatalf("stat keypair: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("keypair file mode = %o, want 600", got)
	}
}

func TestLoadOrCreateKeyPairTightensLoosePermissions(t *testing.T) {
	dir := t.TempDir()

	if _, err := LoadOrCreateKeyPair(dir); err != nil {
		t.Fatalf("LoadOrCreateKeyPair: %v", err)
	}
	if err := os.Chmod(KeyPairPath(dir), 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if _, err := LoadOrCreateKeyPair(dir); err != nil {
		t.Fatalf("reload: %v", err)
	}

	info, err := os.Stat(KeyPairPath(dir))
	if err != nil {
		t.Fatalf("stat keypair: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("keypair file mode = %o, want 600 after reload", got)
	}
}

func TestLoadOrCreateKeyPairRoundTripsStoredEncoding(t *testing.T) {
	dir := t.TempDir()

	kp, err := LoadOrCreateKeyPair(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyPair: %v", err)
	}

	data, err := os.ReadFile(KeyPairPath(dir))
	if err != nil {
		t.Fatalf("read keypair: %v", err)
	}
	var stored persistedKeyPair
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("unmarshal keypair: %v", err)
	}
	if stored.V != keyPairFileVersion {
		t.Errorf("stored version = %d, want %d", stored.V, keyPairFileVersion)
	}
	if stored.PublicKeyB64 == "" || stored.SecretKeyB64 == "" {
		t.Fatalf("stored keypair has empty fields: %+v", stored)
	}
	if stored.PublicKeyB64 == stored.SecretKeyB64 {
		t.Error("stored public and secret keys are identical")
	}
	if got := kp.PublicBase64(); got == "" {
		t.Error("PublicBase64 returned empty string")
	}
}

func TestLoadOrCreateKeyPairRegeneratesCorruptFile(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{name: "not json", contents: "}}} not json"},
		{name: "wrong version", contents: `{"v":99,"publicKeyB64":"","secretKeyB64":""}`},
		{name: "bad base64", contents: `{"v":1,"publicKeyB64":"!!","secretKeyB64":"!!"}`},
		{name: "short secret", contents: `{"v":1,"publicKeyB64":"AAAA","secretKeyB64":"AAAA"}`},
		{name: "mismatched public key", contents: mismatchedKeyPairJSON()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := KeyPairPath(dir)
			if err := os.WriteFile(path, []byte(tc.contents), 0o600); err != nil {
				t.Fatalf("seed keypair file: %v", err)
			}

			kp, err := LoadOrCreateKeyPair(dir)
			if err != nil {
				t.Fatalf("LoadOrCreateKeyPair: %v", err)
			}
			if len(kp.Public()) != PublicKeySize {
				t.Fatalf("regenerated key has %d-byte public key", len(kp.Public()))
			}

			reloaded, err := LoadOrCreateKeyPair(dir)
			if err != nil {
				t.Fatalf("reload after regeneration: %v", err)
			}
			if !bytes.Equal(kp.Public(), reloaded.Public()) {
				t.Error("regenerated keypair was not persisted")
			}
		})
	}
}

func TestParsePublicKeyRejectsWrongSize(t *testing.T) {
	for _, size := range []int{0, 16, 31, 33, 64} {
		if _, err := ParsePublicKey(make([]byte, size)); err == nil {
			t.Errorf("ParsePublicKey(%d bytes) = nil error, want error", size)
		}
	}

	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if _, err := ParsePublicKey(kp.Public()); err != nil {
		t.Errorf("ParsePublicKey(valid key): %v", err)
	}
}

// mismatchedKeyPairJSON builds a keypair file whose public key belongs to a
// different keypair than its secret key.
func mismatchedKeyPairJSON() string {
	a, err := GenerateKeyPair()
	if err != nil {
		panic(err)
	}
	b, err := GenerateKeyPair()
	if err != nil {
		panic(err)
	}
	stored := persistedKeyPair{
		V:            keyPairFileVersion,
		PublicKeyB64: base64.StdEncoding.EncodeToString(a.Public()),
		SecretKeyB64: base64.StdEncoding.EncodeToString(b.private().Bytes()),
	}
	data, err := json.Marshal(stored)
	if err != nil {
		panic(err)
	}
	return string(data)
}
