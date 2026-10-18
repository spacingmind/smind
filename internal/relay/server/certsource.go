package server

// certsource.go -- the certificate a relay serves when the operator brings
// their own (`smind relay --tls-cert/--tls-key`, ADR-0031 P12c): typically a
// Let's Encrypt certificate that a client verifies against the system roots
// instead of pinning. Those are renewed every ~60 days by a tool that
// rewrites the files, so the source re-reads them when they change rather
// than serving the certificate the process started with until it expires.

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// certRecheckEvery bounds how often a handshake stats the certificate files.
const certRecheckEvery = 5 * time.Second

type certSource struct {
	certFile, keyFile string

	mu      sync.Mutex
	cert    *tls.Certificate
	modCert time.Time
	modKey  time.Time
	checked time.Time
}

// newCertSource loads the pair once; an unreadable or mismatched pair is an
// error the relay refuses to start with.
func newCertSource(certFile, keyFile string) (*certSource, error) {
	c := &certSource{certFile: certFile, keyFile: keyFile}
	if err := c.load(); err != nil {
		return nil, fmt.Errorf("relay: --tls-cert/--tls-key: %w", err)
	}
	return c, nil
}

func (c *certSource) load() error {
	cert, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		return err
	}
	ci, err := os.Stat(c.certFile)
	if err != nil {
		return err
	}
	ki, err := os.Stat(c.keyFile)
	if err != nil {
		return err
	}
	c.cert, c.modCert, c.modKey, c.checked = &cert, ci.ModTime(), ki.ModTime(), time.Now()
	return nil
}

// GetCertificate is the tls.Config callback: the current certificate, reloaded
// first if either file changed. A failed reload (a renewal caught half
// written) keeps serving the last good certificate and is retried on the next
// check.
func (c *certSource) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.checked) >= certRecheckEvery {
		c.checked = time.Now()
		ci, cerr := os.Stat(c.certFile)
		ki, kerr := os.Stat(c.keyFile)
		if cerr == nil && kerr == nil && (!ci.ModTime().Equal(c.modCert) || !ki.ModTime().Equal(c.modKey)) {
			if err := c.load(); err != nil {
				log.Printf("relay: reloading %s: %v (still serving the previous certificate)", c.certFile, err)
			} else {
				log.Printf("relay: reloaded certificate %s", c.certFile)
			}
		}
	}
	return c.cert, nil
}
