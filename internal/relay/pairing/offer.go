// Package pairing encodes the daemon's pairing offer — daemon ID, long-lived
// X25519 public key, relay endpoint — into a deep link and renders that link
// as a QR code for a mobile device to scan (ADR-0007 (g)).
//
// The offer payload always lives in the URL *fragment*, never in a query
// string: fragments are not sent to servers, so no relay, CDN, or access log
// ever sees the offer even if the link leaks into a browser.
package pairing

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spacingmind/smind/internal/relay/e2ee"
)

const (
	// OfferVersion is the pairing offer schema version.
	OfferVersion = 1

	// FragmentKey is the fragment parameter the offer payload is stored under.
	FragmentKey = "offer"

	// DefaultPairURL is the deep link a scanned QR code opens. Not fixed by
	// ADR-0007; chosen here as the smallest reasonable default so the daemon
	// has something to render before the mobile app claims a universal link.
	DefaultPairURL = "https://spacingmind.sh/pair"
)

// ErrInvalidOffer is returned when an offer is missing required fields or
// carries values that cannot be used for pairing.
var ErrInvalidOffer = errors.New("pairing: invalid offer")

// Offer is what a mobile device needs to start an E2EE session with a daemon:
// which daemon it is, that daemon's long-lived X25519 public key, and where to
// reach it (the relay endpoint).
type Offer struct {
	// DaemonID identifies the daemon the device is pairing with -- the
	// admission daemon-key-id (internal/relay/admission), NOT the relay
	// workspace ID; see WorkspaceID for that.
	DaemonID string
	// PublicKey is the daemon's long-lived X25519 public key (32 raw bytes).
	PublicKey []byte
	// Relay is the relay endpoint URL the device should connect to.
	Relay string
	// WorkspaceID is the relay workspace (internal/relay/admission.Workspace)
	// the device must present in its own Admit call to reach this daemon's
	// data. Optional from Validate's perspective (like RelayFingerprint) so
	// existing offer construction/round-trip callers that predate this
	// field keep working unmodified, but a mobile client cannot complete
	// Admit without it -- this field's addition is what actually makes
	// pairing-offer-driven admission possible at all (there was previously
	// no way for a scanned/pasted offer to convey which workspace to admit
	// under).
	WorkspaceID string
	// RelayFingerprint optionally pins the relay's self-signed TLS certificate
	// (ADR-0011 pins it daemon-side; the mobile side needs the same pin to
	// reach a self-hosted relay). Empty when the relay uses a public CA.
	RelayFingerprint string
	// Secret is the raw workspace admission secret (internal/relay/admission),
	// the same one `smind relay workspace new` prints once and the daemon
	// hashes for its own Admit calls. Optional (omitted from Validate's
	// requirements, matching RelayFingerprint) since not every offer needs
	// to grant admission -- but a mobile client cannot complete Admit
	// without it.
	//
	// This is a known milestone-1 simplification, not the intended long-term
	// design: admission today (ADR-0011) has exactly one shared secret per
	// workspace, with no separate, revocable per-device credential, so a
	// scanned/pasted pairing offer necessarily carries the same secret the
	// daemon itself holds rather than a narrower one. Milestone 2's real
	// multi-device pairing story should replace this with per-device
	// admission issued through the relay (e.g. a control-frame exchange),
	// so pairing a new device never requires handing out the workspace's
	// master secret. The offer payload living only in the URL fragment
	// (never sent to a server, see this package's doc comment) is this
	// milestone's mitigation, not a fix.
	Secret []byte
}

// wireOffer is the JSON shape that gets base64url'd into the fragment. Keys
// are short because the whole payload has to fit in a scannable QR code.
type wireOffer struct {
	V   int    `json:"v"`
	ID  string `json:"id"`
	PK  string `json:"pk"`
	Rly string `json:"relay"`
	FP  string `json:"fp,omitempty"`
	Sec string `json:"sec,omitempty"`
	WS  string `json:"ws,omitempty"`
}

// Validate reports whether the offer carries everything pairing needs.
func (o Offer) Validate() error {
	if strings.TrimSpace(o.DaemonID) == "" {
		return fmt.Errorf("%w: missing daemon ID", ErrInvalidOffer)
	}
	if _, err := e2ee.ParsePublicKey(o.PublicKey); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidOffer, err)
	}
	if strings.TrimSpace(o.Relay) == "" {
		return fmt.Errorf("%w: missing relay endpoint", ErrInvalidOffer)
	}
	if _, err := url.Parse(o.Relay); err != nil {
		return fmt.Errorf("%w: relay endpoint %q: %s", ErrInvalidOffer, o.Relay, err)
	}
	return nil
}

// EncodePayload returns the offer as an unpadded base64url JSON blob — the
// value that goes after "#offer=".
func (o Offer) EncodePayload() (string, error) {
	if err := o.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(wireOffer{
		V:   OfferVersion,
		ID:  o.DaemonID,
		PK:  base64.RawURLEncoding.EncodeToString(o.PublicKey),
		Rly: o.Relay,
		FP:  o.RelayFingerprint,
		Sec: base64.RawURLEncoding.EncodeToString(o.Secret),
		WS:  o.WorkspaceID,
	})
	if err != nil {
		return "", fmt.Errorf("encode offer: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// DecodePayload parses a base64url offer payload.
func DecodePayload(payload string) (Offer, error) {
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Offer{}, fmt.Errorf("%w: decode payload: %s", ErrInvalidOffer, err)
	}
	var wire wireOffer
	if err := json.Unmarshal(data, &wire); err != nil {
		return Offer{}, fmt.Errorf("%w: parse payload: %s", ErrInvalidOffer, err)
	}
	if wire.V != OfferVersion {
		return Offer{}, fmt.Errorf("%w: unsupported offer version %d", ErrInvalidOffer, wire.V)
	}
	pk, err := base64.RawURLEncoding.DecodeString(wire.PK)
	if err != nil {
		return Offer{}, fmt.Errorf("%w: decode public key: %s", ErrInvalidOffer, err)
	}
	var secret []byte
	if wire.Sec != "" {
		secret, err = base64.RawURLEncoding.DecodeString(wire.Sec)
		if err != nil {
			return Offer{}, fmt.Errorf("%w: decode secret: %s", ErrInvalidOffer, err)
		}
	}
	offer := Offer{
		DaemonID:         wire.ID,
		PublicKey:        pk,
		Relay:            wire.Rly,
		RelayFingerprint: wire.FP,
		Secret:           secret,
		WorkspaceID:      wire.WS,
	}
	if err := offer.Validate(); err != nil {
		return Offer{}, err
	}
	return offer, nil
}

// URL renders the offer as a pairing deep link, with the payload in the
// fragment. An empty base uses DefaultPairURL.
func (o Offer) URL(base string) (string, error) {
	if strings.TrimSpace(base) == "" {
		base = DefaultPairURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("%w: base URL %q: %s", ErrInvalidOffer, base, err)
	}
	if u.Fragment != "" {
		return "", fmt.Errorf("%w: base URL %q already has a fragment", ErrInvalidOffer, base)
	}
	payload, err := o.EncodePayload()
	if err != nil {
		return "", err
	}
	u.Fragment = FragmentKey + "=" + payload
	return u.String(), nil
}

// ParseURL extracts an offer from a pairing deep link. Only the fragment is
// consulted — an offer smuggled into the query string is not accepted, so
// code paths that would leak it to a server cannot round-trip.
func ParseURL(raw string) (Offer, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Offer{}, fmt.Errorf("%w: parse URL: %s", ErrInvalidOffer, err)
	}
	values, err := url.ParseQuery(u.Fragment)
	if err != nil {
		return Offer{}, fmt.Errorf("%w: parse fragment: %s", ErrInvalidOffer, err)
	}
	payload := values.Get(FragmentKey)
	if payload == "" {
		return Offer{}, fmt.Errorf("%w: URL has no %q fragment parameter", ErrInvalidOffer, FragmentKey)
	}
	return DecodePayload(payload)
}
