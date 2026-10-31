// Package fleet is the fleet layer's front door (ADR-0048 F1a). The fleet
// is off unless a human sets fleet.enabled in config.yaml, and the off
// state is byte-for-byte today's daemon: no identity is created, no
// registry is opened, nothing dials out.
package fleet

import (
	"errors"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/store"
)

// ErrOff is returned by every entry point while fleet.enabled is false.
var ErrOff = errors.New("fleet: disabled (fleet.enabled is false)")

// LoadIdentity returns the host key, creating it on first use. While the
// fleet is off it returns ErrOff without touching the filesystem, so an
// off-by-default daemon never grows a fleet directory or host-key file.
func LoadIdentity(home string, cfg config.FleetConfig) (*identity.Key, error) {
	if !cfg.Enabled {
		return nil, ErrOff
	}
	return identity.LoadOrCreate(home)
}

// OpenRegistry returns the host registry over st. While the fleet is off it
// returns ErrOff without reading or writing the store.
func OpenRegistry(st *store.Store, cfg config.FleetConfig) (*registry.Registry, error) {
	if !cfg.Enabled {
		return nil, ErrOff
	}
	return registry.New(st, cfg.MaxHosts), nil
}
