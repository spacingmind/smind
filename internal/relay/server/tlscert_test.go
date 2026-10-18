package server_test

import (
	"context"
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/relay/admission"
	"github.com/spacingmind/smind/internal/relay/client"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/relay/relaytest"
	"github.com/spacingmind/smind/internal/relay/server"
)

func writePEM(t *testing.T, dir string, certPEM, keyPEM []byte) (certPath, keyPath string) {
	t.Helper()
	certPath, keyPath = filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return lis
}

// TestRunServesTheProvidedCertificate: with --tls-cert/--tls-key the relay
// serves that CA-issued certificate on both listeners, a client verifies it
// with no fingerprint (CA mode), and no self-signed certificate is generated.
func TestRunServesTheProvidedCertificate(t *testing.T) {
	ca, err := relaytest.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	leaf, certPEM, keyPEM, err := ca.Issue(relaytest.LeafOptions{IPs: []net.IP{net.ParseIP("127.0.0.1")}})
	if err != nil {
		t.Fatal(err)
	}
	dataDir, certDir := t.TempDir(), t.TempDir()
	certPath, keyPath := writePEM(t, certDir, certPEM, keyPEM)
	if _, err := server.EnrollHost(dataDir, "homelab"); err != nil {
		t.Fatal(err)
	}

	native, web := listen(t), listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- server.Run(ctx, server.Config{
			DataDir: dataDir, Listener: native, GRPCWebListener: web, TLSCertFile: certPath, TLSKeyFile: keyPath,
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-runDone:
		case <-time.After(10 * time.Second):
			t.Error("relay did not stop")
		}
	})

	admit := func(fingerprint string, opts ...client.DialOption) error {
		conn, err := client.Dial(native.Addr().String(), fingerprint, opts...)
		if err != nil {
			return err
		}
		defer conn.Close()
		deadline := time.Now().Add(5 * time.Second)
		for {
			cctx, ccancel := context.WithTimeout(context.Background(), time.Second)
			_, err = relaypb.NewRelayClient(conn).AdmitChallenge(cctx, &relaypb.AdmitChallengeRequest{
				ProtocolVersion: admission.ProtocolVersion, WorkspaceId: "homelab", ClientNonce: make([]byte, admission.NonceSize), DaemonKeyId: "k",
			})
			ccancel()
			if err == nil || time.Now().After(deadline) {
				return err
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if err := admit("", client.WithRootCAs(ca.Pool)); err != nil {
		t.Fatalf("CA-mode client against the provided certificate: %v", err)
	}
	if err := admit(server.CertFingerprint(leaf)); err != nil {
		t.Fatalf("pinning the provided certificate's leaf: %v", err)
	}
	// Not trusted by the system roots, so a default CA-mode client is refused.
	if err := admit(""); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("CA-mode client without the test CA: %v, want a certificate error", err)
	}

	// grpc-web serves the same certificate.
	conn, err := tls.Dial("tcp", web.Addr().String(), &tls.Config{RootCAs: ca.Pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatalf("grpc-web listener does not serve the provided certificate: %v", err)
	}
	conn.Close()

	if _, err := os.Stat(filepath.Join(dataDir, "cert.pem")); !os.IsNotExist(err) {
		t.Fatalf("a self-signed certificate was generated next to the provided one (stat err = %v)", err)
	}
}

func TestRunRefusesAnIncompleteOrBrokenCertificateConfig(t *testing.T) {
	ca, err := relaytest.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	_, certPEM, keyPEM, err := ca.Issue(relaytest.LeafOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath := writePEM(t, dir, certPEM, keyPEM)
	_, _, otherKey, _ := ca.Issue(relaytest.LeafOptions{})
	otherKeyPath := filepath.Join(dir, "other.key")
	if err := os.WriteFile(otherKeyPath, otherKey, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, cfg := range map[string]server.Config{
		"cert without key": {TLSCertFile: certPath},
		"key without cert": {TLSKeyFile: keyPath},
		"mismatched pair":  {TLSCertFile: certPath, TLSKeyFile: otherKeyPath},
		"missing files":    {TLSCertFile: filepath.Join(dir, "no.pem"), TLSKeyFile: filepath.Join(dir, "no.key")},
	} {
		cfg.DataDir = t.TempDir()
		cfg.Listener, cfg.GRPCWebListener = listen(t), listen(t)
		done := make(chan error, 1)
		go func() { done <- server.Run(context.Background(), cfg) }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: Run returned nil, want an error", name)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s: Run started instead of refusing", name)
		}
	}
}
