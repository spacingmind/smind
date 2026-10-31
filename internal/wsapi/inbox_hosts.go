package wsapi

import (
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/inbox"
	"github.com/spacingmind/smind/internal/store"
)

// The fleet-derived inbox sources (ADR-0048 S9): a pairing that awaits this
// human's approval becomes a host_pair row. The link-issue source
// (host_down, host_refused) is not wired here: F1b's link manager will feed
// it, so the Service's HostLinkIssues stays nil for now.

// configFleetEnabled reads fleet.enabled on every call, so a human edit
// applies without a restart. An unreadable or invalid file is OFF. Tests
// replace it.
var configFleetEnabled = func() bool {
	cfg, err := config.Load()
	return err == nil && cfg.Fleet.Enabled
}

// fleetInboxPairs lists the responder-side pending pairings: the hosts that
// reached out to us and wait for this human's approval. The initiator side
// is excluded by its own outbound link being enabled (we dial it), while a
// responder's outbound link stays dark until the pairing is approved and
// reverse is turned on.
func fleetInboxPairs(db *store.Store, enabled func() bool) func() []inbox.HostPair {
	return func() []inbox.HostPair {
		if !enabled() {
			return nil
		}
		hosts, err := db.ListFleetHosts()
		if err != nil {
			return nil
		}
		var out []inbox.HostPair
		for _, h := range hosts {
			if h.State != registry.StatePending {
				continue
			}
			l, err := db.GetFleetLink(h.HostID, store.FleetOutbound)
			if err != nil || l.Enabled {
				continue
			}
			out = append(out, inbox.HostPair{HostID: h.HostID, Alias: h.Alias, Label: h.Label, Since: h.CreatedAt})
		}
		return out
	}
}
