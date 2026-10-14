package server

// multidevice_test.go — server-level tests for the multi-device relay
// additions (ADR-0031 D9, plan P12a): DeviceAttach/DeviceDetach/Sync on
// the per-host control stream, supersession, the per-host device cap,
// frame+byte bounded buffers, keepalive echo, and Revoke.
//
// Helpers here are prefixed md to avoid collisions with harness_test.go.
//
// Note on GC: the implemented server sends DeviceDetach only when a live
// device side ends with no daemon side; garbage-collecting an idle route
// (Sweep) does not announce anything — the daemon would learn waiting
// routes from the next Sync instead. Tests assert the implemented
// behaviour; the divergence from the plan text is reported in the
// handoff, not papered over.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacingmind/smind/internal/relay/admission"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

const (
	mdTimeout = 5 * time.Second
	mdQuiet   = 250 * time.Millisecond
)

// mdControl wraps the host's OpenControl stream: it sends the mandatory
// Ping first frame and exposes received ControlFrames on a channel.
type mdControl struct {
	t      *testing.T
	stream relaypb.Relay_OpenControlClient
	frames chan *relaypb.ControlFrame
	done   chan error
}

func mdOpenControl(t *testing.T, h *harness, ctx context.Context, admissionID string) *mdControl {
	t.Helper()
	stream, err := h.client.OpenControl(admittedContext(ctx, admissionID))
	if err != nil {
		t.Fatalf("OpenControl: %v", err)
	}
	c := &mdControl{
		t:      t,
		stream: stream,
		frames: make(chan *relaypb.ControlFrame, 64),
		done:   make(chan error, 1),
	}
	c.send(&relaypb.ControlFrame{
		WorkspaceId: testWorkspace,
		Body:        &relaypb.ControlFrame_Ping_{Ping: &relaypb.ControlFrame_Ping{Sequence: 7}},
	})
	go func() {
		for {
			f, err := stream.Recv()
			if err != nil {
				c.done <- err
				close(c.frames)
				return
			}
			c.frames <- f
		}
	}()
	return c
}

func (c *mdControl) send(cf *relaypb.ControlFrame) {
	c.t.Helper()
	if err := c.stream.Send(cf); err != nil {
		c.t.Fatalf("control send: %v", err)
	}
}

func (c *mdControl) next(d time.Duration) *relaypb.ControlFrame {
	c.t.Helper()
	select {
	case f, ok := <-c.frames:
		if !ok {
			c.t.Fatalf("control stream closed while waiting for frame")
		}
		return f
	case <-time.After(d):
		c.t.Fatalf("timed out waiting for control frame after %v", d)
		return nil
	}
}

func (c *mdControl) quiet(d time.Duration) {
	c.t.Helper()
	select {
	case f, ok := <-c.frames:
		if !ok {
			c.t.Fatalf("control stream ended while expecting quiet")
		}
		c.t.Fatalf("unexpected control frame while expecting quiet: %+v", f)
	case <-time.After(d):
	}
}

func (c *mdControl) expectPong(seq uint64) {
	c.t.Helper()
	f := c.next(mdTimeout)
	p := f.GetPong()
	if p == nil || p.GetSequence() != seq {
		c.t.Fatalf("expected Pong(seq=%d), got %+v", seq, f.GetBody())
	}
}

func (c *mdControl) expectAttach(deviceID, sessionID string) {
	c.t.Helper()
	f := c.next(mdTimeout)
	a := f.GetDeviceAttach()
	if a == nil || a.GetDeviceId() != deviceID || string(a.GetSessionId()) != sessionID {
		c.t.Fatalf("expected DeviceAttach{%s,%s}, got %+v", deviceID, sessionID, f.GetBody())
	}
}

func (c *mdControl) expectDetach(deviceID, sessionID string) {
	c.t.Helper()
	f := c.next(mdTimeout)
	d := f.GetDeviceDetach()
	if d == nil || d.GetDeviceId() != deviceID || string(d.GetSessionId()) != sessionID {
		c.t.Fatalf("expected DeviceDetach{%s,%s}, got %+v", deviceID, sessionID, f.GetBody())
	}
}

// expectSync asserts the next frame is a Sync listing exactly want
// (device id -> session id), in any order.
func (c *mdControl) expectSync(want map[string]string) {
	c.t.Helper()
	f := c.next(mdTimeout)
	s := f.GetSync()
	if s == nil {
		c.t.Fatalf("expected Sync, got %+v", f.GetBody())
	}
	got := map[string]string{}
	for _, d := range s.GetDevices() {
		got[d.GetDeviceId()] = string(d.GetSessionId())
	}
	if len(got) != len(want) {
		c.t.Fatalf("Sync lists %d devices (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for dev, sess := range want {
		if got[dev] != sess {
			c.t.Fatalf("Sync device %s: got session %q, want %q (full: %v)", dev, got[dev], sess, got)
		}
	}
}

// expectDone asserts the stream ends with the given code and (exact, if
// non-empty) message. Only a clean close (codes.OK) closes the send side
// first — doing so before an expected server-side error would race the
// server's own status against a client-initiated EOF.
func (c *mdControl) expectDone(code codes.Code, msg string) {
	c.t.Helper()
	if code == codes.OK {
		c.stream.CloseSend()
	}
	select {
	case err := <-c.done:
		if status.Code(err) != code {
			c.t.Fatalf("control stream ended with %v, want code %v", err, code)
		}
		if msg != "" && status.Convert(err).Message() != msg {
			c.t.Fatalf("control stream ended with %q, want %q", status.Convert(err).Message(), msg)
		}
	case <-time.After(mdTimeout):
		c.t.Fatalf("control stream did not end within %v", mdTimeout)
	}
}

// mdStream is a raw OpenData wrapper that surfaces the stream's terminal
// status (unlike endpoint, which discards it) while keeping the
// frame/expectRecv ergonomics.
type mdStream struct {
	t      *testing.T
	stream relaypb.Relay_OpenDataClient
	route  frameRoute
	frames chan *relaypb.Frame
	done   chan error
}

func mdOpenStream(t *testing.T, h *harness, ctx context.Context, admissionID string, r frameRoute) *mdStream {
	t.Helper()
	stream, err := h.client.OpenData(admittedContext(ctx, admissionID))
	if err != nil {
		t.Fatalf("OpenData: %v", err)
	}
	s := &mdStream{
		t:      t,
		stream: stream,
		route:  r,
		frames: make(chan *relaypb.Frame, 64),
		done:   make(chan error, 1),
	}
	go func() {
		for {
			f, err := stream.Recv()
			if err != nil {
				s.done <- err
				close(s.frames)
				return
			}
			s.frames <- f
		}
	}()
	return s
}

func (s *mdStream) send(f *relaypb.Frame) {
	s.t.Helper()
	if err := s.stream.Send(f); err != nil {
		s.t.Fatalf("data send: %v", err)
	}
}

func (s *mdStream) frame(sequence uint64, payload []byte) *relaypb.Frame {
	return &relaypb.Frame{
		WorkspaceId: s.route.workspaceID,
		SessionId:   []byte(s.route.sessionID),
		DeviceId:    s.route.deviceID,
		Direction:   s.route.direction,
		Sequence:    sequence,
		Payload:     payload,
	}
}

func (s *mdStream) expectRecv(d time.Duration) *relaypb.Frame {
	s.t.Helper()
	select {
	case f, ok := <-s.frames:
		if !ok {
			select {
			case err := <-s.done:
				s.t.Fatalf("data stream closed while waiting for frame (terminal status: %v)", err)
			case <-time.After(mdTimeout):
				s.t.Fatalf("data stream closed while waiting for frame (no terminal status)")
			}
		}
		return f
	case <-time.After(d):
		s.t.Fatalf("timed out waiting for data frame after %v", d)
		return nil
	}
}

func (s *mdStream) quiet(d time.Duration) {
	s.t.Helper()
	select {
	case f, ok := <-s.frames:
		if !ok {
			s.t.Fatalf("data stream ended while expecting quiet")
		}
		s.t.Fatalf("unexpected data frame while expecting quiet: %+v", f)
	case <-time.After(d):
	}
}

// expectDone asserts the stream ends with the given status, closing the
// send side first. A clean close (the server returned no status) surfaces
// client-side as EOF, i.e. codes.OK; only then is the send side closed
// here (closing it before an expected server-side error races the two).
func (s *mdStream) expectDone(code codes.Code) {
	s.t.Helper()
	if code == codes.OK {
		s.stream.CloseSend()
	}
	select {
	case err := <-s.done:
		if code == codes.OK {
			if err != nil && !errors.Is(err, io.EOF) {
				s.t.Fatalf("data stream ended with %v, want clean close", err)
			}
		} else if status.Code(err) != code {
			s.t.Fatalf("data stream ended with %v, want code %v", err, code)
		}
	case <-time.After(mdTimeout):
		s.t.Fatalf("data stream did not end within %v", mdTimeout)
	}
}

func (s *mdStream) ended() <-chan error { return s.done }

// mdRoute builds a frameRoute for device i.
func mdRoute(deviceID, sessionID string, dir relaypb.Direction) frameRoute {
	return frameRoute{workspaceID: testWorkspace, sessionID: sessionID, deviceID: deviceID, direction: dir}
}

// --- tests ---

// mdSettle sends a keepalive on s and waits for its echo: the relay handles a
// stream's frames in order, so once the echo is back every earlier frame on
// that stream has been forwarded or buffered.
func mdSettle(s *mdStream) {
	s.t.Helper()
	s.send(&relaypb.Frame{
		WorkspaceId: s.route.workspaceID,
		SessionId:   []byte(s.route.sessionID),
		DeviceId:    s.route.deviceID,
		Direction:   s.route.direction,
		Sequence:    1 << 40,
		Keepalive:   true,
	})
	if echo := s.expectRecv(mdTimeout); !echo.GetKeepalive() || echo.GetSequence() != 1<<40 {
		s.t.Fatalf("settle: expected the keepalive echo, got %+v", echo)
	}
}

// mdWaitStreams waits until the relay has n data streams attached: a stream's
// route is attached by the server asynchronously after the client's first
// frame, so a test that depends on earlier attaches must wait for them.
func mdWaitStreams(t *testing.T, h *harness, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.srv.Streams() != n {
		if time.Now().After(deadline) {
			t.Fatalf("relay has %d attached streams, want %d", h.srv.Streams(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDeviceAttachSentWhenDeviceAttachesWithoutDaemonSide(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	ctl := mdOpenControl(t, h, ctx, adm)
	ctl.expectPong(7)
	ctl.expectSync(map[string]string{})

	// A device attaches with no daemon side: the relay announces it.
	v := mdOpenStream(t, h, ctx, adm, mdRoute("dev-a", "sess-a", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v.send(v.frame(0, []byte("reg")))
	ctl.expectAttach("dev-a", "sess-a")

	// A daemon side whose stream ends while the device is live
	// re-announces DeviceAttach.
	d := mdOpenStream(t, h, ctx, adm, mdRoute("dev-a", "sess-a", relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE))
	d.send(d.frame(0, []byte("reg")))
	v.expectRecv(mdTimeout) // reg flushed to the device
	d.stream.CloseSend()
	d.expectDone(codes.OK)
	// No re-announce: the relay no longer sends DeviceAttach when a
	// daemon side ends while the device side is live (reconnect-loop
	// hazard); the daemon would learn the waiting route from the next
	// Sync instead.
	ctl.quiet(mdQuiet)
}

func TestDeviceDetachSentWhenRouteGarbageCollected(t *testing.T) {
	// A device side ending with no daemon side sends DeviceDetach...
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	ctl := mdOpenControl(t, h, ctx, adm)
	ctl.expectPong(7)
	ctl.expectSync(map[string]string{})

	v := mdOpenStream(t, h, ctx, adm, mdRoute("dev-gc", "sess-gc", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v.send(v.frame(0, []byte("reg")))
	ctl.expectAttach("dev-gc", "sess-gc")
	v.stream.CloseSend()
	v.expectDone(codes.OK)
	ctl.expectDetach("dev-gc", "sess-gc")
	ctl.quiet(mdQuiet)

	// ...and garbage-collecting an idle route (fake clock + Sweep past the
	// grace) announces DeviceDetach too (idempotent with the one above).
	clock := newFakeClock()
	h2 := newHarnessWithOptions(t, 0, WithClock(clock.Now), WithRouteGrace(testGrace))
	ctx2, cancel2 := context.WithCancel(context.Background())
	t.Cleanup(cancel2)
	adm2 := h2.admit(ctx2, testWorkspace, "daemon-key-1")
	ctl2 := mdOpenControl(t, h2, ctx2, adm2)
	ctl2.expectPong(7)
	ctl2.expectSync(map[string]string{})

	v2 := mdOpenStream(t, h2, ctx2, adm2, mdRoute("dev-gc2", "sess-gc2", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v2.send(v2.frame(0, []byte("reg")))
	ctl2.expectAttach("dev-gc2", "sess-gc2")
	v2.stream.CloseSend()
	v2.expectDone(codes.OK)
	ctl2.expectDetach("dev-gc2", "sess-gc2")

	waitFor(t, mdTimeout, func() bool { return h2.srv.Streams() == 0 })
	clock.Advance(testGrace + time.Second)
	h2.srv.Sweep()
	if n := routeCount(h2.srv); n != 0 {
		t.Fatalf("routes after grace = %d, want 0", n)
	}
	ctl2.expectDetach("dev-gc2", "sess-gc2")
	ctl2.quiet(mdQuiet)

	// A route that never saw a device-side end (daemon side only) is also
	// collected with a Detach.
	d3 := mdOpenStream(t, h2, ctx2, adm2, mdRoute("dev-gc3", "sess-gc3", relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE))
	d3.send(d3.frame(0, []byte("reg")))
	waitFor(t, mdTimeout, func() bool { return h2.srv.Streams() == 1 })
	d3.stream.CloseSend()
	d3.expectDone(codes.OK)
	waitFor(t, mdTimeout, func() bool { return h2.srv.Streams() == 0 })
	ctl2.quiet(mdQuiet) // a daemon side ending announces nothing
	clock.Advance(testGrace + time.Second)
	h2.srv.Sweep()
	ctl2.expectDetach("dev-gc3", "sess-gc3")
}

func TestSyncListsWaitingRoutesOnControlAccept(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	// Two waiting devices attach BEFORE the control stream exists...
	waiting := [2]struct{ dev, sess string }{
		{"dev-w1", "sess-w1"}, {"dev-w2", "sess-w2"},
	}
	for _, w := range waiting {
		v := mdOpenStream(t, h, ctx, adm, mdRoute(w.dev, w.sess, relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
		v.send(v.frame(0, []byte("reg")))
	}
	// ...and a route that already has a daemon side must NOT be listed.
	both := mdOpenStream(t, h, ctx, adm, mdRoute("dev-full", "sess-full", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	both.send(both.frame(0, []byte("reg")))
	dFull := mdOpenStream(t, h, ctx, adm, mdRoute("dev-full", "sess-full", relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE))
	dFull.send(dFull.frame(0, []byte("reg")))
	both.expectRecv(mdTimeout)

	// The control stream's first frames are Pong(seq of the first ping)
	// then Sync listing exactly the two waiting devices.
	ctl := mdOpenControl(t, h, ctx, adm)
	ctl.expectPong(7)
	ctl.expectSync(map[string]string{"dev-w1": "sess-w1", "dev-w2": "sess-w2"})
	ctl.quiet(mdQuiet)
}

func TestNewControlStreamSupersedesOld(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	old := mdOpenControl(t, h, ctx, adm)
	old.expectPong(7)
	old.expectSync(map[string]string{})

	// A second control stream for the same host ends the first with
	// Aborted "superseded" and becomes the host's control stream.
	fresh := mdOpenControl(t, h, ctx, adm)
	old.expectDone(codes.Aborted, "superseded")
	fresh.expectPong(7)
	fresh.expectSync(map[string]string{})

	// Announcements now arrive only on the new stream.
	v := mdOpenStream(t, h, ctx, adm, mdRoute("dev-s", "sess-s", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v.send(v.frame(0, []byte("reg")))
	fresh.expectAttach("dev-s", "sess-s")
}

func TestDeviceCapPerHostIs16AndConfigurable(t *testing.T) {
	// Default cap of 16: the 17th distinct device id with a live device
	// side gets ResourceExhausted; a new session of an already-attached
	// device id is not counted.
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	for i := 0; i < 16; i++ {
		v := mdOpenStream(t, h, ctx, adm, mdRoute(fmt.Sprintf("dev-%02d", i), "sess-1", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
		v.send(v.frame(0, []byte("reg")))
	}
	mdWaitStreams(t, h, 16)
	v17 := mdOpenStream(t, h, ctx, adm, mdRoute("dev-17", "sess-1", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v17.send(v17.frame(0, []byte("reg")))
	v17.expectDone(codes.ResourceExhausted)

	// A different session of an attached device id still attaches.
	v := mdOpenStream(t, h, ctx, adm, mdRoute("dev-00", "sess-2", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v.send(v.frame(0, []byte("reg")))
	v.quiet(mdQuiet) // still attached, not refused

	// Configured cap of 2: the 3rd distinct device is refused.
	h2 := newHarnessWithOptions(t, 0, WithMaxDevicesPerHost(2))
	adm2 := h2.admit(ctx, testWorkspace, "daemon-key-1")
	for i := 0; i < 2; i++ {
		v := mdOpenStream(t, h2, ctx, adm2, mdRoute(fmt.Sprintf("dev2-%d", i), "sess-1", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
		v.send(v.frame(0, []byte("reg")))
	}
	mdWaitStreams(t, h2, 2)
	v3 := mdOpenStream(t, h2, ctx, adm2, mdRoute("dev2-2", "sess-1", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v3.send(v3.frame(0, []byte("reg")))
	v3.expectDone(codes.ResourceExhausted)
}

func TestBufferBoundedByFramesAndBytes(t *testing.T) {
	// Frame-count bound: with cap 4, the 5th buffered frame evicts the
	// oldest; a late-attaching device receives the newest 4 in order.
	h := newHarness(t, 4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	r := mdRoute("dev-b", "sess-b", relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE)
	d := mdOpenStream(t, h, ctx, adm, r)
	const n = 6
	for i := 0; i < n; i++ {
		d.send(d.frame(uint64(i), []byte{byte('a' + i)}))
	}
	mdSettle(d)
	if got := h.srv.BufferedFrames(testWorkspace, "sess-b", "dev-b"); got != 4 {
		t.Fatalf("buffered frames = %d, want 4", got)
	}
	v := mdOpenStream(t, h, ctx, adm, mdRoute("dev-b", "sess-b", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v.send(v.frame(0, []byte("reg")))
	for i := n - 4; i < n; i++ {
		if got := v.expectRecv(mdTimeout); got.GetSequence() != uint64(i) {
			t.Fatalf("frame-count bound: got sequence %d, want %d", got.GetSequence(), i)
		}
	}
	v.quiet(mdQuiet)

	// Byte bound: bufferCap 200 but WithBufferBytes(4): three 2-byte
	// payloads exceed 4 bytes, so only the newest two survive.
	h2 := newHarnessWithOptions(t, 200, WithBufferBytes(4))
	adm2 := h2.admit(ctx, testWorkspace, "daemon-key-1")
	r2 := mdRoute("dev-b2", "sess-b2", relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE)
	d2 := mdOpenStream(t, h2, ctx, adm2, r2)
	for i := 0; i < 3; i++ {
		d2.send(d2.frame(uint64(i), []byte{byte(i), byte(i)}))
	}
	mdSettle(d2)
	if got := h2.srv.BufferedFrames(testWorkspace, "sess-b2", "dev-b2"); got != 2 {
		t.Fatalf("buffered frames = %d, want 2", got)
	}
	v2 := mdOpenStream(t, h2, ctx, adm2, mdRoute("dev-b2", "sess-b2", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v2.send(v2.frame(0, []byte("reg")))
	for i := 1; i < 3; i++ {
		if got := v2.expectRecv(mdTimeout); got.GetSequence() != uint64(i) {
			t.Fatalf("byte bound: got sequence %d, want %d", got.GetSequence(), i)
		}
	}
	v2.quiet(mdQuiet)

	// A single frame larger than the byte bound is still kept and
	// delivered (the newest is never evicted).
	h3 := newHarnessWithOptions(t, 200, WithBufferBytes(8))
	adm3 := h3.admit(ctx, testWorkspace, "daemon-key-1")
	r3 := mdRoute("dev-b3", "sess-b3", relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE)
	d3 := mdOpenStream(t, h3, ctx, adm3, r3)
	big := randBytes(t, 64)
	d3.send(d3.frame(0, big))
	mdSettle(d3)
	if got := h3.srv.BufferedFrames(testWorkspace, "sess-b3", "dev-b3"); got != 1 {
		t.Fatalf("buffered frames = %d, want 1", got)
	}
	v3 := mdOpenStream(t, h3, ctx, adm3, mdRoute("dev-b3", "sess-b3", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v3.send(v3.frame(0, []byte("reg")))
	if got := v3.expectRecv(mdTimeout); !bytes.Equal(got.GetPayload(), big) {
		t.Fatalf("oversized buffered frame was dropped or mangled")
	}
}

func TestKeepaliveEchoedNotForwardedNotBuffered(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	// Daemon side only: keepalives from it must buffer nothing.
	r := mdRoute("dev-k", "sess-k", relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE)
	d := mdOpenStream(t, h, ctx, adm, r)
	d.send(d.frame(0, []byte("reg")))

	ka := d.frame(42, nil)
	ka.Keepalive = true
	d.send(ka)
	echo := d.expectRecv(mdTimeout)
	if !echo.GetKeepalive() || echo.GetSequence() != 42 {
		t.Fatalf("keepalive echo mismatch: keepalive=%v seq=%d", echo.GetKeepalive(), echo.GetSequence())
	}
	// Answered "from" the peer's direction.
	if echo.GetDirection() != relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON {
		t.Fatalf("keepalive echo direction = %v, want DEVICE_TO_DAEMON", echo.GetDirection())
	}
	if n := h.srv.BufferedFrames(testWorkspace, "sess-k", "dev-k"); n != 1 {
		t.Fatalf("buffered = %d, want 1 (only the reg frame; keepalives must not buffer)", n)
	}
	d.quiet(mdQuiet)

	// With both sides attached, a device keepalive is answered on the
	// same stream and never forwarded to the peer.
	v := mdOpenStream(t, h, ctx, adm, mdRoute("dev-k", "sess-k", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v.send(v.frame(0, []byte("reg")))
	d.expectRecv(mdTimeout)                                     // device reg
	if got := v.expectRecv(mdTimeout); got.GetSequence() != 0 { // daemon reg forwarded to device
		t.Fatalf("unexpected frame before keepalive: %+v", got)
	}
	ka2 := v.frame(99, nil)
	ka2.Keepalive = true
	v.send(ka2)
	echo2 := v.expectRecv(mdTimeout)
	if !echo2.GetKeepalive() || echo2.GetSequence() != 99 || echo2.GetDirection() != relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE {
		t.Fatalf("device keepalive echo mismatch: %+v", echo2)
	}
	d.quiet(mdQuiet)
	if n := h.srv.BufferedFrames(testWorkspace, "sess-k", "dev-k"); n != 0 {
		t.Fatalf("keepalive was buffered (%d frames)", n)
	}
}

func TestRevokeClosesDeviceStreamsAndBlocksAttach(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	ctl := mdOpenControl(t, h, ctx, adm)
	ctl.expectPong(7)
	ctl.expectSync(map[string]string{})

	v := mdOpenStream(t, h, ctx, adm, mdRoute("dev-r", "sess-r", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v.send(v.frame(0, []byte("reg")))
	ctl.expectAttach("dev-r", "sess-r")

	ctl.send(&relaypb.ControlFrame{
		WorkspaceId: testWorkspace,
		Body:        &relaypb.ControlFrame_Revoke_{Revoke: &relaypb.ControlFrame_Revoke{DeviceId: "dev-r"}},
	})

	// The device's live stream ends with PermissionDenied.
	v.expectDone(codes.PermissionDenied)

	// New attaches for that device id are refused while the control
	// stream lives.
	v2 := mdOpenStream(t, h, ctx, adm, mdRoute("dev-r", "sess-r2", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v2.send(v2.frame(0, []byte("reg")))
	v2.expectDone(codes.PermissionDenied)

	// Other devices are unaffected.
	other := mdOpenStream(t, h, ctx, adm, mdRoute("dev-ok", "sess-ok", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	other.send(other.frame(0, []byte("reg")))
	ctl.expectAttach("dev-ok", "sess-ok")

	// The revocation dies with the control stream: after it ends, the
	// device id may attach again.
	ctl.stream.CloseSend()
	<-ctl.done
	v3 := mdOpenStream(t, h, ctx, adm, mdRoute("dev-r", "sess-r3", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v3.send(v3.frame(0, []byte("reg")))
	v3.quiet(mdQuiet) // accepted again
}

func TestDeviceReconnectReplacesOnlyItsOwnRoute(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	sides := [3][2]*mdStream{}
	for i := 0; i < 3; i++ {
		dev := mdRoute(fmt.Sprintf("dev-%d", i), fmt.Sprintf("sess-%d", i), relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON)
		dae := dev
		dae.direction = relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE
		v := mdOpenStream(t, h, ctx, adm, dev)
		d := mdOpenStream(t, h, ctx, adm, dae)
		v.send(v.frame(0, []byte("reg")))
		d.send(d.frame(0, []byte("reg")))
		v.expectRecv(mdTimeout)
		d.expectRecv(mdTimeout)
		sides[i] = [2]*mdStream{d, v}
	}

	// Device 1 re-opens the SAME (session, device) stream; the old one is
	// superseded and the new one works.
	sides[1][1].stream.CloseSend()
	<-sides[1][1].done
	v1 := mdOpenStream(t, h, ctx, adm, mdRoute("dev-1", "sess-1", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v1.send(v1.frame(0, []byte("reg")))
	sides[1][0].expectRecv(mdTimeout)

	// The other two routes' streams stay open and keep carrying frames,
	// in both directions.
	for _, i := range []int{0, 2} {
		d, v := sides[i][0], sides[i][1]
		d.send(d.frame(1, []byte("to-device")))
		if got := v.expectRecv(mdTimeout); got.GetSequence() != 1 || string(got.GetPayload()) != "to-device" {
			t.Fatalf("route %d daemon->device frame lost: %+v", i, got)
		}
		v.send(v.frame(1, []byte("to-daemon")))
		if got := d.expectRecv(mdTimeout); got.GetSequence() != 1 || string(got.GetPayload()) != "to-daemon" {
			t.Fatalf("route %d device->daemon frame lost: %+v", i, got)
		}
	}
	// And the reconnected route works too.
	sides[1][0].send(sides[1][0].frame(1, []byte("re-to-device")))
	if got := v1.expectRecv(mdTimeout); got.GetSequence() != 1 {
		t.Fatalf("reconnected route lost a frame: %+v", got)
	}
	v1.send(v1.frame(1, []byte("re-to-daemon")))
	sides[1][0].expectRecv(mdTimeout)
}

func TestExistingFixedIDClientAndDaemonStillWork(t *testing.T) {
	// The pre-multi-device world: fixed ids, no control stream, no
	// keepalive. Frames must flow both ways exactly as before.
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	sess, dev := "m1-default-session", "m1-default-device"
	dr := mdRoute(dev, sess, relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE)
	vr := mdRoute(dev, sess, relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON)
	d := mdOpenStream(t, h, ctx, adm, dr)
	v := mdOpenStream(t, h, ctx, adm, vr)
	d.send(d.frame(0, []byte("reg")))
	v.send(v.frame(0, []byte("reg")))
	v.expectRecv(mdTimeout)
	d.expectRecv(mdTimeout)

	ct1 := randBytes(t, 32)
	d.send(d.frame(1, ct1))
	if got := v.expectRecv(mdTimeout); !bytes.Equal(got.GetPayload(), ct1) {
		t.Fatalf("fixed-id daemon->device payload mismatch")
	}
	ct2 := randBytes(t, 32)
	v.send(v.frame(1, ct2))
	if got := d.expectRecv(mdTimeout); !bytes.Equal(got.GetPayload(), ct2) {
		t.Fatalf("fixed-id device->daemon payload mismatch")
	}

	// Then also with a control stream that only pings and ignores the
	// Attach/Sync frames the relay sends: data flow is unaffected.
	ctl := mdOpenControl(t, h, ctx, adm)
	ctl.expectPong(7)
	ctl.expectSync(map[string]string{}) // both sides live: nothing waiting
	ct3 := randBytes(t, 32)
	d.send(d.frame(2, ct3))
	if got := v.expectRecv(mdTimeout); !bytes.Equal(got.GetPayload(), ct3) {
		t.Fatalf("fixed-id daemon->device payload mismatch with control stream")
	}
	ct4 := randBytes(t, 32)
	v.send(v.frame(2, ct4))
	if got := d.expectRecv(mdTimeout); !bytes.Equal(got.GetPayload(), ct4) {
		t.Fatalf("fixed-id device->daemon payload mismatch with control stream")
	}
	ctl.quiet(mdQuiet)
}

func TestControlStreamsOfDifferentHostsAreIsolated(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	// A second relay host on the same harness server. Nothing in
	// harness_test.go exposes this, but the verifier is reachable
	// same-package; registering another workspace needs no edits there.
	const hostB = "ws-md-host-b"
	wsB, secretB, err := admission.NewWorkspace(hostB)
	if err != nil {
		t.Fatalf("NewWorkspace(hostB): %v", err)
	}
	h.srv.verifier.Register(wsB)

	admB := mdAdmit(t, h, ctx, hostB, secretB)

	// Host A's control stream is up and drained.
	ctlA := mdOpenControl(t, h, ctx, adm)
	ctlA.expectPong(7)
	ctlA.expectSync(map[string]string{})

	// A device attaching to host B's route must not produce an Attach on
	// host A's control stream.
	const devID = "dev-shared"
	routeB := mdRoute(devID, "sess-b", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON)
	routeB.workspaceID = hostB
	vB := mdOpenStream(t, h, ctx, admB, routeB)
	vB.send(vB.frame(0, []byte("reg")))
	ctlA.quiet(mdQuiet)

	// Revoke of the same device id on host A cannot close host B's
	// device: its stream stays open and still carries frames.
	ctlA.send(&relaypb.ControlFrame{
		WorkspaceId: testWorkspace,
		Body:        &relaypb.ControlFrame_Revoke_{Revoke: &relaypb.ControlFrame_Revoke{DeviceId: devID}},
	})
	daemonB := routeB
	daemonB.direction = relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE
	dB := mdOpenStream(t, h, ctx, admB, daemonB)
	dB.send(dB.frame(0, []byte("reg")))
	vB.expectRecv(mdTimeout)
	ct := randBytes(t, 16)
	dB.send(dB.frame(1, ct))
	if got := vB.expectRecv(mdTimeout); !bytes.Equal(got.GetPayload(), ct) {
		t.Fatalf("host B device lost a frame after host A revoked the same device id")
	}
	vB.quiet(mdQuiet)
}

// mdAdmit is h.admit for a workspace other than the harness's own, with
// that workspace's secret.
func mdAdmit(t *testing.T, h *harness, ctx context.Context, workspaceID string, secret []byte) string {
	t.Helper()
	nonce := make([]byte, admission.NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("client nonce: %v", err)
	}
	chal, err := h.client.AdmitChallenge(ctx, &relaypb.AdmitChallengeRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     workspaceID,
		ClientNonce:     nonce,
		DaemonKeyId:     "daemon-key-b",
	})
	if err != nil {
		t.Fatalf("AdmitChallenge: %v", err)
	}
	req := &relaypb.AdmitRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     workspaceID,
		ClientNonce:     nonce,
		DaemonKeyId:     "daemon-key-b",
		ServerNonce:     chal.GetServerNonce(),
	}
	req.Hmac = admission.ComputeProof(secret, req)
	resp, err := h.client.Admit(ctx, req)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	return hex.EncodeToString(resp.GetAdmissionId())
}
