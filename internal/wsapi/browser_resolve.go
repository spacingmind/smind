package wsapi

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"sync"
	"time"
)

// DNS resolution for the browser's live deny list (the browser-for-agents PR 6
// spec, AC4-AC7): a fleet link addressed by a DNS name must deny the name's
// addresses too, re-resolved at every live-deny refresh, so a page cannot
// reach the peer by an address the name moved away from.

// lookupFunc resolves one DNS name to address strings (like net.Resolver.LookupHost).
type lookupFunc func(ctx context.Context, host string) ([]string, error)

// resolvedName is the stored state of one name, guarded by nameResolver.mu.
type resolvedName struct {
	addrs []string // the last good answer
	prev  []string // the good answer before it
	have  bool     // any good answer ever
}

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

	lookup lookupFunc

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
		timeout: 2 * time.Second,
		wait:    250 * time.Millisecond,
		lookup:  lookup,
		state:   make(map[string]*resolvedName),
		flights: make(map[string]chan struct{}),
	}
}

// Addrs returns, per name, the sorted unique union of the last two good
// answers (the current one and the one before it): an address the name moved
// away from is served one more refresh. Every call re-resolves: a lookup with
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
			waits = append(waits, done)
			continue
		}
		done := make(chan struct{})
		r.flights[name] = done
		waits = append(waits, done)
		go r.lookupOne(name, done)
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
	out := make(map[string][]string, len(unique))
	for _, name := range unique {
		st := r.state[name]
		if st == nil || !st.have {
			return nil, fmt.Errorf("cannot resolve %s", name)
		}
		out[name] = unionSorted(st.addrs, st.prev)
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
	addrs, err := r.lookup(ctx, name)
	if err != nil {
		return // a failure leaves the stored state untouched
	}
	valid := validAddrs(addrs)
	if len(valid) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.state[name]
	if st == nil {
		st = &resolvedName{}
		r.state[name] = st
	}
	st.prev = st.addrs
	st.addrs = valid
	st.have = true
}

// validAddrs parses the lookup answer into canonical address strings; strings
// that do not parse are ignored.
func validAddrs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		a, err := netip.ParseAddr(s)
		if err != nil {
			continue
		}
		out = append(out, a.Unmap().WithZone("").String()) // an IPv4-mapped answer is denied as the IPv4 address
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
