package gitstate

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// TestPoller_ForgeAuthChanged_LiftsSuspensionAtOnce: a login (forge.setToken)
// ends the host suspension a missing login set, so the next poll calls the
// forge again instead of waiting out the 10 minutes. Another host's change
// leaves it alone.
func TestPoller_ForgeAuthChanged_LiftsSuspensionAtOnce(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/a")
	a := f.task(ws, "a")
	f.fg.setOpen("/a", mkPR(1, "other"))
	f.in.focused = []int64{a.ID}
	f.tick()
	f.fg.openErr = forge.NewError(forge.CodeUnauthenticated, "")
	f.step(121 * time.Second)
	if got := f.p.ForgeBlock(f.reload(a)); got != forge.CodeUnauthenticated {
		t.Fatalf("ForgeBlock = %q, want forge_unauthenticated", got)
	}

	f.p.ForgeAuthChanged("gitlab.example.com")
	if got := f.p.ForgeBlock(f.reload(a)); got != forge.CodeUnauthenticated {
		t.Fatalf("another host's login lifted the suspension (ForgeBlock = %q)", got)
	}

	f.fg.openErr = nil
	f.p.ForgeAuthChanged("github.com")
	if got := f.p.ForgeBlock(f.reload(a)); got != "" {
		t.Fatalf("ForgeBlock after the login = %q, want none", got)
	}
	before := f.fg.total()
	f.step(125 * time.Second) // far inside the 10 min suspension
	if f.fg.total() <= before {
		t.Fatalf("no forge call after the login (calls %d -> %d): the host is still suspended", before, f.fg.total())
	}
	var nilPoller *Poller
	nilPoller.ForgeAuthChanged("github.com") // must not panic
}

// TestPoller_ForgeAuthChanged_KeepsRateLimitSuspension: a login does not
// clear a rate limit.
func TestPoller_ForgeAuthChanged_KeepsRateLimitSuspension(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/a")
	a := f.task(ws, "a")
	f.fg.setOpen("/a", mkPR(1, "other"))
	f.in.focused = []int64{a.ID}
	f.tick()
	f.fg.openErr = forge.NewError(forge.CodeRateLimited, "")
	f.step(121 * time.Second)
	f.fg.openErr = nil
	f.p.ForgeAuthChanged("github.com")
	before := f.fg.total()
	f.step(125 * time.Second)
	if f.fg.total() != before {
		t.Fatalf("a login lifted a rate-limit suspension (calls %d -> %d)", before, f.fg.total())
	}
}

// TestFetch_NetworkGitOptionPrefixesTheFetch: the background `git fetch` gets
// the per-invocation options Options.NetworkGit returns, in front of the
// subcommand, for the origin remote; without the option it is unchanged.
func TestFetch_NetworkGitOptionPrefixesTheFetch(t *testing.T) {
	for _, withHook := range []bool{true, false} {
		src := newFakeSource(task(1, "a"))
		var mu sync.Mutex
		var fetches [][]string
		r := &countRunner{}
		r.hook = func(_ context.Context, args []string) {
			for _, a := range args {
				if a == "fetch" {
					mu.Lock()
					fetches = append(fetches, append([]string(nil), args...))
					mu.Unlock()
				}
			}
		}
		var gotRemote string
		s := newSvc(t, src, r, &fakeInterest{watched: true}, newClock(), func(o *Options) {
			if withHook {
				o.NetworkGit = func(_ context.Context, _, remote string) []string {
					gotRemote = remote
					return forge.CredentialHelperArgs()
				}
			}
		})
		if _, err := s.Get(1); err != nil {
			t.Fatal(err)
		}
		s.Tick()
		s.Wait()
		mu.Lock()
		got := fetches
		mu.Unlock()
		if len(got) != 1 {
			t.Fatalf("hook=%v: fetches = %v, want one", withHook, got)
		}
		wantTail := []string{"fetch", "--no-tags", "origin"}
		if withHook {
			want := append(forge.CredentialHelperArgs(), wantTail...)
			if !reflect.DeepEqual(got[0][:len(want)], want) || gotRemote != "origin" {
				t.Fatalf("fetch args = %v (remote %q), want prefix %v", got[0], gotRemote, want)
			}
		} else if !reflect.DeepEqual(got[0][:3], wantTail) {
			t.Fatalf("fetch args without the hook = %v", got[0])
		}
	}
}
