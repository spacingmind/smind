package auth

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

// HostPolicy decides which Host header values the daemon answers (ADR-0031
// D8, ADR-0013 DNS-rebinding defence): "localhost", "*.localhost", any literal
// IP address and the configured extra names. A page on another origin that
// rebinds its DNS name to the daemon's address still sends that foreign name
// in Host, so the request is refused before it reaches any handler. Entries
// with a leading "." match the domain and all its subdomains; others match
// exactly (Vite's server.allowedHosts semantics).
type HostPolicy struct {
	exact   map[string]bool
	domains []string // lower-case, without the leading dot
}

// NewHostPolicy builds a policy from config.server.hostnames.
func NewHostPolicy(hostnames []string) *HostPolicy {
	p := &HostPolicy{exact: map[string]bool{}}
	for _, h := range hostnames {
		h = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
		if h == "" {
			continue
		}
		if d, ok := strings.CutPrefix(h, "."); ok {
			p.domains = append(p.domains, d)
			continue
		}
		p.exact[h] = true
	}
	return p
}

// hostOnly extracts the lower-case host from a Host header value (port and
// IPv6 brackets removed). ok is false for an empty or malformed value.
func hostOnly(hostport string) (string, bool) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "", false
	}
	host := hostport
	if h, port, err := net.SplitHostPort(hostport); err == nil {
		if n, perr := strconv.Atoi(port); perr != nil || n < 1 || n > 65535 {
			return "", false // "localhost:abc", "localhost:", port 0
		}
		host = h
	} else if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		host = hostport[1 : len(hostport)-1]
	} else if strings.Contains(hostport, ":") && net.ParseIP(hostport) == nil {
		return "", false // host:junk
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" || strings.ContainsAny(host, " /\\@?#%") {
		return "", false
	}
	return host, true
}

// Allowed reports whether a Host header value is acceptable.
func (p *HostPolicy) Allowed(hostport string) bool {
	host, ok := hostOnly(hostport)
	if !ok {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if p.exact[host] {
		return true
	}
	for _, d := range p.domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// IsLoopbackHostHeader reports whether the Host header names the machine
// itself: localhost, *.localhost or a loopback IP. An allowed external name
// or a LAN IP does not count.
func IsLoopbackHostHeader(hostport string) bool {
	host, ok := hostOnly(hostport)
	if !ok {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// IsTrueLoopback is the predicate behind GET /api/token and the daemon-token
// WebSocket query: the TCP peer is a loopback address, the Host header names
// the machine itself, and no proxy left its marks on the request. A
// same-host reverse proxy fails the Host test (the browser's external Host
// passes through) and usually the header test; one that rewrites Host to
// localhost and strips its own headers defeats it, which docs/homelab.md
// states.
func IsTrueLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	if !IsLoopbackHostHeader(r.Host) {
		return false
	}
	return !HasProxyHeaders(r)
}

// HasProxyHeaders reports whether the request carries any forwarding header.
func HasProxyHeaders(r *http.Request) bool {
	for name := range r.Header {
		n := strings.ToLower(name)
		if n == "forwarded" || n == "x-real-ip" || n == "via" || strings.HasPrefix(n, "x-forwarded-") {
			return true
		}
	}
	return false
}

// PeerIP is the TCP peer's address without the port ("" if unparsable).
func PeerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}
