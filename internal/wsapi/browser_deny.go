package wsapi

import (
	"encoding/json"
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

	mu     sync.Mutex
	at     time.Time
	cached []browser.Endpoint
}

func newLiveDenySource(dir string, fleet fleetLinkReader) *liveDenySource {
	return &liveDenySource{dir: dir, fleet: fleet, alive: listeners.ProcessAlive}
}

// Endpoints returns the current live reserved endpoints.
func (s *liveDenySource) Endpoints() []browser.Endpoint {
	s.mu.Lock()
	if !s.at.IsZero() && time.Since(s.at) < liveDenyTTL {
		cached := s.cached
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()
	// The reads happen OUTSIDE the lock: a slow filesystem or store must not stall
	// every decision behind it (the result is cached for half a second).
	var out []browser.Endpoint
	for _, e := range listeners.Read(s.dir, s.alive) {
		host, portStr, err := net.SplitHostPort(e.Addr)
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			continue
		}
		if host == "" {
			host = "0.0.0.0" // bound on every address: the port is reserved everywhere
		}
		out = append(out, browser.Endpoint{Host: host, Port: port})
	}
	out = append(out, s.fleetEndpoints()...)
	s.mu.Lock()
	s.cached, s.at = out, time.Now()
	s.mu.Unlock()
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
