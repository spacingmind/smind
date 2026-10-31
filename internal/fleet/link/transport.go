package link

import (
	"encoding/json"
	"fmt"
)

// Transport kinds.
const (
	// KindDirect dials the peer's /ws endpoint over ws or wss.
	KindDirect = "direct"
	// KindRelay reaches the peer through a relay.
	KindRelay = "relay"
)

// RelayRoute locates a peer behind a relay. The relay secret is not stored
// here: it lives in Secrets.
type RelayRoute struct {
	Endpoint    string `json:"endpoint"`
	RelayHostID string `json:"relayHostId"`
	Fingerprint string `json:"fingerprint"`
	DaemonID    string `json:"daemonId"`
	DaemonPub   string `json:"daemonPub"`
}

// Transport is how the outbound link reaches a peer; the registry stores it
// as opaque JSON (Transport.JSON).
type Transport struct {
	Kind string `json:"kind"`
	URL  string `json:"url,omitempty"`
	// Fingerprint is the hex sha256 of the peer's leaf certificate, the pin
	// for wss.
	Fingerprint    string      `json:"fingerprint,omitempty"`
	AllowCleartext bool        `json:"allowCleartext,omitempty"`
	Relay          *RelayRoute `json:"relay,omitempty"`
}

// ParseTransport decodes a stored transport; empty, malformed or unknown
// kinds are errors.
func ParseTransport(raw string) (Transport, error) {
	if raw == "" {
		return Transport{}, fmt.Errorf("link: empty transport")
	}
	var t Transport
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return Transport{}, fmt.Errorf("link: parse transport: %w", err)
	}
	if t.Kind != KindDirect && t.Kind != KindRelay {
		return Transport{}, fmt.Errorf("link: unknown transport kind %q", t.Kind)
	}
	return t, nil
}

// JSON returns the stored form of the transport.
func (t Transport) JSON() string {
	b, err := json.Marshal(t)
	if err != nil {
		return ""
	}
	return string(b)
}
