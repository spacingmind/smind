package client

// idleproxy_test.go — the scaled-down "125 s proxy idle timeout" scenario
// (ADR-0031 D9, keepalive and proxies): a shim in front of the relay closes
// any stream that has carried no application message for 1 s, the way
// Cloudflare closes an idle stream after 125 s. Keepalives every 0.2 s keep
// a quiet session open; without them the same session dies.

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/spacingmind/smind/internal/relay/admission"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/relay/server"
)

const proxyIdleTimeout = time.Second

// idleShimStream closes (cancels) its stream when no message has crossed it
// in either direction for proxyIdleTimeout.
type idleShimStream struct {
	grpc.ServerStream
	ctx  context.Context
	mu   sync.Mutex
	last time.Time
}

func (s *idleShimStream) Context() context.Context { return s.ctx }

func (s *idleShimStream) touch() {
	s.mu.Lock()
	s.last = time.Now()
	s.mu.Unlock()
}

func (s *idleShimStream) idleFor() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.last)
}

func (s *idleShimStream) SendMsg(m any) error {
	err := s.ServerStream.SendMsg(m)
	s.touch()
	return err
}

func (s *idleShimStream) RecvMsg(m any) error {
	err := s.ServerStream.RecvMsg(m)
	s.touch()
	return err
}

func idleShim(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, cancel := context.WithCancel(ss.Context())
	defer cancel()
	w := &idleShimStream{ServerStream: ss, ctx: ctx, last: time.Now()}
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if w.idleFor() > proxyIdleTimeout {
					cancel()
					return
				}
			}
		}
	}()
	return handler(srv, w)
}

// pairedThroughShim runs a relay behind the idle shim and returns an
// E2EE-paired daemon/device connection, each with the given keepalive option
// (nil = none).
func pairedThroughShim(t *testing.T, opts ...DataOption) (daemon, device *DataConn) {
	t.Helper()
	ws, secret, err := admission.NewWorkspace("ws-idle")
	if err != nil {
		t.Fatal(err)
	}
	verifier := admission.NewVerifier()
	verifier.Register(ws)
	srv := server.New(verifier, 0)
	lis := bufconn.Listen(64 * 1024)
	gs := grpc.NewServer(grpc.StreamInterceptor(idleShim))
	srv.Register(gs)
	go gs.Serve(lis)
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	rc := relaypb.NewRelayClient(conn)

	ctx := context.Background()
	admissionID, err := Admit(ctx, rc, "ws-idle", "daemon-key", secret)
	if err != nil {
		t.Fatal(err)
	}
	daemonKey, _ := e2ee.GenerateKeyPair()
	daemon, err = OpenDaemonData(ctx, rc, admissionID, "ws-idle", "s1", "d1", daemonKey, opts...)
	if err != nil {
		t.Fatal(err)
	}
	device, err = OpenClientData(ctx, rc, admissionID, "ws-idle", "s1", "d1",
		append([]DataOption{WithExpectedPeerKey(daemonKey.Public())}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	dErr := make(chan error, 1)
	go func() { dErr <- daemon.Handshake(ctx) }()
	if err := device.Handshake(ctx); err != nil {
		t.Fatalf("device handshake: %v", err)
	}
	if err := <-dErr; err != nil {
		t.Fatalf("daemon handshake: %v", err)
	}
	t.Cleanup(func() { daemon.Close(); device.Close() })
	return daemon, device
}

// receiveLoop reads application messages until the stream ends.
func receiveLoop(c *DataConn) (msgs <-chan []byte, errs <-chan error) {
	m := make(chan []byte, 8)
	e := make(chan error, 1)
	go func() {
		for {
			b, err := c.Receive()
			if err != nil {
				e <- err
				return
			}
			m <- b
		}
	}()
	return m, e
}

func TestIdleStreamSurvivesProxyIdleTimeout(t *testing.T) {
	ka := WithKeepalive(KeepaliveConfig{
		Interval:     200 * time.Millisecond,
		DeadAfter:    750 * time.Millisecond,
		ProbeTimeout: 500 * time.Millisecond,
	})
	daemon, device := pairedThroughShim(t, ka)
	_, daemonErr := receiveLoop(daemon)
	deviceMsgs, deviceErr := receiveLoop(device)

	// Three idle timeouts' worth of silence.
	select {
	case err := <-daemonErr:
		t.Fatalf("daemon stream died while idle despite keepalives: %v", err)
	case err := <-deviceErr:
		t.Fatalf("device stream died while idle despite keepalives: %v", err)
	case <-time.After(3 * proxyIdleTimeout):
	}

	if err := daemon.Send([]byte("still here")); err != nil {
		t.Fatalf("send after idle: %v", err)
	}
	select {
	case got := <-deviceMsgs:
		if string(got) != "still here" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("message after the idle period was not delivered")
	}
}

func TestIdleStreamWithoutKeepaliveIsClosedByTheProxy(t *testing.T) {
	// Control: the shim really closes idle streams, so the test above proves
	// the keepalive and not a permissive shim.
	daemon, device := pairedThroughShim(t)
	_, daemonErr := receiveLoop(daemon)
	_, deviceErr := receiveLoop(device)
	select {
	case <-daemonErr:
	case <-deviceErr:
	case <-time.After(5 * time.Second):
		t.Fatal("the idle shim never closed the quiet stream")
	}
}
