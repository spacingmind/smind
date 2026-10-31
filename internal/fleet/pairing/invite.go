// Package pairing encodes a fleet invite — the inviter's host identity plus
// where to reach it — as a deep link the invitee pastes or scans, and holds
// the pairing-window and SAS primitives the live pairing flow uses
// (ADR-0048 F1b).
//
// The invite payload always lives in the URL fragment, never in a query
// string: fragments are not sent to servers, so no relay, CDN or access log
// ever sees the invite even if the link leaks into a browser. The Relay
// field deliberately carries the relay pairing offer (which contains the
// relay host secret) — that is the one thing an out-of-band invite must
// deliver for the invitee to reach the fleet's relay at all.
package pairing

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/spacingmind/smind/internal/fleet/identity"
	relaypairing "github.com/spacingmind/smind/internal/relay/pairing"
)

const (
	// Scheme is the deep link scheme of a fleet invite.
	Scheme = "smind-host"
	// InviteVersion is the invite payload schema version.
	InviteVersion = 1
	// maxInviteBytes bounds a pasted link before parsing, so a stray huge
	// string is refused without being decoded.
	maxInviteBytes = 4096
	// nonceLen is the invite nonce size in bytes.
	nonceLen = 16
	// maxAddresses bounds how many direct addresses one invite may carry.
	maxAddresses = 4
)

// ErrInvalidInvite is returned for any invite that fails validation or
// decoding; specific reasons are wrapped with %w.
var ErrInvalidInvite = errors.New("pairing: invalid invite")

// Invite is everything an invitee needs to reach and identify the inviter:
// the inviter's host id and pinned public key, optional direct addresses, an
// optional relay pairing offer and the invite's expiry.
type Invite struct {
	V int `json:"v"`
	// HostID is the inviter's host id, self-authenticating from Pub.
	HostID string `json:"hostId"`
	// Pub is the inviter's Ed25519 public key, pinned on first pairing.
	Pub []byte `json:"pub"`
	// Addresses are direct ws/wss addresses the invitee may dial.
	Addresses []string `json:"addresses,omitempty"`
	// Relay is the relay pairing offer payload (relaypairing.Offer
	// EncodePayload), which carries the relay host secret by design: the
	// invite is the out-of-band channel for the one secret the invitee
	// needs to reach the fleet's relay.
	Relay string `json:"relay,omitempty"`
	// Expires bounds the invite's validity.
	Expires time.Time `json:"expires"`
	// Nonce makes each invite unique, so two invites never share a SAS.
	Nonce []byte `json:"nonce"`
}

// Validate reports whether the invite carries a usable identity and at
// least one way to reach the inviter.
func (i Invite) Validate() error {
	if i.V != InviteVersion {
		return fmt.Errorf("%w: version %d, want %d", ErrInvalidInvite, i.V, InviteVersion)
	}
	if len(i.Pub) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: public key is %d bytes, want %d", ErrInvalidInvite, len(i.Pub), ed25519.PublicKeySize)
	}
	if i.HostID != identity.HostID(ed25519.PublicKey(i.Pub)) {
		return fmt.Errorf("%w: host id does not match public key", ErrInvalidInvite)
	}
	if len(i.Addresses) == 0 && i.Relay == "" {
		return fmt.Errorf("%w: no address and no relay offer", ErrInvalidInvite)
	}
	if len(i.Addresses) > maxAddresses {
		return fmt.Errorf("%w: %d addresses, max %d", ErrInvalidInvite, len(i.Addresses), maxAddresses)
	}
	for _, a := range i.Addresses {
		u, err := url.Parse(a)
		if err != nil {
			return fmt.Errorf("%w: address %q: %w", ErrInvalidInvite, a, err)
		}
		if u.Scheme != "ws" && u.Scheme != "wss" {
			return fmt.Errorf("%w: address %q scheme %q, want ws or wss", ErrInvalidInvite, a, u.Scheme)
		}
		if u.Host == "" {
			return fmt.Errorf("%w: address %q has no host", ErrInvalidInvite, a)
		}
		if u.User != nil {
			return fmt.Errorf("%w: address %q carries userinfo", ErrInvalidInvite, a)
		}
	}
	if i.Relay != "" {
		if _, err := relaypairing.DecodePayload(i.Relay); err != nil {
			return fmt.Errorf("%w: relay offer: %w", ErrInvalidInvite, err)
		}
	}
	if len(i.Nonce) != nonceLen {
		return fmt.Errorf("%w: nonce is %d bytes, want %d", ErrInvalidInvite, len(i.Nonce), nonceLen)
	}
	if i.Expires.IsZero() {
		return fmt.Errorf("%w: no expiry", ErrInvalidInvite)
	}
	return nil
}

// Encode returns the invite as a deep link with the payload in the
// fragment: "smind-host://v1#" + unpadded base64url JSON.
func (i Invite) Encode() (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(i)
	if err != nil {
		return "", fmt.Errorf("%w: encode payload: %w", ErrInvalidInvite, err)
	}
	return Scheme + "://v1#" + base64.RawURLEncoding.EncodeToString(data), nil
}

// Decode parses an invite deep link. The payload must come from the
// fragment only — a query string is refused outright — and unknown JSON
// fields are rejected, so a legacy or smuggled field (say a password) can
// never ride along silently.
func Decode(raw string) (Invite, error) {
	if len(raw) > maxInviteBytes {
		return Invite{}, fmt.Errorf("%w: %d bytes, max %d", ErrInvalidInvite, len(raw), maxInviteBytes)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Invite{}, fmt.Errorf("%w: parse URL: %w", ErrInvalidInvite, err)
	}
	if u.Scheme != Scheme {
		return Invite{}, fmt.Errorf("%w: scheme %q, want %q", ErrInvalidInvite, u.Scheme, Scheme)
	}
	if u.Host != "v1" {
		return Invite{}, fmt.Errorf("%w: host %q, want v1", ErrInvalidInvite, u.Host)
	}
	if u.RawQuery != "" {
		return Invite{}, fmt.Errorf("%w: invite payload in query string, not fragment", ErrInvalidInvite)
	}
	if u.Fragment == "" {
		return Invite{}, fmt.Errorf("%w: no payload fragment", ErrInvalidInvite)
	}
	data, err := base64.RawURLEncoding.DecodeString(u.Fragment)
	if err != nil {
		return Invite{}, fmt.Errorf("%w: decode payload: %w", ErrInvalidInvite, err)
	}
	var inv Invite
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&inv); err != nil {
		return Invite{}, fmt.Errorf("%w: parse payload: %w", ErrInvalidInvite, err)
	}
	if err := inv.Validate(); err != nil {
		return Invite{}, err
	}
	return inv, nil
}
