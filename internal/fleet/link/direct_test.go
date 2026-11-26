package link

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeDaemon serves /api/login (password pw-canary -> token t1) and a /ws
// that needs that token and answers every request with {"ok":true}.
func fakeDaemon(t *testing.T, tls bool) *httptest.Server {
	t.Helper()
	up := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Password string }
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b.Password != "pw-canary" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"token":"t1"}`))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t1" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var req struct {
				ID string `json:"id"`
			}
			if ws.ReadJSON(&req) != nil {
				return
			}
			_ = ws.WriteJSON(map[string]any{"id": req.ID, "result": map[string]bool{"ok": true}})
		}
	})
	if tls {
		return httptest.NewTLSServer(mux)
	}
	return httptest.NewServer(mux)
}

func wsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

func TestDirectDialLogsInAndDials(t *testing.T) {
	srv := fakeDaemon(t, false)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := DialDirect(ctx, Transport{Kind: KindDirect, URL: wsURL(srv)}, "pw-canary")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var out struct{ OK bool }
	if err := c.Call(ctx, "x", nil, &out); err != nil || !out.OK {
		t.Fatalf("call: %v %+v", err, out)
	}
}

func TestDirectDialBadPasswordErrorHasNoSecret(t *testing.T) {
	srv := fakeDaemon(t, false)
	defer srv.Close()
	_, err := DialDirect(context.Background(), Transport{Kind: KindDirect, URL: wsURL(srv)}, "wrong-pw")
	if err == nil {
		t.Fatal("want error")
	}
	for _, s := range []string{"pw-canary", "wrong-pw", "t1"} {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("error %q leaks %q", err, s)
		}
	}
}

func TestDirectDialRefusesCleartextOutsideOverlay(t *testing.T) {
	_, err := DialDirect(context.Background(), Transport{Kind: KindDirect, URL: "ws://192.168.1.5:1"}, "pw")
	if !errors.Is(err, ErrCleartext) {
		t.Fatalf("got %v, want ErrCleartext", err)
	}
}

func TestDirectDialWSSPinMismatchFails(t *testing.T) {
	srv := fakeDaemon(t, true)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	other := sha256.Sum256([]byte("another cert"))
	_, err := DialDirect(ctx, Transport{Kind: KindDirect, URL: wsURL(srv), Fingerprint: hex.EncodeToString(other[:])}, "pw-canary")
	if err == nil {
		t.Fatal("pin mismatch must fail")
	}
	sum := sha256.Sum256(srv.Certificate().Raw)
	c, err := DialDirect(ctx, Transport{Kind: KindDirect, URL: wsURL(srv), Fingerprint: hex.EncodeToString(sum[:])}, "pw-canary")
	if err != nil {
		t.Fatalf("correct pin: %v", err)
	}
	c.Close()
}

func TestDirectLoginIgnoresProxyEnv(t *testing.T) {
	var proxied int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&proxied, 1)
		http.Error(w, "no proxying fleet logins", http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")

	srv := fakeDaemon(t, false)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := DialDirect(ctx, Transport{Kind: KindDirect, URL: wsURL(srv)}, "pw-canary"); err != nil {
		t.Fatalf("login through a set proxy env failed: %v", err)
	}
	if got := atomic.LoadInt64(&proxied); got != 0 {
		t.Fatalf("the counting proxy saw %d requests", got)
	}
	// The loopback peer above is never proxied by Go anyway; the transport
	// itself is what a non-loopback peer would use, and its Proxy must be nil.
	if directTransport(&tls.Config{}).Proxy != nil {
		t.Fatal("directTransport must not use the environment proxy")
	}
}
