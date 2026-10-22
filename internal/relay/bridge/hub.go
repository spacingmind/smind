package bridge

// hub.go — the daemon's device hub (ADR-0031 D9, flows 1, 3, 5, 6, 7, 8).
//
// The relay tells the daemon, over the one control stream per relay host,
// which device routes are waiting for a daemon side (DeviceAttach / Sync /
// DeviceDetach). For each one the hub opens the daemon-side OpenData stream,
// completes the E2EE handshake as responder, checks the device's identity
// (device_hello, see hello.go) and then serves the device on its own
// wsapi.Transport — the same call a browser WebSocket gets, so every device
// sees all of the daemon's workspaces, spaces and tasks with its own
// subscriptions and permission prompts. One goroutine tree per device: a
// failure in one never touches another.
//
// Sessions outlive a relay connection: when the control stream or the gRPC
// connection dies, running sessions are only "orphaned" (their E2EE state is
// kept) and the next connection's Sync decides their fate — a listed route
// is resumed in place (client.DataConn.Resume), an unlisted one means the
// device is gone and the session is dropped. Sync is therefore the
// authoritative list of waiting routes.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/wsapi"
)

// Server is what the hub serves each device with: wsapi.API in the daemon, a
// stub in tests.
type Server interface {
	ServeTransport(ctx context.Context, tr wsapi.Transport)
}

// DeviceServer is implemented by servers that want the verified device id (wsapi.API does). The hub prefers it over Server.ServeTransport for an identified device.
type DeviceServer interface {
	ServeDevice(ctx context.Context, tr wsapi.Transport, deviceID string)
}

const (
	// DefaultMaxDevices bounds concurrently SERVED device sessions (the relay
	// bounds attached devices the same way, ADR-0031 limits).
	DefaultMaxDevices = 16

	// pendingSessionSlack is how many sessions beyond MaxDevices may exist
	// before a device has proven who it is (handshake or hello in progress,
	// or about to be refused), so a refused 17th device still gets an answer
	// without letting unauthenticated routes grow without bound.
	pendingSessionSlack = 8

	// DefaultHelloTimeout is how long a freshly handshaken device may stay
	// silent before it is served as a legacy client.
	DefaultHelloTimeout = 5 * time.Second

	handshakeTimeout = 30 * time.Second

	// syncGrace is how long after the first Pong the hub waits for a Sync
	// before concluding the relay predates multi-device (ADR-0031 P12a).
	syncGrace = 3 * time.Second

	// orphanTTL is how long a session whose relay connection died is kept
	// waiting for the next connection's Sync (the relay's route grace is 5
	// minutes; keep the E2EE state at least that long).
	orphanTTL = 6 * time.Minute

	// maxResumeFailures is how many consecutive failed resumes of one session
	// on a live relay connection are tolerated before it is dropped.
	maxResumeFailures = 3

	controlPingInterval = client.DefaultKeepaliveInterval
	controlDeadAfter    = client.DefaultKeepaliveDeadAfter
)

// Options configures a Hub. The zero value is usable for tests.
type Options struct {
	// Registry holds approved/pending/revoked devices. Nil disables
	// device_hello (every hello is refused as unsupported) — only legacy
	// clients can then be served.
	Registry *devices.Registry
	// LegacyDevices serves a client that sends no device_hello as one
	// unlisted "legacy" device on the fixed legacy route (ADR-0031 P12b AC 7).
	// Off by default in the daemon (decided 2026-10-15): while it is on, that
	// route is served without approval and cannot be revoked.
	LegacyDevices bool
	// MaxDevices bounds served device sessions (0 = DefaultMaxDevices).
	MaxDevices int
	// HelloTimeout (0 = DefaultHelloTimeout).
	HelloTimeout time.Duration
	// Clock drives keepalives, the control watchdog, backoff and orphan
	// expiry; nil = real time. Tests inject a fake.
	Clock client.Clock
	// Jitter spreads a backoff delay; nil = ±20% random.
	Jitter func(time.Duration) time.Duration
	// Logf receives hub diagnostics; nil = log.Printf. Never given keys,
	// secrets or message content.
	Logf func(format string, args ...any)
	// Password gates every device_hello with the daemon password (P13c). Nil,
	// or a gate that is not Required, admits devices without one.
	Password PasswordGate
}

// Hub is the device hub. Create with NewHub, run with (*Hub).Run.
type Hub struct {
	opts    Options
	clock   client.Clock
	jitter  func(time.Duration) time.Duration
	logf    func(string, ...any)
	maxDevs int
	hello   time.Duration
	pw      *passwordState

	mu       sync.Mutex
	server   Server
	hubCtx   context.Context
	kp       *e2ee.KeyPair
	cfg      Config
	sessions map[routeRef]*session
	conn     *relayConn // the live relay connection, nil between connections
}

// routeRef names one relay route of this daemon's relay host.
type routeRef struct{ device, session string }

// NewHub builds a hub. It does nothing until Run.
func NewHub(opts Options) *Hub {
	h := &Hub{opts: opts, sessions: make(map[routeRef]*session)}
	h.clock = opts.Clock
	if h.clock == nil {
		h.clock = realClock{}
	}
	h.jitter = opts.Jitter
	if h.jitter == nil {
		h.jitter = defaultJitter
	}
	h.logf = opts.Logf
	if h.logf == nil {
		h.logf = log.Printf
	}
	h.maxDevs = opts.MaxDevices
	if h.maxDevs <= 0 {
		h.maxDevs = DefaultMaxDevices
	}
	h.hello = opts.HelloTimeout
	if h.hello <= 0 {
		h.hello = DefaultHelloTimeout
	}
	h.pw = newPasswordState(h.clock.Now)
	return h
}

func defaultJitter(d time.Duration) time.Duration {
	// ±20%: enough to desynchronise a fleet reconnecting after a relay
	// restart without making any single wait unpredictable.
	f := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(d) * f)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) At(t time.Time) (<-chan time.Time, func()) {
	tm := time.NewTimer(time.Until(t))
	return tm.C, func() { tm.Stop() }
}

// sleep waits d on the hub clock, returning false if ctx ends first.
func (h *Hub) sleep(ctx context.Context, d time.Duration) bool {
	ch, release := h.clock.At(h.clock.Now().Add(d))
	defer release()
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}

// Run dials the relay named by cfg, admits, and serves devices through the
// control stream until ctx ends, reconnecting with jittered backoff. kp is
// the daemon's long-lived E2EE identity; srv serves each device. It never
// returns an error for a transient relay failure (the daemon treats the relay
// as a best-effort background service).
func (h *Hub) Run(ctx context.Context, cfg Config, kp *e2ee.KeyPair, srv Server) error {
	secret, err := decodeSecret(cfg)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.server, h.hubCtx, h.kp, h.cfg = srv, ctx, kp, cfg
	h.mu.Unlock()
	defer h.shutdown()
	h.warnNoPassword()

	backoff := time.Second
	for ctx.Err() == nil {
		start := h.clock.Now()
		err := h.runConn(ctx, cfg, kp, secret)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			h.logf("relay bridge: %v", err)
		}
		if h.clock.Now().Sub(start) >= minStableConnection {
			backoff = time.Second
		}
		if !h.sleep(ctx, h.jitter(backoff)) {
			return nil
		}
		backoff = nextBackoff(backoff)
	}
	return nil
}

func (h *Hub) shutdown() {
	h.mu.Lock()
	ss := make([]*session, 0, len(h.sessions))
	for _, s := range h.sessions {
		ss = append(ss, s)
	}
	h.sessions = make(map[routeRef]*session)
	h.conn = nil
	h.mu.Unlock()
	for _, s := range ss {
		s.cancel()
	}
}

// relayConn is one admitted relay connection with its control stream.
type relayConn struct {
	h           *Hub
	ctx         context.Context
	cancel      context.CancelFunc
	rc          relaypb.RelayClient
	admissionID string
	ctl         relaypb.Relay_OpenControlClient
	ctlSendMu   sync.Mutex

	mu       sync.Mutex
	lastRecv time.Time
	multi    bool // the relay sent a Sync: it supports multi-device
}

func (c *relayConn) noteRecv() {
	c.mu.Lock()
	c.lastRecv = c.h.clock.Now()
	c.mu.Unlock()
}

func (c *relayConn) sendControl(cf *relaypb.ControlFrame) error {
	c.ctlSendMu.Lock()
	defer c.ctlSendMu.Unlock()
	return c.ctl.Send(cf)
}

// runConn runs one relay connection to its end (the control stream died, it
// went silent for 75 s, or ctx ended).
func (h *Hub) runConn(ctx context.Context, cfg Config, kp *e2ee.KeyPair, secret []byte) error {
	grpcConn, admissionID, err := dialAndAdmit(cfg, DaemonKeyID(kp), secret)
	if err != nil {
		return err
	}
	defer grpcConn.Close()
	return h.serveConn(ctx, cfg, grpcConn, admissionID)
}

func (h *Hub) serveConn(ctx context.Context, cfg Config, grpcConn *grpc.ClientConn, admissionID string) error {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	rc := relaypb.NewRelayClient(grpcConn)
	ctl, err := client.OpenControl(cctx, rc, admissionID, cfg.WorkspaceID)
	if err != nil {
		return err
	}
	c := &relayConn{h: h, ctx: cctx, cancel: cancel, rc: rc, admissionID: admissionID, ctl: ctl, lastRecv: h.clock.Now()}
	h.mu.Lock()
	h.conn = c
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.conn == c {
			h.conn = nil
		}
		h.mu.Unlock()
	}()
	h.logf("relay bridge: connected to %s (relay host %s)", cfg.RelayAddress, cfg.WorkspaceID)

	pongSeen := make(chan struct{})
	var pongOnce sync.Once
	go func() {
		defer cancel()
		for {
			cf, err := ctl.Recv()
			if err != nil {
				return
			}
			c.noteRecv()
			switch body := cf.GetBody().(type) {
			case *relaypb.ControlFrame_Pong_:
				pongOnce.Do(func() { close(pongSeen) })
			case *relaypb.ControlFrame_Sync_:
				c.mu.Lock()
				c.multi = true
				c.mu.Unlock()
				h.onSync(c, body.Sync.GetDevices())
			case *relaypb.ControlFrame_DeviceAttach_:
				a := body.DeviceAttach
				h.ensure(c, routeRef{a.GetDeviceId(), string(a.GetSessionId())})
			case *relaypb.ControlFrame_DeviceDetach_:
				d := body.DeviceDetach
				h.detach(routeRef{d.GetDeviceId(), string(d.GetSessionId())})
			}
		}
	}()

	// A relay from before multi-device never sends Sync: after the first
	// Pong, wait a moment and fall back to the old single-device behaviour
	// (open the fixed default route proactively).
	go func() {
		select {
		case <-pongSeen:
		case <-cctx.Done():
			return
		}
		if !h.sleep(cctx, syncGrace) {
			return
		}
		c.mu.Lock()
		multi := c.multi
		c.mu.Unlock()
		if !multi {
			h.logf("relay bridge: relay does not announce devices (older relay); serving the single legacy device only")
			h.ensure(c, routeRef{DefaultDeviceID, DefaultSessionID})
		}
	}()

	// Control liveness: ping every 30 s; nothing from the relay for 75 s =
	// dead (a proxy swallowing the stream), so reconnect.
	next := h.clock.Now().Add(controlPingInterval)
	var seq uint64
	for {
		c.mu.Lock()
		deadline := c.lastRecv.Add(controlDeadAfter)
		c.mu.Unlock()
		wake := next
		if deadline.Before(wake) {
			wake = deadline
		}
		ch, release := h.clock.At(wake)
		select {
		case <-cctx.Done():
			release()
			return nil
		case <-ch:
		}
		now := h.clock.Now()
		c.mu.Lock()
		dead := !now.Before(c.lastRecv.Add(controlDeadAfter))
		c.mu.Unlock()
		if dead {
			return errors.New("control stream silent for 75 s; reconnecting")
		}
		if !now.Before(next) {
			seq++
			if err := c.sendControl(&relaypb.ControlFrame{
				WorkspaceId: cfg.WorkspaceID,
				Body:        &relaypb.ControlFrame_Ping_{Ping: &relaypb.ControlFrame_Ping{Sequence: seq}},
			}); err != nil {
				return fmt.Errorf("control ping: %w", err)
			}
			next = now.Add(controlPingInterval)
		}
	}
}

// onSync handles the relay's list of routes waiting for a daemon side:
// listed routes get (or resume) a session. Absence is NOT proof a device is
// gone — a route whose old daemon side the relay has not yet noticed dead
// (a half-open connection) is not listed either — so an orphaned session that
// is missing here is kept until its orphan TTL or a DeviceDetach.
func (h *Hub) onSync(c *relayConn, refs []*relaypb.ControlFrame_DeviceRef) {
	h.logf("relay bridge: sync: %d waiting route(s)", len(refs))
	for _, r := range refs {
		h.ensure(c, routeRef{r.GetDeviceId(), string(r.GetSessionId())})
	}
	// Revocations are per control stream at the relay: re-send them on every
	// (re)connect. Only now — a Sync proves the relay speaks Revoke (an older
	// relay would kill the stream with Unimplemented).
	h.resendRevocations(c)
}

// ensure starts (or resumes) the session for ref on connection c. It is
// idempotent: an attach for a session that is already running is ignored.
func (h *Hub) ensure(c *relayConn, ref routeRef) {
	h.mu.Lock()
	s := h.sessions[ref]
	if s == nil {
		if len(h.sessions) >= h.maxDevs+pendingSessionSlack {
			h.mu.Unlock()
			h.logf("relay bridge: too many device sessions; ignoring attach")
			return
		}
		s = h.newSessionLocked(ref)
	} else if s.running {
		// Idempotent for the same connection. A DIFFERENT (newer) connection
		// announcing a route whose old runner is still unwinding must not be
		// lost — the relay will not say it again — so the runner picks it up
		// when it exits.
		if s.conn != c {
			s.pending = c
		}
		h.mu.Unlock()
		return
	}
	s.running, s.orphaned = true, false
	h.mu.Unlock()
	h.logf("relay bridge: starting session for a device route")
	go h.runSession(s, c)
}

// detach drops the session for ref: the device side ended with no daemon
// side, or the relay collected the route. Idempotent; unknown refs are no-ops.
func (h *Hub) detach(ref routeRef) {
	h.mu.Lock()
	s := h.sessions[ref]
	h.mu.Unlock()
	if s != nil {
		h.drop(s)
	}
}

// drop cancels a session and forgets it.
func (h *Hub) drop(s *session) {
	h.mu.Lock()
	if h.sessions[s.ref] == s {
		delete(h.sessions, s.ref)
	}
	h.mu.Unlock()
	s.cancel()
}

// runSession is the goroutine tree of one device: open or resume the
// daemon-side stream, handshake, identify, serve; on a transport failure
// over a live relay connection resume in place with jittered backoff; on a
// dead connection stay orphaned for the next Sync.
func (h *Hub) runSession(s *session, c *relayConn) {
	h.setSessionConn(s, c)
	failures := 0
	backoff := time.Second
	for {
		started := h.clock.Now()
		err := h.serveSession(s, c)
		switch {
		case s.ctx.Err() != nil:
			h.drop(s)
			return
		case errors.Is(err, errSessionOver):
			h.drop(s)
			return
		case c.ctx.Err() != nil:
			// The relay connection died under a healthy session: keep its
			// E2EE state and wait for the next connection's Sync — unless a
			// newer connection already announced this route.
			h.mu.Lock()
			if next := s.pending; next != nil && next.ctx.Err() == nil {
				s.pending, s.conn = nil, next
				h.mu.Unlock()
				h.logf("relay bridge: resuming device session on the newer relay connection")
				c = next
				failures, backoff = 0, time.Second
				continue
			}
			s.pending = nil
			s.running, s.orphaned = false, true
			h.logf("relay bridge: relay connection lost; device session kept for the next sync")
			s.orphanGen++
			gen := s.orphanGen
			h.mu.Unlock()
			h.expireOrphan(s, gen)
			return
		}
		if s.dc == nil || (err != nil && isFatalSessionErr(err)) {
			// Never established (handshake failed) or the session itself is
			// corrupt: there is nothing to resume.
			h.logf("relay bridge: device session dropped: %v", err)
			h.drop(s)
			return
		}
		if h.clock.Now().Sub(started) >= minStableConnection {
			failures, backoff = 0, time.Second
		} else {
			failures++
		}
		if failures >= maxResumeFailures {
			h.logf("relay bridge: device session could not be resumed; dropping")
			h.drop(s)
			return
		}
		if !h.sleep(s.ctx, h.jitter(backoff)) {
			h.drop(s)
			return
		}
		backoff = nextBackoff(backoff)
	}
}

// expireOrphan drops s if it is still in the same orphaning (gen) after
// orphanTTL — a session orphaned, resumed and orphaned again is on a new clock.
func (h *Hub) expireOrphan(s *session, gen int) {
	go func() {
		if !h.sleep(s.ctx, orphanTTL) {
			return
		}
		h.mu.Lock()
		still := s.orphaned && s.orphanGen == gen && h.sessions[s.ref] == s
		h.mu.Unlock()
		if still {
			h.drop(s)
		}
	}()
}

// errSessionOver marks an orderly end of a session (the device was refused,
// revoked, or the server closed the transport): drop it, do not resume.
var errSessionOver = errors.New("relay bridge: device session over")

func isFatalSessionErr(err error) bool {
	return errors.Is(err, e2ee.ErrDecrypt) || errors.Is(err, e2ee.ErrReplay) ||
		errors.Is(err, e2ee.ErrCounterExhausted) || errors.Is(err, e2ee.ErrProtocol) ||
		errors.Is(err, e2ee.ErrPeerKeyMismatch) || errors.Is(err, e2ee.ErrKeyRotation) ||
		errors.Is(err, e2ee.ErrHandshakeConfirm) || errors.Is(err, e2ee.ErrUnsupportedVersion)
}

// serveSession runs one attach of a session on connection c: open/resume the
// stream, (first time) identify the device, serve until the transport ends.
func (h *Hub) serveSession(s *session, c *relayConn) (err error) {
	var r *sessionReader
	defer func() {
		// A session that is over (cancelled, refused, closed by the server)
		// releases its DataConn — and with it the keepalive loop — once the
		// reader has ended (Close must never run under a pending Receive).
		if s.dc != nil && r != nil && (s.ctx.Err() != nil || errors.Is(err, errSessionOver)) {
			s.cancel()
			<-r.done
			_ = s.dc.Close()
		}
	}()
	if s.dc == nil {
		dc, err := client.OpenDaemonData(s.ctx, c.rc, c.admissionID, h.cfg.WorkspaceID, s.ref.session, s.ref.device, h.kp, client.WithKeepalive(h.keepaliveConfig()))
		if err != nil {
			return fmt.Errorf("open data: %w", err)
		}
		hctx, cancel := context.WithTimeout(s.ctx, handshakeTimeout)
		err = dc.Handshake(hctx)
		cancel()
		if err != nil {
			// dc is unusable; s.dc stays nil so the caller drops the session.
			go func() { _ = dc.Close() }()
			return fmt.Errorf("handshake: %w", err)
		}
		s.dc = dc
	} else if err := s.dc.Resume(s.ctx, c.rc, c.admissionID); err != nil {
		return fmt.Errorf("resume: %w", err)
	}

	r = newSessionReader(s.ctx, s.dc)
	defer r.stop()
	tr := &deviceTransport{s: s, r: r}

	if !s.identified {
		if err := h.identify(s, r, tr); err != nil {
			return err
		}
		s.identified = true
	}
	h.setServing(s, true)
	defer h.setServing(s, false)
	h.mu.Lock()
	srv := h.server
	id := s.deviceID
	h.mu.Unlock()
	if ds, ok := srv.(DeviceServer); ok && id != "" {
		ds.ServeDevice(s.ctx, tr, id)
	} else {
		srv.ServeTransport(s.ctx, tr)
	}
	if tr.closedByServer() {
		return errSessionOver
	}
	return r.err()
}

func (h *Hub) keepaliveConfig() client.KeepaliveConfig {
	return client.KeepaliveConfig{Clock: h.clock}
}

func (h *Hub) setServing(s *session, v bool) {
	h.mu.Lock()
	s.serving = v
	h.mu.Unlock()
}

func decodeSecret(cfg Config) ([]byte, error) {
	secret, err := decodeHex(cfg.SecretHex)
	if err != nil {
		return nil, fmt.Errorf("bridge: decode secret: %w", err)
	}
	return secret, nil
}
