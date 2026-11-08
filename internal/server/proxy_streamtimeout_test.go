package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// sseUpstream serves a stream whose behaviour per request is chosen by serve;
// it counts the requests it received.
func sseUpstream(t *testing.T, count *atomic.Int32, serve func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		serve(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func timeoutProxy(t *testing.T, upstream *httptest.Server, to upstreamTimeouts) *httptest.Server {
	t.Helper()
	p, reg := newTestProxy(t, withAnthropicHTTPClient(testHTTPClient(t, upstream)), withUpstreamTimeouts(to))
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")
	srv := httptest.NewServer(http.HandlerFunc(p.handleAnthropic))
	t.Cleanup(srv.Close)
	return srv
}

// failFast bounds a test's own client so a broken bound fails the test in
// seconds instead of hanging it.
var failFast = &http.Client{Timeout: 6 * time.Second}

// blockUntilGone holds an upstream handler until the proxy cancels the request
// (or a safety limit, so a broken proxy cannot hang the server's Close).
func blockUntilGone(r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(8 * time.Second):
	}
}

func postMessages(t *testing.T, srv *httptest.Server, body string) *http.Response {
	t.Helper()
	resp, err := failFast.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func streamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
}

// A stream that runs ten times longer than the idle bound, each gap well inside
// it, survives, and the upstream saw exactly one request.
func TestProxy_StreamSurvivesWhileChunksKeepComing(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	up := sseUpstream(t, &n, func(w http.ResponseWriter, r *http.Request) {
		streamHeaders(w)
		for i := 0; i < 20; i++ {
			_, _ = w.Write([]byte("event: ping\n\n"))
			w.(http.Flusher).Flush()
			time.Sleep(25 * time.Millisecond)
		}
		_, _ = w.Write([]byte("event: message_stop\n\n"))
	})
	srv := timeoutProxy(t, up, upstreamTimeouts{header: time.Second, streamIdle: 150 * time.Millisecond, total: 100 * time.Millisecond})
	body, err := io.ReadAll(postMessages(t, srv, `{"stream":true}`).Body)
	if err != nil || !strings.HasSuffix(string(body), "message_stop\n\n") {
		t.Fatalf("stream cut: %d bytes, err = %v", len(body), err)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

// A stream that goes quiet is cut by the idle bound, and the client sees an
// error rather than a clean end.
func TestProxy_StalledStreamIsCutByIdleTimeout(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	cancelled := make(chan struct{})
	up := sseUpstream(t, &n, func(w http.ResponseWriter, r *http.Request) {
		streamHeaders(w)
		_, _ = w.Write([]byte("event: ping\n\n"))
		w.(http.Flusher).Flush()
		blockUntilGone(r) // stalls until the proxy gives up on it
		if r.Context().Err() != nil {
			close(cancelled)
		}
	})
	srv := timeoutProxy(t, up, upstreamTimeouts{header: time.Second, streamIdle: 150 * time.Millisecond, total: time.Minute})
	start := time.Now()
	body, err := io.ReadAll(postMessages(t, srv, `{"stream":true}`).Body)
	if err == nil {
		t.Fatalf("a stalled stream ended cleanly with %q", body)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("cut after %v, want about the 150 ms idle bound", d)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("the upstream request was not cancelled")
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

// An upstream that never answers is cut by the header bound with a 502.
func TestProxy_DeadUpstreamFailsAtHeaderTimeout(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	up := sseUpstream(t, &n, func(w http.ResponseWriter, r *http.Request) { blockUntilGone(r) })
	srv := timeoutProxy(t, up, upstreamTimeouts{header: 150 * time.Millisecond, streamIdle: time.Minute, total: time.Minute})
	start := time.Now()
	resp := postMessages(t, srv, `{"stream":true}`)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
}

// A non-streaming response keeps a total bound: a body that drips past it is cut,
// one that finishes inside it is delivered whole.
func TestProxy_NonStreamingKeepsATotalTimeout(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	up := sseUpstream(t, &n, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		slow := strings.Contains(r.URL.RawQuery, "slow")
		parts := 1
		if slow {
			parts = 30
		}
		for i := 0; i < parts; i++ {
			_, _ = w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			if slow {
				time.Sleep(25 * time.Millisecond)
			}
		}
	})
	srv := timeoutProxy(t, up, upstreamTimeouts{header: time.Second, streamIdle: time.Minute, total: 300 * time.Millisecond})
	resp, err := failFast.Post(srv.URL+"/v1/messages?slow=1", "application/json", strings.NewReader(`{}`))
	if err == nil {
		defer resp.Body.Close()
		if b, rerr := io.ReadAll(resp.Body); rerr == nil {
			t.Fatalf("a body dripping past the total bound ended cleanly (%d bytes)", len(b))
		}
	}
	fast, err := io.ReadAll(postMessages(t, srv, `{}`).Body)
	if err != nil || string(fast) != "x" {
		t.Fatalf("fast body = %q, err = %v", fast, err)
	}
}

// The client going away cancels the upstream request.
func TestProxy_ClientDisconnectCancelsUpstream(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	cancelled := make(chan struct{})
	up := sseUpstream(t, &n, func(w http.ResponseWriter, r *http.Request) {
		streamHeaders(w)
		_, _ = w.Write([]byte("event: ping\n\n"))
		w.(http.Flusher).Flush()
		blockUntilGone(r)
		if r.Context().Err() != nil {
			close(cancelled)
		}
	})
	srv := timeoutProxy(t, up, upstreamTimeouts{header: time.Minute, streamIdle: time.Minute, total: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/messages", strings.NewReader(`{"stream":true}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatal(err)
	}
	cancel()
	resp.Body.Close()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("the upstream request was not cancelled after the client left")
	}
}

// Headers answered, then silence before the first chunk: the idle bound is armed
// from the moment the headers arrive, not from the first chunk.
func TestProxy_SilentAfterHeadersIsCutByIdleTimeout(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	up := sseUpstream(t, &n, func(w http.ResponseWriter, r *http.Request) {
		streamHeaders(w)
		w.(http.Flusher).Flush()
		blockUntilGone(r)
	})
	srv := timeoutProxy(t, up, upstreamTimeouts{header: time.Second, streamIdle: 150 * time.Millisecond, total: time.Minute})
	start := time.Now()
	if body, err := io.ReadAll(postMessages(t, srv, `{"stream":true}`).Body); err == nil {
		t.Fatalf("a silent stream ended cleanly with %q", body)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("cut after %v, want about the 150 ms idle bound", d)
	}
}
