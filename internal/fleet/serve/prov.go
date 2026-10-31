package serve

import (
	"slices"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/fleet/wire"
)

const (
	replayCap    = 8192
	defaultRate  = 20.0
	defaultBurst = 40
)

// replayCache remembers recent (principal, reqId) pairs.
type replayCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newReplayCache() *replayCache { return &replayCache{seen: map[string]time.Time{}} }

// check reports whether the pair was seen inside the replay window; if not it
// records the pair, unless the cache is full (full=true, nothing recorded).
func (c *replayCache) check(principal, reqID string, now time.Time) (replay, full bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, t := range c.seen {
		if now.Sub(t) > wire.MaxReplayWindow {
			delete(c.seen, k)
		}
	}
	key := principal + "\x00" + reqID
	if _, ok := c.seen[key]; ok {
		return true, false
	}
	if len(c.seen) >= replayCap {
		return false, true
	}
	c.seen[key] = now
	return false, false
}

type bucket struct {
	tokens float64
	last   time.Time
}

// limiter is a token bucket per principal.
type limiter struct {
	mu    sync.Mutex
	rate  float64
	burst float64
	m     map[string]*bucket
}

func newLimiter(rate float64, burst int) *limiter {
	if rate <= 0 {
		rate = defaultRate
	}
	if burst <= 0 {
		burst = defaultBurst
	}
	return &limiter{rate: rate, burst: float64(burst), m: map[string]*bucket{}}
}

func (l *limiter) allow(principal string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.m[principal]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.m[principal] = b
	}
	if el := now.Sub(b.last).Seconds(); el > 0 {
		b.tokens = min(l.burst, b.tokens+el*l.rate)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// checkProv runs the provenance checks in a fixed order and returns the
// first failure. Only a request that passed every check but replay may take a
// replay-cache slot.
func (s *Server) checkProv(principal string, p wire.Prov, now time.Time) *Refusal {
	if p.Origin != principal || (len(p.Hops) > 0 && p.Hops[0] != principal) {
		return refuse(wire.CodePrincipalMismatch, "provenance does not match the link")
	}
	if s.opts.HostID == p.Origin || slices.Contains(p.Hops, s.opts.HostID) {
		return refuse(wire.CodeCycle, "request already passed this host")
	}
	if len(p.Hops) != 1 {
		return refuse(wire.CodeHopLimit, "exactly one hop is allowed")
	}
	if p.ReqID == "" || p.Trace == "" {
		return refuse(wire.CodeInvalid, "reqId and trace are required")
	}
	if d := now.Sub(time.Unix(p.TS, 0)); d > wire.MaxClockSkew || d < -wire.MaxClockSkew {
		return refuse(wire.CodeStale, "request timestamp is outside the allowed skew")
	}
	replay, full := s.replay.check(principal, p.ReqID, now)
	if replay {
		return refuse(wire.CodeReplay, "request id already seen")
	}
	if full {
		return refuse(wire.CodeRateLimited, "too many recent requests")
	}
	return nil
}
