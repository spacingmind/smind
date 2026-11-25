package wsapi

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/store"
)

func denyFleetByName(name string) fakeFleet {
	return fakeFleet{
		hosts: []store.FleetHost{{HostID: "h1"}},
		links: map[string][]store.FleetLink{"h1": {{HostID: "h1", TransportJSON: `{"kind":"direct","url":"wss://` + name + `:7443/ws"}`}}},
	}
}

func hasEndpoint(eps []browser.Endpoint, host string, port int) bool {
	for _, e := range eps {
		if e.Host == host && e.Port == port {
			return true
		}
	}
	return false
}

func fastResolver(lookup lookupFunc) *nameResolver {
	r := newNameResolver(lookup)
	r.wait, r.timeout = 150*time.Millisecond, time.Second
	return r
}

// A fleet peer addressed by DNS name is denied by the name AND by the addresses it
// resolves to: a page that reaches the peer by its IP is refused through the policy.
func TestFleetPeerByNameIsDeniedByAddress(t *testing.T) {
	t.Parallel()
	s := newLiveDenySource(t.TempDir(), denyFleetByName("peer.example.test"))
	s.resolver = fastResolver(func(context.Context, string) ([]string, error) {
		return []string{"203.0.113.7", "2001:db8::7"}, nil
	})
	eps, err := s.Endpoints()
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"peer.example.test", "203.0.113.7", "2001:db8::7"} {
		if !hasEndpoint(eps, host, 7443) {
			t.Errorf("%s:7443 missing from the deny list: %+v", host, eps)
		}
	}
	pol, err := browser.NewPolicy([]string{"203.0.113.7"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pol = pol.WithLiveDeny(s.Endpoints)
	for _, u := range []string{"https://203.0.113.7:7443/", "https://peer.example.test:7443/", "https://[2001:db8::7]:7443/"} {
		if pol.CheckURL(u).Allowed {
			t.Errorf("%s must be refused: the peer is denied by name and by address", u)
		}
	}
	// Another port of the same address is not the peer's listener.
	if !pol.CheckURL("https://203.0.113.7:8443/").Allowed {
		t.Error("only the peer's port is reserved")
	}
}

// IP-literal links are not resolved: no lookup at all.
func TestFleetPeerByIPLiteralIsNotResolved(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	s := newLiveDenySource(t.TempDir(), denyFleetByName("10.9.9.9"))
	s.resolver = fastResolver(func(context.Context, string) ([]string, error) { calls.Add(1); return nil, errors.New("no") })
	eps, err := s.Endpoints()
	if err != nil || !hasEndpoint(eps, "10.9.9.9", 7443) || calls.Load() != 0 {
		t.Fatalf("eps %+v err %v lookups %d: a literal is denied as is and never resolved", eps, err, calls.Load())
	}
}

// A name that never resolved fails the whole read closed, and the policy refuses
// every navigation until it resolves; the name entry is still reported.
func TestFleetPeerNameThatNeverResolvedFailsClosed(t *testing.T) {
	t.Parallel()
	var ok atomic.Bool
	s := newLiveDenySource(t.TempDir(), denyFleetByName("peer.example.test"))
	s.resolver = fastResolver(func(context.Context, string) ([]string, error) {
		if ok.Load() {
			return []string{"203.0.113.7"}, nil
		}
		return nil, errors.New("no such host")
	})
	eps, err := s.Endpoints()
	if err == nil {
		t.Fatal("a peer name that cannot be resolved must fail the read closed")
	}
	if !hasEndpoint(eps, "peer.example.test", 7443) {
		t.Errorf("the name entry is kept for the log line: %+v", eps)
	}
	pol, _ := browser.NewPolicy(nil, nil)
	pol = pol.WithLiveDeny(s.Endpoints)
	if pol.CheckURL("http://localhost:3000/").Allowed {
		t.Error("the policy must refuse while the deny list cannot be built")
	}
	// Once it resolves (after the half-second cache), reads work again.
	ok.Store(true)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) { // sleep-ok: past the cache window
		if eps, err = s.Endpoints(); err == nil {
			break
		}
	}
	if err != nil || !hasEndpoint(eps, "203.0.113.7", 7443) {
		t.Fatalf("after resolution: %+v %v", eps, err)
	}
}

// A lookup that later fails or hangs keeps the last good addresses AND the name.
func TestFleetPeerNameKeepsTheLastGoodAnswerWhenTheLookupFails(t *testing.T) {
	t.Parallel()
	var mode atomic.Int32 // 0 good, 1 error, 2 hang
	s := newLiveDenySource(t.TempDir(), denyFleetByName("peer.example.test"))
	s.resolver = fastResolver(func(ctx context.Context, _ string) ([]string, error) {
		switch mode.Load() {
		case 0:
			return []string{"203.0.113.7"}, nil
		case 1:
			return nil, errors.New("servfail")
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if _, err := s.Endpoints(); err != nil {
		t.Fatal(err)
	}
	for _, m := range []int32{1, 2} {
		mode.Store(m)
		time.Sleep(550 * time.Millisecond) // sleep-ok: past the half-second cache window, so the next read refreshes
		start := time.Now()
		eps, err := s.Endpoints()
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("mode %d: the deny read took %v: a hung resolver must not block the call path", m, d)
		}
		if err != nil || !hasEndpoint(eps, "203.0.113.7", 7443) || !hasEndpoint(eps, "peer.example.test", 7443) {
			t.Fatalf("mode %d: %+v %v: the last good answer and the name stay", m, eps, err)
		}
	}
}

func denyFleetByURLs(urls ...string) fakeFleet {
	var links []store.FleetLink
	for _, u := range urls {
		links = append(links, store.FleetLink{HostID: "h1", TransportJSON: `{"kind":"direct","url":"` + u + `"}`})
	}
	return fakeFleet{hosts: []store.FleetHost{{HostID: "h1"}}, links: map[string][]store.FleetLink{"h1": links}}
}

// Stored hosts that are not plain names are canonicalised like the policy does:
// 127.1 and hex forms are denied as the address they are (never resolved), and an
// internationalised name is resolved and denied in its punycode form.
func TestFleetPeerNonCanonicalHostsAreCanonicalised(t *testing.T) {
	t.Parallel()
	var looked sync.Map
	s := newLiveDenySource(t.TempDir(), denyFleetByURLs("wss://127.1:7441/ws", "wss://0x7f.0.0.2:7442/ws", "wss://b\u00fccher.example:7443/ws"))
	s.resolver = fastResolver(func(_ context.Context, name string) ([]string, error) {
		looked.Store(name, true)
		return []string{"203.0.113.9"}, nil
	})
	eps, err := s.Endpoints()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		host string
		port int
	}{{"127.0.0.1", 7441}, {"127.0.0.2", 7442}, {"xn--bcher-kva.example", 7443}, {"203.0.113.9", 7443}} {
		if !hasEndpoint(eps, want.host, want.port) {
			t.Errorf("%s:%d missing: %+v", want.host, want.port, eps)
		}
	}
	if _, ok := looked.Load("127.0.0.1"); ok {
		t.Error("a non-canonical IPv4 literal must never be resolved as a name")
	}
	if _, ok := looked.Load("xn--bcher-kva.example"); !ok {
		t.Error("the internationalised name is resolved in its punycode form")
	}
}

// A stored host smind cannot compare fails the read closed instead of being dropped.
func TestFleetPeerUncomparableHostFailsClosed(t *testing.T) {
	t.Parallel()
	s := newLiveDenySource(t.TempDir(), denyFleetByURLs("wss://bad!host:7443/ws"))
	s.resolver = fastResolver(func(context.Context, string) ([]string, error) { return []string{"203.0.113.9"}, nil })
	if _, err := s.Endpoints(); err == nil {
		t.Fatal("a linked host that cannot be canonicalised must refuse the browser, not vanish from the deny list")
	}
}

// A name that answers only unspecified addresses keeps its name entry and does not
// take the browser down.
func TestFleetPeerSinkholedNameKeepsTheBrowserWorking(t *testing.T) {
	t.Parallel()
	s := newLiveDenySource(t.TempDir(), denyFleetByName("peer.example.test"))
	s.resolver = fastResolver(func(context.Context, string) ([]string, error) { return []string{unspecifiedV4}, nil })
	eps, err := s.Endpoints()
	if err != nil {
		t.Fatalf("a sinkholed peer name must not fail the read: %v", err)
	}
	if !hasEndpoint(eps, "peer.example.test", 7443) || hasEndpoint(eps, unspecifiedV4, 7443) {
		t.Fatalf("the name is denied, the unspecified address is not: %+v", eps)
	}
}

// Store errors fail closed too, as an unresolvable name does.
func TestFleetStoreErrorsFailClosed(t *testing.T) {
	t.Parallel()
	for name, f := range map[string]fakeFleet{
		"hosts": {err: errors.New("db closed")},
		"host links": func() fakeFleet {
			f := denyFleetByName("peer.example.test")
			f.linkErr = errors.New("db closed")
			return f
		}(),
	} {
		s := newLiveDenySource(t.TempDir(), f)
		s.resolver = fastResolver(func(context.Context, string) ([]string, error) { return []string{"203.0.113.9"}, nil })
		if _, err := s.Endpoints(); err == nil {
			t.Errorf("%s: an unreadable store must refuse the browser", name)
		}
	}
}
