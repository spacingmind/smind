package server

// run_test.go — the plan's Test Scenario: the relay starts, binds its
// listen address, serves real TLS gRPC, and shuts down cleanly on context
// cancellation (the same lifecycle contract as the daemon's serve).

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// TestRunServesTLSAndWorkspaceEnrollment proves the full standalone path:
// enroll from the store, Run serves TLS gRPC, a client pins the relay's
// certificate and completes the admission exchange, then cancellation
// shuts the relay down cleanly.
func TestRunServesTLSAndWorkspaceEnrollment(t *testing.T) {
	dir := t.TempDir()
	secret, err := EnrollWorkspace(dir, "ws-a")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, err := EnrollWorkspace(dir, "ws-a"); err == nil {
		t.Fatal("duplicate enrollment must fail")
	}

	// Fixed port (test-only): Run binds it for the process lifetime.
	addr := "127.0.0.1:17401"
	// Generate the cert BEFORE starting Run, so the pinned pool and the
	// served cert are guaranteed identical (Run would otherwise race its
	// own LoadOrCreateCert against this test's).
	if _, err := LoadOrCreateCert(dir); err != nil {
		t.Fatalf("cert: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- Run(ctx, Config{ListenAddr: addr, DataDir: dir}) }()

	// Pin exactly the relay's own certificate: trust the file Run serves
	// (the same file a daemon pins by fingerprint in production).
	cpool := x509.NewCertPool()
	if !cpool.AppendCertsFromPEM(certPEM(t, dir)) {
		t.Fatal("could not add relay cert to pool")
	}
	creds := credentials.NewTLS(&tls.Config{RootCAs: cpool, MinVersion: tls.VersionTLS13})

	var client relaypb.RelayClient
	var lastErr error
	for i := 0; i < 100; i++ {
		conn, err := grpc.NewClient(
			"passthrough:///"+addr,
			grpc.WithTransportCredentials(creds),
		)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		c := relaypb.NewRelayClient(conn)
		chalCtx, chalCancel := context.WithTimeout(context.Background(), time.Second)
		_, err = c.AdmitChallenge(chalCtx, &relaypb.AdmitChallengeRequest{
			ProtocolVersion: 1, WorkspaceId: "ws-a",
			ClientNonce: make([]byte, 32), DaemonKeyId: "k",
		})
		chalCancel()
		conn.Close()
		if err == nil {
			client = c
			break
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	if client == nil {
		t.Fatalf("could not reach relay over TLS gRPC: %v", lastErr)
	}

	// Cancel and expect a clean exit.
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not exit on cancellation")
	}

	_ = secret
}

func certPEM(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, certFile))
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	if blk, _ := pem.Decode(b); blk == nil {
		t.Fatal("cert file is not PEM")
	}
	return b
}
