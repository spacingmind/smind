package server

// cert.go — the relay's self-signed TLS certificate (ADR-0011): generated
// on first run, persisted under the relay data directory so clients can
// pin its fingerprint across restarts (same persistence spirit as the
// e2ee daemon keypair, ADR-0007 (g)).

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	certFile = "cert.pem"
	keyFile  = "key.pem"
)

// LoadOrCreateCert returns the relay's TLS certificate, generating and
// persisting it on first run. The fingerprint it exposes is what a daemon
// pins at pairing time.
func LoadOrCreateCert(dir string) (tls.Certificate, error) {
	certPath := filepath.Join(dir, certFile)
	keyPath := filepath.Join(dir, keyFile)

	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return cert, nil
	} else if !os.IsNotExist(err) {
		return tls.Certificate{}, fmt.Errorf("relay cert: load: %w", err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, fmt.Errorf("relay cert: mkdir: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("relay cert: key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("relay cert: serial: %w", err)
	}
	host, _ := os.Hostname()
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "smind-relay"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		// CertSign is required for a self-signed certificate to act as its own
		// trust anchor when a client adds it to a root pool (as pinning
		// clients effectively do).
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		// Self-signed certs used directly in a client root pool must be
		// valid CAs (IsCA + BasicConstraintsValid) or verification fails
		// with "cannot sign this kind of certificate" — this is the
		// pinned-trust shape chisel/rathole use too.
		IsCA:                  true,
		BasicConstraintsValid: true,
		// SANs cover the common self-hosted addresses; daemons pin the
		// fingerprint anyway (ADR-0011), so these are for standard TLS
		// clients, not the trust mechanism.
		DNSNames:    []string{"smind-relay", "localhost", host},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("relay cert: create: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("relay cert: marshal key: %w", err)
	}

	if err := writeFile0600(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		return tls.Certificate{}, fmt.Errorf("relay cert: write cert: %w", err)
	}
	if err := writeFile0600(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})); err != nil {
		return tls.Certificate{}, fmt.Errorf("relay cert: write key: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// CertFingerprint returns the SHA-256 of the certificate's DER encoding,
// hex-encoded — the value a daemon pins at pairing time (ADR-0011).
func CertFingerprint(cert tls.Certificate) string {
	if len(cert.Certificate) == 0 {
		return ""
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return hex.EncodeToString(sum[:])
}

func writeFile0600(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
