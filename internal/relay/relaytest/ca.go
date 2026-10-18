package relaytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// CA is a throwaway certificate authority for tests of CA-verified TLS
// (ADR-0031 P12c): it issues relay certificates a client trusts through
// Pool, the stand-in for the system roots.
type CA struct {
	Cert *x509.Certificate
	Key  *ecdsa.PrivateKey
	Pool *x509.CertPool
}

// NewCA creates a CA valid for a day.
func NewCA() (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "smind test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &CA{Cert: cert, Key: key, Pool: pool}, nil
}

// LeafOptions describes a certificate issued by CA.Issue. Zero validity
// times mean "valid now, for an hour".
type LeafOptions struct {
	DNSNames  []string
	IPs       []net.IP
	NotBefore time.Time
	NotAfter  time.Time
}

// Issue signs a server certificate. The returned PEM pair is what
// `smind relay --tls-cert/--tls-key` reads.
func (ca *CA) Issue(o LeafOptions) (cert tls.Certificate, certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, nil, err
	}
	if o.NotBefore.IsZero() {
		o.NotBefore = time.Now().Add(-time.Minute)
	}
	if o.NotAfter.IsZero() {
		o.NotAfter = time.Now().Add(time.Hour)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "smind test relay"},
		NotBefore:    o.NotBefore,
		NotAfter:     o.NotAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     o.DNSNames,
		IPAddresses:  o.IPs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return tls.Certificate{}, nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err = tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("relaytest: issued certificate unusable: %w", err)
	}
	return cert, certPEM, keyPEM, nil
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		panic(err)
	}
	return n
}
