package main

import (
	"encoding/hex"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/fleet"
	"github.com/spacingmind/smind/internal/fleet/client"
	"github.com/spacingmind/smind/internal/fleet/link"
	"github.com/spacingmind/smind/internal/fleet/pairing"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypairing "github.com/spacingmind/smind/internal/relay/pairing"
	"github.com/spacingmind/smind/internal/server"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// fleetDir is the directory under the smind home that holds the host key.
const fleetDir = "fleet"

// newFleet wires the fleet layer (ADR-0048 F1b) into the daemon. With
// fleet.enabled false it touches nothing -- no key, registry or file -- and
// the fleet.* methods stay unregistered. Like the other SetX wiring it must
// run before the server accepts a connection. The returned stop closes the
// link manager.
func newFleet(srv *server.Server, cfg config.Config, home, version string, db *store.Store, hub *bridge.Hub) (stop func()) {
	noop := func() {}
	if !cfg.Fleet.Enabled {
		return noop
	}
	key, err := fleet.LoadIdentity(filepath.Join(home, fleetDir), cfg.Fleet)
	if err != nil {
		log.Printf("fleet: disabled, no host identity: %v", err)
		return noop
	}
	reg, err := fleet.OpenRegistry(db, cfg.Fleet)
	if err != nil {
		log.Printf("fleet: disabled, no registry: %v", err)
		return noop
	}
	secrets := link.OpenSecrets(home)
	window := pairing.NewWindow(nil)
	mgr := link.NewManager(link.ManagerOptions{
		Key: key, Registry: reg, Secrets: secrets, Version: version,
		BackoffMin: time.Duration(cfg.Fleet.ObservationBackoffMinSeconds) * time.Second,
		BackoffMax: time.Duration(cfg.Fleet.ObservationBackoffMaxSeconds) * time.Second,
		// FleetHostsChanged is a no-op until SetFleet below has run.
		OnChange: srv.API().FleetHostsChanged,
	})
	so := link.ServiceOptions{
		Key: key, Registry: reg, Secrets: secrets, Manager: mgr, Window: window, Version: version,
		DefaultAddresses: func() []string { return fleetDefaultAddresses(cfg.Server) },
	}
	if hub != nil {
		so.RelayOffer = func() (relaypairing.Offer, error) { return fleetRelayOffer(home) }
		so.OpenRelayWindow = hub.OpenPairingWindow
		so.RevokeRelayDevice = func(id string) { _, _ = hub.RevokeDevice(id) }
	}
	svc := link.NewService(so)
	fo := wsapi.FleetOptions{
		Key: key, Registry: reg, Window: window, Links: svc, Version: version,
		Store: db, Workspaces: workspace.New(db),
	}
	if h, err := os.UserHomeDir(); err == nil {
		fo.HomeDirs = []string{h}
	}
	srv.API().SetFleet(fo)
	srv.API().SetFleetRemote(wsapi.FleetRemoteOptions{
		Conn: func(hostID string) (client.Caller, client.LinkState) {
			if c, ok := mgr.Conn(hostID); ok {
				return c, client.LinkConnected
			}
			switch mgr.Status(hostID).State {
			case link.StateConnecting:
				return nil, client.LinkConnecting
			default:
				return nil, client.LinkOther
			}
		},
		RunOrigin: func(runID string, taskID int64) (string, error) {
			origin, err := db.RunOrigin(runID)
			if err != nil {
				return "", err
			}
			if origin != "" {
				return origin, nil
			}
			if taskID != 0 {
				th, _, err := db.TaskOrigin(taskID)
				if err != nil {
					return "", err
				}
				return th, nil
			}
			return "", nil
		},
	})
	svc.Start()
	return mgr.Close
}

// fleetRelayOffer is the pairing offer for the relay `smind relay connect`
// configured: what a peer host needs to reach this daemon through the relay.
// Unlike the mobile offer it names the relay's native gRPC address (the one
// the daemon itself dials), because a fleet host is a Go client.
func fleetRelayOffer(home string) (relaypairing.Offer, error) {
	rc, ok, err := bridge.LoadConfig(home)
	if err != nil {
		return relaypairing.Offer{}, err
	}
	if !ok {
		return relaypairing.Offer{}, errors.New("no relay configured")
	}
	kp, err := e2ee.LoadOrCreateKeyPair(home)
	if err != nil {
		return relaypairing.Offer{}, err
	}
	secret, err := hex.DecodeString(rc.SecretHex)
	if err != nil {
		return relaypairing.Offer{}, errors.New("stored relay secret is not hex")
	}
	return relaypairing.Offer{
		DaemonID:         bridge.DaemonKeyID(kp),
		PublicKey:        kp.Public(),
		Relay:            "https://" + rc.RelayAddress,
		RelayFingerprint: rc.Fingerprint,
		Secret:           secret,
		WorkspaceID:      rc.WorkspaceID,
	}, nil
}

// fleetDefaultAddresses is the direct address a peer may reach this daemon
// on, as far as config tells: a configured non-wildcard host, or the
// loopback address (useful on one machine only). A wildcard bind says
// nothing about which address is reachable, so the human passes --addr.
func fleetDefaultAddresses(sc config.ServerConfig) []string {
	host, port, err := sc.ListenHostPort()
	if err != nil {
		return nil
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		return nil
	}
	if config.IsLoopbackHost(host) {
		host = "127.0.0.1"
	}
	return []string{"ws://" + net.JoinHostPort(host, strconv.Itoa(port))}
}
