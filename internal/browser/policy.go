// Package browser is the headless half of the agent browser (ADR-0061): a
// URL policy, a Chrome DevTools Protocol client, a launcher for a system
// Chromium and a session that enforces the policy on every request the page
// makes. Nothing here is reachable by an agent yet: the broker, the MCP tools
// and the human-only enable flag are separate steps.
package browser

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Endpoint is a host and port that is never reachable whatever the allowlist
// (the daemon's own listen address, the desktop proxy origin, other daemons).
// Host is an IP literal or a name; a loopback or unspecified (wildcard bind)
// host denies the port on every loopback form, an unspecified host on every
// address.
type Endpoint struct {
	Host string
	Port int
}

// Policy is the navigation policy of one workspace. The zero value allows
// loopback only and denies nothing; the daemon fills Deny.
type Policy struct {
	// AllowHosts are exact, already normalised hosts the human added
	// (see NormalizeAllowHost). Wildcards do not exist.
	AllowHosts map[string]bool
	Deny       []Endpoint
}

// Decision is the verdict on a URL. NeedsResolve means the URL names a host
// whose resolved addresses must still pass CheckResolved.
type Decision struct {
	Allowed      bool
	Reason       string
	Host         string
	Port         int
	NeedsResolve bool
	loopbackName bool
	allowlisted  bool
}

// Resolver is the part of *net.Resolver the policy needs.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// NewPolicy normalises the allowlist and validates the deny list.
func NewPolicy(allow []string, deny []Endpoint) (Policy, error) {
	p := Policy{AllowHosts: map[string]bool{}}
	for _, a := range allow {
		h, err := NormalizeAllowHost(a)
		if err != nil {
			return Policy{}, err
		}
		p.AllowHosts[h] = true
	}
	for _, d := range deny {
		if d.Port < 1 || d.Port > 65535 {
			return Policy{}, fmt.Errorf("browser: deny endpoint %q has no valid port", d.Host)
		}
		raw := d.Host
		if strings.Contains(raw, ":") && !strings.HasPrefix(raw, "[") {
			raw = "[" + raw + "]" // a bare IPv6 literal
		}
		h, _, err := parseHost(raw) // 127.1, octal, hex and mapped forms canonicalise; garbage is an error
		if err != nil {
			return Policy{}, fmt.Errorf("browser: deny endpoint %q: %w", d.Host, err)
		}
		p.Deny = append(p.Deny, Endpoint{Host: h.canon, Port: d.Port})
	}
	return p, nil
}

// NormalizeAllowHost validates one allowlist entry: a bare host (a name or an
// IP literal), no scheme, no port, no path, no wildcard. It returns the
// canonical form that CheckURL compares against.
func NormalizeAllowHost(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("browser: empty allowlist host")
	}
	if strings.ContainsAny(s, "*/@?# \\") || strings.Contains(s, "://") {
		return "", fmt.Errorf("browser: allowlist entry %q must be one exact host, no scheme, wildcard, path or userinfo", s)
	}
	host := s
	if strings.HasPrefix(s, "[") {
		if !strings.HasSuffix(s, "]") {
			return "", fmt.Errorf("browser: allowlist entry %q is not a host", s)
		}
	} else if strings.Contains(s, ":") {
		// A bare IPv6 literal is accepted; host:port is not.
		if a, err := netip.ParseAddr(s); err != nil || a.Zone() != "" {
			return "", fmt.Errorf("browser: allowlist entry %q must not carry a port", s)
		}
		host = "[" + s + "]"
	}
	h, _, err := parseHost(host)
	if err != nil {
		return "", fmt.Errorf("browser: allowlist entry %q: %w", s, err)
	}
	if h.ip.IsValid() && sensitiveAddr(h.ip) {
		return "", fmt.Errorf("browser: allowlist entry %q is a link-local, metadata, multicast or unspecified address and cannot be allowed", s)
	}
	return h.canon, nil
}

type parsedHost struct {
	canon string
	ip    netip.Addr // valid when the host is an IP literal (after IPv4-mapped unmapping)
	name  bool
}

// parseHost normalises a host the way a browser parses it. raw is the
// authority host part (brackets kept for IPv6).
func parseHost(raw string) (parsedHost, bool, error) {
	if raw == "" {
		return parsedHost{}, false, errors.New("empty host")
	}
	if strings.HasPrefix(raw, "[") {
		if !strings.HasSuffix(raw, "]") {
			return parsedHost{}, false, errors.New("malformed IPv6 host")
		}
		inner := raw[1 : len(raw)-1]
		if strings.Contains(inner, "%") {
			return parsedHost{}, false, errors.New("IPv6 zone identifiers are refused")
		}
		a, err := netip.ParseAddr(inner)
		if err != nil || !a.Is6() {
			return parsedHost{}, false, errors.New("malformed IPv6 host")
		}
		a = a.Unmap()
		return parsedHost{canon: a.String(), ip: a}, true, nil
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_'
		if !ok {
			return parsedHost{}, false, fmt.Errorf("host has a character that is refused (%q)", c)
		}
	}
	h := strings.ToLower(raw)
	h = strings.TrimSuffix(h, ".")
	if h == "" || strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") || strings.Contains(h, "..") {
		return parsedHost{}, false, errors.New("empty host label")
	}
	labels := strings.Split(h, ".")
	if numericLabel(labels[len(labels)-1]) {
		a, err := parseIPv4(labels)
		if err != nil {
			return parsedHost{}, false, err
		}
		return parsedHost{canon: a.String(), ip: a}, true, nil
	}
	return parsedHost{canon: h, name: true}, true, nil
}

// numericLabel reports whether a label is a number in a form a browser parses
// as an IPv4 part (decimal, 0x hex, leading-zero octal), which makes the whole
// host an IPv4 address or an error, never a name.
func numericLabel(l string) bool {
	if l == "" {
		return false
	}
	if strings.HasPrefix(l, "0x") {
		for _, c := range l[2:] {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
		return true
	}
	for _, c := range l {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseIPv4(labels []string) (netip.Addr, error) {
	if len(labels) > 4 {
		return netip.Addr{}, errors.New("malformed IPv4 host")
	}
	nums := make([]uint64, len(labels))
	for i, l := range labels {
		n, err := parseIPv4Part(l)
		if err != nil {
			return netip.Addr{}, err
		}
		nums[i] = n
	}
	for _, n := range nums[:len(nums)-1] {
		if n > 255 {
			return netip.Addr{}, errors.New("malformed IPv4 host")
		}
	}
	last := nums[len(nums)-1]
	limit := uint64(1) << (8 * uint(5-len(nums)))
	if last >= limit {
		return netip.Addr{}, errors.New("malformed IPv4 host")
	}
	v := last
	for i, n := range nums[:len(nums)-1] {
		v += n << (8 * uint(3-i))
	}
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), nil
}

func parseIPv4Part(l string) (uint64, error) {
	base := 10
	s := l
	switch {
	case strings.HasPrefix(l, "0x"):
		base, s = 16, l[2:]
		if s == "" {
			return 0, nil
		}
	case len(l) > 1 && l[0] == '0':
		base, s = 8, l[1:]
	}
	n, err := strconv.ParseUint(s, base, 64)
	if err != nil {
		return 0, errors.New("malformed IPv4 host")
	}
	return n, nil
}

func refuse(format string, a ...any) Decision {
	return Decision{Reason: fmt.Sprintf(format, a...)}
}

// CheckURL is the pure half of the policy: scheme, syntax, host class, the
// allowlist and the deny list. It does no DNS; a named host comes back with
// NeedsResolve and must pass CheckResolved before the request is sent.
func (p Policy) CheckURL(raw string) Decision {
	if raw == "about:blank" {
		return Decision{Allowed: true, Host: "about:blank"}
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c <= 0x20 || c == 0x7f || c == '\\' {
			return refuse("the URL has a space, control character or backslash")
		}
	}
	lower := strings.ToLower(raw)
	var rest string
	switch {
	case strings.HasPrefix(lower, "http://"):
		rest = raw[len("http://"):]
	case strings.HasPrefix(lower, "https://"):
		rest = raw[len("https://"):]
	default:
		return refuse("only http and https URLs are allowed")
	}
	defPort := 80
	if strings.HasPrefix(lower, "https://") {
		defPort = 443
	}
	end := len(rest)
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		end = i
	}
	auth := rest[:end]
	if strings.Contains(auth, "@") {
		return refuse("a URL with user information is refused")
	}
	hostPart, portPart := auth, ""
	if strings.HasPrefix(auth, "[") {
		i := strings.Index(auth, "]")
		if i < 0 {
			return refuse("malformed IPv6 host")
		}
		hostPart = auth[:i+1]
		switch tail := auth[i+1:]; {
		case tail == "":
		case strings.HasPrefix(tail, ":"):
			portPart = tail[1:]
		default:
			return refuse("malformed authority")
		}
	} else if i := strings.LastIndex(auth, ":"); i >= 0 {
		hostPart, portPart = auth[:i], auth[i+1:]
	}
	port := defPort
	if portPart != "" {
		n, err := strconv.Atoi(portPart)
		if err != nil || n < 1 || n > 65535 || strings.ContainsAny(portPart, "+-") {
			return refuse("malformed port")
		}
		port = n
	}
	h, _, err := parseHost(hostPart)
	if err != nil {
		return refuse("host refused: %v", err)
	}
	d := Decision{Host: h.canon, Port: port}
	if p.denied(h, port) {
		return Decision{Host: h.canon, Port: port, Reason: "that address is reserved (the daemon, the app or another smind daemon)"}
	}
	allowlisted := p.AllowHosts[h.canon]
	if h.name {
		d.NeedsResolve = true
		if h.canon == "localhost" || strings.HasSuffix(h.canon, ".localhost") {
			d.loopbackName = true
			d.Allowed = true
			return d
		}
		if !allowlisted {
			d.NeedsResolve = false
			d.Reason = fmt.Sprintf("host %q is not allowed (only loopback and hosts the person added)", h.canon)
			return d
		}
		d.allowlisted = true
		d.Allowed = true
		return d
	}
	if h.ip.IsLoopback() {
		d.Allowed = true
		return d
	}
	if !allowlisted {
		d.Reason = fmt.Sprintf("address %s is not allowed (only loopback and hosts the person added)", h.canon)
		return d
	}
	d.allowlisted = true
	d.Allowed = true
	return d
}

// denied reports whether the target is one of the reserved endpoints.
func (p Policy) denied(h parsedHost, port int) bool {
	for _, e := range p.Deny {
		if e.Port != port {
			continue
		}
		ea, err := netip.ParseAddr(e.Host)
		if err == nil && ea.IsUnspecified() {
			return true // bound on every address: the port is reserved everywhere
		}
		loopbackEntry := e.Host == "localhost" || strings.HasSuffix(e.Host, ".localhost") || (err == nil && ea.IsLoopback())
		if h.name {
			if h.canon == e.Host || (loopbackEntry && (h.canon == "localhost" || strings.HasSuffix(h.canon, ".localhost"))) {
				return true
			}
			continue
		}
		// 0.0.0.0 and :: connect to the local machine: same as loopback.
		if loopbackEntry && (h.ip.IsLoopback() || h.ip.IsUnspecified()) {
			return true
		}
		if err == nil && ea == h.ip {
			return true
		}
	}
	return false
}

var metadataAddrs = []netip.Addr{
	netip.MustParseAddr("169.254.169.254"),
	netip.MustParseAddr("169.254.170.2"),
	netip.MustParseAddr("100.100.100.200"),
	netip.MustParseAddr("168.63.129.16"),
	netip.MustParseAddr("fd00:ec2::254"),
	netip.MustParseAddr("fd00:ec2::23"),
}

var (
	ietfProtocolV4 = netip.MustParsePrefix("192.0.0.0/24")
	nat64Prefix    = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour      = netip.MustParsePrefix("2002::/16")
	v4Compatible   = netip.MustParsePrefix("::/96")
	siit           = netip.MustParsePrefix("::ffff:0:0:0/96") // IPv4-translated (RFC 2765)
	nat64Local     = netip.MustParsePrefix("64:ff9b:1::/48")  // local-use NAT64 (RFC 8215): refused outright
	teredo         = netip.MustParsePrefix("2001::/32")       // embeds an obfuscated IPv4: refused outright
)

// embeddedV4 returns the IPv4 address an IPv6 address carries (NAT64, 6to4,
// IPv4-compatible), which a gateway would translate to.
func embeddedV4(a netip.Addr) (netip.Addr, bool) {
	if !a.Is6() {
		return netip.Addr{}, false
	}
	b := a.As16()
	switch {
	case nat64Prefix.Contains(a):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case sixToFour.Contains(a):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	case v4Compatible.Contains(a) && !a.IsUnspecified() && !a.IsLoopback():
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case siit.Contains(a):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	}
	return netip.Addr{}, false
}

// sensitiveAddr reports addresses that are never reachable, even by an exact
// allowlist entry: unspecified, multicast, link-local, cloud metadata, the
// IETF protocol block 192.0.0.0/24, and an IPv6 address that embeds one of
// those (or loopback) in a form a translator would deliver.
func sensitiveAddr(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsUnspecified() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsMulticast() || a.IsInterfaceLocalMulticast() {
		return true
	}
	if a.Is4() && ietfProtocolV4.Contains(a) {
		return true
	}
	for _, m := range metadataAddrs {
		if a == m {
			return true
		}
	}
	if nat64Local.Contains(a) || teredo.Contains(a) {
		return true
	}
	if e, ok := embeddedV4(a); ok {
		return e.IsLoopback() || sensitiveAddr(e)
	}
	return false
}

// CheckResolved vets the addresses a named host resolved to. d must be an
// allowed decision with NeedsResolve set.
func (p Policy) CheckResolved(d Decision, addrs []netip.Addr) Decision {
	if !d.Allowed {
		return d
	}
	if !d.NeedsResolve {
		return d
	}
	if len(addrs) == 0 {
		return refuse("host %q did not resolve", d.Host)
	}
	for _, raw := range addrs {
		a := raw.Unmap()
		if p.denied(parsedHost{canon: a.String(), ip: a}, d.Port) {
			return refuse("host %q resolves to a reserved address", d.Host)
		}
		if d.loopbackName {
			if !a.IsLoopback() {
				return refuse("host %q resolves outside loopback", d.Host)
			}
			continue
		}
		if sensitiveAddr(a) {
			return refuse("host %q resolves to a link-local, metadata, multicast or unspecified address", d.Host)
		}
		// An allowlisted NAME may only reach the public internet: its DNS is
		// not ours, and a name that resolves to loopback, a private or CGNAT
		// range is how an attacker-influenced record reaches the daemon's
		// machine or the LAN. The person who really wants an intranet host adds
		// its IP literal (an exact entry), never a name.
		if internalAddr(a) {
			return refuse("host %q resolves to a loopback, private or internal address; allow its IP address instead", d.Host)
		}
	}
	return d
}

var internalPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),   // reserved
}

// internalAddr reports loopback, private (RFC 1918, unique-local), CGNAT and
// other non-public ranges, including an IPv6 form that embeds one.
func internalAddr(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() {
		return true
	}
	for _, pre := range internalPrefixes {
		if pre.Contains(a) {
			return true
		}
	}
	if e, ok := embeddedV4(a); ok {
		return internalAddr(e)
	}
	return false
}

// Check runs CheckURL and, for a named host, CheckResolved with the resolver.
// A nil resolver refuses a named host rather than skipping the check.
func (p Policy) Check(ctx context.Context, raw string, r Resolver) Decision {
	d := p.CheckURL(raw)
	if !d.Allowed || !d.NeedsResolve {
		return d
	}
	if r == nil {
		return refuse("host %q cannot be checked without a resolver", d.Host)
	}
	addrs, err := r.LookupNetIP(ctx, "ip", d.Host)
	if err != nil {
		return refuse("host %q did not resolve: %v", d.Host, err)
	}
	return p.CheckResolved(d, addrs)
}
