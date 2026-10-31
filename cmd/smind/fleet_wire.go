package main

import (
	"log"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/fleet"
	"github.com/spacingmind/smind/internal/fleet/link"
	"github.com/spacingmind/smind/internal/fleet/pairing"
	"github.com/spacingmind/smind/internal/server"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsapi"
)

// fleetDir is the directory under the smind home that holds the host key.
const fleetDir = "fleet"

// newFleet wires the fleet layer (ADR-0048 F1b) into the daemon. With
// fleet.enabled false it touches nothing -- no key, registry or file -- and
// the fleet.* methods stay unregistered. Like the other SetX wiring it must
// run before the server accepts a connection. The returned stop closes the
// link manager.
func newFleet(srv *server.Server, cfg config.Config, home, version string, db *store.Store) (stop func()) {
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
	})
	svc := link.NewService(link.ServiceOptions{
		Key: key, Registry: reg, Secrets: secrets, Manager: mgr, Window: window, Version: version,
		DefaultAddresses: func() []string { return fleetDefaultAddresses(cfg.Server) },
	})
	srv.API().SetFleet(wsapi.FleetOptions{Key: key, Registry: reg, Window: window, Links: svc, Version: version})
	svc.Start()
	return mgr.Close
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
