package client_test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/spacingmind/smind/internal/relay/client"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/relay/relaytest"
	"github.com/spacingmind/smind/internal/relay/server"
)

// tlsServer serves a gRPC server with no services registered over cert, so a
// call that gets through TLS is answered Unimplemented and one that does not
// is Unavailable with the certificate error.
func tlsServer(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})))
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	return lis.Addr().String()
}

// call makes one RPC over a fresh connection and reports its status code and
// message: Unimplemented means the TLS handshake (and so the trust decision)
// succeeded.
func call(t *testing.T, target, fingerprint string, opts ...client.DialOption) (codes.Code, string) {
	t.Helper()
	conn, err := client.Dial(target, fingerprint, opts...)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = relaypb.NewRelayClient(conn).AdmitChallenge(ctx, &relaypb.AdmitChallengeRequest{})
	st, _ := status.FromError(err)
	return st.Code(), st.Message()
}

func fingerprintOf(cert tls.Certificate) string {
	sum := sha256.Sum256(cert.Certificate[0])
	return hex.EncodeToString(sum[:])
}

func TestDialEmptyFingerprintUsesSystemRoots(t *testing.T) {
	ca, err := relaytest.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	loopback := []net.IP{net.ParseIP("127.0.0.1")}
	good, _, _, err := ca.Issue(relaytest.LeafOptions{DNSNames: []string{"localhost"}, IPs: loopback})
	if err != nil {
		t.Fatal(err)
	}
	addr := tlsServer(t, good)

	if code, msg := call(t, addr, "", client.WithRootCAs(ca.Pool)); code != codes.Unimplemented {
		t.Fatalf("CA-signed cert, trusted root: %v %q, want Unimplemented (handshake ok)", code, msg)
	}

	// Without the test root the same certificate is unknown authority: the
	// default really is the system roots, and nothing is skipped.
	code, msg := call(t, addr, "")
	if code != codes.Unavailable || !strings.Contains(msg, "certificate") {
		t.Fatalf("CA-signed cert, system roots: %v %q, want a certificate error", code, msg)
	}

	// The host name is verified: the same server reached as localhost passes,
	// a certificate for another name does not.
	port := addr[strings.LastIndex(addr, ":")+1:]
	if code, msg := call(t, "localhost:"+port, "", client.WithRootCAs(ca.Pool)); code != codes.Unimplemented {
		t.Fatalf("name in the certificate: %v %q, want Unimplemented", code, msg)
	}
	wrongName, _, _, err := ca.Issue(relaytest.LeafOptions{DNSNames: []string{"relay.example.test"}, IPs: nil})
	if err != nil {
		t.Fatal(err)
	}
	code, msg = call(t, tlsServer(t, wrongName), "", client.WithRootCAs(ca.Pool))
	if code != codes.Unavailable || !strings.Contains(msg, "not valid for") && !strings.Contains(msg, "doesn't contain any IP SANs") {
		t.Fatalf("hostname mismatch: %v %q, want a name error", code, msg)
	}

	// An expired certificate is refused.
	expired, _, _, err := ca.Issue(relaytest.LeafOptions{
		IPs: loopback, NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: time.Now().Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	code, msg = call(t, tlsServer(t, expired), "", client.WithRootCAs(ca.Pool))
	if code != codes.Unavailable || !strings.Contains(msg, "expired") {
		t.Fatalf("expired certificate: %v %q, want an expiry error", code, msg)
	}
}

func TestDialNonEmptyFingerprintPinsExactly(t *testing.T) {
	// The relay's own self-signed certificate (ADR-0011): no CA vouches for it.
	selfSigned, err := server.LoadOrCreateCert(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	addr := tlsServer(t, selfSigned)
	if code, msg := call(t, addr, fingerprintOf(selfSigned)); code != codes.Unimplemented {
		t.Fatalf("pinned self-signed: %v %q, want Unimplemented", code, msg)
	}
	// ...and with an empty fingerprint it is rightly refused.
	if code, _ := call(t, addr, ""); code != codes.Unavailable {
		t.Fatalf("self-signed in CA mode: %v, want Unavailable", code)
	}
	// A different pin does not match.
	other, err := server.LoadOrCreateCert(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if code, msg := call(t, addr, fingerprintOf(other)); code != codes.Unavailable || !strings.Contains(msg, "does not match pin") {
		t.Fatalf("wrong pin: %v %q, want a pin mismatch", code, msg)
	}

	// A CA-signed certificate is pinned by its leaf: the leaf's fingerprint
	// passes (even for a name it is not valid for -- the pin is the trust
	// decision), the CA certificate's does not, and a root pool changes nothing.
	ca, err := relaytest.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	leaf, _, _, err := ca.Issue(relaytest.LeafOptions{DNSNames: []string{"relay.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	laddr := tlsServer(t, leaf)
	if code, msg := call(t, laddr, fingerprintOf(leaf)); code != codes.Unimplemented {
		t.Fatalf("pinned CA leaf: %v %q, want Unimplemented", code, msg)
	}
	caSum := sha256.Sum256(ca.Cert.Raw)
	if code, _ := call(t, laddr, hex.EncodeToString(caSum[:]), client.WithRootCAs(ca.Pool)); code != codes.Unavailable {
		t.Fatalf("pinning the CA certificate instead of the leaf: %v, want Unavailable", code)
	}
}

func TestDialRejectsAMalformedFingerprintInsteadOfFallingBackToCAMode(t *testing.T) {
	for _, fp := range []string{"nothex", "abcd", strings.Repeat("0", 63)} {
		if conn, err := client.Dial("127.0.0.1:1", fp); err == nil {
			conn.Close()
			t.Errorf("Dial with fingerprint %q succeeded", fp)
		}
	}
}
