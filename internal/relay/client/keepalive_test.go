package client

// keepalive_test.go — the 30 s keepalive / 75 s dead-stream rule (ADR-0031
// D9, flow 8) against a fake clock and a scripted stream, so the real
// intervals are exercised without sleeping.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// fakeClock is a manually advanced Clock. Advance fires due timers one at a
// time and waits for the loop to re-arm before moving on, so a long jump
// behaves like the same time passing step by step.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
	arms   int
}

type fakeTimer struct {
	at    time.Time
	ch    chan time.Time
	fired bool
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) At(at time.Time) (<-chan time.Time, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: at, ch: make(chan time.Time, 1)}
	c.timers = append(c.timers, t)
	c.arms++
	if !at.After(c.now) {
		t.fired = true
		t.ch <- c.now
	}
	return t.ch, func() {
		c.mu.Lock()
		t.fired = true
		c.mu.Unlock()
	}
}

func (c *fakeClock) armCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.arms
}

// waitArms waits until the loop has armed at least n timers in total, i.e.
// it has finished its current iteration and is parked waiting for the clock.
// Advancing before that would move time past a timer the loop has not armed yet.
func (c *fakeClock) waitArms(t *testing.T, n int) {
	t.Helper()
	waitUntil(t, "the keepalive loop to park on the clock", func() bool { return c.armCount() >= n })
}

// Advance moves time forward by d, firing timers in order.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.fired && !t.at.After(target) && (next == nil || t.at.Before(next.at)) {
				next = t
			}
		}
		if next == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		if next.at.After(c.now) {
			c.now = next.at
		}
		next.fired = true
		before := c.arms
		now := c.now
		c.mu.Unlock()
		next.ch <- now
		// Wait (bounded) for the woken loop to arm its next timer or exit.
		deadline := time.Now().Add(100 * time.Millisecond)
		for c.armCount() == before && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}
}

// scriptedStream is a relaypb.Relay_OpenDataClient whose Recv is fed by the
// test and whose Send records frames; echo makes it behave like a relay that
// answers keepalives.
type scriptedStream struct {
	ctx  context.Context
	recv chan *relaypb.Frame
	echo bool

	mu        sync.Mutex
	sent      []*relaypb.Frame
	blockSend chan struct{} // when non-nil, Send blocks until closed or ctx done
}

func newScriptedStream(ctx context.Context, echo bool) *scriptedStream {
	return &scriptedStream{ctx: ctx, recv: make(chan *relaypb.Frame, 16), echo: echo}
}

func (s *scriptedStream) Send(f *relaypb.Frame) error {
	s.mu.Lock()
	block := s.blockSend
	s.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
	s.mu.Lock()
	s.sent = append(s.sent, f)
	echo := s.echo && f.GetKeepalive()
	s.mu.Unlock()
	if echo {
		s.recv <- &relaypb.Frame{Keepalive: true, Sequence: f.GetSequence()}
	}
	return nil
}

func (s *scriptedStream) Recv() (*relaypb.Frame, error) {
	select {
	case f := <-s.recv:
		return f, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *scriptedStream) setEcho(v bool) { s.mu.Lock(); s.echo = v; s.mu.Unlock() }

func (s *scriptedStream) keepalives() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, f := range s.sent {
		if f.GetKeepalive() {
			n++
		}
	}
	return n
}

func (s *scriptedStream) Header() (metadata.MD, error) { return nil, nil }
func (s *scriptedStream) Trailer() metadata.MD         { return nil }
func (s *scriptedStream) CloseSend() error             { return nil }
func (s *scriptedStream) Context() context.Context     { return s.ctx }
func (s *scriptedStream) SendMsg(any) error            { return nil }
func (s *scriptedStream) RecvMsg(any) error            { return nil }

// kaFixture wires a frameConn to a scripted stream and a fake clock and runs
// a Receive-like read loop, as every real session has.
type kaFixture struct {
	fc     *frameConn
	clock  *fakeClock
	stream *scriptedStream
	data   chan []byte
	readEr chan error
}

func newKAFixture(t *testing.T, echo bool) *kaFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := newScriptedStream(ctx, echo)
	fc := &frameConn{
		stream:       st,
		cancelStream: cancel,
		route:        route{workspaceID: "ws", sessionID: "s", deviceID: "d"},
		direction:    relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON,
	}
	fx := &kaFixture{fc: fc, clock: newFakeClock(), stream: st, data: make(chan []byte, 8), readEr: make(chan error, 1)}
	fc.enableKeepalive(KeepaliveConfig{Clock: fx.clock}.withDefaults())
	t.Cleanup(fc.stopKeepalive)
	fx.startReader()
	return fx
}

// park waits until the keepalive loop is parked on the clock (it has armed n
// timers in total) and its last probe write has finished: probeAsync skips a
// tick while a write is still in flight, so advancing earlier could skip a
// probe the test expects.
func (fx *kaFixture) park(t *testing.T, n int) {
	t.Helper()
	fx.clock.waitArms(t, n)
	k := fx.fc.keepaliveState()
	waitUntil(t, "the last probe write to finish", func() bool {
		k.mu.Lock()
		defer k.mu.Unlock()
		return !k.sending
	})
}

func (fx *kaFixture) startReader() {
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := fx.fc.Read(buf)
			if err != nil {
				fx.readEr <- err
				return
			}
			fx.data <- append([]byte(nil), buf[:n]...)
		}
	}()
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestKeepaliveSentEvery30s(t *testing.T) {
	fx := newKAFixture(t, true)
	waitUntil(t, "first probe", func() bool { return fx.stream.keepalives() == 1 })
	fx.park(t, 1)
	for i, want := range []int{2, 3, 4} {
		fx.clock.Advance(29 * time.Second)
		if got := fx.stream.keepalives(); got != want-1 {
			t.Fatalf("step %d: %d keepalives after 29 s, want %d", i, got, want-1)
		}
		fx.clock.Advance(time.Second)
		waitUntil(t, "next keepalive", func() bool { return fx.stream.keepalives() == want })
		fx.park(t, i+2)
	}
	select {
	case d := <-fx.data:
		t.Fatalf("keepalive echo leaked to the application: %q", d)
	case err := <-fx.readEr:
		t.Fatalf("stream died while the relay was echoing: %v", err)
	default:
	}
}

func TestDeadStreamAfter75sOfSilence(t *testing.T) {
	fx := newKAFixture(t, true)
	waitUntil(t, "first probe echoed", func() bool { return fx.stream.keepalives() == 1 })
	// Give the reader the echo, then the relay goes silent.
	waitUntil(t, "echo consumed", func() bool { return len(fx.stream.recv) == 0 })
	fx.park(t, 1)
	fx.stream.setEcho(false)

	fx.clock.Advance(74 * time.Second)
	select {
	case err := <-fx.readEr:
		t.Fatalf("stream declared dead after 74 s: %v", err)
	default:
	}
	fx.clock.Advance(time.Second)
	select {
	case err := <-fx.readEr:
		if !errors.Is(err, ErrStreamDead) {
			t.Fatalf("Receive error = %v, want ErrStreamDead", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream not declared dead after 75 s of silence")
	}
	if got := fx.stream.keepalives(); got != 3 { // t=0, 30, 60 — none after death
		t.Fatalf("keepalives sent = %d, want 3", got)
	}
}

func TestAnyRelayFrameResetsTheDeadStreamTimer(t *testing.T) {
	fx := newKAFixture(t, true)
	waitUntil(t, "first probe echoed", func() bool { return fx.stream.keepalives() == 1 })
	waitUntil(t, "echo consumed", func() bool { return len(fx.stream.recv) == 0 })
	fx.park(t, 1)
	fx.stream.setEcho(false)

	fx.clock.Advance(60 * time.Second)
	fx.stream.recv <- &relaypb.Frame{Payload: []byte("data")}
	if got := string(<-fx.data); got != "data" {
		t.Fatalf("payload = %q", got)
	}
	fx.clock.Advance(74 * time.Second) // 74 s after the data frame, 134 s after start
	select {
	case err := <-fx.readEr:
		t.Fatalf("a data frame at t=60 should have reset the dead timer: %v", err)
	default:
	}
	fx.clock.Advance(time.Second)
	select {
	case err := <-fx.readEr:
		if !errors.Is(err, ErrStreamDead) {
			t.Fatalf("Receive error = %v, want ErrStreamDead", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream not declared dead 75 s after the last frame")
	}
}

func TestUnansweredProbeTurnsKeepaliveOff(t *testing.T) {
	// A relay that predates keepalive never echoes: after the probe window
	// no more probes are sent and the connection is never declared dead.
	fx := newKAFixture(t, false)
	waitUntil(t, "first probe", func() bool { return fx.stream.keepalives() == 1 })
	fx.park(t, 1)
	fx.clock.Advance(DefaultKeepaliveProbeTimeout + time.Second)
	fx.clock.Advance(10 * time.Minute)
	if got := fx.stream.keepalives(); got != 1 {
		t.Fatalf("keepalives = %d, want only the single probe", got)
	}
	select {
	case err := <-fx.readEr:
		t.Fatalf("a relay without keepalive support must not be declared dead: %v", err)
	default:
	}
	fx.stream.recv <- &relaypb.Frame{Payload: []byte("still works")}
	if got := string(<-fx.data); got != "still works" {
		t.Fatalf("payload = %q", got)
	}
}

func TestKeepaliveFollowsResumeToTheNewStream(t *testing.T) {
	fx := newKAFixture(t, true)
	waitUntil(t, "first probe echoed", func() bool { return fx.stream.keepalives() == 1 })
	waitUntil(t, "echo consumed", func() bool { return len(fx.stream.recv) == 0 })
	fx.park(t, 1)

	// Drop and resume: a new stream (the dead one's reader exits with an
	// error, as DataConn.Resume documents; start a fresh reader like Receive).
	fx.stream.mu.Lock()
	oldCancel := fx.fc.cancelStream
	fx.stream.mu.Unlock()
	oldCancel()
	<-fx.readEr
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	next := newScriptedStream(ctx, true)
	armsBefore := fx.clock.armCount()
	fx.fc.reopen(next, cancel)
	fx.startReader()

	waitUntil(t, "probe on the resumed stream", func() bool { return next.keepalives() >= 1 })
	fx.park(t, armsBefore+1)
	before := next.keepalives()
	fx.clock.Advance(DefaultKeepaliveInterval)
	waitUntil(t, "periodic keepalive on the resumed stream", func() bool { return next.keepalives() > before })
}

func (fx *kaFixture) supportState() kaSupport {
	k := fx.fc.keepaliveState()
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.support
}

func TestForwardedPeerProbeIsNotAnEcho(t *testing.T) {
	// A relay without keepalive support forwards a peer's probe to us. It is
	// a keepalive frame, but not an answer to ours (different nonce): it is
	// liveness evidence only and must not switch the dead-stream rule on.
	fx := newKAFixture(t, false)
	waitUntil(t, "first probe", func() bool { return fx.stream.keepalives() == 1 })
	fx.park(t, 1)
	nonce := fx.fc.keepaliveState().nonce
	fx.stream.recv <- &relaypb.Frame{Keepalive: true, Sequence: nonce + 1}
	waitUntil(t, "forwarded probe consumed", func() bool { return len(fx.stream.recv) == 0 })

	fx.clock.Advance(DefaultKeepaliveProbeTimeout + time.Second)
	fx.clock.Advance(10 * time.Minute)
	if got := fx.supportState(); got != kaNo {
		t.Fatalf("support = %v, want kaNo: a forwarded peer probe is not an echo", got)
	}
	select {
	case err := <-fx.readEr:
		t.Fatalf("a relay that never echoed must not be declared dead: %v", err)
	default:
	}
}

func TestKeepaliveReprobesAfterResumeWhenFirstEchoWasLate(t *testing.T) {
	fx := newKAFixture(t, false)
	waitUntil(t, "first probe", func() bool { return fx.stream.keepalives() == 1 })
	fx.park(t, 1)
	fx.clock.Advance(DefaultKeepaliveProbeTimeout + time.Second)
	waitUntil(t, "keepalive off for the first stream", func() bool { return fx.supportState() == kaNo })

	// Resume onto a stream whose relay does echo: the verdict was per stream.
	fx.stream.mu.Lock()
	oldCancel := fx.fc.cancelStream
	fx.stream.mu.Unlock()
	oldCancel()
	<-fx.readEr
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	next := newScriptedStream(ctx, true)
	fx.fc.reopen(next, cancel)
	fx.startReader()

	waitUntil(t, "probe on the resumed stream", func() bool { return next.keepalives() >= 1 })
	waitUntil(t, "echo recognised", func() bool { return fx.supportState() == kaYes })
}

func TestBlockedProbeWriteDoesNotDisableTheDeadStreamRule(t *testing.T) {
	fx := newKAFixture(t, true)
	waitUntil(t, "first probe echoed", func() bool { return fx.stream.keepalives() == 1 })
	waitUntil(t, "echo consumed", func() bool { return len(fx.stream.recv) == 0 })
	fx.park(t, 1)

	// The stream stalls: writes block (flow control on a half-open stream)
	// and nothing arrives. The loop must still declare it dead at 75 s.
	fx.stream.mu.Lock()
	fx.stream.blockSend = make(chan struct{})
	fx.stream.echo = false
	fx.stream.mu.Unlock()
	fx.clock.Advance(75 * time.Second)
	select {
	case err := <-fx.readEr:
		if !errors.Is(err, ErrStreamDead) {
			t.Fatalf("Receive error = %v, want ErrStreamDead", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a probe write stuck in Send stopped the dead-stream rule")
	}
}
