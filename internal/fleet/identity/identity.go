// Package identity holds the daemon's long-lived host key (ADR-0048 F1a).
// A host is an Ed25519 key stored as its 32-byte seed in
// $SMIND_HOME/fleet/host-key; the host id is a hash of the public key, so it
// is self-authenticating: nobody can claim an id without the key. The id
// derivation is deliberately the same as the relay's device id, so a host id
// reads like the device ids users already know, but it is re-implemented here
// so the fleet layer does not depend on the relay package.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spacingmind/smind/internal/securefile"
)

// KeyFile is the host key file's name inside the identity directory.
const KeyFile = "host-key"

var (
	ErrNotFound = errors.New("identity: host key not found")
	ErrCorrupt  = errors.New("identity: host key file is corrupt")
	ErrInsecure = errors.New("identity: host key file is readable by others")
)

const (
	// hostIDLen is 26 base32 chars (130 bits), like a relay device id.
	hostIDLen = 26
	// keyPrefix names the on-disk format: a seed line, base64 RawURL.
	keyPrefix = "smind-host-key-v1"
	// maxKeyBytes bounds the file before parsing, so a stray large file is
	// refused as corrupt without being read into a parser.
	maxKeyBytes = 512
)

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// HostID derives the stable host id: lowercase unpadded base32 of
// SHA-256(pub), truncated to 26 characters.
func HostID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return strings.ToLower(idEncoding.EncodeToString(sum[:]))[:hostIDLen]
}

// Key is a loaded host identity. The zero Key is not usable; Load or
// LoadOrCreate build one.
type Key struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	id   string
}

// Public returns a copy of the host public key.
func (k *Key) Public() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), k.pub...)
}

// HostID returns the host id derived from the public key.
func (k *Key) HostID() string { return k.id }

// Sign signs msg with the host key.
func (k *Key) Sign(msg []byte) []byte { return ed25519.Sign(k.priv, msg) }

// Verify checks a host signature. A wrong-length pub can only be a caller
// bug, and reports false rather than panicking like ed25519.Verify.
func Verify(pub ed25519.PublicKey, msg, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pub, msg, sig)
}

func keyFromSeed(seed []byte) *Key {
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return &Key{pub: pub, priv: priv, id: HostID(pub)}
}

// Load reads the host key from dir. A missing file is ErrNotFound; a file
// with loosened permissions is ErrInsecure and is never tightened back --
// someone broadened it on purpose or by accident, and silently hiding that
// would hand the same key to a second reader -- and anything unreadable as
// the one-line seed format is ErrCorrupt. No error path rewrites the file.
func Load(dir string) (*Key, error) {
	path := filepath.Join(dir, KeyFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("identity: read %s: %w", path, err)
	}
	private, err := securefile.IsPrivate(path)
	if err != nil {
		return nil, fmt.Errorf("identity: check %s: %w", path, err)
	}
	if !private {
		return nil, ErrInsecure
	}
	if len(data) > maxKeyBytes {
		return nil, fmt.Errorf("%w: %d bytes, max %d", ErrCorrupt, len(data), maxKeyBytes)
	}
	body, ok := strings.CutPrefix(string(data), keyPrefix+" ")
	if !ok {
		return nil, fmt.Errorf("%w: missing %q prefix", ErrCorrupt, keyPrefix)
	}
	seed, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("%w: bad base64 seed: %v", ErrCorrupt, err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%w: seed is %d bytes, want %d", ErrCorrupt, len(seed), ed25519.SeedSize)
	}
	return keyFromSeed(seed), nil
}

// createMu serialises creation inside this process so concurrent callers
// converge on one identity instead of racing on the file (the O_EXCL reload
// below only helps against other processes).
var createMu sync.Mutex

// LoadOrCreate returns the host key from dir, creating it on first use.
// Creation is the only write: every other Load error (corrupt, insecure,
// I/O) is returned unchanged and the file on disk is never rewritten or
// regenerated -- a corrupt key is a human-visible incident, not something to
// paper over with a fresh identity.
func LoadOrCreate(dir string) (*Key, error) {
	createMu.Lock()
	defer createMu.Unlock()

	k, err := Load(dir)
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("identity: create %s: %w", dir, err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("identity: generate host key: %w", err)
	}
	path := filepath.Join(dir, KeyFile)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			// Another process won the race; adopt its key, never ours.
			return Load(dir)
		}
		return nil, fmt.Errorf("identity: create %s: %w", path, err)
	}
	line := keyPrefix + " " + base64.RawURLEncoding.EncodeToString(priv.Seed()) + "\n"
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return nil, fmt.Errorf("identity: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("identity: close %s: %w", path, err)
	}
	if err := securefile.Restrict(path); err != nil {
		return nil, fmt.Errorf("identity: restrict %s: %w", path, err)
	}
	return &Key{pub: pub, priv: priv, id: HostID(pub)}, nil
}
