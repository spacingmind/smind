package server

// grpcweb.go additionally exposes smind relay's RPCs over grpc-web framing
// (github.com/traefik/grpc-web/go/grpcweb.WrapServer), alongside the same
// native-gRPC *grpc.Server every existing test and internal/relay/client
// already use, unmodified. traefik/grpc-web specifically, not the original
// improbable-eng/grpc-web it's forked from: improbable-eng's repository is
// effectively unmaintained, while traefik picked up maintenance --
// including the websocket transport mode this file turns on.
//
// That websocket mode is not an optional extra: plain HTTP/1.1 grpc-web
// framing only supports unary and server-streaming RPCs, because a
// browser/React-Native fetch can't stream a request body incrementally the
// way a client- or bidi-streaming call needs. internal/relay/relaypb's
// OpenControl and OpenData are both bidi-streaming, so without
// WithWebsockets(true) below, neither would be reachable from any grpc-web
// client at all, including Item 3's TypeScript one.
import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/traefik/grpc-web/go/grpcweb"
	"google.golang.org/grpc"
)

// serveGRPCWeb wraps gs with grpc-web framing and serves it on lis (TLS,
// using the same certificate as the native listener) until ctx is
// cancelled. It is deliberately a *separate* listener from gs.Serve's
// native-gRPC one -- rather than multiplexing both protocols behind one
// http.Server via *grpc.Server's experimental ServeHTTP -- so this
// addition carries zero risk of changing native gRPC's existing,
// already-tested behavior; Run wires the two listeners together.
//
// The relay's real access control remains the SCRAM-style admission handshake
// (internal/relay/admission) every RPC still requires. Browser origins are
// additionally restricted to the hosted client and operator allowlist; native
// clients that send no Origin header retain their existing behavior.
func serveGRPCWeb(ctx context.Context, gs *grpc.Server, getCert func(*tls.ClientHelloInfo) (*tls.Certificate, error), lis net.Listener, policy *originPolicy) error {
	wrapped := grpcweb.WrapServer(gs,
		grpcweb.WithWebsockets(true),
		grpcweb.WithWebsocketOriginFunc(policy.check),
		grpcweb.WithOriginFunc(policy.allows),
	)
	var refusals originRefusalLog
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !policy.check(r) {
			origin := r.Header.Get("Origin")
			if len(origin) > 200 {
				origin = origin[:200]
			}
			if refusals.shouldLog(origin, time.Now()) {
				log.Printf("relay: refused browser origin %q", origin)
			}
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		wrapped.ServeHTTP(w, r)
	})

	httpSrv := &http.Server{
		Handler:   handler,
		TLSConfig: &tls.Config{GetCertificate: getCert, MinVersion: tls.VersionTLS13},
	}

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ServeTLS(lis, "", "") }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("relay: grpc-web shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("relay: grpc-web serve: %w", err)
		}
		return nil
	}
}

// Keep at most 256 origin strings for one minute. When full, suppress new
// entries until one expires, rather than evicting an origin and logging it
// repeatedly during a flood. Callers truncate the origin before storing it.
type originRefusalLog struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (l *originRefusalLog) shouldLog(origin string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == nil {
		l.last = make(map[string]time.Time)
	}
	for value, at := range l.last {
		if now.Sub(at) >= time.Minute {
			delete(l.last, value)
		}
	}
	if _, seen := l.last[origin]; seen || len(l.last) >= 256 {
		return false
	}
	l.last[origin] = now
	return true
}
