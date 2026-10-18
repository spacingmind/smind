package bridge

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"
)

// probeTimeout bounds one dial-and-handshake of a TLSProbe.
const probeTimeout = 10 * time.Second

// ErrNotVerified wraps the reason a relay's certificate did not verify
// against the trusted roots (unknown authority, wrong host name, expired):
// the one failure `smind relay connect` answers by showing the fingerprint
// and asking, instead of giving up. Anything else (nothing listening, a
// handshake that is not about the certificate) is not wrapped and is fatal.
var ErrNotVerified = errors.New("certificate not verified")

// notVerified carries the verifier's own words ("x509: certificate has
// expired ...") and matches ErrNotVerified.
type notVerified struct{ cause error }

func (e notVerified) Error() string        { return e.cause.Error() }
func (e notVerified) Unwrap() error        { return e.cause }
func (e notVerified) Is(target error) bool { return target == ErrNotVerified }

// TLSProbe looks at a relay's TLS certificate at `smind relay connect` time.
// The zero value verifies against the system roots, like the daemon's own
// dial in CA mode (internal/relay/client.Dial with an empty fingerprint).
type TLSProbe struct {
	// Roots replaces the system roots; for tests with a private CA.
	Roots *x509.CertPool
}

func (p TLSProbe) handshake(address string, cfg *tls.Config) (*tls.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	d := tls.Dialer{NetDialer: &net.Dialer{}, Config: cfg}
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	return conn.(*tls.Conn), nil
}

// Verify reports whether address presents a certificate that verifies for
// its host name against the trusted roots -- the condition for storing an
// empty fingerprint (CA mode). A certificate that does not verify returns an
// error wrapping ErrNotVerified; every other failure is returned as is.
func (p TLSProbe) Verify(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("bridge: relay address %q: %w", address, err)
	}
	conn, err := p.handshake(address, &tls.Config{ServerName: host, RootCAs: p.Roots, MinVersion: tls.VersionTLS13})
	if err != nil {
		var cve *tls.CertificateVerificationError
		if errors.As(err, &cve) {
			return notVerified{cve.Err}
		}
		return fmt.Errorf("bridge: dial %s: %w", address, err)
	}
	return conn.Close()
}

// Fingerprint dials address WITHOUT verifying its certificate and returns the
// hex SHA-256 fingerprint of the leaf it presents. The result is only ever
// shown to a person to compare with the one the relay printed, or checked
// against a fingerprint they supplied (`smind relay connect --fingerprint`);
// it is never stored on its own, so a man in the middle at this moment cannot
// be silently trusted.
func (p TLSProbe) Fingerprint(address string) (string, error) {
	conn, err := p.handshake(address, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}) //nolint:gosec // see doc comment: shown for confirmation, not trusted.
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

// FetchFingerprint is TLSProbe{}.Fingerprint.
func FetchFingerprint(address string) (string, error) {
	return TLSProbe{}.Fingerprint(address)
}
