// Package link holds the fleet link layer's local policy (ADR-0048 F1b):
// which direct WebSocket routes a host may dial without an encrypted
// overlay, and the per-host secrets file the dialer reads its credentials
// from. It is deliberately separate from the daemon-side link code: this
// package is what the CLI and the outbound dialer agree on before any
// connection is opened.
package link

import (
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/spacingmind/smind/internal/config"
)

// ErrCleartext is returned for a ws:// route that is neither loopback nor
// inside the overlay's address ranges: every prompt and result sent over it
// would be readable on the network path. Callers that still want the route
// must pass allowCleartext and surface the warning to their human.
var ErrCleartext = errors.New("link: cleartext ws:// route outside an encrypted overlay")

// The overlay's private address space (Tailscale-style): 100.64.0.0/10 for
// IPv4 (CGNAT) and fd7a:115c:a1e0::/48 for IPv6. Traffic inside these
// ranges is already encrypted by the overlay, so ws:// there does not leak.
var (
	cgnatRange     = mustCIDR("100.64.0.0/10")
	overlayV6Range = mustCIDR("fd7a:115c:a1e0::/48")
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic("link: bad overlay CIDR literal " + s)
	}
	return n
}

// CheckDirectRoute decides whether rawURL may be dialed as a direct fleet
// route. wss is always allowed; ws is allowed without comment only to a
// loopback host or an IP inside the overlay's ranges. Any other ws:// is
// ErrCleartext unless allowCleartext, in which case it is allowed with a
// warning the caller must show. A scheme other than ws/wss, a URL without
// a host, embedded userinfo (credentials do not travel in the URL) and a
// malformed URL are errors regardless of allowCleartext.
func CheckDirectRoute(rawURL string, allowCleartext bool) (warning string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("link: parse route %q: %w", rawURL, err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", fmt.Errorf("link: route %q: scheme must be ws or wss, got %q", rawURL, u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("link: route %q has no host", rawURL)
	}
	if u.User != nil {
		return "", fmt.Errorf("link: route %q carries userinfo; credentials do not belong in the URL", rawURL)
	}
	if u.Scheme == "wss" {
		return "", nil
	}
	host := u.Hostname()
	if config.IsLoopbackHost(host) {
		return "", nil
	}
	if ip := net.ParseIP(host); ip != nil && (cgnatRange.Contains(ip) || overlayV6Range.Contains(ip)) {
		return "", nil
	}
	if !allowCleartext {
		return "", ErrCleartext
	}
	return fmt.Sprintf("prompts and results on this route are readable by anyone on the network: %s", u.Host), nil
}
