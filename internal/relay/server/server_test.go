package server

// server_test.go — the plan's relay test scenarios against the bufconn
// harness: forwarding (byte-identical), reconnect buffer (in-order flush,
// eviction past cap), admission gating, workspace binding, and the
// relay-never-has-plaintext assertion.

import (
	"bytes"
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// waitFor polls cond until true or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func daemonRoute() frameRoute {
	return frameRoute{workspaceID: testWorkspace, sessionID: "sess-1", deviceID: "dev-1", direction: relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE}
}

func deviceRoute() frameRoute {
	return frameRoute{workspaceID: testWorkspace, sessionID: "sess-1", deviceID: "dev-1", direction: relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON}
}

// openPair admits a daemon connection, opens both sides' data streams, and
// exchanges each side's registration frame (the first frame on a stream
// establishes its route and is forwarded like any other).
func openPair(t *testing.T, h *harness) (daemon, device *endpoint) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	dctx := admittedContext(ctx, adm)
	daemon = h.openData(dctx, daemonRoute())
	device = h.openData(dctx, deviceRoute())

	// Both sides must register (send their first frame) before either can
	// receive: the relay derives a stream's route and side only from its
	// first frame, so a stream that has not sent is not yet attached to
	// any route and nothing is delivered to it.
	daemon.sendFrame(daemon.frame(0, []byte("reg")))
	device.sendFrame(device.frame(0, []byte("reg")))
	device.expectSeq(t, 5*time.Second, 0)
	daemon.expectSeq(t, 5*time.Second, 0)
	return daemon, device
}

func TestRelayForwardByteIdentical(t *testing.T) {
	h := newHarness(t, 0)
	daemon, device := openPair(t, h)

	// Daemon -> device, several sizes including empty.
	for i, size := range []int{0, 1, 64, 4096} {
		payload := randBytes(t, size)
		daemon.sendFrame(daemon.frame(uint64(i+1), payload))
		got := device.expectRecv(5 * time.Second)
		if got.GetSequence() != uint64(i+1) || !bytes.Equal(got.GetPayload(), payload) {
			t.Fatalf("daemon->device frame %d: seq=%d payload-match=%v", i, got.GetSequence(), bytes.Equal(got.GetPayload(), payload))
		}
	}

	// Device -> daemon.
	for i, size := range []int{16, 2048} {
		payload := randBytes(t, size)
		device.sendFrame(device.frame(uint64(i+1), payload))
		got := daemon.expectRecv(5 * time.Second)
		if got.GetSequence() != uint64(i+1) || !bytes.Equal(got.GetPayload(), payload) {
			t.Fatalf("device->daemon frame %d: seq=%d payload-match=%v", i, got.GetSequence(), bytes.Equal(got.GetPayload(), payload))
		}
	}
}

func TestReconnectBufferFlushInOrder(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	dctx := admittedContext(ctx, adm)

	// Device is absent: daemon's frames must buffer, not error or vanish.
	daemon := h.openData(dctx, daemonRoute())
	const n = 7
	payloads := make([][]byte, n)
	for i := 0; i < n; i++ {
		payloads[i] = randBytes(t, 32)
		daemon.sendFrame(daemon.frame(uint64(i+1), payloads[i]))
	}

	// Device connects (reconnect): registration frame plus all buffered
	// frames arrive, in order.
	device := h.openData(dctx, deviceRoute())
	device.sendFrame(device.frame(0, []byte("reg")))
	daemon.expectSeq(t, 5*time.Second, 0)

	for i := 0; i < n; i++ {
		f := device.expectRecv(5 * time.Second)
		if f.GetSequence() != uint64(i+1) || !bytes.Equal(f.GetPayload(), payloads[i]) {
			t.Fatalf("buffered frame %d: seq=%d payload-match=%v", i, f.GetSequence(), bytes.Equal(f.GetPayload(), payloads[i]))
		}
	}
}

func TestReconnectBufferEvictsOldestPastCap(t *testing.T) {
	const cap = 5
	h := newHarness(t, cap)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	dctx := admittedContext(ctx, adm)

	daemon := h.openData(dctx, daemonRoute())
	const n = 8 // cap+3: the oldest 3 must be evicted
	for i := 0; i < n; i++ {
		daemon.sendFrame(daemon.frame(uint64(i+1), []byte{byte(i)}))
	}
	waitFor(t, 5*time.Second, func() bool {
		return h.srv.BufferedFrames(testWorkspace, "sess-1", "dev-1") == cap
	})

	device := h.openData(dctx, deviceRoute())
	device.sendFrame(device.frame(0, []byte("reg")))
	daemon.expectSeq(t, 5*time.Second, 0)

	// Expect exactly the newest `cap` frames, in order.
	for i := n - cap; i < n; i++ {
		f := device.expectRecv(5 * time.Second)
		if f.GetSequence() != uint64(i+1) {
			t.Fatalf("eviction: got sequence %d, want %d", f.GetSequence(), i+1)
		}
	}
	device.expectQuiet(300 * time.Millisecond)
}

func TestDataStreamRequiresAdmission(t *testing.T) {
	h := newHarness(t, 0)
	// No Admit call: the stream must be rejected on its first frame.
	stream, err := h.client.OpenData(context.Background())
	if err != nil {
		t.Fatalf("OpenData: %v", err)
	}
	if err := stream.Send(daemonRoute().asFrame(1, []byte("x"))); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Recv err = %v, want Unauthenticated", err)
	}
}

func TestFrameWrongWorkspaceBindingRejected(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	dctx := admittedContext(ctx, adm)

	stream, err := h.client.OpenData(dctx)
	if err != nil {
		t.Fatalf("OpenData: %v", err)
	}
	// Claim a workspace the connection is not bound to.
	f := daemonRoute().asFrame(1, []byte("x"))
	f.WorkspaceId = "ws-other"
	if err := stream.Send(f); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Recv err = %v, want PermissionDenied", err)
	}
}

func TestControlPingPongAndAdmissionGate(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Without admission: rejected.
	noAdm, err := h.client.OpenControl(context.Background())
	if err != nil {
		t.Fatalf("OpenControl: %v", err)
	}
	if err := noAdm.Send(&relaypb.ControlFrame{WorkspaceId: testWorkspace, Body: &relaypb.ControlFrame_Ping_{Ping: &relaypb.ControlFrame_Ping{Sequence: 1}}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := noAdm.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Recv err = %v, want Unauthenticated", err)
	}

	// With admission: ping is answered with a matching pong.
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	cctx := admittedContext(ctx, adm)
	cs, err := h.client.OpenControl(cctx)
	if err != nil {
		t.Fatalf("OpenControl: %v", err)
	}
	if err := cs.Send(&relaypb.ControlFrame{WorkspaceId: testWorkspace, Body: &relaypb.ControlFrame_Ping_{Ping: &relaypb.ControlFrame_Ping{Sequence: 7}}}); err != nil {
		t.Fatalf("Send ping: %v", err)
	}
	pong, err := cs.Recv()
	if err != nil {
		t.Fatalf("Recv pong: %v", err)
	}
	if pong.GetPong() == nil || pong.GetPong().GetSequence() != 7 || pong.GetWorkspaceId() != testWorkspace {
		t.Fatalf("unexpected control reply: %+v", pong)
	}
}

func (r frameRoute) asFrame(seq uint64, payload []byte) *relaypb.Frame {
	return &relaypb.Frame{
		WorkspaceId: r.workspaceID,
		SessionId:   []byte(r.sessionID),
		DeviceId:    r.deviceID,
		Direction:   r.direction,
		Sequence:    seq,
		Payload:     payload,
	}
}

// expectSeq asserts the next frame's sequence (payload already checked by
// callers where it matters).
func (e *endpoint) expectSeq(t *testing.T, timeout time.Duration, seq uint64) {
	t.Helper()
	f := e.expectRecv(timeout)
	if f.GetSequence() != seq {
		t.Fatalf("got sequence %d, want %d", f.GetSequence(), seq)
	}
}

// TestFrameWrongWorkspaceMidStreamRejected: a stream that registered and
// is routing fine must still be rejected when a LATER frame claims a
// different workspace ID — the binding is enforced per frame, not just at
// registration.
func TestFrameWrongWorkspaceMidStreamRejected(t *testing.T) {
	h := newHarness(t, 0)
	daemon, _ := openPair(t, h)

	bad := daemon.frame(2, []byte("x"))
	bad.WorkspaceId = "ws-other"
	daemon.sendFrame(bad)
	if _, ok := <-daemon.recvCh; ok {
		t.Fatal("stream should have errored, not delivered a frame")
	}
}

// TestControlWrongWorkspaceRejected: OpenControl enforces the same
// workspace binding — a control frame claiming a workspace other than the
// connection's admitted one is rejected.
func TestControlWrongWorkspaceRejected(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	cctx := admittedContext(ctx, adm)

	cs, err := h.client.OpenControl(cctx)
	if err != nil {
		t.Fatalf("OpenControl: %v", err)
	}
	if err := cs.Send(&relaypb.ControlFrame{
		WorkspaceId: "ws-other",
		Body:        &relaypb.ControlFrame_Ping_{Ping: &relaypb.ControlFrame_Ping{Sequence: 1}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := cs.Recv(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Recv err = %v, want PermissionDenied", err)
	}
}
