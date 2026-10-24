package gitstate

import (
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
)

const lookupTip = "1111111111111111111111111111111111111111"

// lookupFix is a poller fixture with one task on branch "feat" and a PRsByHead
// answer: by default the PR merged at the branch's current tip.
func lookupFix(t *testing.T, mod ...func(*forge.PR)) (*pollerFix, store.Task) {
	t.Helper()
	f := newPollerFix(t)
	f.tip = lookupTip
	tk := f.task(f.workspace("/repo"), "feat")
	pr := mkPR(7, "feat", func(p *forge.PR) { p.State = forge.PRStateMerged; p.HeadSha = lookupTip })
	for _, m := range mod {
		m(&pr)
	}
	f.fg.byHead = map[string][]forge.PR{"feat": {pr}}
	return f, tk
}

func TestPollerLookupByHead_AdoptsMergedPRAtTheBranchTip(t *testing.T) {
	f, tk := lookupFix(t)
	if !f.p.LookupByHead(tk.ID) {
		t.Fatal("LookupByHead = false, want the merged PR adopted")
	}
	got := f.reload(tk)
	if got.PRNumber != 7 || got.PRRepo != "org/repo" || got.PRForge != "github" || got.PRHeadRef != "feat" || got.PRURL == "" {
		t.Fatalf("identity = %+v, want PR 7 of org/repo", got)
	}
	if pr := f.p.PR(got); pr == nil || pr.State != forge.PRStateMerged {
		t.Fatalf("snapshot = %+v, want merged", pr)
	}
	if n := f.calls("PRsByHead"); n != 1 || f.calls("OpenPRs") != 0 {
		t.Fatalf("PRsByHead %d OpenPRs %d, want 1 and 0", n, f.calls("OpenPRs"))
	}
	if len(f.adopted) != 1 || f.adopted[0] != tk.ID {
		t.Fatalf("OnAdopt calls = %v, want [%d]", f.adopted, tk.ID)
	}
}

func TestPollerLookupByHead_IgnoresWhatIsNotThisBranchsPR(t *testing.T) {
	cases := map[string]func(*forge.PR){
		"head sha moved after the merge": func(p *forge.PR) { p.HeadSha = "2222222222222222222222222222222222222222" },
		"fork pull request":              func(p *forge.PR) { p.FromFork = true },
		"another head ref":               func(p *forge.PR) { p.HeadRef = "other" },
		"closed without merging":         func(p *forge.PR) { p.State = forge.PRStateClosed },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			f, tk := lookupFix(t, mod)
			if f.p.LookupByHead(tk.ID) {
				t.Fatal("LookupByHead = true, want nothing adopted")
			}
			if got := f.reload(tk); got.PRNumber != 0 || got.PRURL != "" {
				t.Fatalf("identity written: %+v", got)
			}
			if len(f.adopted) != 0 {
				t.Fatalf("OnAdopt = %v", f.adopted)
			}
		})
	}
}

func TestPollerLookupByHead_AdoptsAnOpenMatchAndPrefersItOverAMergedOne(t *testing.T) {
	f, tk := lookupFix(t)
	f.fg.byHead["feat"] = append(f.fg.byHead["feat"],
		mkPR(9, "feat"), // open, sha differs from the tip: open PRs are not sha-checked
		mkPR(8, "feat", func(p *forge.PR) { p.State = forge.PRStateMerged; p.HeadSha = lookupTip }))
	if !f.p.LookupByHead(tk.ID) {
		t.Fatal("LookupByHead = false")
	}
	if got := f.reload(tk); got.PRNumber != 9 {
		t.Fatalf("adopted PR %d, want the open PR 9", got.PRNumber)
	}
}

func TestPollerLookupByHead_TaskWithAStoredPRCostsNoCall(t *testing.T) {
	f, tk := lookupFix(t)
	tk = f.withPR(tk, 3)
	if f.p.LookupByHead(tk.ID) {
		t.Fatal("LookupByHead adopted over a stored identity")
	}
	if n := f.fg.total(); n != 0 {
		t.Fatalf("forge calls = %d, want 0", n)
	}
	if got := f.reload(tk); got.PRNumber != 3 {
		t.Fatalf("identity changed to %d", got.PRNumber)
	}
}

func TestPollerLookupByHead_FloorAndHourlyCap(t *testing.T) {
	f, tk := lookupFix(t, func(p *forge.PR) { p.HeadSha = "2222222222222222222222222222222222222222" })
	for i := 0; i < 3; i++ {
		f.p.LookupByHead(tk.ID) // same instant: only the first may call
	}
	if n := f.calls("PRsByHead"); n != 1 {
		t.Fatalf("PRsByHead within the floor = %d, want 1", n)
	}
	for i := 0; i < refreshPerHour+3; i++ {
		f.clk.Advance(refreshFloor + time.Second)
		f.p.LookupByHead(tk.ID)
	}
	if n := f.calls("PRsByHead"); n != refreshPerHour {
		t.Fatalf("PRsByHead in an hour = %d, want the cap %d", n, refreshPerHour)
	}
}

func TestPollerLookupByHead_FailuresDegradeAndBookTheHost(t *testing.T) {
	for _, code := range []string{forge.CodeRateLimited, forge.CodeUnauthenticated} {
		t.Run(code, func(t *testing.T) {
			f, tk := lookupFix(t)
			other := f.task(f.workspace("/repo2"), "feat2")
			f.fg.headErr = forge.NewError(code, "")
			if f.p.LookupByHead(tk.ID) {
				t.Fatal("adopted on a failure")
			}
			if st := f.state(tk); st.LastErrorCode != code || st.ConsecutiveFailures != 1 {
				t.Fatalf("state = %+v, want %s booked", st, code)
			}
			f.clk.Advance(refreshFloor + time.Second)
			f.p.LookupByHead(tk.ID)
			f.p.LookupByHead(other.ID) // same host: suspended
			if n := f.calls("PRsByHead"); n != 1 {
				t.Fatalf("PRsByHead after the host was suspended = %d, want 1", n)
			}
		})
	}
	t.Run("other errors back the task off", func(t *testing.T) {
		f, tk := lookupFix(t)
		f.fg.headErr = forge.NewError(forge.CodeError, "")
		f.p.LookupByHead(tk.ID)
		f.clk.Advance(refreshFloor + time.Second) // past the floor, inside the 30 s backoff
		f.p.LookupByHead(tk.ID)
		if n := f.calls("PRsByHead"); n != 1 {
			t.Fatalf("PRsByHead inside the backoff = %d, want 1", n)
		}
		f.clk.Advance(backoff(1))
		f.p.LookupByHead(tk.ID)
		if n := f.calls("PRsByHead"); n != 2 {
			t.Fatalf("PRsByHead after the backoff = %d, want 2", n)
		}
	})
	t.Run("repo cannot be resolved", func(t *testing.T) {
		f, tk := lookupFix(t)
		f.fg.repoErr = forge.NewError(forge.CodeCLIMissing, "")
		if f.p.LookupByHead(tk.ID) || f.calls("PRsByHead") != 0 {
			t.Fatal("looked up without a repo")
		}
		if st := f.state(tk); st.LastErrorCode != forge.CodeCLIMissing {
			t.Fatalf("state = %+v", st)
		}
	})
	t.Run("no provider", func(t *testing.T) {
		f, tk := lookupFix(t)
		f.p.o.Provider = func() forge.Provider { return nil }
		if f.p.LookupByHead(tk.ID) {
			t.Fatal("adopted without a provider")
		}
	})
}

func TestPollerLookupByHead_SingleFlight(t *testing.T) {
	f, tk := lookupFix(t)
	f.fg.headEntered, f.fg.headGate = make(chan struct{}, 4), make(chan struct{})
	var wg sync.WaitGroup
	res := make([]bool, 2)
	wg.Add(1)
	go func() { defer wg.Done(); res[0] = f.p.LookupByHead(tk.ID) }()
	<-f.fg.headEntered
	wg.Add(1)
	go func() { defer wg.Done(); res[1] = f.p.LookupByHead(tk.ID) }()
	time.Sleep(50 * time.Millisecond) // the joiner has parked on the flight
	close(f.fg.headGate)
	wg.Wait()
	if !res[0] || !res[1] {
		t.Fatalf("results = %v, want both callers to see the adoption", res)
	}
	if n := f.calls("PRsByHead"); n != 1 {
		t.Fatalf("PRsByHead = %d, want 1", n)
	}
}
