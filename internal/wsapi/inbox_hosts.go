package wsapi

import (
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/inbox"
	"github.com/spacingmind/smind/internal/store"
)

// The fleet-derived inbox sources (ADR-0048 S9): a pairing that awaits this
// human's approval becomes a host_pair row; a refused or long-down link
// becomes a host_refused or host_down row. The link-issue source is the
// FleetLinks of SetFleet (see fleetInboxLinkIssues).

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

// fleetInboxLinkIssues converts the link manager's issues to inbox ones,
// reading fleet.enabled on every call like the pairs source.
func fleetInboxLinkIssues(links FleetLinks, enabled func() bool) func() []inbox.HostLinkIssue {
	return func() []inbox.HostLinkIssue {
		if !enabled() {
			return nil
		}
		var out []inbox.HostLinkIssue
		for _, is := range links.LinkIssues() {
			out = append(out, inbox.HostLinkIssue{HostID: is.HostID, Alias: is.Alias, Kind: is.Kind, Code: is.Code, Guidance: is.Guidance, Since: is.Since})
		}
		return out
	}
}
