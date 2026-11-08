package runs

import (
	"container/list"
	"sync"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// DefaultRebuildCacheBudget is the default cap on the estimated bytes of rebuilt
// histories kept (config runs.rebuildCacheMB). It is separate from the finished
// runs' history budget: a rebuilt history is never written to the store and is
// never part of a run, it is a throwaway copy of the provider's session.
const DefaultRebuildCacheBudget int64 = 16 << 20

// rebuildCache is a byte-budgeted LRU of rebuilt histories (ADR-0053 AC13),
// keyed by run id and valid only for the session file identity (size, mtime) it
// was built from: a changed file misses. It holds client-form events only
// (heredoc bodies elided, provider secrets scrubbed): never the raw bodies the
// reveal path needs. All methods are safe for concurrent use.
type rebuildCache struct {
	mu     sync.Mutex
	lru    *list.List // *rebuildEntry, most recently used at the front
	items  map[string]*list.Element
	bytes  int64
	budget int64 // <= 0 disables the cache

	hits, misses, evictions int64
}

type rebuildEntry struct {
	runID    string
	size     int64
	mod      int64
	events   []Event
	refs     map[string][]taskrunner.HeredocRef
	detail   string
	estimate int64
}

// RebuildCacheStats is the cache's counters, for tests and diagnostics.
type RebuildCacheStats struct {
	Entries   int   `json:"entries"`
	Bytes     int64 `json:"bytes"`
	Budget    int64 `json:"budgetBytes"`
	Hits      int64 `json:"hits"`
	Misses    int64 `json:"misses"`
	Evictions int64 `json:"evictions"`
}

func (c *rebuildCache) init() {
	if c.lru == nil {
		c.lru = list.New()
		c.items = map[string]*list.Element{}
	}
}

func (c *rebuildCache) setBudget(b int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.budget = b
	c.init()
	c.evictLocked()
}

// get returns a copy of runID's rebuilt history when it was built from a file
// of exactly this size and mtime.
func (c *rebuildCache) get(runID string, size, mod int64) (Rebuilt, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()
	el, ok := c.items[runID]
	if !ok {
		c.misses++
		return Rebuilt{}, false
	}
	e := el.Value.(*rebuildEntry)
	if e.size != size || e.mod != mod {
		c.removeLocked(el) // the session changed: the entry is dead
		c.misses++
		return Rebuilt{}, false
	}
	c.lru.MoveToFront(el)
	c.hits++
	return Rebuilt{Events: append([]Event(nil), e.events...), Refs: e.refs, Detail: e.detail}, true
}

// put stores a successful rebuild. A history bigger than the whole budget is not
// stored (it is served uncached).
func (c *rebuildCache) put(runID string, size, mod int64, r Rebuilt) {
	est := historyBytes(r.Events)
	for _, refs := range r.Refs {
		est += int64(64 * (len(refs) + 1))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()
	if c.budget <= 0 || est > c.budget {
		return
	}
	if el, ok := c.items[runID]; ok {
		c.removeLocked(el)
	}
	e := &rebuildEntry{runID: runID, size: size, mod: mod, events: append([]Event(nil), r.Events...), refs: r.Refs, detail: r.Detail, estimate: est}
	c.items[runID] = c.lru.PushFront(e)
	c.bytes += est
	c.evictLocked()
}

// invalidate drops runID's entry (the run was pruned, or its excerpts were).
func (c *rebuildCache) invalidate(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()
	if el, ok := c.items[runID]; ok {
		c.removeLocked(el)
	}
}

func (c *rebuildCache) removeLocked(el *list.Element) {
	e := el.Value.(*rebuildEntry)
	c.lru.Remove(el)
	delete(c.items, e.runID)
	c.bytes -= e.estimate
}

func (c *rebuildCache) evictLocked() {
	for c.bytes > c.budget || c.budget <= 0 && c.lru.Len() > 0 {
		back := c.lru.Back()
		if back == nil {
			return
		}
		c.removeLocked(back)
		c.evictions++
	}
}

func (c *rebuildCache) stats() RebuildCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()
	return RebuildCacheStats{Entries: c.lru.Len(), Bytes: c.bytes, Budget: c.budget, Hits: c.hits, Misses: c.misses, Evictions: c.evictions}
}

// SetRebuildCacheBudget sets the rebuilt-history cache budget in bytes; 0 turns
// the cache off (every rebuild reads the session) and drops what it holds.
func (reg *Registry) SetRebuildCacheBudget(bytes int64) { reg.rcache.setBudget(bytes) }

// RebuildCacheStats snapshots the cache's counters.
func (reg *Registry) RebuildCacheStats() RebuildCacheStats { return reg.rcache.stats() }
