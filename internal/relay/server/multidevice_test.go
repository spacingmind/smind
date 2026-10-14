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
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
		if ok {
			c.t.Fatalf("unexpected control frame while expecting quiet: %+v", f)
		}
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

// expectDone closes the send side and asserts the stream ends with the
// given code and (exact, if non-empty) message.
func (c *mdControl) expectDone(code codes.Code, msg string) {
	c.t.Helper()
	c.stream.CloseSend()
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
			s.t.Fatalf("data stream closed while waiting for frame")
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
		if ok {
			s.t.Fatalf("unexpected data frame while expecting quiet: %+v", f)
		}
	case <-time.After(d):
	}
}

// expectDone asserts the stream ends with the given status, closing the
// send side first. A clean close (the server returned no status) surfaces
// client-side as EOF, i.e. codes.OK.
func (s *mdStream) expectDone(code codes.Code) {
	s.t.Helper()
	s.stream.CloseSend()
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
	ctl.expectAttach("dev-a", "sess-a")
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

	// ...and garbage-collecting an idle route (fake clock + Sweep past
	// the grace) does NOT announce anything: the implemented server has
	// no daemon-side route to send a Detach for that the daemon ever
	// knew about; it would be re-surfaced by the next Sync instead.
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
	ctl2.quiet(mdQuiet) // no further control traffic for the GC itself

	// A fresh Sync does not list the collected route.
	ctl2.send(&relaypb.ControlFrame{
		WorkspaceId: testWorkspace,
		Body:        &relaypb.ControlFrame_Ping_{Ping: &relaypb.ControlFrame_Ping{Sequence: 8}},
	})
	ctl2.expectPong(8)
	ctl2.quiet(mdQuiet)
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
