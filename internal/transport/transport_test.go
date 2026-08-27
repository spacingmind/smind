package transport

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
