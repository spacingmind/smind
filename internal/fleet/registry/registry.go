package registry

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/store"
)

// Host lifecycle states. A host is pending from either side of a pairing
// until the responder's human approves it (Approve) or the initiator
// records the responder's confirmation (MarkApproved); revoked is terminal.
const (
	StatePending  = "pending"
	StateApproved = "approved"
	StateRevoked  = "revoked"
)

// Registry errors. The store's sentinels are re-exported so callers can
// errors.Is against one package.
var (
	// ErrNotGranted is the only error Authorize returns. Whether the host
	// is unknown, unapproved, disabled or merely unentitled is the callee's
	// business; the caller learns nothing beyond "no".
	ErrNotGranted   = errors.New("registry: not granted")
	ErrTooManyHosts = errors.New("registry: too many hosts")
	ErrBadState     = errors.New("registry: host is in the wrong state")
	ErrNotApproved  = errors.New("registry: host is not approved")

	ErrAliasTaken = store.ErrFleetAliasTaken
	ErrHostExists = store.ErrFleetHostExists
	ErrNotFound   = store.ErrFleetNotFound
)

// Host is one paired peer as the registry presents it.
type Host struct {
	HostID    string
	Alias     string
	Pub       []byte
	Label     string
	State     string
	CreatedAt time.Time
}

func toStoreHost(h Host) store.FleetHost {
	return store.FleetHost{
		HostID:    h.HostID,
		Alias:     h.Alias,
		Pub:       h.Pub,
		Label:     h.Label,
		State:     h.State,
		CreatedAt: h.CreatedAt,
	}
}

func fromStoreHost(h store.FleetHost) Host {
	return Host{
		HostID:    h.HostID,
		Alias:     h.Alias,
		Pub:       h.Pub,
		Label:     h.Label,
		State:     h.State,
		CreatedAt: h.CreatedAt,
	}
}

// Registry is the store-backed host registry. Transport configuration is
// opaque JSON here: the registry pairs hosts and decides what they may do,
// not how they talk.
type Registry struct {
	st       *store.Store
	maxHosts int
}

// New returns a registry over st capped at maxHosts hosts.
func New(st *store.Store, maxHosts int) *Registry {
	return &Registry{st: st, maxHosts: maxHosts}
}

// validatePairingInputs checks the shared pairing arguments.
func validatePairingInputs(pub ed25519.PublicKey, alias, label string) error {
	if err := ValidateAlias(alias); err != nil {
		return err
	}
	if err := ValidateLabel(label); err != nil {
		return err
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("registry: public key must be %d bytes, got %d", ed25519.PublicKeySize, len(pub))
	}
	return nil
}

// PairAsInitiator records the peer this daemon reached out to. The host is
// pending and exactly two links are written in one transaction: the
// outbound link (we dial) is enabled with the transport config, the inbound
// link (the peer dials) starts disabled carrying the default read-only
// reverse grant. Nothing the peer does is trusted until EnableReverse, and
// the pairing only becomes usable once the peer confirms (MarkApproved).
func (r *Registry) PairAsInitiator(pub ed25519.PublicKey, alias, label, transportJSON string) (Host, error) {
	if err := validatePairingInputs(pub, alias, label); err != nil {
		return Host{}, err
	}
	hostID := identity.HostID(pub)
	if err := r.checkCapacity(); err != nil {
		return Host{}, err
	}
	h := Host{
		HostID: hostID,
		Alias:  alias,
		Pub:    append([]byte(nil), pub...),
		Label:  label,
		State:  StatePending,
	}
	links := []store.FleetLink{
		{HostID: hostID, Direction: store.FleetOutbound, Enabled: true, TransportJSON: transportJSON},
		{HostID: hostID, Direction: store.FleetInbound, Enabled: false, GrantJSON: DefaultReverseGrant().JSON()},
	}
	if err := r.st.CreateFleetPair(toStoreHost(h), links); err != nil {
		return Host{}, err
	}
	return h, nil
}

// AcceptPairRequest records a peer that reached out to us. The host is
// pending and both links are disabled: the inbound link carries the default
// forward grant but stays dark until a human approves the pairing with a
// concrete grant, and the outbound link carries nothing because the peer
// dialled us, not the other way round.
func (r *Registry) AcceptPairRequest(pub ed25519.PublicKey, alias, label string) (Host, error) {
	if err := validatePairingInputs(pub, alias, label); err != nil {
		return Host{}, err
	}
	hostID := identity.HostID(pub)
	if err := r.checkCapacity(); err != nil {
		return Host{}, err
	}
	h := Host{
		HostID: hostID,
		Alias:  alias,
		Pub:    append([]byte(nil), pub...),
		Label:  label,
		State:  StatePending,
	}
	links := []store.FleetLink{
		{HostID: hostID, Direction: store.FleetInbound, Enabled: false, GrantJSON: DefaultForwardGrant().JSON()},
		{HostID: hostID, Direction: store.FleetOutbound, Enabled: false},
	}
	if err := r.st.CreateFleetPair(toStoreHost(h), links); err != nil {
		return Host{}, err
	}
	return h, nil
}

func (r *Registry) checkCapacity() error {
	n, err := r.st.CountFleetHosts()
	if err != nil {
		return fmt.Errorf("registry: count hosts: %w", err)
	}
	if n >= r.maxHosts {
		return ErrTooManyHosts
	}
	return nil
}

// Approve is the responder's human act: it turns a pending pairing live
// with a concrete grant. The grant must name at least one write workspace
// for any mutating op. The inbound link is enabled before the host's state
// flips, so a crash between the two leaves the pairing refused (the host is
// still pending) rather than approved without a working grant.
func (r *Registry) Approve(hostID string, g Grant) error {
	if err := g.ValidateForApproval(); err != nil {
		return err
	}
	h, err := r.Get(hostID)
	if err != nil {
		return err
	}
	if h.State != StatePending {
		return fmt.Errorf("registry: approve %s: %w", hostID, ErrBadState)
	}
	l, err := r.st.GetFleetLink(hostID, store.FleetInbound)
	if err != nil {
		return err
	}
	if err := r.st.PutFleetLink(store.FleetLink{
		HostID:        hostID,
		Direction:     store.FleetInbound,
		Enabled:       true,
		GrantJSON:     g.JSON(),
		TransportJSON: l.TransportJSON,
	}); err != nil {
		return err
	}
	return r.setState(h, StateApproved)
}

// MarkApproved is the initiator's mirror of Approve: the responder
// confirmed the pairing out of band, so the host leaves pending. Its links
// are untouched -- the reverse grant only goes live via EnableReverse.
func (r *Registry) MarkApproved(hostID string) error {
	h, err := r.Get(hostID)
	if err != nil {
		return err
	}
	if h.State != StatePending {
		return fmt.Errorf("registry: mark approved %s: %w", hostID, ErrBadState)
	}
	return r.setState(h, StateApproved)
}

// Reject deletes a pending pairing outright. An approved host must be
// revoked (leaving an audit trail), not silently removed.
func (r *Registry) Reject(hostID string) error {
	h, err := r.Get(hostID)
	if err != nil {
		return err
	}
	if h.State != StatePending {
		return fmt.Errorf("registry: reject %s: %w", hostID, ErrBadState)
	}
	return r.st.DeleteFleetHost(hostID)
}

// EnableReverse lets the peer use the inbound link: it must be enabled
// (default after approval, before the first human look at the grant), the
// stored grant is kept and only repaired when it does not parse. It is
// idempotent.
func (r *Registry) EnableReverse(hostID string) error {
	return r.setInbound(hostID, true, true)
}

// DisableReverse darkens the inbound link without touching the grant, so
// re-enabling returns to exactly the same permissions.
func (r *Registry) DisableReverse(hostID string) error {
	return r.setInbound(hostID, false, false)
}

func (r *Registry) setInbound(hostID string, enabled, requireApproved bool) error {
	if requireApproved {
		h, err := r.Get(hostID)
		if err != nil {
			return err
		}
		if h.State != StateApproved {
			return fmt.Errorf("registry: enable reverse %s: %w", hostID, ErrNotApproved)
		}
	}
	l, err := r.st.GetFleetLink(hostID, store.FleetInbound)
	if err != nil {
		return err
	}
	grant := l.GrantJSON
	if enabled {
		if _, err := ParseGrant(grant); err != nil {
			grant = DefaultReverseGrant().JSON()
		}
	}
	l.Enabled = enabled
	l.GrantJSON = grant
	return r.st.PutFleetLink(l)
}

// SetOutboundEnabled marks our dial-out direction usable or not, after a
// link-state notice from the transport. The grant and transport config are
// kept.
func (r *Registry) SetOutboundEnabled(hostID string, on bool) error {
	l, err := r.st.GetFleetLink(hostID, store.FleetOutbound)
	if err != nil {
		return err
	}
	l.Enabled = on
	return r.st.PutFleetLink(l)
}

// SetGrant replaces the inbound link's grant. A grant that keeps the link
// enabled must satisfy the approval rules (a mutating op needs a write
// workspace); a grant written while the link is dark only needs a valid
// shape. Enabled never changes here.
func (r *Registry) SetGrant(hostID string, g Grant) error {
	l, err := r.st.GetFleetLink(hostID, store.FleetInbound)
	if err != nil {
		return err
	}
	if l.Enabled {
		if err := g.ValidateForApproval(); err != nil {
			return err
		}
	} else if err := g.Validate(); err != nil {
		return err
	}
	l.GrantJSON = g.JSON()
	return r.st.PutFleetLink(l)
}

// Revoke is terminal: the host stays in the registry, marked revoked, with
// both links dark. Revoking keeps the alias and history queryable instead
// of freeing the alias for an impostor.
func (r *Registry) Revoke(hostID string) error {
	h, err := r.Get(hostID)
	if err != nil {
		return err
	}
	for _, direction := range []string{store.FleetOutbound, store.FleetInbound} {
		l, err := r.st.GetFleetLink(hostID, direction)
		if err != nil {
			return err
		}
		l.Enabled = false
		if err := r.st.PutFleetLink(l); err != nil {
			return err
		}
	}
	return r.setState(h, StateRevoked)
}

// Remove deletes the host and its links entirely.
func (r *Registry) Remove(hostID string) error {
	return r.st.DeleteFleetHost(hostID)
}

// Rename changes the alias a host is addressed by.
func (r *Registry) Rename(hostID, alias string) error {
	if err := ValidateAlias(alias); err != nil {
		return err
	}
	h, err := r.Get(hostID)
	if err != nil {
		return err
	}
	h.Alias = alias
	return r.setState(h, h.State)
}

func (r *Registry) setState(h Host, state string) error {
	return r.st.UpdateFleetHost(h.HostID, h.Alias, h.Label, state)
}

// CanCall reports whether the daemon may dial this host: it must be
// approved with the outbound link enabled. Any doubt is a no.
func (r *Registry) CanCall(hostID string) bool {
	h, err := r.Get(hostID)
	if err != nil || h.State != StateApproved {
		return false
	}
	l, err := r.st.GetFleetLink(hostID, store.FleetOutbound)
	return err == nil && l.Enabled
}

// Authorize is the callee-side check for a peer request. It returns
// ErrNotGranted -- and only ErrNotGranted -- unless the host is approved,
// its inbound link is enabled, the grant parses and names the op, and the
// workspace is in scope: read needs workspaceID 0 (any) or a readable id;
// the mutating ops need a specific writable id. A caller therefore never
// learns why it was refused, only that it was.
func (r *Registry) Authorize(hostID, op string, workspaceID int64) (Grant, error) {
	refuse := func() (Grant, error) { return Grant{}, ErrNotGranted }
	h, err := r.Get(hostID)
	if err != nil || h.State != StateApproved {
		return refuse()
	}
	l, err := r.st.GetFleetLink(hostID, store.FleetInbound)
	if err != nil || !l.Enabled {
		return refuse()
	}
	g, err := ParseGrant(l.GrantJSON)
	if err != nil || !g.Allows(op) {
		return refuse()
	}
	if op == OpRead {
		if workspaceID != 0 && !g.CanRead(workspaceID) {
			return refuse()
		}
		return g, nil
	}
	if workspaceID == 0 || !g.CanWrite(workspaceID) {
		return refuse()
	}
	return g, nil
}

// List returns every host ordered by alias.
func (r *Registry) List() ([]Host, error) {
	hs, err := r.st.ListFleetHosts()
	if err != nil {
		return nil, err
	}
	out := make([]Host, 0, len(hs))
	for _, h := range hs {
		out = append(out, fromStoreHost(h))
	}
	return out, nil
}

// Get returns the host with the given host id.
func (r *Registry) Get(hostID string) (Host, error) {
	h, err := r.st.GetFleetHost(hostID)
	if err != nil {
		return Host{}, err
	}
	return fromStoreHost(h), nil
}

// ByAlias returns the host the user addresses by that alias.
func (r *Registry) ByAlias(alias string) (Host, error) {
	h, err := r.st.GetFleetHostByAlias(alias)
	if err != nil {
		return Host{}, err
	}
	return fromStoreHost(h), nil
}

// Links returns the host's outbound and inbound links.
func (r *Registry) Links(hostID string) (outbound, inbound store.FleetLink, err error) {
	outbound, err = r.st.GetFleetLink(hostID, store.FleetOutbound)
	if err != nil {
		return store.FleetLink{}, store.FleetLink{}, err
	}
	inbound, err = r.st.GetFleetLink(hostID, store.FleetInbound)
	if err != nil {
		return store.FleetLink{}, store.FleetLink{}, err
	}
	return outbound, inbound, nil
}
