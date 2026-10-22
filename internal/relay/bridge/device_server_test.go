package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/wsapi"
)

// recordingServer implements both Server and DeviceServer and records which
// entry point the hub used.
type recordingServer struct {
	mu        sync.Mutex
	transport int
	device    int
	ids       []string
	called    chan struct{}
}

func newRecordingServer() *recordingServer {
	return &recordingServer{called: make(chan struct{}, 8)}
}

func (r *recordingServer) hold(ctx context.Context, tr wsapi.Transport) {
	r.called <- struct{}{}
	<-ctx.Done()
	_ = tr.Close()
}

func (r *recordingServer) ServeTransport(ctx context.Context, tr wsapi.Transport) {
	r.mu.Lock()
	r.transport++
	r.mu.Unlock()
	r.hold(ctx, tr)
}

func (r *recordingServer) ServeDevice(ctx context.Context, tr wsapi.Transport, deviceID string) {
	r.mu.Lock()
	r.device++
	r.ids = append(r.ids, deviceID)
	r.mu.Unlock()
	r.hold(ctx, tr)
}

func (r *recordingServer) snapshot() (transport, device int, ids []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.transport, r.device, append([]string(nil), r.ids...)
}

func (r *recordingServer) waitCalled(t *testing.T) {
	t.Helper()
	select {
	case <-r.called:
	case <-time.After(fixtureTimeout):
		t.Fatal("the server was never invoked")
	}
}

func TestHubServesIdentifiedDeviceThroughDeviceServer(t *testing.T) {
	f := newHubFixture(t, Options{})
	approvedWindow(t, f, 1)
	srv := newRecordingServer()
	f.start(srv)

	d := f.newDevice(false, "phone")
	d.connect()
	srv.waitCalled(t)

	transport, device, ids := srv.snapshot()
	if device != 1 || len(ids) != 1 || ids[0] != d.id {
		t.Fatalf("ServeDevice calls = %d ids %v, want exactly one with %q", device, ids, d.id)
	}
	if transport != 0 {
		t.Fatalf("ServeTransport called %d times for an identified device", transport)
	}
}

func TestHubServesLegacyClientThroughServeTransport(t *testing.T) {
	f := newHubFixture(t, Options{LegacyDevices: true})
	srv := newRecordingServer()
	f.start(srv)

	d := f.newDevice(false, "old-app")
	d.id = DefaultDeviceID
	if err := d.open(DefaultSessionID); err != nil { // sends no hello
		t.Fatal(err)
	}
	// A legacy client sends no hello; the hub serves it after the first
	// message, so send an ordinary request to trigger that.
	// The stub never answers, so do not wait for a reply.
	go func() { _, _ = d.request("workspace.list", nil) }()
	srv.waitCalled(t)

	transport, device, _ := srv.snapshot()
	if transport != 1 || device != 0 {
		t.Fatalf("ServeTransport = %d, ServeDevice = %d; want 1 and 0", transport, device)
	}
}
