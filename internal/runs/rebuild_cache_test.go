package runs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// ADR-0053 AC13: the rebuilt-history cache and the rebuild on open.

func setBudget(env *rebuildEnv, mb int64) { env.reg.SetRebuildCacheBudget(mb << 20) }

// A second rebuild of an unchanged session is served from the cache and does not
// read the file again; both reads see the same events.
func TestRebuildCacheHitDoesNotReadTheFile(t *testing.T) {
	env := newRebuildEnv(t, true)
	setBudget(env, 16)
	ctx := context.Background()
	t0 := time.Now()
	first := env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	cold := time.Since(t0)
	t0 = time.Now()
	second := env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	warm := time.Since(t0)
	if !first.OK() || !second.OK() {
		t.Fatalf("rebuild refused: %q / %q", first.Reason, second.Reason)
	}
	if n := env.reg.rebuilder.Parses(); n != 1 {
		t.Fatalf("the session was parsed %d times for two reads, want 1", n)
	}
	if st := env.reg.RebuildCacheStats(); st.Hits != 1 || st.Entries != 1 {
		t.Fatalf("stats %+v", st)
	}
	if fmt.Sprint(clientView(first.Events, false)) != fmt.Sprint(clientView(second.Events, false)) {
		t.Fatal("a cached read differs from the cold read")
	}
	t.Logf("cold %v, cached %v", cold, warm)
	// the cache hands out a copy: a caller scribbling on it does not poison it
	second.Events[0].Text = "scribble"
	third := env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	if third.Events[0].Text == "scribble" {
		t.Fatal("the cache returned its own slice")
	}
}

// A changed session file (size or mtime) invalidates the entry.
func TestRebuildCacheInvalidatedByAChangedSession(t *testing.T) {
	env := newRebuildEnv(t, true)
	setBudget(env, 16)
	ctx := context.Background()
	env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	// same size, newer mtime
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(env.file, later, later); err != nil {
		t.Fatal(err)
	}
	if r := env.reg.RebuildHistory(ctx, "r", RequesterHuman); !r.OK() {
		t.Fatalf("after touch: %q", r.Reason)
	}
	if n := env.reg.rebuilder.Parses(); n != 2 {
		t.Fatalf("a touched file was not re-read (%d parses)", n)
	}
	// bigger file: a trailing blank line changes the size and nothing else
	f, err := os.OpenFile(env.file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\n")
	f.Close()
	env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	if n := env.reg.rebuilder.Parses(); n != 3 {
		t.Fatalf("a grown file was not re-read (%d parses)", n)
	}
}

// Only client-form events are cached: no heredoc body, and the raw bodies the
// reveal path needs are not in the entry.
func TestRebuildCacheHoldsClientFormOnly(t *testing.T) {
	env := newRebuildEnv(t, true)
	setBudget(env, 16)
	env.reg.RebuildHistory(context.Background(), "r", RequesterHuman)
	env.reg.rcache.mu.Lock()
	defer env.reg.rcache.mu.Unlock()
	for _, el := range env.reg.rcache.items {
		b, _ := json.Marshal(el.Value.(*rebuildEntry).events)
		if strings.Contains(string(b), secretBody) {
			t.Fatalf("a heredoc body is in the cache: %s", b)
		}
		if refs := el.Value.(*rebuildEntry).refs; len(refs) == 0 {
			t.Fatal("the heredoc references were not cached with the events")
		}
	}
}

// The heredoc reveal still reads the session: it is never answered from the cache.
func TestRebuildHeredocIgnoresTheCache(t *testing.T) {
	env := newRebuildEnvFiller(t, true, 600)
	setBudget(env, 16)
	ctx := context.Background()
	env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	before := env.reg.rebuilder.Parses()
	rev, err := env.reg.RebuildHeredoc(ctx, "r", "toolu_B1", 0, RequesterHuman)
	if err != nil || !strings.Contains(rev.Text, secretBody) {
		t.Fatalf("reveal = %v", err)
	}
	if env.reg.rebuilder.Parses() != before+1 {
		t.Fatal("the reveal did not read the session itself")
	}
}

// A zero budget turns the cache off; a history over the budget is served uncached;
// least recently used entries go first.
func TestRebuildCacheBudget(t *testing.T) {
	env := newRebuildEnv(t, true)
	ctx := context.Background()
	setBudget(env, 0)
	env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	if n := env.reg.rebuilder.Parses(); n != 2 || env.reg.RebuildCacheStats().Entries != 0 {
		t.Fatalf("budget 0: %d parses, %+v", n, env.reg.RebuildCacheStats())
	}
	env.reg.SetRebuildCacheBudget(200) // smaller than one history
	env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	if env.reg.RebuildCacheStats().Entries != 0 {
		t.Fatal("a history over the budget was cached")
	}

	var c rebuildCache
	c.setBudget(1 << 20)
	evs := []Event{{Type: taskrunner.EventTypeText, Text: strings.Repeat("x", 300<<10)}}
	for _, id := range []string{"a", "b", "c", "d"} {
		c.put(id, 1, 1, Rebuilt{Events: evs})
		if id == "b" {
			c.get("a", 1, 1) // a is now more recent than b
		}
	}
	st := c.stats()
	if st.Bytes > st.Budget || st.Entries != 3 || st.Evictions != 1 {
		t.Fatalf("lru: %+v", st)
	}
	if _, ok := c.get("b", 1, 1); ok {
		t.Fatal("the least recently used entry (b) survived")
	}
	if _, ok := c.get("a", 1, 1); !ok {
		t.Fatal("a recently used entry was evicted")
	}
}

// Pruning a run's excerpts or its history clears its cached rebuild.
func TestRebuildCacheClearedOnPrune(t *testing.T) {
	env := newRebuildEnv(t, true)
	setBudget(env, 16)
	ctx := context.Background()
	env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	env.reg.dropResident("r")
	if env.reg.RebuildCacheStats().Entries != 0 {
		t.Fatal("excerpt prune left the entry")
	}
	env.reg.RebuildHistory(ctx, "r", RequesterHuman)
	env.reg.markHistoryPruned("r", time.Now())
	if env.reg.RebuildCacheStats().Entries != 0 {
		t.Fatal("history prune left the entry")
	}
}

// H7: nothing but a human's request reaches the rebuilder or the cache.
func TestRebuildOnOpenNeverForAnAgent(t *testing.T) {
	env := newRebuildEnv(t, true)
	setBudget(env, 16)
	ctx := context.Background()
	env.reg.RebuildHistory(ctx, "r", RequesterHuman) // warm: a cache hit exists
	env.reg.rcache.mu.Lock()
	hits := env.reg.rcache.hits
	env.reg.rcache.mu.Unlock()
	before := env.reg.rebuilder.Parses()
	got := env.reg.RebuildOnOpen(ctx, "r", RequesterAgent)
	if got.OK() || got.Reason != native.ReasonHumanOnly || len(got.Events) != 0 {
		t.Fatalf("agent open: %+v", got)
	}
	env.reg.rcache.mu.Lock()
	defer env.reg.rcache.mu.Unlock()
	if env.reg.rebuilder.Parses() != before || env.reg.rcache.hits != hits {
		t.Fatal("an agent's request touched the session or the cache")
	}
}

// A human's open rebuilds when the file is small, serves a cached rebuild of any
// size, and skips (reading nothing) a big uncached one.
func TestRebuildOnOpenIsBoundedByCostNotByIdentity(t *testing.T) {
	env := newRebuildEnv(t, true)
	setBudget(env, 16)
	ctx := context.Background()

	small := env.reg.RebuildOnOpen(ctx, "r", RequesterHuman)
	if !small.OK() || env.reg.rebuilder.Parses() != 1 {
		t.Fatalf("small file: %+v", small)
	}
	// now cached: lower the bar below the file size and it is still served
	defer func(v int64) { autoRebuildMax = v }(autoRebuildMax)
	autoRebuildMax = 1
	if hit := env.reg.RebuildOnOpen(ctx, "r", RequesterHuman); !hit.OK() || env.reg.rebuilder.Parses() != 1 {
		t.Fatalf("cached: %+v, %d parses", hit, env.reg.rebuilder.Parses())
	}
	// uncached and over the bar: skipped, nothing read
	env.reg.rcache.invalidate("r")
	if skip := env.reg.RebuildOnOpen(ctx, "r", RequesterHuman); !skip.Skipped || skip.OK() || skip.Reason != native.ReasonNone {
		t.Fatalf("big uncached: %+v", skip)
	}
	if env.reg.rebuilder.Parses() != 1 {
		t.Fatal("a skipped open read the session")
	}
	// the explicit request still rebuilds it
	if full := env.reg.RebuildHistory(ctx, "r", RequesterHuman); !full.OK() {
		t.Fatalf("explicit: %q", full.Reason)
	}
}
