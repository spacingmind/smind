package link

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"runtime"
	"testing"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/devices"
	relaypairing "github.com/spacingmind/smind/internal/relay/pairing"
	"github.com/spacingmind/smind/internal/relay/relaytest"
	"github.com/spacingmind/smind/internal/server"
	"github.com/spacingmind/smind/internal/wsapi"
)

const (
	relayE2EPasswordA = "pw-alpha-0123456"
	relayE2EPasswordB = "pw-bravo-0123456"
)

// TestFleetPairRelayE2E pairs two real daemons through a real relay: B (the
// responder) runs a real relay hub in front of its wsapi server and invites
// via the relay with no direct address; A (the initiator) reaches it only
// through the relay.
func TestFleetPairRelayE2E(t *testing.T) {
	relay := relaytest.StartRelay(t)
	kp := newTestKeyPair(t)
	var hub *bridge.Hub
	var devReg *devices.Registry

	a := newE2EDaemon(t, "a", relayE2EPasswordA)
	b := newE2EDaemonWith(t, "b", relayE2EPasswordB, func(srv *server.Server, home string, so *ServiceOptions) {
		hub, devReg = startTestHub(t, relay, kp, home, srv.API(), bridge.Options{Password: srv.RelayPasswordGate()})
		so.RelayOffer = func() (relaypairing.Offer, error) {
			return relaypairing.Offer{
				DaemonID: bridge.DaemonKeyID(kp), PublicKey: kp.Public(),
				Relay: "https://" + relay.NativeAddr, RelayFingerprint: relay.Fingerprint,
				Secret: relay.Secret, WorkspaceID: relay.HostID,
			}, nil
		}
		so.OpenRelayWindow = hub.OpenPairingWindow
		so.RevokeRelayDevice = func(id string) { _, _ = hub.RevokeDevice(id) }
	})
	secretHex := hex.EncodeToString(relay.Secret)

	var inv wsapi.FleetInviteResult
	b.call(t, "fleet.host.invite", wsapi.FleetInviteParams{Via: "relay"}, &inv)
	if inv.HostID != b.key.HostID() {
		t.Fatalf("invite host = %s, want %s", inv.HostID, b.key.HostID())
	}

	var added wsapi.FleetAddResult
	a.call(t, "fleet.host.add", wsapi.FleetAddParams{Invite: inv.Invite, Alias: "bee", Password: relayE2EPasswordB}, &added)
	if added.State != "pending" || len(added.SAS) != 6 || added.HostID != b.key.HostID() {
		t.Fatalf("add result = %+v", added)
	}
	bv := b.hostView(t, a.key.HostID())
	if bv == nil || bv.State != "pending" || bv.SAS != added.SAS {
		t.Fatalf("B's view of A = %+v, want pending with SAS %s", bv, added.SAS)
	}
	poll(t, "A's link to B awaiting approval", func() bool {
		v := a.hostView(t, b.key.HostID())
		return v != nil && v.Link.State == string(StateAwaitingApproval)
	})

	var approved json.RawMessage
	b.call(t, "fleet.host.approve", map[string]any{"host": a.key.HostID(), "writeWorkspaces": []int64{1}}, &approved)
	poll(t, "A's link to B connected over the relay", func() bool {
		v := a.hostView(t, b.key.HostID())
		return v != nil && v.Alias == "bee" && v.State == registry.StateApproved && v.Link.State == string(StateConnected)
	})

	// The stored route is a relay route and holds no credential.
	out, _, err := a.reg.Links(b.key.HostID())
	if err != nil {
		t.Fatal(err)
	}
	tr, err := ParseTransport(out.TransportJSON)
	if err != nil || tr.Kind != KindRelay || tr.Relay == nil {
		t.Fatalf("A's outbound transport = %q (%v), want a relay route", out.TransportJSON, err)
	}
	for _, needle := range []string{secretHex, relayE2EPasswordA, relayE2EPasswordB} {
		if bytes.Contains([]byte(out.TransportJSON), []byte(needle)) {
			t.Fatalf("A's registry transport holds a credential: %s", out.TransportJSON)
		}
	}
	ent, ok, err := a.secrets.Get(b.key.HostID())
	if err != nil || !ok || ent.RelaySecret != secretHex || ent.Password != relayE2EPasswordB {
		t.Fatalf("A's secrets entry ok=%v err=%v", ok, err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(a.secrets.path)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("fleet-secrets mode = %v (%v), want 0600", fi.Mode().Perm(), err)
		}
	}

	// fleet.host.list never carries a secret, key or password.
	for _, d := range []*e2eDaemon{a, b} {
		d.hostView(t, "")
		for _, raw := range d.raw {
			for _, needle := range []string{secretHex, relayE2EPasswordA, relayE2EPasswordB} {
				if bytes.Contains(raw, []byte(needle)) {
					t.Fatalf("%s: fleet.host.list leaks a credential: %s", d.name, raw)
				}
			}
		}
	}

	// The pairing window admitted exactly one new relay device on B's hub:
	// the host, approved.
	devs := devReg.List()
	if len(devs) != 1 || devs[0].ID != a.key.HostID() || devs[0].State != devices.StateApproved {
		t.Fatalf("B's relay devices = %+v, want exactly A approved", devs)
	}
	if w, open := hub.PairingWindow(); open {
		t.Fatalf("B's relay pairing window is still open: %+v", w)
	}

	// Not covered: reverse-off-by-default over the relay. B would need a
	// route back to A, and A runs no relay hub in this test.
}
