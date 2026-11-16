package wsapi

import (
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
//   - the direct address of every fleet-linked daemon (a paired peer's URL).
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

	mu       sync.Mutex
	at       time.Time
	cached   []browser.Endpoint
	cachedEr error
	flight   chan struct{} // non-nil while one refresh runs: callers wait for it (single flight)
}

func newLiveDenySource(dir string, fleet fleetLinkReader) *liveDenySource {
	return &liveDenySource{dir: dir, fleet: fleet, alive: listeners.ProcessAlive}
}

// Endpoints returns the current live reserved endpoints. An error means the
// listener records cannot be read (an unreadable or flooded directory): the
// caller fails CLOSED. The listeners THIS process registered come from memory,
// so they are reserved even when their files were deleted.
func (s *liveDenySource) Endpoints() ([]browser.Endpoint, error) {
	for {
		s.mu.Lock()
		if !s.at.IsZero() && time.Since(s.at) < liveDenyTTL {
			eps, err := s.cached, s.cachedEr
			s.mu.Unlock()
			return eps, err
		}
		if s.flight != nil { // another goroutine is refreshing: wait for it, then re-check
			wait := s.flight
			s.mu.Unlock()
			<-wait
			continue
		}
		s.flight = make(chan struct{})
		flight := s.flight
		s.mu.Unlock()

		// The reads happen OUTSIDE the lock, by exactly one goroutine at a time.
		eps, err := s.refresh()

		s.mu.Lock()
		s.cached, s.cachedEr, s.at = eps, err, time.Now()
		s.flight = nil
		s.mu.Unlock()
		close(flight)
		return eps, err
	}
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
	out = append(out, s.fleetEndpoints()...)
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

// fleetEndpoints are the direct addresses of linked daemons. A store error
// yields nothing here (the listener files still apply); the deny list is a
// floor, and an unreadable store is already a broken daemon.
func (s *liveDenySource) fleetEndpoints() []browser.Endpoint {
	if s.fleet == nil {
		return nil
	}
	hosts, err := s.fleet.ListFleetHosts()
	if err != nil {
		return nil
	}
	var out []browser.Endpoint
	for _, h := range hosts {
		links, err := s.fleet.ListFleetLinks(h.HostID)
		if err != nil {
			continue
		}
		for _, l := range links {
			if e, ok := transportEndpoint(l.TransportJSON); ok {
				out = append(out, e)
			}
		}
	}
	return out
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
