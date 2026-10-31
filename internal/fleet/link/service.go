package link

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/fleet/pairing"
	"github.com/spacingmind/smind/internal/fleet/registry"
	relaypairing "github.com/spacingmind/smind/internal/relay/pairing"
	"github.com/spacingmind/smind/internal/wsapi"
)

// ServiceOptions configures NewService.
type ServiceOptions struct {
	Key      *identity.Key
	Registry *registry.Registry
	Secrets  *Secrets
	Manager  *Manager
	Window   *pairing.Window
	Version  string
	// DefaultAddresses are the direct addresses this daemon is reachable
	// on, from config; nil when it does not know them.
	DefaultAddresses func() []string
	// Hostname names this host to a peer; os.Hostname when nil.
	Hostname func() string
	Now      func() time.Time
	// Dial opens the pairing connection; the default is DialDirect with the
	// password the human gave.
	Dial DialFunc
	// RelayOffer is the daemon's current relay pairing offer (relay endpoint,
	// relay host id, pinned fingerprint, host secret, daemon E2EE key); it
	// errors when no relay is configured. Nil disables relay invites.
	RelayOffer func() (relaypairing.Offer, error)
	// OpenRelayWindow opens the relay hub's device pairing window so the first
	// new device, the inviting peer, is admitted at relay level.
	OpenRelayWindow func(d time.Duration, maxNew int) error
	// RevokeRelayDevice revokes a relay device by id; best effort.
	RevokeRelayDevice func(deviceID string)
}

// Service is the dial-side admin half of the fleet: it mints invites, pairs
// to a pasted invite, probes links and sets routes. It implements
// wsapi.FleetLinks over the registry and the link manager.
type Service struct {
	o ServiceOptions
}

var _ wsapi.FleetLinks = (*Service)(nil)

// maxLabelBytes caps the label this host announces to a peer.
const maxLabelBytes = 64

// NewService returns a service over o.
func NewService(o ServiceOptions) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Hostname == nil {
		o.Hostname = func() string { h, _ := os.Hostname(); return h }
	}
	s := &Service{o: o}
	if o.Manager != nil {
		o.Manager.setOnConnected(s.sendReverseState)
	}
	return s
}

// onConnectedTimeout bounds the state notice sent when a link comes up.
const onConnectedTimeout = 10 * time.Second

// sendReverseState tells a freshly connected peer the current state of our
// direction to it. It is idempotent, so a peer that missed an earlier notice
// is healed; errors are ignored.
func (s *Service) sendReverseState(hostID string, c Conn) {
	h, err := s.o.Registry.Get(hostID)
	if err != nil || h.State != registry.StateApproved {
		return
	}
	_, in, err := s.o.Registry.Links(hostID)
	if err != nil {
		return
	}
	state := "reverse_disabled"
	if in.Enabled {
		state = "reverse_enabled"
	}
	ctx, cancel := context.WithTimeout(context.Background(), onConnectedTimeout)
	defer cancel()
	_ = c.Call(ctx, hello.MethodLinkState, map[string]string{"state": state}, nil)
}

// Start resumes the links of every host whose outbound direction is enabled
// and whose pairing is pending or approved, after a daemon restart.
func (s *Service) Start() {
	hosts, err := s.o.Registry.List()
	if err != nil {
		return
	}
	for _, h := range hosts {
		if h.State != registry.StatePending && h.State != registry.StateApproved {
			continue
		}
		if out, _, err := s.o.Registry.Links(h.HostID); err == nil && out.Enabled {
			s.o.Manager.Ensure(h.HostID)
		}
	}
}

// Invite mints an invite deep link and opens the pairing window for it. The
// invite carries no password: the human gives that to the invitee apart.
func (s *Service) Invite(_ context.Context, p wsapi.FleetInviteParams) (wsapi.FleetInviteResult, error) {
	if p.Via != "" && p.Via != "direct" && p.Via != "relay" {
		return wsapi.FleetInviteResult{}, fmt.Errorf("unknown invite route %q", p.Via)
	}
	viaRelay := p.Via == "relay"
	var relayPayload string
	addrs := p.Addresses
	if viaRelay {
		if s.o.RelayOffer == nil {
			return wsapi.FleetInviteResult{}, errNoRelay
		}
		offer, err := s.o.RelayOffer()
		if err != nil {
			return wsapi.FleetInviteResult{}, fmt.Errorf("%w (%v)", errNoRelay, err)
		}
		if relayPayload, err = offer.EncodePayload(); err != nil {
			return wsapi.FleetInviteResult{}, err
		}
	} else {
		if len(addrs) == 0 && s.o.DefaultAddresses != nil {
			addrs = s.o.DefaultAddresses()
		}
		if len(addrs) == 0 {
			return wsapi.FleetInviteResult{}, errors.New("pass --addr: this daemon does not know its own reachable address")
		}
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return wsapi.FleetInviteResult{}, err
	}
	inv := pairing.Invite{
		V:         pairing.InviteVersion,
		HostID:    s.o.Key.HostID(),
		Pub:       s.o.Key.Public(),
		Addresses: addrs,
		Relay:     relayPayload,
		Nonce:     nonce,
	}
	// Validate before opening the window, so a bad address does not leave
	// a window open for nothing.
	inv.Expires = s.o.Now().Add(pairing.DefaultWindow)
	if err := inv.Validate(); err != nil {
		return wsapi.FleetInviteResult{}, err
	}
	dur := time.Duration(p.WindowSeconds) * time.Second
	_, expires := s.o.Window.Open(dur, 1)
	inv.Expires = expires
	if viaRelay && s.o.OpenRelayWindow != nil {
		if err := s.o.OpenRelayWindow(expires.Sub(s.o.Now()), 1); err != nil {
			s.o.Window.Close()
			return wsapi.FleetInviteResult{}, fmt.Errorf("open the relay pairing window: %w", err)
		}
	}
	link, err := inv.Encode()
	if err != nil {
		s.o.Window.Close()
		return wsapi.FleetInviteResult{}, err
	}
	return wsapi.FleetInviteResult{Invite: link, ExpiresAt: expires, HostID: inv.HostID}, nil
}

var errNoRelay = errors.New("this daemon is not connected to a relay: run `smind relay connect` first")

// pairResult is the callee's answer to fleet.pair.request.
type pairResult struct {
	HostID string `json:"hostId"`
	Alias  string `json:"alias"`
	State  string `json:"state"`
	SAS    string `json:"sas"`
}

// Add pairs to the inviter behind p.Invite: it dials the invite's address,
// runs the handshake, asks to pair and compares the SAS before recording
// anything. The password goes only to the dialer.
func (s *Service) Add(ctx context.Context, p wsapi.FleetAddParams) (wsapi.FleetAddResult, error) {
	inv, err := pairing.Decode(p.Invite)
	if err != nil {
		return wsapi.FleetAddResult{}, err
	}
	if s.o.Now().After(inv.Expires) {
		return wsapi.FleetAddResult{}, errors.New("invite expired")
	}
	alias := p.Alias
	if alias == "" {
		alias = "host-" + inv.HostID[:6]
	}
	if err := registry.ValidateAlias(alias); err != nil {
		return wsapi.FleetAddResult{}, err
	}
	if _, err := s.o.Registry.ByAlias(alias); err == nil {
		return wsapi.FleetAddResult{}, fmt.Errorf("alias %q is already used by a paired host", alias)
	} else if !errors.Is(err, registry.ErrNotFound) {
		return wsapi.FleetAddResult{}, err
	}
	if _, err := s.o.Registry.Get(inv.HostID); err == nil {
		return wsapi.FleetAddResult{}, errors.New("already paired")
	} else if !errors.Is(err, registry.ErrNotFound) {
		return wsapi.FleetAddResult{}, err
	}
	var (
		tr      Transport
		warning string
		entry   = Entry{Password: p.Password}
	)
	if addr, w, cerr := chooseAddress(inv, p.AllowCleartext); cerr == nil {
		tr = Transport{Kind: KindDirect, URL: addr, AllowCleartext: p.AllowCleartext}
		warning = w
	} else if inv.Relay != "" {
		offer, derr := relaypairing.DecodePayload(inv.Relay)
		if derr != nil {
			return wsapi.FleetAddResult{}, derr
		}
		tr = Transport{Kind: KindRelay, Relay: RelayRouteFromOffer(offer)}
		entry.RelaySecret = RelaySecretHex(offer)
	} else {
		return wsapi.FleetAddResult{}, cerr
	}
	conn, err := s.dial(ctx, tr, inv.HostID, entry)
	if err != nil {
		return wsapi.FleetAddResult{}, err
	}
	defer conn.Close()
	peer := Peer{HostID: inv.HostID, Pub: ed25519.PublicKey(inv.Pub), Alias: alias}
	res, err := Handshake(ctx, conn, s.o.Key, peer, s.o.Version, s.o.Now)
	if err != nil {
		return wsapi.FleetAddResult{}, err
	}
	if res.State != "pairing" {
		return wsapi.FleetAddResult{}, errors.New("the host is not accepting a new pairing; open a window with `smind host invite` on it")
	}
	label := s.o.Hostname()
	if len(label) > maxLabelBytes {
		label = label[:maxLabelBytes]
	}
	if registry.ValidateLabel(label) != nil {
		label = ""
	}
	var pr pairResult
	req := map[string]string{"alias": s.ownAlias(label), "label": label}
	if err := conn.Call(ctx, hello.MethodPairRequest, req, &pr); err != nil {
		return wsapi.FleetAddResult{}, fmt.Errorf("pairing request: %w", err)
	}
	localSAS := pairing.SAS(s.o.Key.Public(), inv.Pub, res.NonceC, res.NonceA)
	if pr.SAS != localSAS {
		return wsapi.FleetAddResult{}, errors.New("the SAS differs: possible interception, aborting")
	}
	h, err := s.o.Registry.PairAsInitiator(ed25519.PublicKey(inv.Pub), alias, "", tr.JSON())
	if err != nil {
		return wsapi.FleetAddResult{}, err
	}
	if entry != (Entry{}) {
		if err := s.o.Secrets.Put(h.HostID, entry); err != nil {
			return wsapi.FleetAddResult{}, fmt.Errorf("store the credentials: %w", err)
		}
	}
	s.o.Manager.Ensure(h.HostID)
	return wsapi.FleetAddResult{HostID: h.HostID, Alias: alias, State: registry.StatePending, SAS: localSAS, Warning: warning}, nil
}

// chooseAddress picks the first invite address a direct link may use.
func chooseAddress(inv pairing.Invite, allowCleartext bool) (addr, warning string, err error) {
	if len(inv.Addresses) == 0 {
		return "", "", errors.New("the invite has no direct address")
	}
	for _, a := range inv.Addresses {
		w, cerr := CheckDirectRoute(a, allowCleartext)
		if cerr != nil {
			err = cerr
			continue
		}
		return a, w, nil
	}
	return "", "", err
}

func (s *Service) dial(ctx context.Context, tr Transport, hostID string, entry Entry) (Conn, error) {
	if s.o.Dial != nil {
		return s.o.Dial(ctx, tr, hostID)
	}
	if tr.Kind == KindRelay {
		return DialRelay(ctx, tr, s.o.Key, entry, "fleet")
	}
	return DialDirect(ctx, tr, entry.Password)
}

// ownAlias derives the alias this host proposes for itself from its label.
func (s *Service) ownAlias(label string) string {
	fallback := "host-" + s.o.Key.HostID()[:6]
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	a := b.String()
	if a == "" {
		return fallback
	}
	if a[0] < 'a' || a[0] > 'z' {
		a = "h" + a
	}
	if len(a) > 32 {
		a = a[:32]
	}
	if registry.ValidateAlias(a) != nil {
		return fallback
	}
	return a
}

// Test probes the link to hostID with one fresh handshake.
func (s *Service) Test(ctx context.Context, hostID string) (wsapi.FleetTestResult, error) {
	st, d, err := s.o.Manager.Test(ctx, hostID)
	if err != nil {
		return wsapi.FleetTestResult{}, err
	}
	return wsapi.FleetTestResult{OK: st.State == StateConnected, LatencyMs: d.Milliseconds(), State: string(st.State), Code: st.Code, Detail: st.Detail}, nil
}

// NotifyReverse tells the peer over the live link that its direction to us
// flipped.
func (s *Service) NotifyReverse(ctx context.Context, hostID string, on bool) error {
	c, ok := s.o.Manager.Conn(hostID)
	if !ok {
		return errors.New("no live link to the host; it will learn the new state when the link comes up")
	}
	state := "reverse_disabled"
	if on {
		state = "reverse_enabled"
	}
	return c.Call(ctx, hello.MethodLinkState, map[string]string{"state": state}, nil)
}

// PeerReverseChanged starts our link to a peer that enabled its direction,
// or stops it when the peer disabled it. Without a route the loop reports
// "no route" until a human runs `smind host route`.
func (s *Service) PeerReverseChanged(hostID string, on bool) {
	if on {
		s.o.Manager.Ensure(hostID)
		return
	}
	s.o.Manager.Drop(hostID)
}

// LinkState reports the live link's state, refusal code and detail; "none"
// when no loop exists for the host.
func (s *Service) LinkState(hostID string) (state, code, detail string) {
	st := s.o.Manager.Status(hostID)
	if st.State == StateDown && st.Detail == detailNotLinked {
		return "none", "", ""
	}
	return string(st.State), st.Code, st.Detail
}

// linkDownAfter is how long a link may be down before it is worth a human's
// attention; shorter outages are ordinary reconnects.
const linkDownAfter = 5 * time.Minute

// LinkIssues lists the approved hosts whose outbound link is refused, or has
// been down (or disabled by the peer) for more than five minutes.
func (s *Service) LinkIssues() []wsapi.FleetLinkIssue {
	hosts, err := s.o.Registry.List()
	if err != nil {
		return nil
	}
	now := s.o.Now()
	var out []wsapi.FleetLinkIssue
	for _, h := range hosts {
		if h.State != registry.StateApproved {
			continue
		}
		if o, _, err := s.o.Registry.Links(h.HostID); err != nil || !o.Enabled {
			continue
		}
		st, since := s.o.Manager.Health(h.HostID)
		if since.IsZero() {
			since = st.Since
		}
		issue := wsapi.FleetLinkIssue{HostID: h.HostID, Alias: h.Alias, Since: since}
		switch st.State {
		case StateRefused:
			issue.Kind, issue.Code, issue.Guidance = "refused", st.Code, st.Detail
		case StateDown, StatePeerDisabled:
			if st.Detail == detailNotLinked || now.Sub(since) <= linkDownAfter {
				continue
			}
			issue.Kind = "down"
		default:
			continue
		}
		out = append(out, issue)
	}
	return out
}

// Forget stops the link to a removed host and drops its secrets.
func (s *Service) Forget(hostID string) {
	s.o.Manager.Drop(hostID)
	_ = s.o.Secrets.Delete(hostID)
	if s.o.RevokeRelayDevice != nil {
		s.o.RevokeRelayDevice(hostID)
	}
}

// SetRoute stores the direct route (and password) for hostID and restarts
// its link so the new route takes effect.
func (s *Service) SetRoute(_ context.Context, p wsapi.FleetRouteParams) error {
	if _, err := CheckDirectRoute(p.Address, p.AllowCleartext); err != nil {
		return err
	}
	tr := Transport{Kind: KindDirect, URL: p.Address, AllowCleartext: p.AllowCleartext}
	if err := s.o.Registry.SetOutboundTransport(p.HostID, tr.JSON()); err != nil {
		return err
	}
	if p.Password != "" {
		if err := s.o.Secrets.Put(p.HostID, Entry{Password: p.Password}); err != nil {
			return fmt.Errorf("store the password: %w", err)
		}
	}
	out, _, err := s.o.Registry.Links(p.HostID)
	if err != nil {
		return err
	}
	if out.Enabled {
		s.o.Manager.Drop(p.HostID)
		s.o.Manager.Ensure(p.HostID)
	}
	return nil
}
