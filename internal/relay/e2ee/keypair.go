// Package e2ee implements the end-to-end encrypted session crypto shared by
// the smind daemon and a paired mobile device: X25519 key agreement plus
// ChaCha20-Poly1305 AEAD with 12-byte counter-based nonces, per
// docs/decisions/0007-relay-architecture.md (c)/(d)/(e)/(g).
//
// The relay itself never sees any of this material: it forwards opaque
// ciphertext frames between the two ends (ADR-0007 (a)/(b)).
package e2ee

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	keyPairFileName = "relay-keypair.json"
	// keyPairFileVersion is the on-disk schema version of the keypair file.
	keyPairFileVersion = 1
	// PublicKeySize is the length in bytes of an X25519 public key.
	PublicKeySize = 32
)

// ErrInvalidPublicKey is returned when bytes do not form a valid X25519
// public key.
var ErrInvalidPublicKey = errors.New("e2ee: invalid X25519 public key")

// KeyPair is an X25519 keypair. The daemon persists one long-lived keypair
// (ADR-0007 (g)); mobile devices generate a fresh ephemeral one per pairing
// session and never persist it.
type KeyPair struct {
	priv *ecdh.PrivateKey
}

// persistedKeyPair is the on-disk JSON shape, mirroring paseo's
// daemon-keypair.json so the two stay recognisably the same file.
type persistedKeyPair struct {
	V            int    `json:"v"`
	PublicKeyB64 string `json:"publicKeyB64"`
	SecretKeyB64 string `json:"secretKeyB64"`
}

// GenerateKeyPair returns a fresh random X25519 keypair.
func GenerateKeyPair() (*KeyPair, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate X25519 key: %w", err)
	}
	return &KeyPair{priv: priv}, nil
}

// Public returns a copy of the public key bytes.
func (k *KeyPair) Public() []byte {
	return k.priv.PublicKey().Bytes()
}

// PublicBase64 returns the public key in unpadded base64url form, the
// encoding used in pairing offers.
func (k *KeyPair) PublicBase64() string {
	return base64.RawURLEncoding.EncodeToString(k.Public())
}

// private returns the underlying X25519 private key for key agreement.
func (k *KeyPair) private() *ecdh.PrivateKey { return k.priv }

// ParsePublicKey validates raw X25519 public key bytes.
func ParsePublicKey(b []byte) (*ecdh.PublicKey, error) {
	if len(b) != PublicKeySize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidPublicKey, len(b), PublicKeySize)
	}
	pub, err := ecdh.X25519().NewPublicKey(b)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidPublicKey, err)
	}
	return pub, nil
}

// KeyPairPath returns the location of the persisted daemon keypair under dir.
func KeyPairPath(dir string) string {
	return filepath.Join(dir, keyPairFileName)
}

// LoadOrCreateKeyPair reads the daemon's long-lived X25519 keypair from
// dir/relay-keypair.json, generating and persisting a new one (mode 0600) if
// the file is missing or cannot be parsed — ADR-0007 (g). Reusing the stored
// keypair across restarts is what makes an already-scanned pairing offer
// still valid after the daemon restarts.
func LoadOrCreateKeyPair(dir string) (*KeyPair, error) {
	path := KeyPairPath(dir)

	kp, err := loadKeyPair(path)
	if err == nil {
		return kp, nil
	}
	if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, errCorruptKeyPair) {
		return nil, err
	}

	kp, err = GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	if err := writeKeyPair(dir, path, kp); err != nil {
		return nil, err
	}
	return kp, nil
}

// errCorruptKeyPair marks an unreadable/undecodable keypair file, which
// LoadOrCreateKeyPair treats as "regenerate" rather than as a hard failure.
var errCorruptKeyPair = errors.New("e2ee: keypair file is corrupt")

func loadKeyPair(path string) (*KeyPair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("read keypair %s: %w", path, err)
	}

	var stored persistedKeyPair
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %s", errCorruptKeyPair, path, err)
	}
	if stored.V != keyPairFileVersion {
		return nil, fmt.Errorf("%w: %s has version %d, want %d", errCorruptKeyPair, path, stored.V, keyPairFileVersion)
	}
	secret, err := base64.StdEncoding.DecodeString(stored.SecretKeyB64)
	if err != nil {
		return nil, fmt.Errorf("%w: decode secret key in %s: %s", errCorruptKeyPair, path, err)
	}
	priv, err := ecdh.X25519().NewPrivateKey(secret)
	if err != nil {
		return nil, fmt.Errorf("%w: secret key in %s: %s", errCorruptKeyPair, path, err)
	}
	kp := &KeyPair{priv: priv}
	if stored.PublicKeyB64 != base64.StdEncoding.EncodeToString(kp.Public()) {
		return nil, fmt.Errorf("%w: public key in %s does not match its secret key", errCorruptKeyPair, path)
	}

	// Not covered by ADR-0007: an existing file with looser permissions is
	// tightened back to 0600 rather than failing the daemon's startup.
	if err := tightenKeyPairPerms(path); err != nil {
		return nil, err
	}
	return kp, nil
}

func writeKeyPair(dir, path string, kp *KeyPair) error {
	stored := persistedKeyPair{
		V:            keyPairFileVersion,
		PublicKeyB64: base64.StdEncoding.EncodeToString(kp.Public()),
		SecretKeyB64: base64.StdEncoding.EncodeToString(kp.private().Bytes()),
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("encode keypair: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write keypair %s: %w", path, err)
	}
	return nil
}

func tightenKeyPairPerms(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat keypair %s: %w", path, err)
	}
	if info.Mode().Perm()&^fs.FileMode(0o600) == 0 {
		return nil
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod keypair %s: %w", path, err)
	}
	return nil
}
