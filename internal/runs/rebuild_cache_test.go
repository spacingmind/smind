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
		c.put(id, c.generation(), 1, 1, Rebuilt{Events: evs})
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

// A reader that scribbles on a returned result (events or heredoc references)
// does not change what the next reader gets.
func TestRebuildCacheHandsOutCopies(t *testing.T) {
	var c rebuildCache
	c.setBudget(1 << 20)
	refs := map[string][]taskrunner.HeredocRef{"call": {{Index: 0}}}
	c.put("a", c.generation(), 1, 1, Rebuilt{Events: []Event{{Text: "x"}}, Refs: refs})
	refs["call"][0].Index = 7 // the producer's own map
	got, _ := c.get("a", 1, 1)
	got.Refs["call"][0].Index = 9
	delete(got.Refs, "call")
	again, _ := c.get("a", 1, 1)
	if len(again.Refs["call"]) != 1 || again.Refs["call"][0].Index != 0 {
		t.Fatalf("the cache shares its refs: %+v", again.Refs)
	}
}

// A rebuild that was in flight when its run was pruned is not cached afterwards.
func TestRebuildCacheDropsAPutThatRacedAnInvalidate(t *testing.T) {
	var c rebuildCache
	c.setBudget(1 << 20)
	gen := c.generation()
	c.invalidate("a") // the prune lands while the rebuild runs
	c.put("a", gen, 1, 1, Rebuilt{Events: []Event{{Text: "x"}}})
	if c.stats().Entries != 0 {
		t.Fatal("a pruned run's rebuild was cached")
	}
}

// The events a reader gets share nothing with the cache: a scribbled element or
// a raw JSON byte does not show up in the next read; an event with a pointer or
// slice payload is never cached.
func TestRebuildCacheEventsAreDeepCopies(t *testing.T) {
	var c rebuildCache
	c.setBudget(1 << 20)
	in := []Event{{Type: taskrunner.EventTypeToolCall, Text: "t", ToolInput: json.RawMessage(`{"a":1}`), ToolResult: json.RawMessage(`"ok"`), RawPayload: json.RawMessage(`{"k":1}`)}}
	c.put("a", c.generation(), 1, 1, Rebuilt{Events: in})
	in[0].ToolInput[2] = 'Z' // the producer's own bytes
	first, _ := c.get("a", 1, 1)
	if string(first.Events[0].ToolInput) != `{"a":1}` {
		t.Fatalf("put kept the producer's bytes: %s", first.Events[0].ToolInput)
	}
	first.Events[0].Text = "scribble"
	first.Events[0].ToolInput[2] = 'Y'
	first.Events[0].ToolResult[1] = 'Y'
	first.Events[0].RawPayload[2] = 'Y'
	again, _ := c.get("a", 1, 1)
	e := again.Events[0]
	if e.Text != "t" || string(e.ToolInput) != `{"a":1}` || string(e.ToolResult) != `"ok"` || string(e.RawPayload) != `{"k":1}` {
		t.Fatalf("a reader changed the cached history: %+v", e)
	}
	// an event that carries pointers is served uncached
	c.put("b", c.generation(), 1, 1, Rebuilt{Events: []Event{{Type: taskrunner.EventTypeUsage, Usage: &taskrunner.Usage{}}}})
	if _, ok := c.get("b", 1, 1); ok {
		t.Fatal("an event with a pointer payload was cached")
	}
}

// ---- ACP: a load starts an agent, so only a confirmed human request does it ----

// acpEnv turns rebuild env run "r" into an ACP run whose "agent" is a counter.
func acpRunsEnv(t *testing.T, replayErr error) (*rebuildEnv, *int) {
	t.Helper()
	env := newRebuildEnv(t, true)
	starts := new(int)
	live := liveTurn("")
	var replay []Event
	replay = append(replay, Event{Type: taskrunner.EventTypeUserMessage, Text: rebuildPrompt})
	for _, e := range live {
		if e.Type == taskrunner.EventTypeText || e.Type == taskrunner.EventTypeThinking || e.Type == taskrunner.EventTypeToolCall {
			e.Raw = nil
			replay = append(replay, e)
		}
	}
	env.reg.SetRebuilder(native.New(native.Config{
		ACPReplay: func(ctx context.Context, ref native.Ref, max int64) ([]Event, error) {
			*starts++
			return replay, replayErr
		},
	}))
	ref := native.Ref{Provider: "glm", Harness: native.HarnessACP, SessionID: "s1", Cwd: t.TempDir(), PromptSha256: sha256Hex(rebuildPrompt)}
	b, _ := json.Marshal(ref)
	if err := env.e.st.SetRunNativeRef("r", string(b)); err != nil {
		t.Fatal(err)
	}
	return env, starts
}

func TestRebuildOnOpenSkipsACP(t *testing.T) {
	env, starts := acpRunsEnv(t, nil)
	got := env.reg.RebuildOnOpen(context.Background(), "r", RequesterHuman)
	if !got.Skipped || got.OK() || *starts != 0 {
		t.Fatalf("an automatic open of an ACP run: %+v, %d agent starts", got, *starts)
	}
}

func TestRebuildACPHistoryNeedsAConfirmedHuman(t *testing.T) {
	env, starts := acpRunsEnv(t, nil)
	ctx := context.Background()
	if r := env.reg.RebuildHistory(ctx, "r", RequesterHuman); r.Reason != native.ReasonConfirmRequired || *starts != 0 {
		t.Fatalf("unconfirmed: %q, %d starts", r.Reason, *starts)
	}
	if r := env.reg.RebuildHistoryConfirmed(ctx, "r", RequesterAgent, true); r.Reason != native.ReasonHumanOnly || *starts != 0 {
		t.Fatalf("agent: %q, %d starts", r.Reason, *starts)
	}
	got := env.reg.RebuildHistoryConfirmed(ctx, "r", RequesterHuman, true)
	if !got.OK() || *starts != 1 || !strings.Contains(fmt.Sprint(clientView(got.Events, false)), "need the failing test first") {
		t.Fatalf("confirmed: %q (%s), %d starts", got.Reason, got.Detail, *starts)
	}
	// not cached: a second confirmed load starts the agent again
	env.reg.RebuildHistoryConfirmed(ctx, "r", RequesterHuman, true)
	if *starts != 2 || env.reg.RebuildCacheStats().Entries != 0 {
		t.Fatalf("starts %d, cache %+v", *starts, env.reg.RebuildCacheStats())
	}
}

// An ACP rebuild is equivalent to the stored-full read of the same turn.
func TestRebuildACPEquivalentToLive(t *testing.T) {
	env, _ := acpRunsEnv(t, nil)
	got := env.reg.RebuildHistoryConfirmed(context.Background(), "r", RequesterHuman, true)
	if !got.OK() {
		t.Fatalf("refused: %q (%s)", got.Reason, got.Detail)
	}
	want := clientView(liveTurn(""), true)
	// the replay coalesces text chunks like the live path does for the same message
	if fmt.Sprint(clientView(got.Events, false)) != fmt.Sprint(want) {
		t.Fatalf("differs from the live turn\n got %q\nwant %q", clientView(got.Events, false), want)
	}
}

func TestHistoryInfoACPAndNoLoadSession(t *testing.T) {
	env, _ := acpRunsEnv(t, native.ErrNoLoadSession)
	info := env.reg.HistoryInfo("r")
	if info.Source != "skeleton" || !info.CanLoadFull || !info.LoadStartsAgent {
		t.Fatalf("before: %+v", info)
	}
	if r := env.reg.RebuildHistoryConfirmed(context.Background(), "r", RequesterHuman, true); r.Reason != native.ReasonCannotRebuild {
		t.Fatalf("no loadSession: %q", r.Reason)
	}
	if info = env.reg.HistoryInfo("r"); info.CanLoadFull || info.LoadStartsAgent || info.Reason != string(native.ReasonCannotRebuild) {
		t.Fatalf("after the agent said it cannot load: %+v", info)
	}
}

// A run with no recorded prompt hash cannot be matched in a replay: no button, no dialog.
func TestHistoryInfoACPNeedsAPromptHash(t *testing.T) {
	env, _ := acpRunsEnv(t, nil)
	ref := native.Ref{Provider: "glm", Harness: native.HarnessACP, SessionID: "s1", Cwd: t.TempDir()}
	b, _ := json.Marshal(ref)
	if err := env.e.st.SetRunNativeRef("r", string(b)); err != nil {
		t.Fatal(err)
	}
	if info := env.reg.HistoryInfo("r"); info.CanLoadFull || info.LoadStartsAgent {
		t.Fatalf("offered a load that cannot match a turn: %+v", info)
	}
}
