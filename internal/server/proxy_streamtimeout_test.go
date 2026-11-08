package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/spacingmind/smind/internal/transport"
)

// productionLikeClient is the client newProxy builds by default (transport.Client
// carries an overall http.Client.Timeout), with that timeout shortened so a test
// can outlast it, pointed at upstream.
func productionLikeClient(t *testing.T, upstream *httptest.Server, timeout time.Duration) *http.Client {
	t.Helper()
	c := testHTTPClient(t, upstream)
	prod := transport.Client(utls.HelloFirefox_Auto)
	prod.Transport = c.Transport
	prod.Timeout = timeout
	return prod
}

// TestProxy_LongStreamOutlastsClientTimeout: a healthy SSE stream that keeps
// sending must not be cut by the client's overall timeout (60 s in production).
func TestProxy_LongStreamOutlastsClientTimeout(t *testing.T) {
	t.Parallel()

	const events = 12
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < events; i++ {
			_, _ = w.Write([]byte("event: ping\n\n"))
			w.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
		_, _ = w.Write([]byte("event: message_stop\n\n"))
	}))
	defer upstream.Close()

	// total stream time (~600 ms) is three times the client timeout
	p, reg := newTestProxy(t, withAnthropicHTTPClient(productionLikeClient(t, upstream, 200*time.Millisecond)))
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")
	proxySrv := httptest.NewServer(http.HandlerFunc(p.handleAnthropic))
	defer proxySrv.Close()

	resp, err := http.Post(proxySrv.URL+"/v1/messages", "application/json", strings.NewReader(`{"stream":true}`))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("stream cut after %d bytes: %v", len(body), err)
	}
	if !strings.HasSuffix(string(body), "event: message_stop\n\n") {
		t.Fatalf("stream truncated: %d bytes, no message_stop", len(body))
	}
}
