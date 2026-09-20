package bridge

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
)

// FetchFingerprint dials address and returns the hex-SHA-256 fingerprint of
// the TLS certificate it presents -- a trust-on-first-use capture, used
// only by `smind relay connect` at pairing-setup time. Every dial
// thereafter (internal/relay/client.Dial) pins this exact fingerprint
// instead of trusting any CA, so this is the one moment a MITM on the
// connect step could substitute a different relay; that's the same
// trust-on-first-use tradeoff ssh host keys and paseo's own daemon pairing
// already make, not a new weakening introduced here.
func FetchFingerprint(address string) (string, error) {
	conn, err := tls.Dial("tcp", address, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // TOFU capture, see doc comment.
	if err != nil {
		return "", fmt.Errorf("bridge: dial %s for fingerprint: %w", address, err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", fmt.Errorf("bridge: %s presented no certificate", address)
	}
	sum := sha256.Sum256(certs[0].Raw)
	return hex.EncodeToString(sum[:]), nil
}
