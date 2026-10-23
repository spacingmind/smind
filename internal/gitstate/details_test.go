package gitstate

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
)

// A detail fetch is one fake PRDetails call = one `gh pr view` + one graphql.
// The first fetch of a workspace also pays one `gh repo view` (Repo).

func detailsFixture(state string) forge.PRDetails {
	return forge.PRDetails{
		Repo: "org/repo", URL: "https://github.com/org/repo/pull/5", Title: "t", State: forge.PRStateOpen,
		Checks: forge.DetailChecks{
			Checks: forge.Checks{State: state, Total: 1},
			Items:  []forge.CheckItem{{ID: "run:11:job:22", Name: "lint", State: forge.CheckFailure}},
		},
		Reviews: forge.DetailReviews{Items: []forge.ReviewItem{}, Awaiting: []string{}},
		Threads: forge.DetailThreads{Items: []forge.Thread{}},
	}
}

type detailsFix struct {
	*pollerFix
	tk store.Task
}

func newDetailsFix(t *testing.T, state string) *detailsFix {
	f := newPollerFix(t)
	f.fg.detail = detailsFixture(state)
	ws := f.workspace("/repo")
	tk := f.withPR(f.task(ws, "a"), 5)
	return &detailsFix{pollerFix: f, tk: tk}
}

func (f *detailsFix) read(focused, refresh bool) (forge.PRDetails, error) {
	return f.p.PRDetails(f.tk.ID, focused, refresh)
}

// focusRead is what a connection does when it opens the tab.
func (f *detailsFix) focusRead() (forge.PRDetails, error) {
	f.p.NoteDetailsFocus(f.tk.ID)
	return f.read(true, false)
}

func (f *detailsFix) booked() int {
	n, _, err := f.db.GetForgeBudget("github.com", f.clk.Now())
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func wantCalls(t *testing.T, f *pollerFix, method string, want int) {
	t.Helper()
	if got := f.fg.count(method); got != want {
		t.Fatalf("%s calls = %d, want %d", method, got, want)
	}
}

func TestPRDetails_NeverOpened_ZeroCalls(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksPending)
	f.fg.setOpen("/repo", mkPR(5, "a"))
	f.in.focused = []int64{f.tk.ID} // the git card is on screen, not the PR tab
	for range 60 {
		f.step(time.Minute)
		d, err := f.read(false, false) // a connection that did not focus the tab
		if err != nil || d.Stale != forge.StaleNotFocused {
			t.Fatalf("unfocused read = %+v, %v", d, err)
		}
	}
	wantCalls(t, f.pollerFix, "PRDetails", 0)
	wantCalls(t, f.pollerFix, "CheckLog", 0)
	if f.fg.count("OpenPRs") == 0 {
		t.Fatal("the list poll never ran: the hour proves nothing")
	}
}

func TestPRDetails_OpenedFetchesOnce(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksSuccess)
	for range 3 { // three connections focus and read
		d, err := f.focusRead()
		if err != nil || d.TaskID != f.tk.ID || d.Number != 5 || d.Stale != "" || d.FetchedAt.IsZero() {
			t.Fatalf("read = %+v, %v", d, err)
		}
	}
	wantCalls(t, f.pollerFix, "PRDetails", 1)
	if got := f.booked(); got != 3 { // gh repo view + gh pr view + graphql
		t.Fatalf("booked %d forge calls, want 3 (repo + both detail calls)", got)
	}
}

func TestPRDetails_ConcurrentReadsSingleFlight(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksSuccess)
	f.fg.detEntered, f.fg.detGate = make(chan struct{}, 8), make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.p.NoteDetailsFocus(f.tk.ID)
			if _, err := f.read(true, false); err != nil {
				errs <- err
			}
		}()
	}
	<-f.fg.detEntered
	time.Sleep(50 * time.Millisecond) // let the others reach the flight
	close(f.fg.detGate)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 1)
	if got := f.booked(); got != 3 {
		t.Fatalf("booked %d, want 3", got)
	}
}

func TestPRDetails_SecondReadWithinFloorServedFromCache(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksPending)
	if _, err := f.focusRead(); err != nil {
		t.Fatal(err)
	}
	t0 := f.clk.Now()
	f.clk.Advance(10 * time.Second)
	d, err := f.read(true, false)
	if err != nil || d.Stale != "" || !d.FetchedAt.Equal(t0) {
		t.Fatalf("second read = %+v, %v", d, err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 1)

	// refresh inside the 15 s repo floor: the cache and refresh_limited.
	d, err = f.read(true, true)
	var lim *RefreshLimitedError
	if !errors.As(err, &lim) || !lim.RetryAt.Equal(t0.Add(repoFloor)) || d.Number != 5 || d.FetchedAt.IsZero() {
		t.Fatalf("refresh in floor = %+v, %v", d, err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 1)

	f.clk.Advance(6 * time.Second) // 16 s: floor passed
	if _, err := f.read(true, true); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 2)

	f.clk.Advance(2 * time.Second) // task floor (5 s) after a refresh
	_, err = f.read(true, true)
	if !errors.As(err, &lim) || lim.Hourly {
		t.Fatalf("refresh in task floor err = %v", err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 2)
}

func TestPRDetails_PendingChecksOneCallPerTick(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksPending)
	if _, err := f.focusRead(); err != nil {
		t.Fatal(err)
	}
	for tick := 1; tick <= 6; tick++ {
		for step := 0; step < 4; step++ { // reads between ticks cost nothing
			f.clk.Advance(5 * time.Second)
			if step == 3 {
				f.clk.Advance(0)
			}
			for range 10 { // many clients
				if _, err := f.read(true, false); err != nil {
					t.Fatal(err)
				}
			}
			if step < 3 {
				wantCalls(t, f.pollerFix, "PRDetails", tick)
			}
		}
		wantCalls(t, f.pollerFix, "PRDetails", tick+1)
	}
}

func TestPRDetails_SettledChecksNoRefetch(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksSuccess)
	if _, err := f.focusRead(); err != nil {
		t.Fatal(err)
	}
	for range 20 { // 10 minutes of reads from many clients
		f.clk.Advance(30 * time.Second)
		for range 5 {
			if _, err := f.read(true, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	wantCalls(t, f.pollerFix, "PRDetails", 1)

	if _, err := f.read(true, true); err != nil { // explicit refresh
		t.Fatal(err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 2)

	// A fresh focus after the cache went older than 120 s refetches once.
	f.clk.Advance(3 * time.Minute)
	if _, err := f.focusRead(); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 3)
	f.clk.Advance(30 * time.Second)
	if _, err := f.focusRead(); err != nil { // fresh focus, cache still young
		t.Fatal(err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 3)
}

func TestPRDetails_BlurredOrClosedZeroFurtherCalls(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksPending)
	if _, err := f.focusRead(); err != nil {
		t.Fatal(err)
	}
	for range 30 { // tab blurred or connection gone: reads arrive unfocused
		f.clk.Advance(20 * time.Second)
		d, err := f.read(false, false)
		if err != nil || d.Stale != forge.StaleNotFocused || d.FetchedAt.IsZero() {
			t.Fatalf("blurred read = %+v, %v", d, err)
		}
		if _, err := f.read(false, true); err != nil { // even an explicit refresh
			t.Fatal(err)
		}
	}
	wantCalls(t, f.pollerFix, "PRDetails", 1)
}

func TestPRDetails_BudgetExhaustedServesCache(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksPending)
	if _, err := f.focusRead(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.AddForgeCalls("github.com", 239-f.booked(), f.clk.Now()); err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(30 * time.Second) // pending: a fetch is due
	d, err := f.read(true, false)
	if err != nil || d.Stale != forge.StaleBudget || d.RefreshAvailableAt == "" || d.FetchedAt.IsZero() || d.Number != 5 {
		t.Fatalf("read = %+v, %v", d, err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 1)
	if got := f.booked(); got != 239 {
		t.Fatalf("booked %d, want 239 (nothing booked when both calls cannot fit)", got)
	}
	// The one call that still fits is left to the list poll.
	if h := f.p.hosts["github.com"]; h != nil && h.budgetUntil.After(f.clk.Now()) {
		t.Fatal("a 239/240 ledger paused the whole host")
	}

	f.clk.Advance(time.Hour) // the window resets
	if _, err := f.read(true, false); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 2)
}

func TestPRDetails_UnauthenticatedSuspends(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksSuccess)
	other := f.withPR(f.pollerFix.task(f.workspaceByTask(f.tk), "b"), 6)
	f.fg.detailErr = forge.NewError(forge.CodeUnauthenticated, "")

	_, err := f.focusRead()
	var fe *forge.Error
	if !errors.As(err, &fe) || fe.Code != forge.CodeUnauthenticated {
		t.Fatalf("first read err = %v", err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 1)
	for range 5 {
		f.clk.Advance(time.Minute)
		_, err := f.read(true, false)
		if !errors.As(err, &fe) || fe.Code != forge.CodeUnauthenticated {
			t.Fatalf("suspended read err = %v", err)
		}
		// A different task on the same host is suspended too.
		_, err = f.p.PRDetails(other.ID, true, true)
		if !errors.As(err, &fe) || fe.Code != forge.CodeUnauthenticated {
			t.Fatalf("other task err = %v", err)
		}
	}
	wantCalls(t, f.pollerFix, "PRDetails", 1)

	// The suspension is the poller's own: the list poll makes no call either.
	f.fg.setOpen("/repo", mkPR(5, "a"))
	f.in.focused = []int64{f.tk.ID}
	f.step(time.Minute)
	wantCalls(t, f.pollerFix, "OpenPRs", 0)

	// With a cache the read serves it, flagged.
	g := newDetailsFix(t, forge.ChecksPending)
	if _, err := g.focusRead(); err != nil {
		t.Fatal(err)
	}
	g.fg.mu.Lock()
	g.fg.detailErr = forge.NewError(forge.CodeUnauthenticated, "")
	g.fg.mu.Unlock()
	g.clk.Advance(30 * time.Second)
	d, err := g.read(true, false)
	if err != nil || d.Stale != forge.StaleUnauthenticated || d.RefreshAvailableAt == "" || d.Number != 5 {
		t.Fatalf("failing refetch = %+v, %v", d, err)
	}
	g.clk.Advance(time.Minute)
	d, err = g.read(true, false)
	if err != nil || d.Stale != forge.StaleUnauthenticated {
		t.Fatalf("suspended read = %+v, %v", d, err)
	}
	wantCalls(t, g.pollerFix, "PRDetails", 2)
}

func (f *detailsFix) workspaceByTask(t store.Task) store.Workspace {
	w, err := f.db.GetWorkspace(t.WorkspaceID)
	if err != nil {
		f.t.Fatal(err)
	}
	return w
}

func TestPRDetails_RateLimitedBackoff(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksPending)
	if _, err := f.focusRead(); err != nil {
		t.Fatal(err)
	}
	limited := forge.NewError(forge.CodeRateLimited, "")
	limited.RetryAt = f.clk.Now().Add(30*time.Second + 30*time.Minute)
	f.fg.mu.Lock()
	f.fg.detailErr = limited
	f.fg.mu.Unlock()
	f.clk.Advance(30 * time.Second)
	d, err := f.read(true, false)
	if err != nil || d.Stale != forge.StaleRateLimited || d.RefreshAvailableAt != limited.RetryAt.UTC().Format(time.RFC3339) {
		t.Fatalf("read = %+v, %v", d, err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 2)
	for _, wait := range []time.Duration{time.Minute, 10 * time.Minute, 18 * time.Minute} {
		f.clk.Advance(wait)
		d, err := f.read(true, false)
		if err != nil || d.Stale != forge.StaleRateLimited {
			t.Fatalf("read during backoff = %+v, %v", d, err)
		}
		if _, err := f.read(true, true); err != nil {
			t.Fatal(err)
		}
	}
	wantCalls(t, f.pollerFix, "PRDetails", 2)
	// The host is suspended for the list poll as well.
	f.fg.setOpen("/repo", mkPR(5, "a"))
	f.in.focused = []int64{f.tk.ID}
	f.step(0)
	wantCalls(t, f.pollerFix, "OpenPRs", 0)

	f.fg.mu.Lock()
	f.fg.detailErr = nil
	f.fg.mu.Unlock()
	f.clk.Advance(time.Minute) // past retryAt
	d, err = f.read(true, false)
	if err != nil || d.Stale != "" {
		t.Fatalf("after backoff = %+v, %v", d, err)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 3)
}

func TestPRCheckLog_OnlyOnClickAndBounded(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksFailure)
	if _, err := f.focusRead(); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		f.clk.Advance(30 * time.Second)
		if _, err := f.read(true, false); err != nil {
			t.Fatal(err)
		}
	}
	wantCalls(t, f.pollerFix, "CheckLog", 0)

	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, fmt.Sprintf("line %d\x00", i))
	}
	f.fg.logOut = strings.Join(lines, "\n")
	tail, err := f.p.CheckLog(f.tk.ID, "run:11:job:22")
	if err != nil {
		t.Fatal(err)
	}
	wantCalls(t, f.pollerFix, "CheckLog", 1)
	got := strings.Split(tail.Text, "\n")
	if !tail.Truncated || tail.Lines != 200 || len(got) != 200 || got[0] != "line 300" || got[199] != "line 499" ||
		tail.CheckID != "run:11:job:22" || tail.FetchedAt.IsZero() {
		t.Fatalf("tail = %d lines (%d), first %q truncated %v", len(got), tail.Lines, got[0], tail.Truncated)
	}
	if args := f.fg.logArgs; len(args) != 1 || args[0] != [2]int64{11, 22} {
		t.Fatalf("log args = %v", args)
	}

	// The provider's own cut marker is stripped and reported.
	f.clk.Advance(6 * time.Second)
	f.fg.logOut = forge.LogCutMarker + "\nshort"
	tail, err = f.p.CheckLog(f.tk.ID, "run:11:job:22")
	if err != nil || tail.Text != "short" || !tail.Truncated || tail.Lines != 1 {
		t.Fatalf("marked tail = %+v, %v", tail, err)
	}
	wantCalls(t, f.pollerFix, "CheckLog", 2)

	// A second click inside 5 s costs nothing.
	var lim *RefreshLimitedError
	if _, err := f.p.CheckLog(f.tk.ID, "run:11:job:22"); !errors.As(err, &lim) {
		t.Fatalf("immediate second click err = %v", err)
	}
	wantCalls(t, f.pollerFix, "CheckLog", 2)

	// Ids the daemon never issued are rejected without a call.
	for _, id := range []string{"run:99", "run:11", "", "run:11:job:22;rm -rf /", "-x", "../../etc/passwd"} {
		_, err := f.p.CheckLog(f.tk.ID, id)
		var pe *forge.PRError
		if !errors.As(err, &pe) || pe.Code != forge.CodeCheckUnknown {
			t.Fatalf("id %q err = %v", id, err)
		}
	}
	wantCalls(t, f.pollerFix, "CheckLog", 2)

	// A short log is not truncated.
	f.clk.Advance(6 * time.Second)
	f.fg.logOut = "boom\n"
	tail, err = f.p.CheckLog(f.tk.ID, "run:11:job:22")
	if err != nil || tail.Truncated || tail.Text != "boom" {
		t.Fatalf("short tail = %+v, %v", tail, err)
	}
}

func TestPRDetails_NoPRAndNoWorktree(t *testing.T) {
	f := newPollerFix(t)
	ws := f.workspace("/repo")
	tk := f.task(ws, "a") // no PR number
	_, err := f.p.PRDetails(tk.ID, true, false)
	var pe *forge.PRError
	if !errors.As(err, &pe) || pe.Code != forge.CodeNoPR {
		t.Fatalf("err = %v", err)
	}
	if _, err := f.p.CheckLog(tk.ID, "run:1"); !errors.As(err, &pe) || pe.Code != forge.CodeNoPR {
		t.Fatalf("log err = %v", err)
	}
	wantCalls(t, f, "PRDetails", 0)
}

// refreshAvailableAt has second resolution; a client that waits until it must
// never be early (it would be answered with the same value and stall).
func TestPRDetails_RefreshAvailableAtRoundsUp(t *testing.T) {
	f := newDetailsFix(t, forge.ChecksSuccess)
	f.p.mu.Lock()
	f.p.wsLocked(f.tk.WorkspaceID).lastPoll = f.clk.Now().Add(-4300 * time.Millisecond)
	f.p.mu.Unlock()
	floorEnd := f.clk.Now().Add(-4300 * time.Millisecond).Add(repoFloor)
	d, err := f.focusRead()
	if err != nil || d.Stale != forge.StaleBackoff {
		t.Fatalf("read = %+v, %v", d, err)
	}
	at, perr := time.Parse(time.RFC3339, d.RefreshAvailableAt)
	if perr != nil || at.Before(floorEnd) {
		t.Fatalf("refreshAvailableAt %q (%v) is before the floor end %v", d.RefreshAvailableAt, perr, floorEnd)
	}
	wantCalls(t, f.pollerFix, "PRDetails", 0)
}
