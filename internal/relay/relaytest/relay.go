package relaytest

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/relay/server"
)

// DefaultHostID is the relay host StartRelay enrols.
const DefaultHostID = "relay-host-1"

// Option configures StartRelay.
type Option func(*options)

type options struct {
	maxDevices int
}

// WithMaxDevicesPerHost sets the relay's per-host device cap
// (server.Config.MaxDevicesPerHost); the default is the server's own.
func WithMaxDevicesPerHost(n int) Option { return func(o *options) { o.maxDevices = n } }

// Relay is a running relay (the real server.Run) with one enrolled relay
// host. The fields are what a client needs to reach it.
type Relay struct {
	// HostID and Secret are the enrolled relay host and its raw admission
	// secret (never print the secret).
	HostID string
	Secret []byte
	// NativeAddr and WebAddr are host:port of the native-gRPC and grpc-web
	// listeners. They stay the same across Restart.
	NativeAddr, WebAddr string
	// Fingerprint is the hex SHA-256 of the relay's TLS certificate, the pin.
	Fingerprint string
	// DataDir is the relay's data directory (certificate, enrolments).
	DataDir string
	// Frames logs every data Frame sent or received by clients from
	// NativeClient / WebClient.
	Frames *FrameLog

	opts options

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan error
}

// StartRelay enrols one relay host in a fresh temp data dir and runs the real
// relay on ephemeral native and grpc-web ports. It is stopped when the test
// ends.
func StartRelay(t testing.TB, opts ...Option) *Relay {
	t.Helper()
	r := &Relay{HostID: DefaultHostID, DataDir: t.TempDir(), Frames: &FrameLog{}}
	for _, o := range opts {
		o(&r.opts)
	}
	secret, err := server.EnrollHost(r.DataDir, r.HostID)
	if err != nil {
		t.Fatalf("relaytest: enrol host: %v", err)
	}
	r.Secret = secret
	cert, err := server.LoadOrCreateCert(r.DataDir)
	if err != nil {
		t.Fatalf("relaytest: relay certificate: %v", err)
	}
	r.Fingerprint = server.CertFingerprint(cert)

	nativeLis := listen(t, "127.0.0.1:0")
	webLis := listen(t, "127.0.0.1:0")
	r.NativeAddr, r.WebAddr = nativeLis.Addr().String(), webLis.Addr().String()
	r.run(nativeLis, webLis)
	t.Cleanup(func() { r.Stop(t) })
	return r
}

func listen(t testing.TB, addr string) net.Listener {
	t.Helper()
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("relaytest: listen %s: %v", addr, err)
	}
	return lis
}

func (r *Relay) run(nativeLis, webLis net.Listener) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	cfg := server.Config{
		DataDir:           r.DataDir,
		Listener:          nativeLis,
		GRPCWebListener:   webLis,
		MaxDevicesPerHost: r.opts.maxDevices,
	}
	go func() { done <- server.Run(ctx, cfg) }()
	r.mu.Lock()
	r.cancel, r.done = cancel, done
	r.mu.Unlock()
}

// Stop stops the relay and waits for server.Run to return. It is idempotent.
// Streams still open delay a graceful stop, so close clients first (the
// harness's own cleanups already run before the relay's).
func (r *Relay) Stop(t testing.TB) {
	t.Helper()
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("relaytest: relay Run: %v", err)
		}
	case <-time.After(Timeout):
		t.Error("relaytest: relay did not stop")
	}
}

// Restart stops the relay and starts it again on the same data dir and the
// same addresses: the host enrolment, the TLS certificate (hence the pin) and
// the addresses survive, all in-memory state (admissions, routes, buffers,
// control streams) does not -- exactly what a relay process restart does.
func (r *Relay) Restart(t testing.TB) {
	t.Helper()
	r.Stop(t)
	var nativeLis, webLis net.Listener
	Eventually(t, "relay ports to be rebindable", func() bool {
		var err error
		if nativeLis == nil {
			if nativeLis, err = net.Listen("tcp", r.NativeAddr); err != nil {
				nativeLis = nil
				return false
			}
		}
		if webLis == nil {
			if webLis, err = net.Listen("tcp", r.WebAddr); err != nil {
				webLis = nil
				return false
			}
		}
		return true
	})
	r.run(nativeLis, webLis)
}

// pinnedTLS is the client TLS config pinning the relay certificate by
// fingerprint, the same check internal/relay/client.Dial applies (no CA
// chain, ADR-0011).
func pinnedTLS(fingerprint string) (*tls.Config, error) {
	fp, err := hex.DecodeString(fingerprint)
	if err != nil || len(fp) != sha256.Size {
		return nil, fmt.Errorf("relaytest: bad fingerprint")
	}
	return &tls.Config{
		InsecureSkipVerify: true, // replaced by the pin below
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			// Leaf only, like client.Dial: extra chain certs are unauthenticated.
			if len(rawCerts) > 0 {
				if sum := sha256.Sum256(rawCerts[0]); string(sum[:]) == string(fp) {
					return nil
				}
			}
			return fmt.Errorf("relaytest: certificate fingerprint does not match pin")
		},
		MinVersion: tls.VersionTLS13,
	}, nil
}
