package client

// keepalive.go — application-level liveness for OpenData streams (ADR-0031
// D9, flow 8). A proxy in front of the relay (Cloudflare: 125 s read timeout)
// drops a stream that carries no bytes, so a connected-but-idle session sends
// a Frame{keepalive} every 30 s, which the relay answers with a keepalive
// frame on the same stream; a stream the relay has not said anything on for
// 75 s is treated as dead so the caller reconnects (DataConn.Resume) instead
// of waiting on a connection nothing is flowing through.
//
// It is opt-in (WithKeepalive) and negotiated by probing: a relay from before
// this feature would forward the probe to the peer instead of echoing it, so
// the first probe must be echoed within ProbeTimeout or keepalive stays off
// for the connection (the peer ignores a keepalive frame it receives, and the
// old relay's forwarding of an empty frame is harmless to the E2EE reader).
//
// The echo is consumed by DataConn.Receive's read path, so a connection using
// WithKeepalive needs a Receive loop running, as every real session has.

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

const (
	// DefaultKeepaliveInterval is how often an idle session probes the relay.
	DefaultKeepaliveInterval = 30 * time.Second
	// DefaultKeepaliveDeadAfter is how long a stream may stay silent (no frame
	// of any kind from the relay) before it is declared dead. Two missed
	// intervals plus slack; below every proxy idle timeout we care about.
	DefaultKeepaliveDeadAfter = 75 * time.Second
	// DefaultKeepaliveProbeTimeout is how long the first probe may go
	// unanswered before the relay is assumed not to support keepalive.
	DefaultKeepaliveProbeTimeout = 10 * time.Second
)

// ErrStreamDead is returned by Receive/Send once the dead-stream rule fires.
// The session is intact: reconnect with DataConn.Resume.
var ErrStreamDead = errors.New("relay client: stream dead: nothing received from the relay within the keepalive window")

// Clock is the time source of the keepalive loop, injectable for tests.
type Clock interface {
	Now() time.Time
	// At returns a channel that fires once the clock reaches t (immediately if
	// it already has) and a function that releases the timer early. An
	// absolute deadline, so a loop that computed it from an earlier reading
	// of Now cannot oversleep.
	At(t time.Time) (<-chan time.Time, func())
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) At(at time.Time) (<-chan time.Time, func()) {
	t := time.NewTimer(time.Until(at))
	return t.C, func() { t.Stop() }
}

// KeepaliveConfig tunes WithKeepalive; zero values take the defaults.
type KeepaliveConfig struct {
	Interval     time.Duration
	DeadAfter    time.Duration
	ProbeTimeout time.Duration
	Clock        Clock
}

func (c KeepaliveConfig) withDefaults() KeepaliveConfig {
	if c.Interval <= 0 {
		c.Interval = DefaultKeepaliveInterval
	}
	if c.DeadAfter <= 0 {
		c.DeadAfter = DefaultKeepaliveDeadAfter
	}
	if c.ProbeTimeout <= 0 {
		c.ProbeTimeout = DefaultKeepaliveProbeTimeout
	}
	if c.Clock == nil {
		c.Clock = realClock{}
	}
	return c
}

// WithKeepalive enables the keepalive loop on the connection (see the file
// comment). It starts immediately and follows the connection across Resume.
func WithKeepalive(cfg KeepaliveConfig) DataOption {
	return func(d *DataConn) { d.frames.enableKeepalive(cfg.withDefaults()) }
}

type kaSupport uint8

const (
	kaUnknown kaSupport = iota
	kaYes
	kaNo
)

// keepalive is the per-connection state shared by the read path (which notes
// what the relay sent) and the loop goroutine.
type keepalive struct {
	cfg KeepaliveConfig

	// nonce identifies this connection's probes: it rides in Frame.sequence and
	// the relay echoes it back, so an echo can be told from a probe a peer sent
	// that a relay without keepalive support merely forwarded (also a keepalive
	// frame, but with the peer's nonce). Only an echo proves support.
	nonce uint64

	mu       sync.Mutex
	lastRecv time.Time
	support  kaSupport
	dead     bool
	stop     chan struct{} // closes the running loop; also its generation
	sending  bool          // a probe write is in flight
}

// noteRecv records that the relay sent a frame: any frame is evidence the
// stream is alive, and a keepalive frame carrying our nonce is an echo.
func (k *keepalive) noteRecv(f *relaypb.Frame) {
	k.mu.Lock()
	k.lastRecv = k.cfg.Clock.Now()
	if f.GetKeepalive() && f.GetSequence() == k.nonce {
		k.support = kaYes
	}
	k.mu.Unlock()
}

func (f *frameConn) enableKeepalive(cfg KeepaliveConfig) {
	k := &keepalive{cfg: cfg}
	var b [8]byte
	for k.nonce == 0 {
		_, _ = rand.Read(b[:])
		k.nonce = binary.BigEndian.Uint64(b[:])
	}
	f.ctlMu.Lock()
	f.ka = k
	f.ctlMu.Unlock()
	f.startKeepaliveLoop()
}

// startKeepaliveLoop (re)starts the loop for the current stream. Called when
// keepalive is enabled and after every reopen; the previous loop is stopped.
func (f *frameConn) startKeepaliveLoop() {
	k := f.keepaliveState()
	if k == nil {
		return
	}
	k.mu.Lock()
	if k.stop != nil {
		close(k.stop)
	}
	stop := make(chan struct{})
	k.stop = stop
	k.lastRecv = k.cfg.Clock.Now()
	k.dead = false
	if k.support == kaNo {
		// "No echo" is a verdict on one stream, not on the relay for good: a
		// slow first echo must not switch protection off for the session's
		// lifetime, so every (re)opened stream probes once more.
		k.support = kaUnknown
	}
	k.mu.Unlock()
	go f.keepaliveLoop(k, stop)
}

func (f *frameConn) stopKeepalive() {
	k := f.keepaliveState()
	if k == nil {
		return
	}
	k.mu.Lock()
	if k.stop != nil {
		close(k.stop)
		k.stop = nil
	}
	k.mu.Unlock()
}

func (f *frameConn) keepaliveLoop(k *keepalive, stop chan struct{}) {
	clock := k.cfg.Clock
	k.mu.Lock()
	probing := k.support == kaUnknown
	k.mu.Unlock()
	start := clock.Now()
	nextSend := start // first probe immediately
	for {
		k.mu.Lock()
		support, last := k.support, k.lastRecv
		k.mu.Unlock()
		if support == kaNo {
			return
		}
		now := clock.Now()
		if support == kaUnknown && probing && !now.Before(start.Add(k.cfg.ProbeTimeout)) {
			// The relay never echoed the probe: it predates keepalive.
			k.mu.Lock()
			k.support = kaNo
			k.mu.Unlock()
			return
		}
		if support == kaYes && !now.Before(last.Add(k.cfg.DeadAfter)) {
			f.killStream(k, stop)
			return
		}
		if !now.Before(nextSend) {
			f.probeAsync(k)
			nextSend = now.Add(k.cfg.Interval)
		}
		// Sleep until the earliest of: next probe, probe deadline, dead line.
		wake := nextSend
		switch support {
		case kaUnknown:
			if probing {
				if d := start.Add(k.cfg.ProbeTimeout); d.Before(wake) {
					wake = d
				}
			}
		case kaYes:
			if d := last.Add(k.cfg.DeadAfter); d.Before(wake) {
				wake = d
			}
		}
		ch, release := clock.At(wake)
		select {
		case <-stop:
			release()
			return
		case <-ch:
		}
	}
}

// probeAsync writes one probe on the current stream without blocking the
// loop: a Send stalled by flow control on a half-open stream must not stop
// the dead-stream rule from firing. At most one probe is in flight; a
// stalled one makes later ticks skip rather than pile up goroutines.
func (f *frameConn) probeAsync(k *keepalive) {
	k.mu.Lock()
	if k.sending {
		k.mu.Unlock()
		return
	}
	k.sending = true
	k.mu.Unlock()
	go func() {
		f.sendMu.Lock()
		if !f.closed {
			_ = f.stream.Send(&relaypb.Frame{
				WorkspaceId: f.route.workspaceID,
				SessionId:   []byte(f.route.sessionID),
				DeviceId:    f.route.deviceID,
				Direction:   f.direction,
				Sequence:    k.nonce,
				Keepalive:   true,
			})
		}
		f.sendMu.Unlock()
		k.mu.Lock()
		k.sending = false
		k.mu.Unlock()
	}()
}

// killStream applies the dead-stream rule: remember why, then cancel the
// stream's context so a blocked Receive returns.
// A loop that was superseded meanwhile (Resume swapped in a new stream and
// restarted it) must not cancel the new stream, hence the generation check.
func (f *frameConn) killStream(k *keepalive, gen chan struct{}) {
	k.mu.Lock()
	if k.stop != gen {
		k.mu.Unlock()
		return
	}
	k.dead = true
	k.mu.Unlock()
	f.ctlMu.Lock()
	cancel := f.cancelStream
	f.ctlMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// streamErr maps a transport error to ErrStreamDead when the dead-stream
// rule caused it.
func (f *frameConn) streamErr(err error) error {
	k := f.keepaliveState()
	if k == nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.dead {
		return ErrStreamDead
	}
	return err
}

// keepaliveState returns the keepalive state, or nil when not enabled.
func (f *frameConn) keepaliveState() *keepalive {
	f.ctlMu.Lock()
	defer f.ctlMu.Unlock()
	return f.ka
}
