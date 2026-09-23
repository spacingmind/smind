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
	"net"
	"net/http"
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
// The relay's real access control is the HMAC admission handshake
// (internal/relay/admission) every RPC still requires, not same-origin
// policy, so the CORS/websocket origin checks here are permissive by
// design: tightening them would not add security (a browser's CORS
// sandbox has no bearing on an admission-authenticated mobile client that
// isn't a browser at all), only break legitimate clients that don't send a
// browser-style Origin header.
func serveGRPCWeb(ctx context.Context, gs *grpc.Server, cert tls.Certificate, lis net.Listener) error {
	wrapped := grpcweb.WrapServer(gs,
		grpcweb.WithWebsockets(true),
		grpcweb.WithWebsocketOriginFunc(func(*http.Request) bool { return true }),
		grpcweb.WithOriginFunc(func(string) bool { return true }),
	)
	httpSrv := &http.Server{
		Handler:   wrapped,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13},
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
