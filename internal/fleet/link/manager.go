package link

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/fleet/registry"
)

// State is where one outbound link stands.
type State string

// Link states.
const (
	StateConnecting       State = "connecting"
	StateConnected        State = "connected"
	StateAwaitingApproval State = "awaiting_approval"
	StateRefused          State = "refused"
	StatePeerDisabled     State = "peer_disabled"
	StateDown             State = "down"
)

// Status is a link's state with the reason for it. Code is the machine
// readable refusal code when there is one; Detail is safe to show a human
// and never carries a secret.
type Status struct {
	State  State
	Code   string
	Detail string
	Since  time.Time
}

// detailNotLinked is the Detail of the status of a host with no loop.
const detailNotLinked = "not linked"

// Defaults for ManagerOptions durations left zero.
const (
	defaultBackoffMin   = time.Second
	defaultBackoffMax   = time.Minute
	defaultApprovalPoll = 10 * time.Second
	handshakeTimeout    = 15 * time.Second
)

// ManagerOptions configures NewManager.
type ManagerOptions struct {
	Key      *identity.Key
	Registry *registry.Registry
	Secrets  *Secrets
	// Dial opens a connection; the default reads the host's secrets and
	// calls DialDirect.
	Dial    DialFunc
	Version string
	// BackoffMin and BackoffMax bound the redial delay after a failure;
	// ApprovalPoll is how often to ask a peer that has not approved yet.
	BackoffMin, BackoffMax, ApprovalPoll time.Duration
	Now                                  func() time.Time
	// OnConnected, if set, is called in the host's loop goroutine right after
	// a successful handshake with an active peer; it must not block.
	OnConnected func(hostID string, c Conn)
}

// Manager keeps one outbound link per host alive: it dials, runs the
// handshake, redials with backoff and checks the peer's fleet version on
// every connection.
type Manager struct {
	o      ManagerOptions
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu    sync.Mutex
	loops map[string]*hostLoop
}

type hostLoop struct {
	cancel  context.CancelFunc
	running bool
	status  Status
	conn    Conn
}

// NewManager returns a manager; no link is dialed until Ensure.
func NewManager(o ManagerOptions) *Manager {
	if o.BackoffMin <= 0 {
		o.BackoffMin = defaultBackoffMin
	}
	if o.BackoffMax < o.BackoffMin {
		o.BackoffMax = max(defaultBackoffMax, o.BackoffMin)
	}
	if o.ApprovalPoll <= 0 {
		o.ApprovalPoll = defaultApprovalPoll
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	m := &Manager{o: o, loops: map[string]*hostLoop{}}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	if m.o.Dial == nil {
		m.o.Dial = m.defaultDial
	}
	return m
}

// setOnConnected installs the connected hook unless ManagerOptions already
// carries one.
func (m *Manager) setOnConnected(f func(hostID string, c Conn)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.o.OnConnected == nil {
		m.o.OnConnected = f
	}
}

func (m *Manager) onConnected() func(hostID string, c Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.o.OnConnected
}

func (m *Manager) defaultDial(ctx context.Context, t Transport, hostID string) (Conn, error) {
	var entry Entry
	if m.o.Secrets != nil {
		e, _, err := m.o.Secrets.Get(hostID)
		if err != nil {
			return nil, err
		}
		entry = e
	}
	if t.Kind == KindRelay {
		return DialRelay(ctx, t, m.o.Key, entry, "fleet")
	}
	if t.Kind != KindDirect {
		return nil, fmt.Errorf("link: %s transport is not supported yet", t.Kind)
	}
	return DialDirect(ctx, t, entry.Password)
}

// Ensure starts the host's link loop unless one is already running.
func (m *Manager) Ensure(hostID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return
	}
	if l, ok := m.loops[hostID]; ok && l.running {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	l := &hostLoop{cancel: cancel, running: true, status: Status{State: StateConnecting, Since: m.o.Now()}}
	m.loops[hostID] = l
	m.wg.Add(1)
	go m.run(ctx, hostID, l)
}

// Drop stops the host's loop, closes its connection and forgets its status.
func (m *Manager) Drop(hostID string) {
	m.mu.Lock()
	l := m.loops[hostID]
	delete(m.loops, hostID)
	var c Conn
	if l != nil {
		l.cancel()
		c = l.conn
		l.conn = nil
	}
	m.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// Status returns the host's current link status.
func (m *Manager) Status(hostID string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.loops[hostID]; ok {
		return l.status
	}
	return Status{State: StateDown, Detail: detailNotLinked, Since: m.o.Now()}
}

// Conn returns the live connection to the host, only while it is connected.
func (m *Manager) Conn(hostID string) (Conn, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.loops[hostID]
	if !ok || l.status.State != StateConnected || l.conn == nil {
		return nil, false
	}
	return l.conn, true
}

// Test reports whether the host is reachable. A live, handshaken link is the
// answer by itself: dialing again would make the callee, which keeps one
// connection per peer, close the live one. Without a live link it makes one
// synchronous handshake attempt on a fresh connection and closes it again,
// leaving the loop's state alone. The error is for setup problems; a refusal
// by the peer is reported in the Status.
func (m *Manager) Test(ctx context.Context, hostID string) (Status, time.Duration, error) {
	m.mu.Lock()
	if l, ok := m.loops[hostID]; ok && l.status.State == StateConnected && l.conn != nil {
		st := l.status
		m.mu.Unlock()
		return st, 0, nil
	}
	m.mu.Unlock()
	start := time.Now()
	a := m.attempt(ctx, hostID, nil)
	if a.conn != nil {
		_ = a.conn.Close()
	}
	return a.status, time.Since(start), a.setupErr
}

// Close stops every loop, closes their connections and waits for them.
func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	var conns []Conn
	for _, l := range m.loops {
		if l.conn != nil {
			conns = append(conns, l.conn)
			l.conn = nil
		}
	}
	m.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	m.wg.Wait()
}

func (m *Manager) setStatus(l *hostLoop, s Status) {
	s.Since = m.o.Now()
	m.mu.Lock()
	l.status = s
	m.mu.Unlock()
}

// attemptResult is the outcome of one dial plus handshake.
type attemptResult struct {
	status   Status
	conn     Conn // non-nil only when connected
	retry    time.Duration
	stop     bool // needs a human; do not retry
	setupErr error
	host     registry.Host
	active   bool
}

// attempt reads the host, dials it and runs the handshake. onConn, if set,
// is told the connection as soon as it exists so Drop can close it.
func (m *Manager) attempt(ctx context.Context, hostID string, onConn func(Conn)) attemptResult {
	down := func(detail string, retry time.Duration) attemptResult {
		return attemptResult{status: Status{State: StateDown, Detail: detail}, retry: retry}
	}
	h, err := m.o.Registry.Get(hostID)
	if err != nil {
		r := down("host not found", 0)
		r.stop, r.setupErr = true, err
		return r
	}
	out, _, err := m.o.Registry.Links(hostID)
	if err != nil {
		r := down("link not found", 0)
		r.stop, r.setupErr = true, err
		return r
	}
	if !out.Enabled || (h.State != registry.StatePending && h.State != registry.StateApproved) {
		r := down("outbound disabled", 0)
		r.stop = true
		return r
	}
	t, err := ParseTransport(out.TransportJSON)
	if err != nil {
		r := down(err.Error(), m.o.BackoffMax)
		r.setupErr = err
		return r
	}
	c, err := m.o.Dial(ctx, t, hostID)
	if err != nil {
		return down(err.Error(), -1)
	}
	if onConn != nil {
		onConn(c)
	}
	hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	res, err := Handshake(hctx, c, m.o.Key, Peer{HostID: h.HostID, Pub: h.Pub, Alias: h.Alias}, m.o.Version, m.o.Now)
	r := m.classify(res, err)
	r.host = h
	if r.status.State == StateConnected {
		r.conn = c
		return r
	}
	_ = c.Close()
	return r
}

// classify turns a handshake outcome into a status and a retry policy. A
// retry of -1 means ordinary backoff.
func (m *Manager) classify(res HelloResult, err error) attemptResult {
	if err == nil {
		if res.State == "active" {
			return attemptResult{status: Status{State: StateConnected}, active: true}
		}
		return attemptResult{status: Status{State: StateAwaitingApproval, Code: hello.CodeAwaitingApproval, Detail: "peer reports state " + res.State}, retry: m.o.ApprovalPoll}
	}
	var ue *hello.UnsupportedError
	if errors.As(err, &ue) {
		return attemptResult{status: Status{State: StateRefused, Code: hello.CodeVersionUnsupported, Detail: ue.Guidance}, retry: m.o.BackoffMax}
	}
	var he *hello.Error
	if errors.As(err, &he) && he.Code == hello.CodePeerIdentityChanged {
		return attemptResult{status: Status{State: StateRefused, Code: he.Code, Detail: he.Msg}, stop: true}
	}
	code := ""
	switch {
	case errors.As(err, &he):
		code = he.Code
	default:
		var re *RefusedError
		if errors.As(err, &re) {
			code = re.Code
		}
	}
	switch code {
	case hello.CodeAwaitingApproval:
		return attemptResult{status: Status{State: StateAwaitingApproval, Code: code, Detail: "waiting for the peer to approve this host"}, retry: m.o.ApprovalPoll}
	case hello.CodeRevoked, hello.CodeNotGranted, hello.CodeUnknownHost:
		return attemptResult{status: Status{State: StatePeerDisabled, Code: code, Detail: "the peer has not enabled this link"}, retry: m.o.BackoffMax}
	}
	return attemptResult{status: Status{State: StateDown, Code: code, Detail: err.Error()}, retry: -1}
}

func (m *Manager) run(ctx context.Context, hostID string, l *hostLoop) {
	defer m.wg.Done()
	defer func() {
		m.mu.Lock()
		l.running = false
		m.mu.Unlock()
	}()
	backoff := m.o.BackoffMin
	for ctx.Err() == nil {
		m.setStatus(l, Status{State: StateConnecting})
		a := m.attempt(ctx, hostID, func(c Conn) {
			m.mu.Lock()
			l.conn = c
			m.mu.Unlock()
		})
		if ctx.Err() != nil {
			m.clearConn(l, a.conn)
			return
		}
		if a.conn == nil {
			m.clearConn(l, nil)
		}
		m.setStatus(l, a.status)
		if a.stop {
			return
		}
		if a.conn != nil {
			if a.host.State == registry.StatePending && a.active {
				if err := m.o.Registry.MarkApproved(hostID); err != nil && !errors.Is(err, registry.ErrBadState) {
					m.setStatus(l, Status{State: StateDown, Detail: "record approval: " + err.Error()})
				}
			}
			if a.active {
				if f := m.onConnected(); f != nil {
					f(hostID, a.conn)
				}
			}
			select {
			case <-a.conn.Done():
			case <-ctx.Done():
			}
			m.clearConn(l, a.conn)
			backoff = m.o.BackoffMin
			if !sleep(ctx, backoff) {
				return
			}
			continue
		}
		d := a.retry
		if d < 0 {
			d = backoff
			backoff = min(backoff*2, m.o.BackoffMax)
		}
		if !sleep(ctx, d) {
			return
		}
	}
}

// clearConn closes and forgets the loop's connection.
func (m *Manager) clearConn(l *hostLoop, c Conn) {
	m.mu.Lock()
	cur := l.conn
	l.conn = nil
	m.mu.Unlock()
	if cur != nil {
		_ = cur.Close()
	}
	if c != nil && c != cur {
		_ = c.Close()
	}
}

// sleep waits d or until ctx ends, reporting whether the wait completed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
