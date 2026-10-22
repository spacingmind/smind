package ghctx

import (
	"context"
	"fmt"
	"sync"

	"github.com/spacingmind/smind/internal/forge"
)

// call is one in-flight (or completed) Search/Get computation keyed by its
// full argument tuple. done closes when the underlying runner invocation
// has finished; all waiters then read res/err. Errors are never memoized
// (the cache stores successes only), so a transient gh failure never
// sticks for the TTL.
type call struct {
	done chan struct{}
	res  any
	err  error
}

// inflight serializes identical calls: the first caller computes, the
// rest wait on its done channel.
type inflight struct {
	mu    sync.Mutex
	calls map[string]*call
}

func (f *inflight) do(key string, compute func() (any, error)) (any, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]*call{}
	}
	if c, ok := f.calls[key]; ok {
		f.mu.Unlock()
		<-c.done
		return c.res, c.err
	}
	c := &call{done: make(chan struct{})}
	f.calls[key] = c
	f.mu.Unlock()

	c.res, c.err = compute()
	close(c.done)

	f.mu.Lock()
	// Only delete if still ours: a later entry could in principle have
	// replaced us if the map were cleared; it isn't, so this is a guard
	// in name only but keeps the invariant explicit.
	if f.calls[key] == c {
		delete(f.calls, key)
	}
	f.mu.Unlock()
	return c.res, c.err
}

// searchCacheKey is Search's cache key: dir + kind + query + limit.
func searchCacheKey(dir, kind, query string, limit int) string {
	return dir + "\x00" + kind + "\x00" + query + "\x00" + fmt.Sprint(limit)
}

// getCacheKey is Get's cache key: dir + kind + number + repo.
func getCacheKey(dir, kind string, number int, repo string) string {
	return dir + "\x00" + kind + "\x00" + fmt.Sprint(number) + "\x00" + repo
}

// cachedSearch is Search wrapped in cache + single-flight.
func (c *Client) cachedSearch(ctx context.Context, dir, kind, query string, limit int) (forge.SearchResult, error) {
	key := searchCacheKey(dir, kind, query, limit)
	if v, ok := c.results.get(key); ok {
		return v.(forge.SearchResult), nil
	}
	v, err := c.flights.do("s\x00"+key, func() (any, error) {
		if v2, ok := c.results.get(key); ok {
			return v2, nil
		}
		res, err := c.search(ctx, dir, kind, query, limit)
		if err == nil {
			c.results.set(key, res)
		}
		return res, err
	})
	if err != nil {
		return forge.SearchResult{}, err
	}
	return v.(forge.SearchResult), nil
}

// cachedGet is Get wrapped in cache + single-flight.
func (c *Client) cachedGet(ctx context.Context, dir, kind string, number int, repo string) (*forge.Snapshot, error) {
	key := getCacheKey(dir, kind, number, repo)
	if v, ok := c.results.get(key); ok {
		return v.(*forge.Snapshot), nil
	}
	v, err := c.flights.do("g\x00"+key, func() (any, error) {
		if v2, ok := c.results.get(key); ok {
			return v2, nil
		}
		res, err := c.get(ctx, dir, kind, number, repo)
		if err == nil {
			c.results.set(key, res)
		}
		return res, err
	})
	if err != nil {
		return nil, err
	}
	return v.(*forge.Snapshot), nil
}

// Search is the cached, single-flight Search: identical concurrent or
// repeat-within-TTL calls share one gh invocation.
func (c *Client) Search(ctx context.Context, dir, kind, query string, limit int) (forge.SearchResult, error) {
	return c.cachedSearch(ctx, dir, kind, query, limit)
}

// Get is the cached, single-flight Get.
func (c *Client) Get(ctx context.Context, dir, kind string, number int, repo string) (*forge.Snapshot, error) {
	return c.cachedGet(ctx, dir, kind, number, repo)
}
