package server

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// installCert copies the self-signed pair generated in a fresh dir into
// dst/{cert,key}.pem and returns its leaf DER.
func installCert(t *testing.T, dst string) []byte {
	t.Helper()
	src := t.TempDir()
	cert, err := LoadOrCreateCert(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{certFile, keyFile} {
		data, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, f), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return cert.Certificate[0]
}

func current(t *testing.T, c *certSource) []byte {
	t.Helper()
	cert, err := c.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	return cert.Certificate[0]
}

// TestCertSourceReloadsRenewedCertificate: a Let's Encrypt renewal rewrites the
// files; the relay must serve the new certificate without a restart, and a
// renewal caught half written must not take it down.
func TestCertSourceReloadsRenewedCertificate(t *testing.T) {
	dir := t.TempDir()
	first := installCert(t, dir)
	cp, kp := filepath.Join(dir, certFile), filepath.Join(dir, keyFile)
	src, err := newCertSource(cp, kp)
	if err != nil {
		t.Fatal(err)
	}
	expire := func() { src.checked = time.Now().Add(-time.Hour) } // let the next call re-stat
	bump := func() {                                              // distinct mtimes even on coarse filesystems
		future := time.Now().Add(time.Minute)
		_ = os.Chtimes(cp, future, future)
		_ = os.Chtimes(kp, future, future)
	}

	if string(current(t, src)) != string(first) {
		t.Fatal("did not start with the certificate on disk")
	}

	second := installCert(t, dir)
	bump()
	if string(current(t, src)) != string(first) {
		t.Fatal("re-statted before the recheck interval")
	}
	expire()
	if got := current(t, src); string(got) != string(second) {
		t.Fatal("renewed certificate was not picked up")
	}

	// Half-written renewal: the key no longer matches the certificate.
	if err := os.WriteFile(kp, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Minute)
	_ = os.Chtimes(kp, future, future)
	expire()
	if got := current(t, src); string(got) != string(second) {
		t.Fatal("a failed reload dropped the last good certificate")
	}

	// And it recovers once the files are whole again.
	third := installCert(t, dir)
	future = time.Now().Add(3 * time.Minute)
	_ = os.Chtimes(cp, future, future)
	_ = os.Chtimes(kp, future, future)
	expire()
	if got := current(t, src); string(got) != string(third) {
		t.Fatal("did not recover after the files were repaired")
	}
}

func TestNewCertSourceRefusesAMissingOrMismatchedPair(t *testing.T) {
	dir := t.TempDir()
	if _, err := newCertSource(filepath.Join(dir, "no.pem"), filepath.Join(dir, "no.key")); err == nil {
		t.Fatal("missing files accepted")
	}
	installCert(t, dir)
	other := t.TempDir()
	installCert(t, other)
	if _, err := newCertSource(filepath.Join(dir, certFile), filepath.Join(other, keyFile)); err == nil {
		t.Fatal("a certificate with someone else's key was accepted")
	}
}
