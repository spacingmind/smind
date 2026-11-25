package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/listeners"
	"github.com/spacingmind/smind/internal/store"
)

// The live part of the browser's deny list (ADR-0061 section 5, the PR 3 gaps
// checklist): addresses a page must never reach that change while a session is
// open. Two sources, both read at every decision (cached for half a second):
//
//   - every smind listener of any process: each registers a small file under
//     <home>/listeners (internal/listeners), live only while it listens (an OAuth
//     callback exists for one login), dead processes' files are ignored;
//   - the direct address of every fleet-linked daemon (a paired peer's URL). A
//     peer addressed by a DNS NAME is denied by the name AND by every address the
//     name resolves to (browser_resolve.go): re-resolved at every refresh, the last
//     good answer served when a lookup is slow or fails, and a name that never
//     resolved makes the whole read fail closed.
//
// The deny list beats the allowlist: a human cannot allow these.

const liveDenyTTL = 500 * time.Millisecond

type fleetLinkReader interface {
	ListFleetHosts() ([]store.FleetHost, error)
	ListFleetLinks(hostID string) ([]store.FleetLink, error)
}

type liveDenySource struct {
	dir   string
	fleet fleetLinkReader
	alive func(pid int) bool
	// resolver turns a linked peer's DNS name into addresses (nil: names are denied by name only).
	resolver *nameResolver

	mu       sync.Mutex
	at       time.Time
	cached   []browser.Endpoint
	cachedEr error
	flight   chan struct{} // non-nil while one refresh runs: callers wait for it (single flight)
}

func newLiveDenySource(dir string, fleet fleetLinkReader) *liveDenySource {
	return &liveDenySource{dir: dir, fleet: fleet, alive: listeners.ProcessAlive, resolver: newNameResolver(nil)}
}

// Endpoints returns the current live reserved endpoints. An error means the
// listener records cannot be read (an unreadable or flooded directory): the
// caller fails CLOSED. The listeners THIS process registered come from memory,
// so they are reserved even when their files were deleted.
func (s *liveDenySource) Endpoints() ([]browser.Endpoint, error) {
	return s.EndpointsContext(context.Background())
}

// EndpointsContext is Endpoints whose waiters (callers that find a refresh in
// flight) leave when ctx ends. The refresh itself always completes and always
// releases its waiters (deferred), even if it panics: a stuck flight would
// deadlock every later decision.
func (s *liveDenySource) EndpointsContext(ctx context.Context) ([]browser.Endpoint, error) {
	for {
		s.mu.Lock()
		if !s.at.IsZero() && time.Since(s.at) < liveDenyTTL {
			eps, err := s.cached, s.cachedEr
			s.mu.Unlock()
			return eps, err
		}
		if s.flight != nil { // another goroutine is refreshing: wait for it (or for ctx), then re-check
			wait := s.flight
			s.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return s.refreshAsLeader()
	}
}

// refreshAsLeader runs the one refresh; it is entered with s.mu held and
// releases it. The flight is closed and cleared in a defer.
func (s *liveDenySource) refreshAsLeader() (eps []browser.Endpoint, err error) {
	flight := make(chan struct{})
	s.flight = flight
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if r := recover(); r != nil {
			// A panic in the refresh fails CLOSED (an error, never "no entries") and
			// still releases the waiters; the next call tries again.
			eps, err = nil, fmt.Errorf("the daemon's listener records cannot be read: internal error")
			s.cached, s.cachedEr, s.at = eps, err, time.Time{}
		} else {
			s.cached, s.cachedEr, s.at = eps, err, time.Now()
		}
		s.flight = nil
		s.mu.Unlock()
		close(flight)
	}()
	// The reads happen OUTSIDE the lock, by exactly one goroutine at a time.
	return s.refresh()
}

func endpointOf(e listeners.Entry) (browser.Endpoint, bool) {
	host, portStr, err := net.SplitHostPort(e.Addr)
	if err != nil {
		return browser.Endpoint{}, false
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return browser.Endpoint{}, false
	}
	if host == "" {
		host = "0.0.0.0" // bound on every address: the port is reserved everywhere
	}
	return browser.Endpoint{Host: host, Port: port}, true
}

func (s *liveDenySource) refresh() ([]browser.Endpoint, error) {
	var out []browser.Endpoint
	for _, e := range listeners.Local() { // this process's own listeners, from memory
		if ep, ok := endpointOf(e); ok {
			out = append(out, ep)
		}
	}
	files, err := listeners.Read(s.dir, s.alive)
	if err != nil {
		// Keep what is known for the log line, but the caller must refuse.
		return out, fmt.Errorf("the daemon's listener records cannot be read: %w", err)
	}
	for _, e := range files {
		if ep, ok := endpointOf(e); ok {
			out = append(out, ep)
		}
	}
	fleet, ferr := s.fleetEndpoints()
	out = append(out, fleet...)
	if ferr != nil {
		// Keep what is known (the names included) for the log line, but the caller must refuse.
		return dedupeEndpoints(out), ferr
	}
	return dedupeEndpoints(out), nil
}

// dedupeEndpoints drops repeats (this process's listeners come from memory AND
// from their own files).
func dedupeEndpoints(in []browser.Endpoint) []browser.Endpoint {
	seen := make(map[browser.Endpoint]bool, len(in))
	out := in[:0:0]
	for _, e := range in {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

// fleetEndpoints are the direct addresses of linked daemons. A store error FAILS
// CLOSED (an error: the browser refuses), like an unresolvable peer name: a deny
// list that silently lacks the linked daemons is a hole, and the listener files
// alone are not the whole floor.
func (s *liveDenySource) fleetEndpoints() ([]browser.Endpoint, error) {
	if s.fleet == nil {
		return nil, nil
	}
	hosts, err := s.fleet.ListFleetHosts()
	if err != nil {
		return nil, fmt.Errorf("the linked daemons cannot be read, so the browser refuses until they can: %w", err)
	}
	var out []browser.Endpoint
	for _, h := range hosts {
		links, err := s.fleet.ListFleetLinks(h.HostID)
		if err != nil {
			return out, fmt.Errorf("the links of a linked daemon cannot be read, so the browser refuses until they can: %w", err)
		}
		for _, l := range links {
			if e, ok := transportEndpoint(l.TransportJSON); ok {
				out = append(out, e)
			}
		}
	}
	return s.withResolvedAddresses(out)
}

// withResolvedAddresses adds, for every endpoint named by a DNS name, one entry per
// address the name resolves to (same port), keeping the name entry too. IP-literal
// hosts are not resolved. A name that has never resolved is an error (fail closed).
func (s *liveDenySource) withResolvedAddresses(eps []browser.Endpoint) ([]browser.Endpoint, error) {
	// Every host is canonicalised the way the policy compares it first: an IP literal
	// in any form (127.1, octal, hex, mapped) becomes its canonical address and is
	// never resolved; a name is lower-cased and, when internationalised, converted
	// to punycode before it is resolved and denied. A host that is neither is an
	// error (the entry is never dropped silently).
	canon := make([]browser.Endpoint, 0, len(eps))
	var names []string
	isName := map[string]bool{}
	for _, e := range eps {
		h, name, err := browser.CanonicalHost(e.Host)
		if err != nil {
			return eps, fmt.Errorf("a linked daemon's address is not a host smind can compare, so the browser refuses until it is fixed: %w", err)
		}
		canon = append(canon, browser.Endpoint{Host: h, Port: e.Port})
		if name && !isName[h] {
			isName[h] = true
			names = append(names, h)
		}
	}
	if s.resolver == nil || len(names) == 0 {
		return canon, nil
	}
	addrs, err := s.resolver.Addrs(names)
	if err != nil {
		return canon, fmt.Errorf("a linked daemon's address cannot be resolved, so the browser refuses until it can: %w", err)
	}
	out := append([]browser.Endpoint(nil), canon...)
	for _, e := range canon {
		for _, ip := range addrs[e.Host] {
			out = append(out, browser.Endpoint{Host: ip, Port: e.Port})
		}
	}
	return out, nil
}

// transportEndpoint extracts host and port from a stored link transport's url
// (the only field this needs; the link package owns the full type, and wsapi
// cannot import it).
func transportEndpoint(raw string) (browser.Endpoint, bool) {
	if raw == "" {
		return browser.Endpoint{}, false
	}
	var t struct {
		URL string `json:"url"`
	}
	if json.Unmarshal([]byte(raw), &t) != nil || t.URL == "" {
		return browser.Endpoint{}, false
	}
	u, err := url.Parse(t.URL)
	if err != nil || u.Hostname() == "" {
		return browser.Endpoint{}, false
	}
	port := 0
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			return browser.Endpoint{}, false
		}
	} else {
		switch u.Scheme {
		case "ws", "http":
			port = 80
		case "wss", "https":
			port = 443
		}
	}
	if port < 1 || port > 65535 {
		return browser.Endpoint{}, false
	}
	return browser.Endpoint{Host: u.Hostname(), Port: port}, true
}
