package relaytest

import (
	"bytes"
	"context"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// FrameRecord is one data Frame as a harness client sent it to the relay or
// received it from the relay.
type FrameRecord struct {
	// Sent is true for a frame the client sent (what the relay received),
	// false for one the relay delivered to the client (what it forwarded).
	Sent  bool
	Frame *relaypb.Frame
}

// FrameLog collects every data Frame that crosses the relay through a client
// handed out by NativeClient / WebClient. It is the black-box vantage point
// for "the relay never sees plaintext": these are exactly the bytes the relay
// received and forwarded.
type FrameLog struct {
	mu      sync.Mutex
	records []FrameRecord
}

func (l *FrameLog) add(sent bool, f *relaypb.Frame) {
	c := proto.Clone(f).(*relaypb.Frame)
	l.mu.Lock()
	l.records = append(l.records, FrameRecord{Sent: sent, Frame: c})
	l.mu.Unlock()
}

// Records returns a snapshot of everything logged so far.
func (l *FrameLog) Records() []FrameRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]FrameRecord(nil), l.records...)
}

// Contains reports whether needle appears in any field of any logged frame,
// in either direction.
func (l *FrameLog) Contains(needle []byte) bool {
	for _, r := range l.Records() {
		if FrameContains(r.Frame, needle) {
			return true
		}
	}
	return false
}

// FrameContains reports whether needle appears in the frame's payload or
// anywhere in its wire encoding (ids, session id included).
func FrameContains(f *relaypb.Frame, needle []byte) bool {
	if bytes.Contains(f.GetPayload(), needle) {
		return true
	}
	wire, err := proto.Marshal(f)
	return err == nil && bytes.Contains(wire, needle)
}

// recordingClient logs every Frame passing through OpenData streams.
type recordingClient struct {
	relaypb.RelayClient
	log *FrameLog
}

func (c *recordingClient) OpenData(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[relaypb.Frame, relaypb.Frame], error) {
	s, err := c.RelayClient.OpenData(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &recordingStream{BidiStreamingClient: s, log: c.log}, nil
}

type recordingStream struct {
	grpc.BidiStreamingClient[relaypb.Frame, relaypb.Frame]
	log *FrameLog
}

func (s *recordingStream) Send(f *relaypb.Frame) error {
	s.log.add(true, f)
	return s.BidiStreamingClient.Send(f)
}

func (s *recordingStream) Recv() (*relaypb.Frame, error) {
	f, err := s.BidiStreamingClient.Recv()
	if err == nil {
		s.log.add(false, f)
	}
	return f, err
}

// tapClient remembers the most recent OpenData stream so a test can half-close
// it (the "network drop" model) without going through DataConn.DropTransport,
// which takes the read lock a background receive loop holds and would block
// until the next frame arrives.
type tapClient struct {
	relaypb.RelayClient
	mu  sync.Mutex
	cur grpc.BidiStreamingClient[relaypb.Frame, relaypb.Frame]
}

func (c *tapClient) OpenData(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[relaypb.Frame, relaypb.Frame], error) {
	s, err := c.RelayClient.OpenData(ctx, opts...)
	if err == nil {
		c.mu.Lock()
		c.cur = s
		c.mu.Unlock()
	}
	return s, err
}

// closeSend half-closes the current stream: the relay sees EOF, its handler
// returns (detaching the side), and only then does the trailer reach our
// Recv. A caller that waits for its receive loop to end therefore knows the
// relay has already processed the drop.
func (c *tapClient) closeSend() error {
	c.mu.Lock()
	s := c.cur
	c.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.CloseSend()
}
