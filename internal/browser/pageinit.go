package browser

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Page initiators (docs/plans/completed/browser-for-agents-live-findings.md).
//
// The browser makes requests of its own that no page asked for (the Google sign-in
// cookie query, search pre-connects, update and metrics services). The proxy refuses
// every one of them, but the agent must hear only about what a PAGE tried: a notice
// about www.google.com in the first result of a local test page is noise the model
// repeats. The rule: a refusal by the proxy becomes a notice only when the target
// (host and port) was also named by a request a page made, as CDP reports it: a
// Fetch.requestPaused URL, a Network.requestWillBeSent URL or a
// Network.webSocketCreated URL (a WebSocket only ever reaches the proxy, so the
// Network domain is the only place it is named). A refusal with no such page request
// is counted (Session.Unattributed), never reported. The CDP event can trail the
// proxy's refusal by a moment, so the check is made again after a short grace.
//
// What this does NOT report, by design: a connection hint a page causes without a request
// (a <link rel=preconnect>, a DNS prefetch) and a service worker's own script fetch name
// no request on the page's sessions, so their refusals are counted, not reported; and a
// page that names its target only AFTER the grace loses that one notice. The proxy still
// refuses all of them. A refusal that is not about a request target at all (the proxy's own
// credential-guessing notice) is never filtered.

// pageTargetsMax bounds the remembered targets; a rolling window like seenSet.
const pageTargetsMax = 2000

// pageAttributionGrace is how long a refusal waits for the page request that names
// its target before it is called unattributed.
const pageAttributionGrace = 400 * time.Millisecond

type pageTargets struct {
	mu    sync.Mutex
	m     map[string]struct{}
	order []string // insertion order: the oldest target is evicted first
}

// add remembers a target. A target already known is left alone (a page cannot push its own
// target out by repeating it), and a full set drops its OLDEST entry, one at a time: a flood
// of distinct hosts ages old targets out, it does not wipe the set in one step.
func (p *pageTargets) add(hostport string) {
	if hostport == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m == nil {
		p.m = map[string]struct{}{}
	}
	if _, ok := p.m[hostport]; ok {
		return
	}
	if len(p.order) >= pageTargetsMax {
		delete(p.m, p.order[0])
		p.order = p.order[1:]
	}
	p.m[hostport] = struct{}{}
	p.order = append(p.order, hostport)
}

func (p *pageTargets) has(hostport string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.m[hostport]
	return ok
}

// hostPortOf names the target of a request: an absolute URL, or the host:port of a
// CONNECT. The host is lower-cased and an IPv6 literal keeps its canonical form; the
// scheme's default port fills a missing one. Anything else is not a target.
func hostPortOf(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return "", false
		}
		port := u.Port()
		if port == "" {
			switch strings.ToLower(u.Scheme) {
			case "http", "ws":
				port = "80"
			case "https", "wss":
				port = "443"
			default:
				return "", false
			}
		}
		return net.JoinHostPort(strings.ToLower(u.Hostname()), port), true
	}
	h, p, err := net.SplitHostPort(raw)
	if err != nil || h == "" {
		return "", false
	}
	if _, err := strconv.Atoi(p); err != nil {
		return "", false
	}
	return net.JoinHostPort(strings.ToLower(h), p), true
}
