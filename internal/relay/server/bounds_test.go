package server

// bounds_test.go — the relay-state bounds (plan: relay-security-hardening,
// Fix B): idle-route garbage collection, per-workspace/TTL admission
// bindings. Time is a fake clock throughout; nothing here sleeps for a
// grace period or TTL.

import (
	"context"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacingmind/smind/internal/relay/admission"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// fakeClock is a manually advanced time source.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

const testGrace = time.Minute

func routeCount(s *Server) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mu.routes)
}

func bindingCount(s *Server, ws string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, b := range s.mu.bindings {
		if ws == "" || b.workspaceID == ws {
			n++
		}
	}
	return n
}

// attachDaemonOnly admits and opens just the daemon side of the standard
// test route, sends frames so the device queue buffers them, and returns a
// cancel that detaches the daemon.
func attachDaemonOnly(t *testing.T, h *harness, frames int) (admissionID string, detach context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	admissionID = h.admit(ctx, testWorkspace, "daemon-key-1")
	daemon := h.openData(admittedContext(ctx, admissionID), daemonRoute())
	for i := 0; i < frames; i++ {
		daemon.sendFrame(daemon.frame(uint64(i), []byte{byte(i)}))
	}
	waitFor(t, 5*time.Second, func() bool { return h.srv.BufferedFrames(testWorkspace, "sess-1", "dev-1") == frames })
	return admissionID, cancel
}

func TestRouteGCAfterGrace(t *testing.T) {
	clock := newFakeClock()
	h := newHarnessWithOptions(t, 0, WithClock(clock.Now), WithRouteGrace(testGrace))

	_, detach := attachDaemonOnly(t, h, 3)
	detach()
	waitFor(t, 5*time.Second, func() bool { return h.srv.Streams() == 0 })

	clock.Advance(testGrace + time.Second)
	h.srv.Sweep()

	if n := routeCount(h.srv); n != 0 {
		t.Fatalf("routes after grace = %d, want 0 (collected)", n)
	}
	if n := h.srv.BufferedFrames(testWorkspace, "sess-1", "dev-1"); n != 0 {
		t.Fatalf("buffered frames after GC = %d, want 0", n)
	}
}

// TestRouteKeptWithinGrace is the reconnect-buffer regression: inside the
// grace window the buffered frames survive a sweep and are flushed, in
// order, to a peer that attaches late.
func TestRouteKeptWithinGrace(t *testing.T) {
	clock := newFakeClock()
	h := newHarnessWithOptions(t, 0, WithClock(clock.Now), WithRouteGrace(testGrace))

	adm, detach := attachDaemonOnly(t, h, 3)
	detach()
	waitFor(t, 5*time.Second, func() bool { return h.srv.Streams() == 0 })

	clock.Advance(testGrace - time.Second)
	h.srv.Sweep()
	if n := h.srv.BufferedFrames(testWorkspace, "sess-1", "dev-1"); n != 3 {
		t.Fatalf("buffered frames within grace = %d, want 3", n)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	device := h.openData(admittedContext(ctx, adm), deviceRoute())
	device.sendFrame(device.frame(0, []byte("reg")))
	for i := 0; i < 3; i++ {
		f := device.expectRecv(5 * time.Second)
		if f.GetSequence() != uint64(i) {
			t.Fatalf("flushed frame %d has sequence %d", i, f.GetSequence())
		}
	}
}

func TestRouteNotGCedWhileAttached(t *testing.T) {
	clock := newFakeClock()
	h := newHarnessWithOptions(t, 0, WithClock(clock.Now), WithRouteGrace(testGrace))

	_, detach := attachDaemonOnly(t, h, 1)

	// Far longer than the grace period, but a stream is attached.
	clock.Advance(10 * testGrace)
	h.srv.Sweep()
	if n := routeCount(h.srv); n != 1 {
		t.Fatalf("attached route was collected (routes = %d)", n)
	}

	// The grace clock starts at detach, not at attach.
	detach()
	waitFor(t, 5*time.Second, func() bool { return h.srv.Streams() == 0 })
	clock.Advance(testGrace - time.Second)
	h.srv.Sweep()
	if n := routeCount(h.srv); n != 1 {
		t.Fatalf("route collected before grace elapsed since detach (routes = %d)", n)
	}
	clock.Advance(2 * time.Second)
	h.srv.Sweep()
	if n := routeCount(h.srv); n != 0 {
		t.Fatalf("route not collected after grace (routes = %d)", n)
	}
}

func TestRouteReattachResetsGraceClock(t *testing.T) {
	clock := newFakeClock()
	h := newHarnessWithOptions(t, 0, WithClock(clock.Now), WithRouteGrace(testGrace))

	adm, detach := attachDaemonOnly(t, h, 1)
	detach()
	waitFor(t, 5*time.Second, func() bool { return h.srv.Streams() == 0 })

	clock.Advance(testGrace - time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	daemon := h.openData(admittedContext(ctx, adm), daemonRoute())
	daemon.sendFrame(daemon.frame(1, []byte("again")))
	waitFor(t, 5*time.Second, func() bool { return h.srv.Streams() == 1 })
	cancel()
	waitFor(t, 5*time.Second, func() bool { return h.srv.Streams() == 0 })

	// Total time since the FIRST detach is now > grace, but since the last
	// detach it is not.
	clock.Advance(testGrace - time.Second)
	h.srv.Sweep()
	if n := routeCount(h.srv); n != 1 {
		t.Fatalf("grace clock was not reset by reattach (routes = %d)", n)
	}
}

// --- bindings ---

// directServer is a Server plus enrolled workspaces whose admissions are
// driven in-process (no gRPC), for binding-bound tests.
type directServer struct {
	t       *testing.T
	srv     *Server
	secrets map[string][]byte
	clock   *fakeClock
}

func newDirectServer(t *testing.T, workspaces []string, opts ...Option) *directServer {
	t.Helper()
	clock := newFakeClock()
	v := admission.NewVerifier(admission.WithClock(clock.Now))
	secrets := map[string][]byte{}
	for _, id := range workspaces {
		ws, secret, err := admission.NewWorkspace(id)
		if err != nil {
			t.Fatal(err)
		}
		v.Register(ws)
		secrets[id] = secret
	}
	opts = append([]Option{WithClock(clock.Now)}, opts...)
	return &directServer{t: t, srv: New(v, 0, opts...), secrets: secrets, clock: clock}
}

// admit runs a full admission against the server and returns the hex
// admission ID, advancing the fake clock 1s afterwards so LRU order is
// deterministic.
func (d *directServer) admit(ws string) string {
	d.t.Helper()
	ctx := context.Background()
	nonce := make([]byte, admission.NonceSize)
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	chal, err := d.srv.AdmitChallenge(ctx, &relaypb.AdmitChallengeRequest{
		ProtocolVersion: admission.ProtocolVersion, WorkspaceId: ws, ClientNonce: nonce, DaemonKeyId: "k",
	})
	if err != nil {
		d.t.Fatalf("AdmitChallenge: %v", err)
	}
	req := &relaypb.AdmitRequest{
		ProtocolVersion: admission.ProtocolVersion, WorkspaceId: ws, ClientNonce: nonce,
		DaemonKeyId: "k", ServerNonce: chal.GetServerNonce(),
	}
	req.Hmac = admission.ComputeProof(d.secrets[ws], req)
	resp, err := d.srv.Admit(ctx, req)
	if err != nil {
		d.t.Fatalf("Admit: %v", err)
	}
	d.clock.Advance(time.Second)
	return hex.EncodeToString(resp.GetAdmissionId())
}

func incoming(id string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKeyAdmission, id))
}

// use presents id like a stream open+end would; returns whether the
// binding resolved.
func (d *directServer) use(id string) bool {
	ws, release, err := d.srv.binding(incoming(id))
	if err != nil {
		if status.Code(err) != codes.Unauthenticated {
			d.t.Fatalf("binding(%s): %v, want Unauthenticated", id, err)
		}
		return false
	}
	_ = ws
	release()
	return true
}

func TestBindingCapPerWorkspaceEvictsLRU(t *testing.T) {
	d := newDirectServer(t, []string{"ws-a", "ws-b"}, WithBindingLimits(3, 24*time.Hour))

	b1 := d.admit("ws-b")
	a1, a2, a3 := d.admit("ws-a"), d.admit("ws-a"), d.admit("ws-a")
	if n := bindingCount(d.srv, "ws-a"); n != 3 {
		t.Fatalf("ws-a bindings = %d, want 3", n)
	}

	// Touch a1 so a2 is now the least recently used.
	if !d.use(a1) {
		t.Fatal("a1 should still resolve")
	}
	d.clock.Advance(time.Second)

	a4 := d.admit("ws-a")
	if n := bindingCount(d.srv, "ws-a"); n != 3 {
		t.Fatalf("ws-a bindings after overflow = %d, want cap 3", n)
	}
	if d.use(a2) {
		t.Fatal("LRU binding a2 should have been evicted")
	}
	for name, id := range map[string]string{"a1": a1, "a3": a3, "a4": a4} {
		if !d.use(id) {
			t.Fatalf("binding %s should have survived", name)
		}
	}
	// The other workspace is untouched by ws-a's churn.
	if !d.use(b1) || bindingCount(d.srv, "ws-b") != 1 {
		t.Fatal("ws-b binding was disturbed by ws-a eviction")
	}

	// Many more admits never exceed the cap.
	for i := 0; i < 20; i++ {
		d.admit("ws-a")
	}
	if n := bindingCount(d.srv, "ws-a"); n != 3 {
		t.Fatalf("ws-a bindings after flood = %d, want 3", n)
	}
}

func TestBindingCapPrefersEvictingIdleOverInUse(t *testing.T) {
	d := newDirectServer(t, []string{"ws-a"}, WithBindingLimits(2, 24*time.Hour))
	inUse := d.admit("ws-a") // oldest
	_, release, err := d.srv.binding(incoming(inUse))
	if err != nil {
		t.Fatal(err)
	}
	idle := d.admit("ws-a")
	d.admit("ws-a") // overflow: must evict `idle`, not the older in-use one

	if _, rel, err := d.srv.binding(incoming(inUse)); err != nil {
		t.Fatalf("in-use binding was evicted: %v", err)
	} else {
		rel()
	}
	if d.use(idle) {
		t.Fatal("idle binding should have been the eviction victim")
	}
	release()
}

func TestBindingTTLExpiry(t *testing.T) {
	const ttl = time.Hour
	d := newDirectServer(t, []string{"ws-a"}, WithBindingLimits(64, ttl))

	idle := d.admit("ws-a")
	touched := d.admit("ws-a")

	d.clock.Advance(ttl - 10*time.Second)
	if !d.use(touched) { // refreshes lastUsed
		t.Fatal("touched binding should resolve inside TTL")
	}
	d.clock.Advance(20 * time.Second) // idle is now past TTL; touched is not
	if d.use(idle) {
		t.Fatal("binding unused past TTL should be rejected")
	}
	if n := bindingCount(d.srv, "ws-a"); n != 1 {
		t.Fatalf("expired binding not removed on lookup: %d left, want 1", n)
	}
	if !d.use(touched) {
		t.Fatal("binding used within TTL should survive")
	}

	// Sweep removes expired bindings that nobody looked up.
	d.clock.Advance(2 * ttl)
	d.srv.Sweep()
	if n := bindingCount(d.srv, ""); n != 0 {
		t.Fatalf("Sweep left %d expired bindings", n)
	}
}

// TestBindingInUseNeverExpiresAndResumeWorks: a binding held by a live
// stream outlives the TTL; when that stream ends, the idle clock restarts,
// so a Resume (re-presenting the same admission ID) still works.
func TestBindingInUseNeverExpiresAndResumeWorks(t *testing.T) {
	const ttl = time.Hour
	d := newDirectServer(t, []string{"ws-a"}, WithBindingLimits(64, ttl))
	id := d.admit("ws-a")

	_, release, err := d.srv.binding(incoming(id))
	if err != nil {
		t.Fatal(err)
	}
	d.clock.Advance(5 * ttl)
	d.srv.Sweep()
	if bindingCount(d.srv, "ws-a") != 1 {
		t.Fatal("binding with a live stream was expired")
	}

	release() // stream dropped; idle clock restarts here
	d.clock.Advance(ttl - time.Minute)
	if !d.use(id) {
		t.Fatal("Resume shortly after a long-lived stream ended must still be admitted")
	}
	d.clock.Advance(ttl + time.Minute)
	if d.use(id) {
		t.Fatal("binding should expire once idle past TTL after its stream ended")
	}
}

// TestDataStreamResumeAfterLongLivedStream drives the same property
// through real OpenData streams on the harness.
func TestDataStreamResumeAfterLongLivedStream(t *testing.T) {
	clock := newFakeClock()
	h := newHarnessWithOptions(t, 0, WithClock(clock.Now), WithBindingLimits(64, time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	sctx, scancel := context.WithCancel(ctx)
	daemon := h.openData(admittedContext(sctx, adm), daemonRoute())
	daemon.sendFrame(daemon.frame(0, []byte("reg")))
	waitFor(t, 5*time.Second, func() bool { return h.srv.Streams() == 1 })

	clock.Advance(3 * time.Hour)
	h.srv.Sweep()
	if bindingCount(h.srv, testWorkspace) != 1 {
		t.Fatal("binding expired under a live stream")
	}

	scancel()
	waitFor(t, 5*time.Second, func() bool { return h.srv.Streams() == 0 })

	resumed := h.openData(admittedContext(ctx, adm), daemonRoute())
	resumed.sendFrame(resumed.frame(1, []byte("resume")))
	waitFor(t, 5*time.Second, func() bool { return h.srv.Streams() == 1 })
}
