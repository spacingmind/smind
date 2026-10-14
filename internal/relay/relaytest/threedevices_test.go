package relaytest_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/relay/relaytest"
)

// Device ids of the three-device scenario: two native "desktops" and one
// grpc-web "mobile".
const (
	desktopA = "desktop-a"
	desktopB = "desktop-b"
	mobileC  = "mobile-c"
)

// threeDevices is a relay, a daemon stub with its control stream up, and the
// three connected devices.
type threeDevices struct {
	relay   *relaytest.Relay
	daemon  *relaytest.Daemon
	devices []*relaytest.Device // desktopA, desktopB, mobileC
}

func startThreeDevices(t *testing.T) *threeDevices {
	t.Helper()
	relay := relaytest.StartRelay(t)
	daemon := relaytest.NewDaemon(t, relay)
	daemon.MustConnect(t)

	td := &threeDevices{relay: relay, daemon: daemon, devices: []*relaytest.Device{
		relaytest.NewDevice(t, relay, relaytest.Native, desktopA, daemon),
		relaytest.NewDevice(t, relay, relaytest.Native, desktopB, daemon),
		relaytest.NewDevice(t, relay, relaytest.Web, mobileC, daemon),
	}}
	// All three attach at the same time; each handshake needs the daemon
	// stub's side, which the relay's DeviceAttach triggers.
	errs := make(chan error, len(td.devices))
	for _, d := range td.devices {
		go func() { errs <- d.Connect() }()
	}
	for range td.devices {
		if err := <-errs; err != nil {
			t.Fatalf("device connect: %v", err)
		}
	}
	daemon.WaitLive(t, desktopA, desktopB, mobileC)
	return td
}

// roundTrips has the device send n messages and checks each is answered with
// its own device-derived reply, in order.
func roundTrips(d *relaytest.Device, tag string, n int) error {
	for i := 0; i < n; i++ {
		msg := []byte(fmt.Sprintf("%s/%s/%d", tag, d.ID, i))
		if err := d.Send(msg); err != nil {
			return fmt.Errorf("%s send %d: %w", d.ID, i, err)
		}
		got, err := d.Receive()
		if err != nil {
			return fmt.Errorf("%s receive %d: %w", d.ID, i, err)
		}
		if want := relaytest.ReplyTo(d.ID, msg); !bytes.Equal(got, want) {
			return fmt.Errorf("%s got a reply meant for something else (message %d)", d.ID, i)
		}
	}
	return nil
}

// allRoundTrips runs roundTrips for every device concurrently.
func allRoundTrips(t *testing.T, devices []*relaytest.Device, tag string, n int) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make([]error, len(devices))
	for i, d := range devices {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = roundTrips(d, tag, n)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

func requireIdle(t *testing.T, devices ...*relaytest.Device) {
	t.Helper()
	for _, d := range devices {
		if n := d.Pending(); n != 0 {
			t.Errorf("device %s has %d unexpected unread message(s) (cross-delivery?)", d.ID, n)
		}
	}
}

func TestThreeDevicesConcurrentViaHarness(t *testing.T) {
	td := startThreeDevices(t)

	// The relay told the stub about every device (Attach, or Sync if the stub
	// connected late; here the control stream was up first, so Attach).
	for _, id := range []string{desktopA, desktopB, mobileC} {
		if !td.daemon.SawDevice(id) {
			t.Errorf("daemon never learned of device %s from the control stream", id)
		}
	}
	// Three distinct sessions: nobody was evicted by the others.
	if got := td.daemon.Devices(); len(got) != 3 {
		t.Fatalf("daemon routes = %v, want 3 devices", got)
	}

	// Daemon-initiated traffic first (a push per device), each device sees
	// only its own.
	for _, d := range td.devices {
		if err := td.daemon.SendTo(d.ID, []byte("push/"+d.ID)); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range td.devices {
		got, err := d.Receive()
		if err != nil {
			t.Fatalf("%s push: %v", d.ID, err)
		}
		if want := []byte("push/" + d.ID); !bytes.Equal(got, want) {
			t.Fatalf("%s received %q, want its own push", d.ID, got)
		}
	}

	// Then all three talk to the daemon at once, both directions.
	allRoundTrips(t, td.devices, "concurrent", 25)
	requireIdle(t, td.devices...)
}

func TestThreeDevicesConcurrentViaHarnessThenOneReconnects(t *testing.T) {
	td := startThreeDevices(t)
	a, b, c := td.devices[0], td.devices[1], td.devices[2]
	eventsBefore := len(td.daemon.Events())

	// The two devices that stay up keep exchanging traffic the whole time
	// device C is away: any eviction or disturbance fails a round trip.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	loopErrs := make([]error, 2)
	loopCounts := make([]int, 2)
	for i, d := range []*relaytest.Device{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				if err := roundTrips(d, fmt.Sprintf("steady-%d", n), 1); err != nil {
					loopErrs[i] = err
					return
				}
				loopCounts[i]++
			}
		}()
	}

	if err := c.DropTransport(); err != nil {
		t.Fatalf("drop: %v", err)
	}
	// While C is down the daemon keeps pushing to it; the relay buffers.
	want := [][]byte{[]byte("while-down/1"), []byte("while-down/2"), []byte("while-down/3")}
	for _, m := range want {
		if err := td.daemon.SendTo(mobileC, m); err != nil {
			t.Fatalf("daemon send while C is down: %v", err)
		}
	}
	if err := c.Resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	for i, w := range want {
		got, err := c.Receive()
		if err != nil || !bytes.Equal(got, w) {
			t.Fatalf("buffered frame %d after resume = %q, %v; want %q", i, got, err, w)
		}
	}
	// C's resumed route works both ways, with the others still running.
	if err := roundTrips(c, "after-resume", 5); err != nil {
		t.Fatal(err)
	}

	close(stop)
	wg.Wait()
	for i, err := range loopErrs {
		if err != nil {
			t.Errorf("steady device %d disturbed by C's reconnect: %v", i, err)
		}
		if loopCounts[i] == 0 {
			t.Errorf("steady device %d made no round trips", i)
		}
	}
	// Only C's own stream was replaced: the relay announced nothing new
	// (a device side that ends with a daemon side still attached is not a
	// DeviceDetach, and the resumed one finds its route).
	if got := len(td.daemon.Events()); got != eventsBefore {
		t.Errorf("control events changed from %d to %d across C's reconnect: %+v", eventsBefore, got, td.daemon.Events()[eventsBefore:])
	}
	requireIdle(t, a, b, c)
}

func TestThreeDevicesConcurrentViaHarnessThenDaemonControlReconnects(t *testing.T) {
	td := startThreeDevices(t)
	allRoundTrips(t, td.devices, "before", 3)

	// Make the devices "waiting without a daemon side" deterministically:
	// the control stream goes first (so the relay has nobody to announce the
	// daemon side's end to, and the stub cannot react early), then every data
	// route drops. DropRoutes returns once the relay processed each drop.
	td.daemon.DropControl()
	if err := td.daemon.DropRoutes(); err != nil {
		t.Fatal(err)
	}
	// The devices' own streams stay up and keep sending: their frames wait at
	// the relay for the daemon side.
	for _, d := range td.devices {
		if err := d.Send([]byte("while-away/" + d.ID)); err != nil {
			t.Fatalf("%s send while daemon away: %v", d.ID, err)
		}
	}

	// The daemon reconnects its control stream; the relay's Sync, sent right
	// after accepting it, must list exactly the three waiting devices.
	if err := td.daemon.Connect(); err != nil {
		t.Fatal(err)
	}
	sync := td.daemon.WaitEvent(t, "Sync listing the waiting devices", func(e relaytest.ControlEvent) bool {
		return e.Kind == relaytest.EventSync && e.Stream == 2
	})
	var got []string
	for _, ref := range sync.Devices {
		got = append(got, ref.DeviceID+"@"+ref.SessionID)
	}
	var want []string
	for _, d := range td.devices {
		want = append(want, d.ID+"@"+d.SessionID)
	}
	sort.Strings(got)
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Sync devices = %v, want %v", got, want)
	}

	// The stub re-opened every route from the Sync. It RESUMED the sessions
	// (see Daemon's doc: a fresh handshake cannot work against a device whose
	// channel is already established), so the frames sent while it was away
	// decrypt and are answered.
	td.daemon.WaitLive(t, desktopA, desktopB, mobileC)
	for _, d := range td.devices {
		reply, err := d.Receive()
		if err != nil {
			t.Fatalf("%s: no reply to its while-away message: %v", d.ID, err)
		}
		if want := relaytest.ReplyTo(d.ID, []byte("while-away/"+d.ID)); !bytes.Equal(reply, want) {
			t.Fatalf("%s: wrong reply to its while-away message", d.ID)
		}
	}
	allRoundTrips(t, td.devices, "after", 5)
	requireIdle(t, td.devices...)
}

func TestThreeDevicesConcurrentViaHarnessThenOneRevoked(t *testing.T) {
	td := startThreeDevices(t)
	a, b, c := td.devices[0], td.devices[1], td.devices[2]
	allRoundTrips(t, td.devices, "before", 3)

	if err := td.daemon.Revoke(desktopB); err != nil {
		t.Fatal(err)
	}
	// B's stream ends with PermissionDenied.
	err := b.WaitEnded()
	if relaytest.StatusCode(err) != codes.PermissionDenied {
		t.Fatalf("revoked device's stream ended with %v, want PermissionDenied", err)
	}
	// The relay closed the daemon side of B's route too; the stub drops it.
	td.daemon.WaitLive(t, desktopA, mobileC)

	// A new attach for B (a fresh session) is refused.
	b2 := relaytest.NewDevice(t, td.relay, relaytest.Native, desktopB, td.daemon)
	if err := b2.Connect(); relaytest.StatusCode(err) != codes.PermissionDenied {
		t.Fatalf("revoked device re-attached: Connect = %v, want PermissionDenied", err)
	}
	// The same for a grpc-web attach of that device id.
	b3 := relaytest.NewDevice(t, td.relay, relaytest.Web, desktopB, td.daemon)
	if err := b3.Connect(); relaytest.StatusCode(err) != codes.PermissionDenied {
		t.Fatalf("revoked device re-attached over grpc-web: Connect = %v, want PermissionDenied", err)
	}

	// The other two never noticed.
	allRoundTrips(t, []*relaytest.Device{a, c}, "after", 10)
	requireIdle(t, a, c)
}

// TestNoPlaintextVisibleToRelay sends a unique marker through a native and a
// grpc-web device and shows it appears in no Frame that crossed the relay
// (every frame any client sent to it or received from it), nor in its data
// directory. The log is taken at the clients' gRPC boundary, which is exactly
// the relay's view of the wire.
func TestNoPlaintextVisibleToRelay(t *testing.T) {
	relay := relaytest.StartRelay(t)
	daemon := relaytest.NewDaemon(t, relay)
	daemon.MustConnect(t)
	native := relaytest.NewDevice(t, relay, relaytest.Native, desktopA, daemon)
	web := relaytest.NewDevice(t, relay, relaytest.Web, mobileC, daemon)
	native.MustConnect(t)
	web.MustConnect(t)
	daemon.WaitLive(t, desktopA, mobileC)

	marker := []byte("PLAINTEXT-MARKER-7f3a9c1e-do-not-leak")
	for _, d := range []*relaytest.Device{native, web} {
		// Device -> daemon -> device (the reply embeds the marker) ...
		if err := d.Send(marker); err != nil {
			t.Fatal(err)
		}
		got, err := d.Receive()
		if err != nil || !bytes.Contains(got, marker) {
			t.Fatalf("%s: marker did not make the round trip: %v", d.ID, err)
		}
		// ... and daemon-initiated.
		if err := daemon.SendTo(d.ID, marker); err != nil {
			t.Fatal(err)
		}
		if got, err := d.Receive(); err != nil || !bytes.Equal(got, marker) {
			t.Fatalf("%s: pushed marker: %v", d.ID, err)
		}
	}

	records := relay.Frames.Records()
	if len(records) < 8 {
		t.Fatalf("only %d frames logged; the log is not seeing the traffic", len(records))
	}
	var sent, received int
	for _, r := range records {
		if r.Sent {
			sent++
		} else {
			received++
		}
	}
	if sent == 0 || received == 0 {
		t.Fatalf("frame log saw %d sent / %d received frames, want both directions", sent, received)
	}
	// The matcher must be able to fail: a frame that does carry the marker is
	// found (guards against a vacuous "never contains").
	probe := proto.Clone(records[0].Frame).(*relaypb.Frame)
	probe.Payload = append(probe.Payload, marker...)
	if !relaytest.FrameContains(probe, marker) {
		t.Fatal("FrameContains missed a marker it was given")
	}
	if relay.Frames.Contains(marker) {
		t.Fatal("plaintext marker is visible in frames that crossed the relay")
	}
	// The relay's persistent state holds no plaintext either.
	_ = filepath.WalkDir(relay.DataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, err := os.ReadFile(path); err == nil && bytes.Contains(b, marker) {
			t.Errorf("plaintext marker found in relay data file %s", filepath.Base(path))
		}
		return nil
	})
}
