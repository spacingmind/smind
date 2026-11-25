package wsapi

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/crash"
)

// DNS resolution for the browser's live deny list (the browser-for-agents PR 6
// spec, AC4-AC7): a fleet link addressed by a DNS name must deny the name's
// addresses too, re-resolved at every live-deny refresh, so a page cannot
// reach the peer by an address the name moved away from.

// lookupFunc resolves one DNS name to address strings (like net.Resolver.LookupHost).
type lookupFunc func(ctx context.Context, host string) ([]string, error)

// resolvedName is the stored state of one name, guarded by nameResolver.mu.
type resolvedName struct {
	addrs []string             // the last good answer
	seen  map[string]time.Time // every address of a good answer, when it was last seen
	have  bool                 // any good answer ever
}

// resolveRetention is how long an address a name moved away from is still served:
// a peer behind a rotating record, or one that re-addressed, stays denied on its
// old address for a minute.
const resolveRetention = 60 * time.Second

// nameResolver turns DNS names into the addresses the live deny list serves
// alongside the name. Lookups run in the background bounded by timeout, so a
// hung resolver never blocks the call path: Addrs waits at most wait and then
// serves the stored answers.
type nameResolver struct {
	// timeout bounds one lookup (default 2s); wait bounds how long Addrs
	// waits for lookups it started or joined (default 250ms). Plain fields,
	// settable by tests after construction.
	timeout time.Duration
	wait    time.Duration
	// retention is how long an old address stays served (default resolveRetention);
	// now is the clock (tests replace it).
	retention time.Duration
	now       func() time.Time

	lookup lookupFunc

	// joins counts calls that joined a lookup already in flight (a test seam: it lets
	// a test release a blocked lookup only once every caller has joined it).
	joins atomic.Int32

	mu      sync.Mutex
	state   map[string]*resolvedName
	flights map[string]chan struct{} // in-flight lookups, one per name
}

func newNameResolver(lookup lookupFunc) *nameResolver {
	if lookup == nil {
		lookup = func(ctx context.Context, host string) ([]string, error) {
			return net.DefaultResolver.LookupHost(ctx, host)
		}
	}
	return &nameResolver{
		timeout:   2 * time.Second,
		wait:      250 * time.Millisecond,
		retention: resolveRetention,
		now:       time.Now,
		lookup:    lookup,
		state:     make(map[string]*resolvedName),
		flights:   make(map[string]chan struct{}),
	}
}

// Addrs returns, per name, the sorted unique union of the latest good answer and
// every address a good answer held within the retention window: an address the
// name moved away from stays served for a minute. Every call re-resolves: a lookup with
// no flight in progress is started (concurrent callers share it); one already
// in flight is joined. Addrs waits at most r.wait for them, then builds the
// result from the stored state — lookups that finish later still update it.
// A name with no good answer ever makes the whole call fail closed; a name
// that resolved before keeps its last good answer on failure.
func (r *nameResolver) Addrs(names []string) (map[string][]string, error) {
	if len(names) == 0 {
		return map[string][]string{}, nil
	}
	unique := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		unique = append(unique, name)
	}

	r.mu.Lock()
	waits := make([]<-chan struct{}, 0, len(unique))
	for _, name := range unique {
		if done, ok := r.flights[name]; ok {
			r.joins.Add(1)
			// Joined, not started: a name that already has a good answer does not
			// make this call wait for a lookup that is still pending (a slow
			// resolver would otherwise cost every refresh the full wait).
			if st := r.state[name]; st == nil || !st.have {
				waits = append(waits, done)
			}
			continue
		}
		done := make(chan struct{})
		r.flights[name] = done
		waits = append(waits, done)
		crash.Go("wsapi.browserResolve", func() { r.lookupOne(name, done) })
	}
	r.mu.Unlock()

	// One bound for every lookup this call started or joined.
	timer := time.NewTimer(r.wait)
	defer timer.Stop()
	expired := false
	for _, done := range waits {
		if expired {
			break
		}
		select {
		case <-done:
		case <-timer.C:
			expired = true
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Names no longer asked for (a link was removed) are forgotten.
	for name := range r.state {
		if !seen[name] {
			delete(r.state, name)
		}
	}
	now := r.now()
	out := make(map[string][]string, len(unique))
	for _, name := range unique {
		st := r.state[name]
		if st == nil || !st.have {
			return nil, fmt.Errorf("cannot resolve %s", name)
		}
		served := append([]string(nil), st.addrs...)
		latest := familiesOf(st.addrs)
		for a, at := range st.seen {
			if !retired(now, at, r.retention, a, latest) {
				served = append(served, a)
			}
		}
		out[name] = unionSorted(served, nil)
	}
	return out, nil
}

// lookupOne runs one lookup for name and updates the stored state from its
// result (even after every waiter stopped waiting). The flight is cleared
// before done closes, so the next call starts a fresh lookup.
func (r *nameResolver) lookupOne(name string, done chan struct{}) {
	defer func() {
		r.mu.Lock()
		if r.flights[name] == done {
			delete(r.flights, name)
		}
		r.mu.Unlock()
		close(done)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	addrs, err := r.safeLookup(ctx, name)
	if err != nil {
		return // a failure leaves the stored state untouched
	}
	valid := validAddrs(addrs)
	if len(valid) == 0 && len(addrs) == 0 {
		return // an empty answer says nothing: keep what is known
	}
	// A non-empty answer of only unspecified addresses (a sinkholed name, a VPN that
	// is down) IS a resolution, with no usable address: the name entry still denies,
	// and the browser is not taken down for it.
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.state[name]
	if st == nil {
		st = &resolvedName{}
		r.state[name] = st
	}
	now := r.now()
	if st.seen == nil {
		st.seen = map[string]time.Time{}
	}
	for _, a := range valid {
		st.seen[a] = now
	}
	st.addrs = valid
	st.have = true
	fams := familiesOf(valid)
	for a, at := range st.seen {
		if retired(now, at, r.retention, a, fams) {
			delete(st.seen, a)
		}
	}
}

// familiesOf says which address families an answer holds (4 and 6).
func familiesOf(addrs []string) map[int]bool {
	fams := map[int]bool{}
	for _, s := range addrs {
		fams[familyOf(s)] = true
	}
	return fams
}

func familyOf(addr string) int {
	if strings.Contains(addr, ":") {
		return 6
	}
	return 4
}

// retired says whether an address last seen at `at` may stop being served: only
// when it is older than the retention window AND the latest answer holds its
// family. An answer without a family (an A-only answer after A+AAAA: the AAAA
// lookup may have been lost, not removed) cannot retire that family's addresses.
func retired(now, at time.Time, retention time.Duration, addr string, latest map[int]bool) bool {
	return now.Sub(at) >= retention && latest[familyOf(addr)]
}

// safeLookup runs the lookup; a panic in it is a failed lookup, never a crashed daemon.
func (r *nameResolver) safeLookup(ctx context.Context, name string) (addrs []string, err error) {
	defer func() {
		if p := recover(); p != nil {
			addrs, err = nil, fmt.Errorf("lookup panicked: %v", p)
		}
	}()
	return r.lookup(ctx, name)
}

// validAddrs parses the lookup answer into canonical address strings; strings
// that do not parse are ignored, and so are unspecified addresses (a sinkholed
// name answers 0.0.0.0, and a deny entry for it would reserve the port on every
// address on the internet).
func validAddrs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		a, err := netip.ParseAddr(s)
		if err != nil {
			continue
		}
		a = a.Unmap().WithZone("") // an IPv4-mapped answer is denied as the IPv4 address
		if a.IsUnspecified() {
			continue
		}
		out = append(out, a.String())
	}
	return out
}

// unionSorted merges two address lists, dropping repeats.
func unionSorted(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, s := range list {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}
