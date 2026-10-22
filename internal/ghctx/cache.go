package ghctx

import "time"

// ttlCache is a small TTL cache using the client's (injectable) clock --
// shared by the repo-inference cache (60s TTL) and the Search/Get result
// cache (client TTL). Errors are never stored: only successful results
// are cached, so a transient gh failure never sticks.
type ttlCache struct {
	ttl   time.Duration
	now   func() time.Time
	entry map[string]cacheEntry
}

type cacheEntry struct {
	val any
	at  time.Time
}

func newTTLCache(ttl time.Duration, now func() time.Time) *ttlCache {
	return &ttlCache{ttl: ttl, now: now, entry: map[string]cacheEntry{}}
}

func (c *ttlCache) get(key string) (any, bool) {
	e, ok := c.entry[key]
	if !ok || c.now().Sub(e.at) > c.ttl {
		return nil, false
	}
	return e.val, true
}

func (c *ttlCache) set(key string, val any) {
	c.entry[key] = cacheEntry{val: val, at: c.now()}
}
