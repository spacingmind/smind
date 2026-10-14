package relaytest

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// EventKind classifies a control frame the Daemon stub received.
type EventKind int

const (
	// EventAttach is a ControlFrame.DeviceAttach.
	EventAttach EventKind = iota + 1
	// EventDetach is a ControlFrame.DeviceDetach.
	EventDetach
	// EventSync is a ControlFrame.Sync.
	EventSync
	// EventControlEnded is the end of a control stream (Err says why).
	EventControlEnded
)

// DeviceRef names one device route: the E2EE session id is part of the
// identity (a device that re-handshakes gets a new one).
type DeviceRef struct {
	DeviceID  string
	SessionID string
}

// ControlEvent is one thing the control stream delivered to the stub.
type ControlEvent struct {
	Kind EventKind
	// Stream numbers the control stream (1 = the first Connect) so a test can
	// tell the Sync of a reconnect from an earlier one.
	Stream int
	// DeviceID and SessionID are set for Attach and Detach.
	DeviceID  string
	SessionID string
	// Devices is the Sync's route list.
	Devices []DeviceRef
	// Err is set for EventControlEnded.
	Err error
}

// ReplyTo is what the stub answers to a device's plaintext: derived from both
// the device id and the message, so a test can tell that a reply was produced
// for this device's message and delivered to this device only.
func ReplyTo(deviceID string, plaintext []byte) []byte {
	return append([]byte("reply/"+deviceID+"/"), plaintext...)
}

type routeState int

const (
	routeOpening  routeState = iota // handshake in progress
	routeLive                       // handshaken, transport up
	routeDown                       // handshaken, transport dropped, session kept
	routeResuming                   // reopening the transport
)

type daemonRoute struct {
	ref   DeviceRef
	state routeState
	ep    *endpoint
}

// Daemon is a bridge-like stub for the relay's device hub: it admits, keeps
// the control stream, records what it receives, and for every DeviceAttach /
// Sync entry opens the daemon side of that route (E2EE responder), then
// answers each device message with ReplyTo. It is NOT the bridge (P12b).
//
// Reconnect semantics, chosen for what DataConn can actually do: a route whose
// transport dropped keeps its E2EE session and is RESUMED (DataConn.Resume)
// when the relay announces it again -- a fresh handshake on the same
// session id is impossible, because the device's established Channel only
// sends a hello inside Handshake. After a real daemon process restart the
// session state is gone and a device must open a new session (new session id),
// which arrives as an ordinary DeviceAttach and a fresh handshake.
type Daemon struct {
	t     testing.TB
	relay *Relay
	rc    relaypb.RelayClient
	kp    *e2ee.KeyPair

	admID string

	mu       sync.Mutex
	closed   bool
	stream   int // generation of the current control stream
	ctrl     relaypb.Relay_OpenControlClient
	ctrlStop context.CancelFunc
	ctrlDone chan struct{}
	routes   map[DeviceRef]*daemonRoute
	events   []ControlEvent
	changed  chan struct{} // closed and replaced whenever state changes
	errs     []error       // route handshake/resume failures

	sendMu sync.Mutex // one sender on the control stream
	wg     sync.WaitGroup
}

// NewDaemon admits a daemon to the relay host over native gRPC and returns the
// stub; call Connect to open the control stream. It is closed when the test
// ends.
func NewDaemon(t testing.TB, r *Relay) *Daemon {
	t.Helper()
	kp, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatalf("relaytest: daemon keypair: %v", err)
	}
	d := &Daemon{
		t: t, relay: r, kp: kp,
		rc:      NativeClient(t, r),
		routes:  make(map[DeviceRef]*daemonRoute),
		changed: make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	d.admID, err = client.Admit(ctx, d.rc, r.HostID, "daemon-key-1", r.Secret)
	if err != nil {
		t.Fatalf("relaytest: daemon admit: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// PublicKey is the daemon's E2EE public key, what a pairing offer carries and
// a device pins.
func (d *Daemon) PublicKey() []byte { return d.kp.Public() }

// notifyLocked wakes everyone waiting on a state change. Caller holds d.mu.
func (d *Daemon) notifyLocked() {
	close(d.changed)
	d.changed = make(chan struct{})
}

// Connect opens a control stream (client.OpenControl) and starts reacting to
// it. A previous stream must have been dropped (DropControl).
func (d *Daemon) Connect() error {
	d.mu.Lock()
	if d.closed || d.ctrl != nil {
		d.mu.Unlock()
		return fmt.Errorf("relaytest: daemon closed or control already connected")
	}
	d.stream++
	gen := d.stream
	ctx, cancel := context.WithCancel(context.Background())
	d.mu.Unlock()

	stream, err := client.OpenControl(ctx, d.rc, d.admID, d.relay.HostID)
	if err != nil {
		cancel()
		return err
	}
	done := make(chan struct{})
	d.mu.Lock()
	d.ctrl, d.ctrlStop, d.ctrlDone = stream, cancel, done
	d.mu.Unlock()
	go func() {
		defer close(done)
		for {
			cf, err := stream.Recv()
			if err != nil {
				d.record(ControlEvent{Kind: EventControlEnded, Stream: gen, Err: err})
				return
			}
			d.handle(gen, cf)
		}
	}()
	return nil
}

// MustConnect is Connect failing the test on error, then waits for the
// relay's initial Sync so later assertions start from a known point.
func (d *Daemon) MustConnect(t testing.TB) {
	t.Helper()
	if err := d.Connect(); err != nil {
		t.Fatalf("relaytest: daemon control connect: %v", err)
	}
	d.mu.Lock()
	gen := d.stream
	d.mu.Unlock()
	d.WaitEvent(t, "initial Sync", func(e ControlEvent) bool { return e.Kind == EventSync && e.Stream == gen })
}

// DropControl ends the control stream (the daemon's connection to the relay
// dropped) and waits for its reader to stop; events from it are ignored from
// then on.
func (d *Daemon) DropControl() {
	d.mu.Lock()
	stop, done := d.ctrlStop, d.ctrlDone
	d.ctrl, d.ctrlStop, d.ctrlDone = nil, nil, nil
	d.mu.Unlock()
	if stop == nil {
		return
	}
	stop()
	<-done
}

// Reconnect drops the control stream and opens a new one, returning once the
// relay's Sync for the new stream has been received and acted on.
func (d *Daemon) Reconnect(t testing.TB) {
	t.Helper()
	d.DropControl()
	d.MustConnect(t)
}

// DropRoutes drops the transport of every device route while keeping their E2EE
// sessions, and returns once the relay has processed each drop. Call
// DropControl first if the routes must stay "waiting": while a control stream
// is up the relay immediately announces a DeviceAttach for a device whose
// daemon side ended, and the stub (correctly) resumes it.
func (d *Daemon) DropRoutes() error {
	d.mu.Lock()
	var eps []*endpoint
	for _, r := range d.routes {
		if r.state == routeLive {
			r.state = routeDown
			eps = append(eps, r.ep)
		}
	}
	d.mu.Unlock()
	for _, ep := range eps {
		if err := ep.dropTransport(); err != nil {
			return err
		}
	}
	return nil
}

// Revoke sends ControlFrame.Revoke for deviceID.
func (d *Daemon) Revoke(deviceID string) error {
	d.mu.Lock()
	ctrl := d.ctrl
	d.mu.Unlock()
	if ctrl == nil {
		return fmt.Errorf("relaytest: no control stream")
	}
	d.sendMu.Lock()
	defer d.sendMu.Unlock()
	return ctrl.Send(&relaypb.ControlFrame{
		WorkspaceId: d.relay.HostID,
		Body:        &relaypb.ControlFrame_Revoke_{Revoke: &relaypb.ControlFrame_Revoke{DeviceId: deviceID}},
	})
}

// SendTo pushes a plaintext message to the device (daemon-initiated traffic),
// on that device's session.
func (d *Daemon) SendTo(deviceID string, msg []byte) error {
	d.mu.Lock()
	var ep *endpoint
	for ref, r := range d.routes {
		if ref.DeviceID == deviceID && (r.state == routeLive || r.state == routeDown) {
			ep = r.ep
		}
	}
	d.mu.Unlock()
	if ep == nil {
		return fmt.Errorf("relaytest: no route for device %s", deviceID)
	}
	return ep.send(msg)
}

// Devices lists the routes with an established E2EE session (transport up or
// dropped), sorted by device id.
func (d *Daemon) Devices() []DeviceRef {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.devicesLocked(func(s routeState) bool { return s == routeLive || s == routeDown || s == routeResuming })
}

func (d *Daemon) liveLocked() []DeviceRef {
	return d.devicesLocked(func(s routeState) bool { return s == routeLive })
}

func (d *Daemon) devicesLocked(keep func(routeState) bool) []DeviceRef {
	var out []DeviceRef
	for ref, r := range d.routes {
		if keep(r.state) {
			out = append(out, ref)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return out
}

// WaitLive waits until exactly the named devices have a live (handshaken,
// transport up) route.
func (d *Daemon) WaitLive(t testing.TB, deviceIDs ...string) {
	t.Helper()
	want := append([]string(nil), deviceIDs...)
	sort.Strings(want)
	for {
		d.mu.Lock()
		var got []string
		for _, ref := range d.liveLocked() {
			got = append(got, ref.DeviceID)
		}
		ch, errs := d.changed, append([]error(nil), d.errs...)
		d.mu.Unlock()
		if fmt.Sprint(got) == fmt.Sprint(want) {
			return
		}
		if len(errs) > 0 {
			t.Fatalf("relaytest: daemon route failed while waiting for live %v: %v", want, errs[0])
		}
		select {
		case <-ch:
		case <-timeoutC():
			t.Fatalf("relaytest: timed out waiting for live routes %v (have %v)", want, got)
		}
	}
}

// Events returns a snapshot of the control events received so far.
func (d *Daemon) Events() []ControlEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]ControlEvent(nil), d.events...)
}

// SawDevice reports whether a DeviceAttach for the device, or a Sync listing
// it, was received.
func (d *Daemon) SawDevice(deviceID string) bool {
	for _, e := range d.Events() {
		switch e.Kind {
		case EventAttach:
			if e.DeviceID == deviceID {
				return true
			}
		case EventSync:
			for _, ref := range e.Devices {
				if ref.DeviceID == deviceID {
					return true
				}
			}
		}
	}
	return false
}

// WaitEvent blocks until an event (past or future) satisfies match.
func (d *Daemon) WaitEvent(t testing.TB, what string, match func(ControlEvent) bool) ControlEvent {
	t.Helper()
	for {
		d.mu.Lock()
		for _, e := range d.events {
			if match(e) {
				d.mu.Unlock()
				return e
			}
		}
		ch := d.changed
		d.mu.Unlock()
		select {
		case <-ch:
		case <-timeoutC():
			t.Fatalf("relaytest: timed out waiting for control event: %s", what)
		}
	}
}

func (d *Daemon) record(e ControlEvent) {
	d.mu.Lock()
	d.events = append(d.events, e)
	d.notifyLocked()
	d.mu.Unlock()
}

// handle records a control frame and reacts to it. Frames from a control
// stream that is no longer the current one are dropped.
func (d *Daemon) handle(gen int, cf *relaypb.ControlFrame) {
	var ev ControlEvent
	var serve []DeviceRef
	switch body := cf.GetBody().(type) {
	case *relaypb.ControlFrame_DeviceAttach_:
		a := body.DeviceAttach
		ev = ControlEvent{Kind: EventAttach, DeviceID: a.GetDeviceId(), SessionID: string(a.GetSessionId())}
		serve = []DeviceRef{{ev.DeviceID, ev.SessionID}}
	case *relaypb.ControlFrame_DeviceDetach_:
		x := body.DeviceDetach
		ev = ControlEvent{Kind: EventDetach, DeviceID: x.GetDeviceId(), SessionID: string(x.GetSessionId())}
	case *relaypb.ControlFrame_Sync_:
		ev = ControlEvent{Kind: EventSync}
		for _, ref := range body.Sync.GetDevices() {
			r := DeviceRef{ref.GetDeviceId(), string(ref.GetSessionId())}
			ev.Devices = append(ev.Devices, r)
		}
		serve = ev.Devices
	default:
		return // Pong and anything newer
	}
	ev.Stream = gen

	d.mu.Lock()
	if d.closed || gen != d.stream || d.ctrl == nil {
		d.mu.Unlock()
		return
	}
	d.events = append(d.events, ev)
	var open, resume []*daemonRoute
	for _, ref := range serve {
		r := d.routes[ref]
		switch {
		case r == nil:
			r = &daemonRoute{ref: ref, state: routeOpening}
			d.routes[ref] = r
			open = append(open, r)
		case r.state == routeDown:
			r.state = routeResuming
			resume = append(resume, r)
		}
	}
	d.notifyLocked()
	d.wg.Add(len(open) + len(resume))
	d.mu.Unlock()

	for _, r := range open {
		go d.open(r)
	}
	for _, r := range resume {
		go d.resume(r)
	}
}

// open is the daemon side of a new route: open the stream, handshake as
// responder, answer messages.
func (d *Daemon) open(r *daemonRoute) {
	defer d.wg.Done()
	ep, err := openEndpoint(endpointConfig{
		rc: d.rc, admID: d.admID, host: d.relay.HostID, session: r.ref.SessionID, deviceID: r.ref.DeviceID,
		kp: d.kp, role: e2ee.RoleDaemon,
		onMsg: func(msg []byte) { d.reply(r, msg) },
	})
	d.mu.Lock()
	if err != nil || d.closed {
		delete(d.routes, r.ref)
		if err != nil && !d.closed {
			d.errs = append(d.errs, fmt.Errorf("open route %s: %w", r.ref.DeviceID, err))
		}
		d.notifyLocked()
		d.mu.Unlock()
		if err == nil {
			ep.close()
		}
		return
	}
	r.ep, r.state = ep, routeLive
	ep.startPump(func(run *pumpRun) { d.routeEnded(r, run) })
	d.notifyLocked()
	d.mu.Unlock()
}

func (d *Daemon) resume(r *daemonRoute) {
	defer d.wg.Done()
	err := r.ep.resume(func(run *pumpRun) { d.routeEnded(r, run) })
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		delete(d.routes, r.ref)
		if !d.closed {
			d.errs = append(d.errs, fmt.Errorf("resume route %s: %w", r.ref.DeviceID, err))
		}
	} else {
		r.state = routeLive
	}
	d.notifyLocked()
}

// routeEnded handles a pump that stopped while the route was live: the relay
// closed the stream (Revoke) or it failed; the route is gone. A pump that
// stops because the stub dropped the transport finds the route already down.
func (d *Daemon) routeEnded(r *daemonRoute, run *pumpRun) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r.state != routeLive || r.ep.currentRun() != run {
		return
	}
	delete(d.routes, r.ref)
	d.notifyLocked()
}

func (d *Daemon) reply(r *daemonRoute, msg []byte) {
	// A send failing means the transport is going away; the relay's own
	// buffering and the device's resume handle that, nothing to do here.
	_ = r.ep.send(ReplyTo(r.ref.DeviceID, bytes.Clone(msg)))
}

// Close stops the control stream and every route.
func (d *Daemon) Close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	d.DropControl()
	d.wg.Wait()
	d.mu.Lock()
	var eps []*endpoint
	for _, r := range d.routes {
		if r.ep != nil {
			eps = append(eps, r.ep)
		}
	}
	d.routes = nil
	d.mu.Unlock()
	for _, ep := range eps {
		ep.close()
	}
}
