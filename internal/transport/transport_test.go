package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

// newTestServer returns an httptest TLS server plus a RootCAs pool that
// trusts its certificate, so tests can point a RoundTripper at it without
// InsecureSkipVerify. This exercises the same certificate-verification path
// production traffic uses, unlike skipping verification entirely.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *x509.CertPool) {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	return srv, pool
}

func TestRoundTripper_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		helloID  utls.ClientHelloID
		wantBody string
	}{
		{name: "chrome auto", helloID: utls.HelloChrome_Auto, wantBody: "hello from server"},
		{name: "firefox auto", helloID: utls.HelloFirefox_Auto, wantBody: "hello from server"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv, pool := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/ping" {
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.wantBody))
			})

			rt := New(tt.helloID, WithRootCAs(pool))
			client := &http.Client{Transport: rt}

			resp, err := client.Get(srv.URL + "/ping")
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}

			state, ok := rt.ConnectionState()
			if !ok {
				t.Fatal("ConnectionState() ok = false, want true after a completed handshake")
			}
			if !state.HandshakeComplete {
				t.Error("ConnectionState().HandshakeComplete = false, want true")
			}
		})
	}
}

func TestRoundTripper_UntrustedCert(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// No WithRootCAs: the server's self-signed cert isn't in the system
	// trust store, so the handshake must fail verification.
	rt := New(utls.HelloChrome_Auto)
	client := &http.Client{Transport: rt}

	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("Get() error = nil, want a certificate verification failure")
	}
}

func TestClient(t *testing.T) {
	t.Parallel()

	srv, pool := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	client := Client(utls.HelloChrome_Auto, WithRootCAs(pool))

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestRoundTripper_NegotiatesHTTP1 documents and asserts the HTTP/2
// limitation described on RoundTripper: even against a server that offers
// h2 via ALPN, this RoundTripper always negotiates HTTP/1.1.
func TestRoundTripper_NegotiatesHTTP1(t *testing.T) {
	t.Parallel()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{NextProtos: []string{"h2", "http/1.1"}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	rt := New(utls.HelloChrome_Auto, WithRootCAs(pool))
	client := &http.Client{Transport: rt}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.ProtoMajor != 1 {
		t.Errorf("resp.ProtoMajor = %d, want 1 (HTTP/2 is not supported by this RoundTripper)", resp.ProtoMajor)
	}
}

func TestRoundTripper_ConnectionStateBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	rt := New(utls.HelloChrome_Auto)
	if _, ok := rt.ConnectionState(); ok {
		t.Error("ConnectionState() ok = true before any request, want false")
	}
}

func TestRoundTripper_DialContextCancelled(t *testing.T) {
	t.Parallel()

	rt := New(utls.HelloChrome_Auto)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := rt.dialTLS(ctx, "tcp", "example.com:443")
	if err == nil {
		t.Fatal("dialTLS() error = nil, want error for an already-cancelled context")
	}
}

func TestRoundTripper_ConnectionRefused(t *testing.T) {
	t.Parallel()

	// Bind and immediately close a local listener to get a port nothing is
	// listening on, so the dial fails fast (connection refused) rather than
	// hanging until dialTimeout.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("Listener.Close() error = %v", err)
	}

	rt := New(utls.HelloChrome_Auto)
	client := &http.Client{Transport: rt, Timeout: 5 * time.Second}

	start := time.Now()
	_, err = client.Get("https://" + addr)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Get() error = nil, want connection-refused error")
	}
	if elapsed >= dialTimeout {
		t.Errorf("Get() took %v, want well under dialTimeout (%v) on connection refused", elapsed, dialTimeout)
	}
}

func TestRoundTripper_InvalidAddress(t *testing.T) {
	t.Parallel()

	rt := New(utls.HelloChrome_Auto)
	_, err := rt.dialTLS(context.Background(), "tcp", "missing-port")
	if err == nil {
		t.Fatal("dialTLS() error = nil, want error for an address missing a port")
	}
}

func TestRoundTripper_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	srv, pool := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	rt := New(utls.HelloChrome_Auto, WithRootCAs(pool))
	client := &http.Client{Transport: rt}

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(srv.URL)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			if _, err := io.ReadAll(resp.Body); err != nil {
				errs <- err
				return
			}
			if resp.StatusCode != http.StatusOK {
				errs <- errors.New("unexpected status")
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Get() error = %v", err)
	}

	if _, ok := rt.ConnectionState(); !ok {
		t.Error("ConnectionState() ok = false after concurrent requests, want true")
	}
}
