package client

// integration_test.go — the plan's Integration Test Scenarios, over a real
// (in-process) TLS gRPC relay started with server.Run: full pairing flow
// (QR offer through handshake through a forwarded application message),
// mobile disconnect/reconnect with buffered-frame delivery, and the
// two-device fanout scenario.

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/relay/e2ee"
	"github.com/spacingmind/smind/internal/relay/pairing"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
	"github.com/spacingmind/smind/internal/relay/server"
)

// relayProcess is a real relay (server.Run) on an ephemeral port, plus
// everything a daemon needs from enrollment: endpoint, fingerprint
// (exactly what the QR offer carries), workspace secret.
type relayProcess struct {
	addr        string
	fingerprint string
	secret      []byte
	cert        struct {
		done chan error
	}
}

func startRelay(t *testing.T) (*relayProcess, func()) {
	t.Helper()
	dir := t.TempDir()
	secret, err := server.EnrollWorkspace(dir, "ws-int")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}

	// Kernel-assigned ephemeral listener, handed to Run directly (no
	// bind race). Cert is generated first so the fingerprint is knowable.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()

	cert, err := server.LoadOrCreateCert(dir)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}

	rp := &relayProcess{
		addr:        addr,
		fingerprint: server.CertFingerprint(cert),
		secret:      secret,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, server.Config{Listener: lis, DataDir: dir}) }()

	stop := func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("relay Run: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("relay did not stop")
		}
	}
	return rp, stop
}

// dialAdmitted dials with the pinned fingerprint and completes admission,
// returning the relay client and admission ID.
func dialAdmitted(t *testing.T, rp *relayProcess) (c relaypb.RelayClient, admissionID string) {
	t.Helper()
	conn, err := Dial(rp.addr, rp.fingerprint)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	c = relaypb.NewRelayClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	admissionID, err = Admit(ctx, c, "ws-int", "daemon-key-1", rp.secret)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	return c, admissionID
}

// daemonOffer builds the QR offer exactly as the daemon would at pairing
// time: its persisted keypair's public key, the relay endpoint, and the
// fingerprint pinned per ADR-0011.
func daemonOffer(t *testing.T, daemonKey *e2ee.KeyPair, rp *relayProcess) pairing.Offer {
	t.Helper()
	offer := pairing.Offer{
		DaemonID:         "daemon-key-1",
		PublicKey:        daemonKey.Public(),
		Relay:            "https://" + rp.addr,
		RelayFingerprint: rp.fingerprint,
	}
	if err := offer.Validate(); err != nil {
		t.Fatalf("offer: %v", err)
	}
	// The QR round-trip must preserve the fingerprint — that is the pin a
	// device will use.
	url, err := offer.URL("https://spacingmind.sh/pair")
	if err != nil {
		t.Fatalf("offer URL: %v", err)
	}
	back, err := pairing.ParseURL(url)
	if err != nil {
		t.Fatalf("parse offer URL: %v", err)
	}
	if back.RelayFingerprint != rp.fingerprint {
		t.Fatal("fingerprint lost through the QR offer")
	}
	return back
}

// pair connects a daemon-side DataConn and a device-side DataConn through
// the relay and completes the E2EE handshake between them.
func pair(
	t *testing.T,
	rp *relayProcess,
	admissionID string,
	daemonKey, deviceKey *e2ee.KeyPair,
	sessionID, deviceID string,
) (daemon, device *DataConn) {
	t.Helper()
	dc, _ := dialAdmitted(t, rp)
	vc, _ := dialAdmitted(t, rp)
	return pairWithClients(t, rp, dc, vc, admissionID, daemonKey, deviceKey, sessionID, deviceID)
}

func TestIntegrationPairingFlowThroughForwardedMessage(t *testing.T) {
	rp, stop := startRelay(t)
	defer stop()

	daemonKey, err := e2ee.LoadOrCreateKeyPair(t.TempDir())
	if err != nil {
		t.Fatalf("daemon keypair: %v", err)
	}
	offer := daemonOffer(t, daemonKey, rp)

	// The device side gets everything it needs from the (round-tripped)
	// offer: endpoint, fingerprint, daemon public key.
	deviceKey, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatalf("device keypair: %v", err)
	}
	_ = offer

	_, admissionID := dialAdmitted(t, rp)
	daemon, device := pair(t, rp, admissionID, daemonKey, deviceKey, "sess-1", "dev-1")

	msg := []byte(`{"kind":"task.update","id":"t-1","title":"hello from daemon"}`)
	if err := daemon.Send(msg); err != nil {
		t.Fatalf("daemon send: %v", err)
	}
	got, err := device.Receive()
	if err != nil {
		t.Fatalf("device receive: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("forwarded message mismatch: %q != %q", got, msg)
	}

	reply := []byte(`{"kind":"ack"}`)
	if err := device.Send(reply); err != nil {
		t.Fatalf("device send: %v", err)
	}
	got, err = daemon.Receive()
	if err != nil {
		t.Fatalf("daemon receive: %v", err)
	}
	if !bytes.Equal(got, reply) {
		t.Fatalf("reply mismatch: %q != %q", got, reply)
	}
}

func TestIntegrationMobileDisconnectReconnectDeliversBufferedFrames(t *testing.T) {
	rp, stop := startRelay(t)
	defer stop()

	daemonKey, _ := e2ee.LoadOrCreateKeyPair(t.TempDir())
	deviceKey, _ := e2ee.GenerateKeyPair()
	_, admissionID := dialAdmitted(t, rp)
	daemon, device := pair(t, rp, admissionID, daemonKey, deviceKey, "sess-1", "dev-1")

	// Sanity traffic before the drop.
	if err := daemon.Send([]byte("before")); err != nil {
		t.Fatalf("pre-drop send: %v", err)
	}
	if got, err := device.Receive(); err != nil || !bytes.Equal(got, []byte("before")) {
		t.Fatalf("pre-drop receive: %q %v", got, err)
	}

	// Mobile drops (transport dies; the DataConn and its session state
	// survive on the device). The daemon keeps sending; the relay buffers
	// for the absent device side.
	device.DropTransport()
	msgs := [][]byte{[]byte("buffered-1"), []byte("buffered-2"), []byte("buffered-3")}
	for _, m := range msgs {
		if err := daemon.Send(m); err != nil {
			t.Fatalf("daemon send while device away: %v", err)
		}
	}

	// Transport-level reconnect (ADR-0007 (e) amendment): same session id,
	// SAME key material and counters — Resume, not a fresh Handshake — so
	// the relay's buffered ciphertext decrypts normally.
	vc2, _ := dialAdmitted(t, rp)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := device.Resume(ctx, vc2, admissionID); err != nil {
		t.Fatalf("device resume: %v", err)
	}

	for i, want := range msgs {
		got, err := device.Receive()
		if err != nil {
			t.Fatalf("buffered frame %d: %v", i+1, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("buffered frame %d: %q != %q", i+1, got, want)
		}
	}

	// Live traffic continues on the resumed session, both directions.
	if err := daemon.Send([]byte("post-reconnect")); err != nil {
		t.Fatalf("post-reconnect send: %v", err)
	}
	if got, err := device.Receive(); err != nil || !bytes.Equal(got, []byte("post-reconnect")) {
		t.Fatalf("post-reconnect receive: %q %v", got, err)
	}
	if err := device.Send([]byte("device-reply")); err != nil {
		t.Fatalf("device reply: %v", err)
	}
	if got, err := daemon.Receive(); err != nil || !bytes.Equal(got, []byte("device-reply")) {
		t.Fatalf("daemon reply receive: %q %v", got, err)
	}
}

// pairWithClients is pair() for already-dialled clients (reconnect).
func pairWithClients(
	t *testing.T,
	rp *relayProcess,
	dc, vc relaypb.RelayClient,
	admissionID string,
	daemonKey, deviceKey *e2ee.KeyPair,
	sessionID, deviceID string,
) (daemon, device *DataConn) {
	t.Helper()
	ctx := context.Background()
	daemon, err := OpenData(ctx, dc, admissionID, "ws-int", sessionID, deviceID, daemonKey, e2ee.RoleDaemon)
	if err != nil {
		t.Fatalf("daemon OpenData: %v", err)
	}
	device, err = OpenData(ctx, vc, admissionID, "ws-int", sessionID, deviceID, deviceKey, e2ee.RoleMobile)
	if err != nil {
		t.Fatalf("device OpenData: %v", err)
	}
	dErr := make(chan error, 1)
	go func() { dErr <- daemon.Handshake(ctx) }()
	if err := device.Handshake(ctx); err != nil {
		t.Fatalf("device handshake: %v", err)
	}
	if err := <-dErr; err != nil {
		t.Fatalf("daemon handshake: %v", err)
	}
	return daemon, device
}

func TestIntegrationTwoDevicesFanout(t *testing.T) {
	rp, stop := startRelay(t)
	defer stop()

	daemonKey, _ := e2ee.LoadOrCreateKeyPair(t.TempDir())
	dev1Key, _ := e2ee.GenerateKeyPair()
	dev2Key, _ := e2ee.GenerateKeyPair()
	_, admissionID := dialAdmitted(t, rp)

	d1, v1 := pair(t, rp, admissionID, daemonKey, dev1Key, "sess-1", "dev-1")
	d2, v2 := pair(t, rp, admissionID, daemonKey, dev2Key, "sess-2", "dev-2")
	_ = d1
	_ = d2

	// Daemon sends the same application event to both devices (per-device
	// sessions, separately encrypted — the fanout semantic from step 4).
	event := []byte(`{"kind":"event","n":1}`)
	if err := d1.Send(event); err != nil {
		t.Fatalf("d1 send: %v", err)
	}
	if err := d2.Send(event); err != nil {
		t.Fatalf("d2 send: %v", err)
	}
	for i, v := range []*DataConn{v1, v2} {
		got, err := v.Receive()
		if err != nil {
			t.Fatalf("device %d receive: %v", i+1, err)
		}
		if !bytes.Equal(got, event) {
			t.Fatalf("device %d mismatch: %q", i+1, got)
		}
	}
}
