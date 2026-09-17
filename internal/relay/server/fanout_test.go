package server

// fanout_test.go — multi-device fanout (ADR-0007 (b)): more than one
// paired device per workspace, each with its own OpenData stream and its
// own E2EE session. Semantic (conservative reading, documented in
// server.go): no device-to-device forwarding — the daemon reaches each
// device over its own per-device route, and a device's frames go only to
// the daemon. One ciphertext frame could not be shared across devices
// anyway: each session has its own key (ADR-0007 (e)).

import (
	"bytes"
	"context"
	"testing"
	"time"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// fanoutFixture admits one daemon connection and stands up N per-device
// routes: for device i, a daemon-side stream and a device-side stream on
// (ws, sess-i, dev-i).
type fanoutFixture struct {
	daemon  []*endpoint
	devices []*endpoint
}

func setupFanout(t *testing.T, h *harness, n int) *fanoutFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	dctx := admittedContext(ctx, adm)

	f := &fanoutFixture{}
	for i := 0; i < n; i++ {
		sess := "sess-" + string(rune('1'+i))
		dev := "dev-" + string(rune('1'+i))
		dr := frameRoute{workspaceID: testWorkspace, sessionID: sess, deviceID: dev, direction: relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE}
		vr := dr
		vr.direction = relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON

		d := h.openData(dctx, dr)
		v := h.openData(dctx, vr)
		// Register both sides before expecting delivery (a stream's route
		// is derived from its first frame).
		d.sendFrame(d.frame(0, []byte("reg")))
		v.sendFrame(v.frame(0, []byte("reg")))
		v.expectSeq(t, 5*time.Second, 0)
		d.expectSeq(t, 5*time.Second, 0)

		f.daemon = append(f.daemon, d)
		f.devices = append(f.devices, v)
	}
	return f
}

func TestFanoutBothDevicesReceiveDaemonBroadcast(t *testing.T) {
	h := newHarness(t, 0)
	f := setupFanout(t, h, 2)

	// The daemon "broadcasts" an event: same application payload,
	// separately encrypted per session — so the ciphertexts differ, and
	// each device must receive exactly its own.
	ct1 := randBytes(t, 48)
	ct2 := randBytes(t, 48)
	if bytes.Equal(ct1, ct2) {
		t.Fatal("test bug: ciphertexts identical")
	}
	f.daemon[0].sendFrame(f.daemon[0].frame(1, ct1))
	f.daemon[1].sendFrame(f.daemon[1].frame(1, ct2))

	got1 := f.devices[0].expectRecv(5 * time.Second)
	got2 := f.devices[1].expectRecv(5 * time.Second)
	if !bytes.Equal(got1.GetPayload(), ct1) {
		t.Fatalf("device 1 payload mismatch")
	}
	if !bytes.Equal(got2.GetPayload(), ct2) {
		t.Fatalf("device 2 payload mismatch")
	}
	// No cross-delivery: nothing further arrives on either device.
	f.devices[0].expectQuiet(200 * time.Millisecond)
	f.devices[1].expectQuiet(200 * time.Millisecond)
}

func TestFanoutDeviceMessageDoesNotLeakToOtherDevice(t *testing.T) {
	h := newHarness(t, 0)
	f := setupFanout(t, h, 2)

	// Device 1 -> daemon: only the daemon-side stream of route 1 receives.
	ct := randBytes(t, 32)
	f.devices[0].sendFrame(f.devices[0].frame(1, ct))

	got := f.daemon[0].expectRecv(5 * time.Second)
	if !bytes.Equal(got.GetPayload(), ct) {
		t.Fatalf("daemon stream 1 payload mismatch")
	}

	// Device 2 sees nothing; its own daemon stream sees nothing either.
	f.devices[1].expectQuiet(200 * time.Millisecond)
	f.daemon[1].expectQuiet(200 * time.Millisecond)

	// And a frame from device 1 never arrives on daemon stream 2 either
	// (already implied by the routing checks above, asserted explicitly
	// for the leak scenario).
	f.daemon[1].expectQuiet(100 * time.Millisecond)
}

func TestFanoutReconnectBuffersIndependent(t *testing.T) {
	const cap = 4
	h := newHarness(t, cap)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	adm := h.admit(ctx, testWorkspace, "daemon-key-1")
	dctx := admittedContext(ctx, adm)

	// Route 1 fully connected.
	dr1 := frameRoute{workspaceID: testWorkspace, sessionID: "sess-1", deviceID: "dev-1", direction: relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE}
	vr1 := dr1
	vr1.direction = relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON
	d1 := h.openData(dctx, dr1)
	v1 := h.openData(dctx, vr1)
	d1.sendFrame(d1.frame(0, []byte("reg")))
	v1.sendFrame(v1.frame(0, []byte("reg")))
	v1.expectSeq(t, 5*time.Second, 0)
	d1.expectSeq(t, 5*time.Second, 0)

	// Route 2: daemon side only — device 2 is away, its frames buffer.
	dr2 := frameRoute{workspaceID: testWorkspace, sessionID: "sess-2", deviceID: "dev-2", direction: relaypb.Direction_DIRECTION_DAEMON_TO_DEVICE}
	vr2 := dr2
	vr2.direction = relaypb.Direction_DIRECTION_DEVICE_TO_DAEMON
	d2 := h.openData(dctx, dr2)
	const n2 = 3
	ct2 := make([][]byte, n2)
	for i := 0; i < n2; i++ {
		ct2[i] = randBytes(t, 24)
		d2.sendFrame(d2.frame(uint64(i), ct2[i]))
	}
	waitFor(t, 5*time.Second, func() bool {
		return h.srv.BufferedFrames(testWorkspace, "sess-2", "dev-2") == n2
	})
	// Route 1's buffer stays empty while route 2 buffers.
	if got := h.srv.BufferedFrames(testWorkspace, "sess-1", "dev-1"); got != 0 {
		t.Fatalf("route 1 buffered %d frames, want 0", got)
	}

	// Route 1 traffic flows unaffected by route 2's buffering.
	ct := randBytes(t, 16)
	d1.sendFrame(d1.frame(1, ct))
	if got := v1.expectRecv(5 * time.Second); !bytes.Equal(got.GetPayload(), ct) {
		t.Fatalf("route 1 traffic disturbed by route 2 buffering")
	}

	// Device 2 reconnects: gets exactly its own buffered frames, in order.
	v2 := h.openData(dctx, vr2)
	v2.sendFrame(v2.frame(0, []byte("reg")))
	d2.expectSeq(t, 5*time.Second, 0)
	for i := 0; i < n2; i++ {
		got := v2.expectRecv(5 * time.Second)
		if got.GetSequence() != uint64(i) || !bytes.Equal(got.GetPayload(), ct2[i]) {
			t.Fatalf("device 2 buffered frame %d mismatch", i)
		}
	}
	// Device 1 receives nothing from device 2's reconnect flush.
	v1.expectQuiet(200 * time.Millisecond)
}
