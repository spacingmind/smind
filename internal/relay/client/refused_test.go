package client

import (
	"context"
	"io"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// refusedStream is a data stream the relay already terminated with a status:
// writes report io.EOF (gRPC's rule for a terminated stream) and the status
// only comes out of Recv.
type refusedStream struct {
	ctx    context.Context
	status error

	mu    sync.Mutex
	drain int // frames still readable before the status
}

func (s *refusedStream) Send(*relaypb.Frame) error { return io.EOF }
func (s *refusedStream) Recv() (*relaypb.Frame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.drain > 0 {
		s.drain--
		return &relaypb.Frame{}, nil
	}
	return nil, s.status
}
func (s *refusedStream) Header() (metadata.MD, error) { return nil, nil }
func (s *refusedStream) Trailer() metadata.MD         { return nil }
func (s *refusedStream) CloseSend() error             { return nil }
func (s *refusedStream) Context() context.Context     { return s.ctx }
func (s *refusedStream) SendMsg(any) error            { return io.EOF }
func (s *refusedStream) RecvMsg(any) error            { return s.status }

type refusingRelay struct {
	relaypb.RelayClient // only OpenData is used
	stream              *refusedStream
}

func (r *refusingRelay) OpenData(ctx context.Context, _ ...grpc.CallOption) (relaypb.Relay_OpenDataClient, error) {
	r.stream.ctx = ctx
	return r.stream, nil
}

// TestHandshakeOnRefusedStreamReportsTheRelaysStatus: a relay that refuses the
// stream (revoked device, full host) must surface as that status, not as the
// bare io.EOF a write on an already-terminated gRPC stream returns.
func TestHandshakeOnRefusedStreamReportsTheRelaysStatus(t *testing.T) {
	for name, drain := range map[string]int{"status first": 0, "buffered frames ahead of the status": 3} {
		t.Run(name, func(t *testing.T) {
			rc := &refusingRelay{stream: &refusedStream{status: status.Error(codes.PermissionDenied, "relay: device revoked"), drain: drain}}
			kp, err := e2ee.GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			dc, err := OpenData(context.Background(), rc, "adm", "host", "sess", "dev", kp, e2ee.RoleMobile)
			if err != nil {
				t.Fatal(err)
			}
			err = dc.Handshake(context.Background())
			if err == nil {
				t.Fatal("handshake on a refused stream succeeded")
			}
			if got := status.Code(err); got != codes.PermissionDenied {
				t.Fatalf("handshake error = %v (code %v), want PermissionDenied", err, got)
			}
		})
	}
}
