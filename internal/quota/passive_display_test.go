package quota

import (
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func obsHdr(util5h, status string) http.Header {
	return hdr(uf+"5h-status", status, uf+"5h-utilization", util5h)
}

func Test_observed_replaces_not_merges_snapshot(t *testing.T) {
	t.Parallel()
	pe := newPassiveEnv(t)
	pe.observe("m", 200, hdr(
		uf+"5h-status", "allowed", uf+"5h-utilization", "0.2",
		uf+"7d-status", "allowed_warning", uf+"7d-utilization", "0.5",
		uf+"7d_oi-status", "allowed", uf+"7d_oi-utilization", "0.1"))
	obs, ok := pe.p.Observed(pe.id)
	if !ok || len(obs.Windows) != 3 {
		t.Fatalf("first observation = %+v/%v, want 3 windows", obs, ok)
	}

	pe.clk.Advance(61 * time.Second)
	pe.observe("m", 200, obsHdr("0.3", "allowed")) // the watermark windows are gone from this response
	obs, _ = pe.p.Observed(pe.id)
	if len(obs.Windows) != 1 || obs.Windows[0].ID != "five_hour" || *obs.Windows[0].UsedPct != 30 {
		t.Fatalf("second observation windows = %+v, want only five_hour at 30%% (replaced, not merged)", obs.Windows)
	}

	// A response with no quota signal leaves the observation untouched.
	pe.clk.Advance(time.Second)
	pe.observe("m", 200, hdr("X-Request-Id", "r1"))
	pe.observe("m", 500, nil)
	if again, _ := pe.p.Observed(pe.id); !again.Equal(obs) || !again.At.Equal(obs.At) {
		t.Errorf("observation changed on a response without a signal: %+v -> %+v", obs, again)
	}

	flush(t, pe.p)
	snap, ok, err := pe.st.GetLatestQuotaSnapshotKind(pe.id, store.QuotaKindObserved)
	if err != nil || !ok {
		t.Fatalf("observed snapshot = %v, %v", ok, err)
	}
	rep, decoded := DecodeReport(snap.UsageData)
	if !decoded || len(rep.Windows) != 1 || rep.Windows[0].ID != "five_hour" {
		t.Fatalf("stored observed report = %+v, want the replacing one", rep)
	}
	all, _ := pe.st.ListQuotaSnapshots()
	observed := 0
	for _, s := range all {
		if s.Kind == store.QuotaKindObserved {
			observed++
		}
	}
	if observed != 1 {
		t.Errorf("observed snapshot rows = %d, want 1 (latest per account and kind)", observed)
	}
	if _, fetched, _ := pe.st.GetLatestQuotaSnapshotKind(pe.id, store.QuotaKindFetched); fetched {
		t.Errorf("an observation wrote a fetched snapshot")
	}

	// After a restart the display path reads the stored observation.
	pe.p.Close()
	pe.restart()
	p2 := pe.newPassive()
	t.Cleanup(p2.Close)
	got, ok := p2.Observed(pe.id)
	if !ok || len(got.Windows) != 1 || *got.Windows[0].UsedPct != 30 || got.Windows[0].Source != "observed" {
		t.Errorf("Observed after restart = %+v/%v, want the stored five_hour 30%%", got, ok)
	}
}

func Test_observed_write_only_on_change_and_at_most_once_per_60s(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	p, clk := newFakePassive(t, fs)
	t0 := clk.Now()
	send := func(offset time.Duration, h http.Header) {
		p.Observe(respAt(1, "m", 200, h, t0.Add(offset)))
		flush(t, p)
	}
	writes := func(want int, when string) {
		t.Helper()
		if got := fs.count("ReplaceQuotaSnapshot"); got != want {
			t.Fatalf("%s: observation writes = %d, want %d", when, got, want)
		}
	}
	latest := func() float64 {
		t.Helper()
		rep, ok := DecodeReport(fs.snaps[1].UsageData)
		if !ok || len(rep.Windows) == 0 || rep.Windows[0].UsedPct == nil {
			t.Fatalf("stored snapshot = %+v", fs.snaps[1])
		}
		return *rep.Windows[0].UsedPct
	}

	a := obsHdr("0.1", "allowed")
	for i := 0; i < 200; i++ { // 200 identical responses
		send(time.Duration(i)*100*time.Millisecond, a)
	}
	writes(1, "200 identical responses")

	send(10*time.Second, obsHdr("0.2", "allowed")) // changed, but inside 60 s and the same verdict
	writes(1, "a change inside 60s")
	send(20*time.Second, a) // back to what is persisted
	writes(1, "a return to the persisted value")

	send(30*time.Second, obsHdr("0.9", "allowed_warning")) // verdict flip inside 60 s writes
	writes(2, "a verdict flip inside 60s")
	if got := latest(); got != 90 {
		t.Errorf("stored utilization = %v, want 90", got)
	}
	send(40*time.Second, obsHdr("0.95", "allowed_warning"))
	writes(2, "a change inside 60s of the flip")
	send(95*time.Second, obsHdr("0.95", "allowed_warning")) // 65 s after the last write
	writes(3, "the change once 60s passed")
	if got := latest(); got != 95 {
		t.Errorf("stored utilization = %v, want 95", got)
	}
	for i := 0; i < 200; i++ {
		send(100*time.Second+time.Duration(i)*time.Second, obsHdr("0.95", "allowed_warning"))
	}
	writes(3, "200 more identical responses")

	// Memory is always current even when the store is not written.
	pObs, _ := p.Observed(1)
	if *pObs.Windows[0].UsedPct != 95 {
		t.Errorf("in-memory observation = %v, want 95", *pObs.Windows[0].UsedPct)
	}
	if fs.count("SaveQuotaCooldown") != 0 {
		t.Errorf("an observation wrote a cooldown row")
	}
}

func Test_observed_write_does_not_block_proxy_response(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	fs.saveGate, fs.saveEnter = make(chan struct{}), make(chan struct{}, 4)
	var openOnce sync.Once
	open := func() { openOnce.Do(func() { close(fs.saveGate) }) }
	t.Cleanup(open)
	p, clk := newFakePassive(t, fs)
	now := clk.Now()

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Observe(respAt(1, "m", 429, reject5h(now.Add(time.Hour)), now)) // cooldown write + observation write
		a3aWait(t, fs.saveEnter, 1, "the writer to park in the store")
		for i := 0; i < 200; i++ { // the writer is blocked; none of these may wait for it
			p.Observe(respAt(1, "m", 200, obsHdr("0.5", "allowed"), now.Add(time.Duration(i)*time.Minute)))
			p.Observe(respAt(2, "m", 429, reject5h(now.Add(time.Hour)), now))
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Observe blocked while the store was slow")
	}
	if _, ok := p.Cooldown(1, "m", now); !ok {
		t.Errorf("cooldown not visible while the write is parked")
	}
	if _, ok := p.Observed(1); !ok {
		t.Errorf("observation not visible while the write is parked")
	}

	open()
	closed := make(chan struct{})
	go func() { p.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return after the store was unblocked")
	}
	if _, ok := fs.row(1, ""); !ok {
		t.Errorf("cooldown row of account 1 not drained by Close")
	}
	if _, ok := fs.row(2, ""); !ok {
		t.Errorf("cooldown row of account 2 not drained by Close")
	}
	if _, ok := fs.snaps[1]; !ok {
		t.Errorf("observation of account 1 not drained by Close")
	}
}

func Test_observed_display_rule(t *testing.T) {
	t.Parallel()
	base := newFakeClock().Now()
	fetchedAt := base
	win := func(id string, used float64) Window {
		return Window{ID: id, Label: id, UsedPct: ptr(used), RemainingPct: ptr(100 - used), Tone: ToneOK}
	}
	obsWin := func(id string, used float64) Window {
		w := win(id, used)
		w.Source, w.Status = "observed", "allowed"
		return w
	}
	fetchedRep := func(status Status) *Report {
		r := &Report{Version: ReportVersion, Status: status, PlanLabel: "pro", FetchedAt: fetchedAt,
			Windows: []Window{win("five_hour", 10), win("weekly", 20)}}
		if status != StatusAvailable {
			r.Problem = &Problem{Kind: ProblemExpired, Detail: detailExpired}
		}
		return r
	}
	obs := func(at time.Time) *Observation {
		return &Observation{At: at, Windows: []Window{obsWin("five_hour", 90), obsWin("weekly_oi", 40)}}
	}
	const ttl = 5 * time.Minute

	tests := []struct {
		name    string
		fetched Result
		obs     *Observation
		now     time.Time
		want    func() (Report, bool)
	}{
		{
			name:    "fresh fetched report is never overridden",
			fetched: Result{Report: fetchedRep(StatusAvailable)},
			obs:     obs(base.Add(time.Minute)),
			now:     base.Add(2 * time.Minute),
			want:    func() (Report, bool) { return *fetchedRep(StatusAvailable), true },
		},
		{
			name:    "stale fetched, newer observation: replace same id, add the rest",
			fetched: Result{Report: fetchedRep(StatusAvailable), Stale: true},
			obs:     obs(base.Add(time.Minute)),
			now:     base.Add(10 * time.Minute),
			want: func() (Report, bool) {
				r := *fetchedRep(StatusAvailable)
				r.Windows = []Window{obsWin("five_hour", 90), win("weekly", 20), obsWin("weekly_oi", 40)}
				return r, true
			},
		},
		{
			name:    "not stale but older than the ttl: observed windows may show",
			fetched: Result{Report: fetchedRep(StatusAvailable)},
			obs:     obs(base.Add(time.Minute)),
			now:     base.Add(ttl + time.Second),
			want: func() (Report, bool) {
				r := *fetchedRep(StatusAvailable)
				r.Windows = []Window{obsWin("five_hour", 90), win("weekly", 20), obsWin("weekly_oi", 40)}
				return r, true
			},
		},
		{
			name:    "stale fetched, older observation: the fetched report as it is",
			fetched: Result{Report: fetchedRep(StatusAvailable), Stale: true},
			obs:     obs(base.Add(-time.Minute)),
			now:     base.Add(10 * time.Minute),
			want:    func() (Report, bool) { return *fetchedRep(StatusAvailable), true },
		},
		{
			name:    "stale fetched, observation at the same instant is not newer",
			fetched: Result{Report: fetchedRep(StatusAvailable), Stale: true},
			obs:     obs(base),
			now:     base.Add(10 * time.Minute),
			want:    func() (Report, bool) { return *fetchedRep(StatusAvailable), true },
		},
		{
			name:    "unavailable fetched report, newer observation: windows added, problem kept",
			fetched: Result{Report: fetchedRep(StatusUnavailable), Stale: true},
			obs:     obs(base.Add(time.Minute)),
			now:     base.Add(10 * time.Minute),
			want: func() (Report, bool) {
				r := *fetchedRep(StatusUnavailable)
				r.Windows = []Window{obsWin("five_hour", 90), win("weekly", 20), obsWin("weekly_oi", 40)}
				return r, true
			},
		},
		{
			name:    "no fetched report: the observation's own report",
			fetched: Result{},
			obs:     obs(base.Add(time.Minute)),
			now:     base.Add(2 * time.Minute),
			want:    func() (Report, bool) { return obs(base.Add(time.Minute)).Report(), true },
		},
		{
			name:    "stale fetched report and no observation",
			fetched: Result{Report: fetchedRep(StatusAvailable), Stale: true},
			now:     base.Add(10 * time.Minute),
			want:    func() (Report, bool) { return *fetchedRep(StatusAvailable), true },
		},
		{
			name: "neither",
			now:  base,
			want: func() (Report, bool) { return Report{}, false },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var fetchedBefore Report
			if tt.fetched.Report != nil {
				fetchedBefore = cloneReport(*tt.fetched.Report)
			}
			var obsBefore Observation
			if tt.obs != nil {
				obsBefore = cloneObservation(*tt.obs)
			}

			got, ok := DisplayReport(tt.fetched, tt.obs, ttl, tt.now)
			want, wantOK := tt.want()
			if ok != wantOK {
				t.Fatalf("ok = %v, want %v", ok, wantOK)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("report =\n%+v\nwant\n%+v", got, want)
			}

			// Inputs are never mutated, and the result shares nothing with them.
			for i := range got.Windows {
				got.Windows[i].Label = "mutated"
				if got.Windows[i].UsedPct != nil {
					*got.Windows[i].UsedPct = -1
				}
			}
			if tt.fetched.Report != nil && !reflect.DeepEqual(*tt.fetched.Report, fetchedBefore) {
				t.Errorf("fetched report was mutated")
			}
			if tt.obs != nil && !reflect.DeepEqual(*tt.obs, obsBefore) {
				t.Errorf("observation was mutated")
			}
		})
	}
}

func Test_observed_no_raw_headers_in_stored_json_logs_wire(t *testing.T) {
	// Not parallel: it captures the process-wide logger.
	const s = "SENTINEL-9f3a7c"
	logs, restore := captureLog()
	defer restore()

	h := hdr(
		uf+"5h-status", "allowed", uf+"5h-utilization", "0.25", uf+"5h-reset", s,
		uf+"representative-claim", "five_hour", uf+"overage-status", s+"-OVER", uf+"status", s+"-STATUS",
		uf+"unknown-"+s, s, "Authorization", "Bearer "+s, "Anthropic-Workspace-Id", s,
		"Retry-After", s+"-RA", "X-Api-Key", s)
	model := s + "-MODEL"

	// Phase 1, a working store: nothing sentinel reaches the stored rows.
	fs := newFakeStore()
	p, clk := newFakePassive(t, fs)
	now := clk.Now()
	p.Observe(respAt(1, model, 200, h, now))
	p.Observe(respAt(1, model, 429, h, now))                                               // bare 429: unparseable Retry-After
	p.Observe(respAt(1, "m", 429, hdr(uf+"5h-status", "rejected", uf+"5h-reset", s), now)) // fallback rejection
	flush(t, p)
	obs, ok := p.Observed(1)
	if !ok {
		t.Fatal("no observation")
	}
	wire, err := EncodeReport(obs.Report())
	if err != nil {
		t.Fatalf("EncodeReport: %v", err)
	}
	stored := fs.snaps[1].UsageData
	if stored == "" {
		t.Fatal("no stored snapshot")
	}
	for name, text := range map[string]string{"stored JSON": stored, "wire/EncodeReport": wire} {
		if strings.Contains(text, s) {
			t.Errorf("%s contains a raw header value: %s", name, text)
		}
	}
	for _, r := range fs.rows {
		if strings.Contains(r.Reason, s) {
			t.Errorf("cooldown row carries header text: %+v", r)
		}
	}

	// Phase 2, a failing store whose errors carry the sentinel: every log line
	// is a fixed string with an account id.
	bad := newFakeStore()
	bad.errText = "store failed " + s
	bad.setFail(true, true)
	p2, clk2 := newFakePassive(t, bad)
	now2 := clk2.Now()
	p2.Observe(respAt(2, model, 429, rejectOIOnly(600), now2))
	p2.Observe(respAt(2, model, 200, h, now2))
	p2.Cooldown(3, model, now2)
	p2.Observed(4)
	flush(t, p2)
	p2.ClearCooldown(2)
	p2.Close()

	out := logs.String()
	if out == "" {
		t.Fatal("no log output captured from the failing store")
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "quota: account ") || strings.Contains(line, s) || strings.Contains(line, "Bearer") ||
			strings.Contains(line, "store failed") {
			t.Errorf("log line is not a fixed string with an account id: %q", line)
		}
	}
}

func Test_cooldown_reader_has_no_observation_accessor(t *testing.T) {
	t.Parallel()
	reader := reflect.TypeOf((*Reader)(nil)).Elem()
	for i := 0; i < reader.NumMethod(); i++ {
		if name := reader.Method(i).Name; strings.Contains(name, "Observ") {
			t.Errorf("Reader exposes %s: routing must not be able to read observations", name)
		}
	}
	if _, has := reader.MethodByName("Observed"); has {
		t.Errorf("Reader has Observed")
	}
	if _, has := reflect.TypeOf(&Poller{}).MethodByName("Observed"); has {
		t.Errorf("*Poller has Observed")
	}
	if _, has := reflect.TypeOf(&Passive{}).MethodByName("Observed"); !has {
		t.Errorf("*Passive lost its Observed display accessor")
	}

	// The proxy's side is write-only.
	observer := reflect.TypeOf((*Observer)(nil)).Elem()
	if observer.NumMethod() != 1 || observer.Method(0).Name != "Observe" {
		t.Errorf("Observer methods = %d, want exactly Observe", observer.NumMethod())
	}
	var _ Observer = (*Passive)(nil)
	var _ CooldownSource = (*Passive)(nil)
}

func Test_observed_status_only_window_keeps_fetched_numbers(t *testing.T) {
	t.Parallel()
	base := newFakeClock().Now()
	reset := base.Add(2 * time.Hour)
	fetched := Result{Stale: true, Report: &Report{Version: ReportVersion, Status: StatusAvailable, FetchedAt: base, Windows: []Window{
		{ID: "five_hour", Label: "5-hour", UsedPct: ptr(42.0), RemainingPct: ptr(58.0), ResetsAt: &reset, Tone: ToneOK, Source: "fetched"},
		{ID: "weekly", Label: "Weekly", UsedPct: ptr(10.0), RemainingPct: ptr(90.0), ResetsAt: &reset, Tone: ToneOK, Source: "fetched"},
	}}}
	obs := &Observation{At: base.Add(time.Minute), Windows: []Window{
		// status only: nothing but the status is new
		observedWindow(Window{ID: "five_hour", Label: "5-hour"}, "allowed_warning"),
		// numbers present: they win, the reset it lacks stays
		observedWindow(newWindow("weekly", "Weekly", "7d", false, ptr(55.0), nil), "allowed"),
	}}

	rep, ok := DisplayReport(fetched, obs, 5*time.Minute, base.Add(10*time.Minute))
	if !ok || len(rep.Windows) != 2 {
		t.Fatalf("DisplayReport = %+v/%v, want 2 windows", rep, ok)
	}
	five, weekly := rep.Windows[0], rep.Windows[1]
	if five.UsedPct == nil || *five.UsedPct != 42 || five.RemainingPct == nil || *five.RemainingPct != 58 ||
		five.ResetsAt == nil || !five.ResetsAt.Equal(reset) {
		t.Errorf("status-only window lost the fetched numbers: %+v", five)
	}
	if five.Source != "observed" || five.Status != "allowed_warning" || five.Tone != ToneWarning {
		t.Errorf("status-only window source/status/tone = %q/%q/%q, want observed/allowed_warning/warning", five.Source, five.Status, five.Tone)
	}
	if weekly.UsedPct == nil || *weekly.UsedPct != 55 || weekly.ResetsAt == nil || !weekly.ResetsAt.Equal(reset) ||
		weekly.Source != "observed" || weekly.Status != "allowed" {
		t.Errorf("observed numbers must win and a missing reset stay: %+v", weekly)
	}
	// The inputs are not mutated.
	if got := fetched.Report.Windows[0]; got.Source != "fetched" || got.Status != "" {
		t.Errorf("fetched report mutated: %+v", got)
	}
	if obs.Windows[0].UsedPct != nil || obs.Windows[0].ResetsAt != nil {
		t.Errorf("observation mutated: %+v", obs.Windows[0])
	}
}
