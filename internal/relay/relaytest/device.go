package relaytest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// Kind is the transport a Device uses to reach the relay.
type Kind int

const (
	// Native is native gRPC (a desktop client).
	Native Kind = iota
	// Web is grpc-web over websocket (the mobile client).
	Web
)

func (k Kind) String() string {
	if k == Web {
		return "web"
	}
	return "native"
}

// Device is one paired device: its own device id, E2EE keypair and session id,
// its own relay connection and admission. Connect completes a real E2EE
// handshake with the daemon (the stub answers it); a background pump collects
// the plaintext the daemon sends.
type Device struct {
	ID        string
	SessionID string
	Kind      Kind

	relay *Relay
	rc    relaypb.RelayClient
	pin   []byte

	admID string
	ep    *endpoint
	msgs  chan []byte
}

// NewDevice prepares a device (no network yet) that will pin daemon's E2EE
// key, as a device does with the key from its pairing offer. daemon may be nil
// to run unpinned. The device is closed when the test ends.
func NewDevice(t testing.TB, r *Relay, kind Kind, id string, daemon *Daemon) *Device {
	t.Helper()
	var sess [8]byte
	if _, err := rand.Read(sess[:]); err != nil {
		t.Fatalf("relaytest: session id: %v", err)
	}
	d := &Device{
		ID:        id,
		SessionID: "sess-" + id + "-" + hex.EncodeToString(sess[:]),
		Kind:      kind,
		relay:     r,
		msgs:      make(chan []byte, 256),
	}
	if daemon != nil {
		d.pin = daemon.PublicKey()
	}
	switch kind {
	case Web:
		d.rc = WebClient(t, r)
	default:
		d.rc = NativeClient(t, r)
	}
	t.Cleanup(d.Close)
	return d
}

// Connect admits the device, opens its data stream and completes the E2EE
// handshake with the daemon. It returns the error the relay or handshake
// produced (a revoked device's is a PermissionDenied status, see StatusCode).
func (d *Device) Connect() error {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	admID, err := client.Admit(ctx, d.rc, d.relay.HostID, "device-"+d.ID, d.relay.Secret)
	if err != nil {
		return err
	}
	d.admID = admID
	ep, err := openEndpoint(endpointConfig{
		rc: d.rc, admID: admID, host: d.relay.HostID, session: d.SessionID, deviceID: d.ID,
		role: e2ee.RoleMobile, pin: d.pin, onMsg: d.deliver,
	})
	if err != nil {
		return err
	}
	d.ep = ep
	ep.startPump(nil)
	return nil
}

// MustConnect is Connect failing the test on error.
func (d *Device) MustConnect(t testing.TB) {
	t.Helper()
	if err := d.Connect(); err != nil {
		t.Fatalf("relaytest: device %s (%s) connect: %v", d.ID, d.Kind, err)
	}
}

func (d *Device) deliver(msg []byte) {
	select {
	case d.msgs <- msg:
	case <-d.ep.ctx.Done():
	}
}

// Send encrypts and sends one plaintext message to the daemon.
func (d *Device) Send(msg []byte) error { return d.ep.send(msg) }

// Receive returns the next plaintext message from the daemon or an error after
// Timeout.
func (d *Device) Receive() ([]byte, error) { return d.ReceiveWithin(Timeout) }

// ReceiveWithin is Receive with an explicit bound.
func (d *Device) ReceiveWithin(within time.Duration) ([]byte, error) {
	select {
	case m := <-d.msgs:
		return m, nil
	case <-time.After(within):
		return nil, fmt.Errorf("device %s: no message within %s", d.ID, within)
	}
}

// Pending reports how many received messages nobody has read yet.
func (d *Device) Pending() int { return len(d.msgs) }

// WaitEnded waits for the stream to end and returns why: a status error for a
// relay-initiated close (Revoke is PermissionDenied), see StatusCode.
func (d *Device) WaitEnded() error {
	run := d.ep.currentRun()
	select {
	case <-run.done:
		return run.err
	case <-time.After(Timeout):
		return fmt.Errorf("device %s: stream did not end", d.ID)
	}
}

// DropTransport kills the data stream without touching the E2EE session (a
// network drop). It returns once the relay has processed the drop. Messages
// the relay already delivered stay readable.
func (d *Device) DropTransport() error { return d.ep.dropTransport() }

// Resume reopens the data stream on the same session (DataConn.Resume), so
// frames the relay buffered while the device was away decrypt in order.
func (d *Device) Resume() error { return d.ep.resume(nil) }

// Close tears the device down. Safe to call more than once and on a device
// that never connected.
func (d *Device) Close() {
	if d.ep != nil {
		d.ep.close()
	}
}

// StatusCode extracts the gRPC status code from err or any error it wraps
// (codes.Unknown when there is none), for asserting relay refusals.
func StatusCode(err error) codes.Code {
	var se interface{ GRPCStatus() *status.Status }
	if errors.As(err, &se) {
		return se.GRPCStatus().Code()
	}
	return codes.Unknown
}
