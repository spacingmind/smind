package server

// multidevice_limits_test.go — resource bounds and state-key hygiene added by
// the independent review of P12a: route cap, id length, collision-free route
// keys, and the control outbox's overflow-to-resync path.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

func TestRouteCapPerHostBoundsDaemonSideRoutes(t *testing.T) {
	// Daemon-side attaches create routes (and buffers) without counting as
	// devices; a host may hold at most maxDevices*routesPerDevice of them.
	h := newHarnessWithOptions(t, 0, WithMaxDevicesPerHost(2))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")

	limit := 2 * routesPerDevice
	for i := 0; i < limit; i++ {
		d := mdOpenStream(t, h, ctx, adm, mdRoute(fmt.Sprintf("dev-%d", i), "s", relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE))
		d.send(d.frame(0, []byte("reg")))
	}
	mdWaitStreams(t, h, limit)
	over := mdOpenStream(t, h, ctx, adm, mdRoute("dev-over", "s", relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE))
	over.send(over.frame(0, []byte("reg")))
	over.expectDone(codes.ResourceExhausted)
}

func TestOverlongIDsAreRejected(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	long := strings.Repeat("x", maxIDLen+1)

	v := mdOpenStream(t, h, ctx, adm, mdRoute(long, "s", relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v.send(v.frame(0, []byte("reg")))
	v.expectDone(codes.InvalidArgument)

	v2 := mdOpenStream(t, h, ctx, adm, mdRoute("d", long, relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON))
	v2.send(v2.frame(0, []byte("reg")))
	v2.expectDone(codes.InvalidArgument)
}

func TestRouteKeysCannotCollideAcrossIDBoundaries(t *testing.T) {
	if routeKey("ws", "a\x00b", "c") == routeKey("ws", "a", "b\x00c") {
		t.Fatal("route keys collide when ids contain the separator byte")
	}
	if routeKey("w", "ab", "c") == routeKey("wa", "b", "c") {
		t.Fatal("route keys collide across the host/session boundary")
	}
}

func TestControlOutboxOverflowFallsBackToResync(t *testing.T) {
	c := newControlConn("ws")
	for i := 0; i < controlOutboxCap+10; i++ {
		c.enqueue(pongFrame("ws", uint64(i)))
	}
	frames, resync := c.drain()
	if len(frames) != controlOutboxCap {
		t.Fatalf("queued %d frames, want the cap %d", len(frames), controlOutboxCap)
	}
	if !resync {
		t.Fatal("overflow must owe the daemon a fresh Sync")
	}
	if frames, resync := c.drain(); len(frames) != 0 || resync {
		t.Fatal("drain must clear the outbox and the resync debt")
	}
}

func TestControlStreamMustStartWithPing(t *testing.T) {
	h := newHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	stream, err := h.client.OpenControl(admittedContext(ctx, adm))
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&relaypb.ControlFrame{
		WorkspaceId: testWorkspace,
		Body:        &relaypb.ControlFrame_Revoke_{Revoke: &relaypb.ControlFrame_Revoke{DeviceId: "d"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatal("a control stream whose first frame is not a Ping must be refused")
	}
}
