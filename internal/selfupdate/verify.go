package selfupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// UpdaterPublicKey is the minisign public key release checksums are signed
// with, in the form Tauri stores it: the base64 of the two-line public key
// file. It is the desktop updater's key (ADR-0023 §2), so there is one key and
// one rotation story; TestEmbeddedPubkeyMatchesTauriConf fails if it drifts
// from `plugins.updater.pubkey` in desktop/src-tauri/tauri.conf.json.
//
// It is a var only so a test can stamp a throwaway key into a test-built
// binary with -ldflags -X; release builds never do, and there is deliberately
// no flag or environment variable that replaces it at run time.
var UpdaterPublicKey = "dW50cnVzdGVkIGNvbW1lbnQ6IG1pbmlzaWduIHB1YmxpYyBrZXk6IEUwRjgxQzdFOThCNUJGRjQKUldUMHY3V1lmaHo0NEFPTDAxTkdrTjlZbUV6cjFaVDJTRHpmb3ZVTURrakJmdmgyTEZKQmkreDEK"

// SignatureName is the release asset holding the minisign signature of
// ChecksumsName.
const SignatureName = ChecksumsName + ".minisig"

const (
	// algPrehashed is the signature over BLAKE2b-512 of the file, what minisign
	// and `tauri signer sign` emit. The legacy "Ed" (signature over the file
	// itself) is refused: the algorithm byte is not covered by any signature,
	// so accepting both would let a prehashed signature be relabelled and
	// verified over the 64 digest bytes. (Public key files still say "Ed": that
	// is the key's algorithm, a different field.)
	algPrehashed = "ED"
	keyAlg       = "Ed"
	maxSigSize   = 4 << 10
)

// ErrNoSignature means the release has no checksums.txt.minisig: it was
// published before signing existed (or by someone without the key). There is
// no checksum-only fallback.
var ErrNoSignature = errors.New("release has no signature")

// ErrBadSignature means a signature is present but does not verify against the
// embedded key for these bytes.
var ErrBadSignature = errors.New("signature does not verify")

// PublicKey is a parsed minisign public key.
type PublicKey struct {
	keyID [8]byte
	key   ed25519.PublicKey
}

// ParsePublicKey parses a key in Tauri's stored form (base64 of the public key
// file: an "untrusted comment:" line, then the base64 key line).
func ParsePublicKey(tauriForm string) (PublicKey, error) {
	text, err := base64.StdEncoding.DecodeString(strings.TrimSpace(tauriForm))
	if err != nil {
		return PublicKey{}, fmt.Errorf("public key is not base64: %w", err)
	}
	lines := splitLines(string(text))
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "untrusted comment:") {
		return PublicKey{}, errors.New("public key is not a minisign public key file")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(raw) != 2+8+ed25519.PublicKeySize || string(raw[:2]) != keyAlg {
		return PublicKey{}, errors.New("malformed minisign public key")
	}
	var pk PublicKey
	copy(pk.keyID[:], raw[2:10])
	pk.key = ed25519.PublicKey(append([]byte(nil), raw[10:]...))
	return pk, nil
}

// Signature is the verified content of a minisign signature file.
type Signature struct {
	// TrustedComment is the signed free-text comment, e.g.
	// "timestamp:1791994870\tfile:checksums.txt\tversion:9.9.9". Unlike the
	// "untrusted comment" it is covered by the global signature.
	TrustedComment string
}

// VerifySignature checks that sigFile (a minisign signature file, text) is a
// valid signature of data under pub, including the global signature over the
// trusted comment. Only the prehashed "ED" algorithm is accepted. Any
// malformation is an error: this fails closed.
func VerifySignature(pub PublicKey, data, sigFile []byte) (Signature, error) {
	if len(sigFile) > maxSigSize {
		return Signature{}, fmt.Errorf("%w: signature file is %d bytes", ErrBadSignature, len(sigFile))
	}
	lines := splitLines(string(sigFile))
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "untrusted comment:") || !strings.HasPrefix(lines[2], "trusted comment:") {
		return Signature{}, fmt.Errorf("%w: not a minisign signature file", ErrBadSignature)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(raw) != 2+8+ed25519.SignatureSize {
		return Signature{}, fmt.Errorf("%w: malformed signature line", ErrBadSignature)
	}
	global, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || len(global) != ed25519.SignatureSize {
		return Signature{}, fmt.Errorf("%w: malformed global signature line", ErrBadSignature)
	}
	alg, keyID, sig := string(raw[:2]), raw[2:10], raw[10:]
	if !bytes.Equal(keyID, pub.keyID[:]) {
		return Signature{}, fmt.Errorf("%w: signed by key %X, not the smind release key %X", ErrBadSignature, keyID, pub.keyID[:])
	}

	if alg != algPrehashed {
		return Signature{}, fmt.Errorf("%w: unsupported signature algorithm %q (want prehashed %q)", ErrBadSignature, alg, algPrehashed)
	}
	sum := blake2b.Sum512(data)
	if !ed25519.Verify(pub.key, sum[:], sig) {
		return Signature{}, fmt.Errorf("%w: the data does not match its signature", ErrBadSignature)
	}
	comment := strings.TrimPrefix(lines[2], "trusted comment:")
	comment = strings.TrimPrefix(comment, " ")
	if !ed25519.Verify(pub.key, append(append([]byte(nil), sig...), comment...), global) {
		return Signature{}, fmt.Errorf("%w: the trusted comment was altered", ErrBadSignature)
	}
	return Signature{TrustedComment: comment}, nil
}

// SignatureVerifier returns the ChecksumVerifier `smind update` installs: it
// fetches checksums.txt.minisig, verifies it over the downloaded checksums.txt
// with the given key (a Tauri-form public key, normally UpdaterPublicKey), and
// requires the signed trusted comment to name this file and this exact
// version, so a validly signed checksums.txt from another release cannot be
// replayed as the target's. Every failure aborts the update; a release without
// a signature is refused, never downgraded to checksum-only.
func SignatureVerifier(tauriPublicKey string) ChecksumVerifier {
	return func(ctx context.Context, version string, checksums []byte, fetch func(context.Context, string) ([]byte, error)) error {
		pub, err := ParsePublicKey(tauriPublicKey)
		if err != nil {
			return fmt.Errorf("embedded release key: %w", err)
		}
		sigFile, err := fetch(ctx, SignatureName)
		if err != nil {
			if errors.Is(err, ErrAssetNotFound) {
				return fmt.Errorf("%w: v%s has no %s (releases published before signing was added cannot be updated to; install it with the install script instead)", ErrNoSignature, version, SignatureName)
			}
			return err
		}
		sig, err := VerifySignature(pub, checksums, sigFile)
		if err != nil {
			return err
		}
		return checkTrustedComment(sig.TrustedComment, ChecksumsName, version)
	}
}

// checkTrustedComment requires tab-separated fields "file:<name>" and
// "version:<version>" matching exactly (`tauri signer sign --app-version`).
func checkTrustedComment(comment, file, version string) error {
	var gotFile, gotVersion string
	var nFile, nVersion int
	for _, f := range strings.Split(comment, "\t") {
		if v, ok := strings.CutPrefix(f, "file:"); ok {
			gotFile, nFile = v, nFile+1
		}
		if v, ok := strings.CutPrefix(f, "version:"); ok {
			gotVersion, nVersion = v, nVersion+1
		}
	}
	if nFile > 1 || nVersion > 1 {
		return fmt.Errorf("%w: the signed comment repeats its file or version field", ErrBadSignature)
	}
	if gotFile != file {
		return fmt.Errorf("%w: signature is for file %q, not %s", ErrBadSignature, gotFile, file)
	}
	if gotVersion != version {
		return fmt.Errorf("%w: signature is for version %q, not %s (a replayed release?)", ErrBadSignature, gotVersion, version)
	}
	return nil
}

// splitLines splits a minisign file into lines, dropping only line endings
// (LF, CRLF): the trusted comment is signed byte for byte, so its own
// whitespace must survive. Base64 lines are trimmed where they are decoded.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}
