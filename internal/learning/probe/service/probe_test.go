package probesvc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/probe/arm"
	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

type recNotify struct {
	mu sync.Mutex
	ev []string
}

func (n *recNotify) add(s string)               { n.mu.Lock(); n.ev = append(n.ev, s); n.mu.Unlock() }
func (n *recNotify) ProbeStarted(_, _, _ int64) { n.add("started") }
func (n *recNotify) ProbeArm(_, _, _ int64, role, status string) {
	n.add("arm:" + role + ":" + status)
}
func (n *recNotify) ProbeFinished(_, _, _ int64, status, verdict string) {
	n.add("finished:" + status + ":" + verdict)
}
func (n *recNotify) ProbeFailed(_, _, _ int64, reason string) { n.add("failed") }
func (n *recNotify) events() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.ev...)
}

// fakeArm describes what the stand-in arm runner does for one job.
type fakeArm struct {
	cost    *float64 // nil = usage never reported
	end     arm.End  // "" = done
	writes  map[string]string
	final   string
	block   bool // wait for cancellation
	trip    bool // write into the real checkout
	noInit  bool
	noRun   bool
	nothing bool
}

func cost(v float64) *float64 { return &v }

type fakeArms struct {
	t       *testing.T
	f       *fx
	mu      sync.Mutex
	jobs    []armJob
	sys     store.Task
	behave  func(job armJob, n int) fakeArm
	started chan struct{}
	now     func() time.Time // the service clock; nil = time.Now
}

func newFakeArms(t *testing.T, f *fx) *fakeArms {
	sys, _, err := f.st.CreateSystemTask(f.ws.ID, "learning", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &fakeArms{t: t, f: f, sys: sys, started: make(chan struct{}, 16),
		behave: func(armJob, int) fakeArm { return fakeArm{cost: cost(0.05)} }}
}

func (fa *fakeArms) count() int { fa.mu.Lock(); defer fa.mu.Unlock(); return len(fa.jobs) }

func (fa *fakeArms) roles() []string {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	var out []string
	for _, j := range fa.jobs {
		out = append(out, j.Role)
	}
	return out
}

func (fa *fakeArms) run(ctx context.Context, job armJob) (armOutcome, error) {
	fa.mu.Lock()
	n := len(fa.jobs)
	fa.jobs = append(fa.jobs, job)
	fa.mu.Unlock()
	// Like the real runner, refuse a stale or zero attestation.
	now := time.Now
	if fa.now != nil {
		now = fa.now
	}
	if err := job.Attestation.Require(now()); err != nil {
		fa.t.Errorf("arm %s got an unusable attestation: %v", job.Role, err)
		return armOutcome{}, err
	}
	// The arm is a real materialised checkout and the launch dir exists.
	if _, err := os.Stat(filepath.Join(job.ArmDir, "README.md")); err != nil {
		fa.t.Errorf("arm dir not materialised: %v", err)
	}
	if fi, err := os.Stat(job.PrivateDir); err != nil || fi.Mode().Perm() != 0o700 {
		fa.t.Errorf("launch dir: %v %v", fi, err)
	}
	b := fa.behave(job, n)
	runID := fmt.Sprintf("probe-run-%d-%s", job.ProbeID, job.Role)
	if b.noRun {
		return armOutcome{}, fmt.Errorf("no run")
	}
	r := store.Run{ID: runID, TaskID: fa.sys.ID, Provider: "claude-native", Prompt: job.Prompt, Status: "done",
		StartedAt: time.Now().UTC(), Purpose: store.RunPurposeProbe}
	if _, err := fa.f.st.CreateRun(r); err != nil {
		fa.t.Error(err)
	}
	fa.started <- struct{}{}
	job.setRun(runID)
	if b.block {
		<-ctx.Done()
		return armOutcome{RunID: runID, Report: taskrunner.ProbeReport{End: arm.EndKilled, InitTools: sandbox.AllowedTools()}}, ctx.Err()
	}
	for name, body := range b.writes {
		p := filepath.Join(job.ArmDir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			fa.t.Error(err)
		}
	}
	if b.trip {
		if err := os.WriteFile(filepath.Join(fa.f.repo, "stolen.txt"), []byte("x"), 0o644); err != nil {
			fa.t.Error(err)
		}
	}
	if b.final != "" {
		ev, _ := json.Marshal(map[string]any{"type": int(taskrunner.EventTypeText), "text": b.final})
		if _, err := fa.f.st.AppendRunEvent(runID, 0, string(ev)); err != nil {
			fa.t.Error(err)
		}
	}
	if b.cost != nil {
		if err := fa.f.st.UpsertRunUsage(store.RunUsage{RunID: runID, TaskID: fa.sys.ID, Provider: "claude-native", CostUSD: b.cost, Source: "test"}); err != nil {
			fa.t.Error(err)
		}
	}
	end := b.end
	if end == "" {
		end = arm.EndDone
	}
	rep := taskrunner.ProbeReport{End: end, Turns: 3, InitTools: sandbox.AllowedTools()}
	if b.noInit {
		rep.InitTools = []string{"Bash", "WebFetch"}
	}
	return armOutcome{RunID: runID, Report: rep}, nil
}

type pfx struct {
	*fx
	svc    *Service
	arms   *fakeArms
	notify *recNotify
	canary int
	att    func() (sandbox.Attestation, float64, error)
}

func newPfx(t *testing.T, mut func(*Deps)) *pfx {
	t.Helper()
	f := newFx(t)
	p := &pfx{fx: f, notify: &recNotify{}}
	p.svc = f.svc(func(d *Deps) {
		d.Notify = p.notify
		d.Redact = func(s string) string { return strings.ReplaceAll(s, "SECRET-TOKEN-123", "[redacted]") }
		if mut != nil {
			mut(d)
		}
	})
	p.arms = newFakeArms(t, f)
	p.svc.armRun = p.arms.run
	p.arms.now = func() time.Time { return p.svc.d.Now() }
	p.att = func() (sandbox.Attestation, float64, error) {
		return sandbox.AttestationForTest(t, p.svc.d.Now()), 0.002, nil
	}
	p.svc.canaryRun = func(context.Context, int64) (sandbox.Attestation, float64, error) {
		p.canary++
		return p.att()
	}
	return p
}

func (p *pfx) start(quick bool) store.LearningProbe {
	p.t.Helper()
	pr, err := p.svc.Start(context.Background(), p.prop.ID, quick)
	if err != nil {
		p.t.Fatalf("Start: %v", err)
	}
	return pr
}

func (p *pfx) wait(id int64) store.LearningProbe {
	p.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		pr, err := p.st.GetProbe(id)
		if err != nil {
			p.t.Fatal(err)
		}
		if pr.Status != "queued" && pr.Status != "running" && pr.FinishedAt != nil {
			p.svc.wg.Wait() // the goroutine's cleanup has run
			return pr
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.t.Fatal("probe did not finish")
	return store.LearningProbe{}
}

func (p *pfx) noLeftovers() {
	p.t.Helper()
	for _, root := range []string{p.svc.armsRoot(), p.svc.launchRoot()} {
		ents, _ := os.ReadDir(root)
		if len(ents) > 0 {
			p.t.Errorf("leftover entries in %s: %v", root, ents)
		}
	}
	if ents, _ := os.ReadDir(filepath.Join(p.home, "learning", "probe-projections")); len(ents) > 0 {
		p.t.Errorf("leftover projections: %v", ents)
	}
}

func TestProbe_RunsExactlyPlannedArms(t *testing.T) {
	t.Parallel()
	for _, quick := range []bool{false, true} {
		t.Run(fmt.Sprintf("quick=%v", quick), func(t *testing.T) {
			t.Parallel()
			p := newPfx(t, nil)
			pr := p.start(quick)
			if pr.Status != "queued" || pr.Quick != quick {
				t.Fatalf("returned row = %+v", pr)
			}
			got := p.wait(pr.ID)
			want := []string{"A", "A2", "B"}
			if quick {
				want = []string{"A", "B"}
			}
			if r := p.arms.roles(); strings.Join(r, ",") != strings.Join(want, ",") {
				t.Fatalf("arms started = %v, want %v", r, want)
			}
			rows, _ := p.st.ListProbeArms(pr.ID)
			if len(rows) != len(want) {
				t.Fatalf("arm rows = %d, want %d", len(rows), len(want))
			}
			for i, a := range rows {
				if a.Role != want[i] || a.Status != "done" || a.RunID == "" || a.Seq != i {
					t.Fatalf("arm %d = %+v", i, a)
				}
			}
			if p.canary != 1 {
				t.Fatalf("canaries = %d, want 1", p.canary)
			}
			if got.Status != "done" || got.Verdict == "" {
				t.Fatalf("probe = %+v", got)
			}
			p.noLeftovers()
		})
	}
}

func TestProbe_NoDuplicateRunIDs(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	pr := p.start(false)
	p.wait(pr.ID)
	rows, _ := p.st.ListProbeArms(pr.ID)
	seen := map[string]bool{}
	for _, a := range rows {
		if a.RunID == "" || seen[a.RunID] || a.RunID == pr.SourceRunID {
			t.Fatalf("duplicate or missing run id in %+v", rows)
		}
		seen[a.RunID] = true
	}
	// Roles carry the planned projections: A == A2 != B, recorded on the rows.
	if rows[0].ProjectionHash != rows[1].ProjectionHash || rows[0].ProjectionHash == rows[2].ProjectionHash {
		t.Fatalf("hashes A=%q A2=%q B=%q", rows[0].ProjectionHash, rows[1].ProjectionHash, rows[2].ProjectionHash)
	}
	if rows[2].ProjectionHash == "" {
		t.Fatal("arm B has no projection hash")
	}
}

func TestBudget_CapStopsRunningArmAndSkipsRest(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	p.arms.behave = func(j armJob, n int) fakeArm {
		if n == 0 && j.CostCapUSD > 0.241 { // est arm 0.12 -> cap 0.24
			t.Errorf("first arm cap = %v, want 0.24", j.CostCapUSD)
		}
		return fakeArm{cost: cost(0.75), end: arm.EndCapped}
	}
	pr := p.start(false)
	got := p.wait(pr.ID)
	if n := p.arms.count(); n != 2 {
		t.Fatalf("arms started = %d (%v), want exactly 2: the second arm reaches the 1.50 cap", n, p.arms.roles())
	}
	if got.Verdict != "inconclusive" || got.Status != "done" || !strings.Contains(got.VerdictReason, "budget") {
		t.Fatalf("probe = %+v", got)
	}
	rows, _ := p.st.ListProbeArms(pr.ID)
	if rows[2].Status != "pending" || rows[2].RunID != "" {
		t.Fatalf("skipped arm B = %+v", rows[2])
	}
	p.noLeftovers()
}

func TestBudget_UnreportedUsageChargedAtCap(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	p.arms.behave = func(armJob, int) fakeArm { return fakeArm{cost: nil} } // never reports usage
	pr := p.start(false)
	got := p.wait(pr.ID)
	rows, _ := p.st.ListProbeArms(pr.ID)
	// cap = min(2 x 0.12, ...) = 0.24 for A and A2; B gets what is left.
	if rows[0].CostUSD < 0.2399 || rows[0].CostUSD > 0.2401 || rows[1].CostUSD < 0.2399 {
		t.Fatalf("unreported arm costs = %v, %v; want the cap 0.24", rows[0].CostUSD, rows[1].CostUSD)
	}
	v, _, _ := p.svc.Get(p.prop.ID)
	if v.CostUSD < 0.72 || got.Status == "" {
		t.Fatalf("view cost = %v", v.CostUSD)
	}
}

func TestProbe_SecondStartIsBusy(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	p.arms.behave = func(armJob, int) fakeArm { return fakeArm{block: true} }
	pr := p.start(false)
	<-p.arms.started
	_, err := p.svc.Start(context.Background(), p.prop.ID, false)
	var ie *IneligibleError
	if !asIneligible(err, &ie) || ie.Code() != ReasonBusy {
		t.Fatalf("second Start err = %v, want busy", err)
	}
	// Concurrent Starts: exactly one wins.
	p2 := newPfx(t, nil)
	p2.arms.behave = func(armJob, int) fakeArm { return fakeArm{block: true} }
	var wins, busies int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p2.svc.Start(context.Background(), p2.prop.ID, false)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else {
				busies++
			}
		}()
	}
	wg.Wait()
	if wins != 1 || busies != 7 {
		t.Fatalf("wins=%d busies=%d, want 1/7", wins, busies)
	}
	_ = p.svc.Cancel(p.prop.ID)
	_ = p2.svc.Cancel(p2.prop.ID)
	p.wait(pr.ID)
}

func asIneligible(err error, out **IneligibleError) bool {
	ie, ok := err.(*IneligibleError)
	*out = ie
	return ok
}

func TestProbe_CancelStopsAndCleans(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	p.arms.behave = func(armJob, int) fakeArm { return fakeArm{block: true} }
	pr := p.start(false)
	<-p.arms.started
	if err := p.svc.Cancel(p.prop.ID); err != nil {
		t.Fatal(err)
	}
	got := p.wait(pr.ID)
	if got.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
	if n := p.arms.count(); n != 1 {
		t.Fatalf("arms started = %d, want 1 (no arm after cancel)", n)
	}
	p.noLeftovers()
	// Cancelling again, or an unknown proposal, is a no-op.
	if err := p.svc.Cancel(p.prop.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ := p.st.ListProbeArms(pr.ID)
	if rows[0].Status != "killed" {
		t.Fatalf("running arm = %q, want killed", rows[0].Status)
	}
	evs := p.notify.events()
	if evs[len(evs)-1] != "finished:cancelled:" {
		t.Fatalf("events = %v", evs)
	}
}

func TestTripwire_StopsRemainingArms(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	p.arms.behave = func(armJob, int) fakeArm { return fakeArm{cost: cost(0.05), trip: true} }
	pr := p.start(false)
	got := p.wait(pr.ID)
	if n := p.arms.count(); n != 1 {
		t.Fatalf("arms started = %d, want 1: the tripwire stops the rest", n)
	}
	if got.Status != "unsafe" || got.Verdict != "unsafe" {
		t.Fatalf("probe = %+v", got)
	}
	p.noLeftovers()
}

func TestTripwire_WrongToolSurfaceIsUnsafe(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	p.arms.behave = func(armJob, int) fakeArm { return fakeArm{cost: cost(0.05), noInit: true} }
	pr := p.start(false)
	if got := p.wait(pr.ID); got.Status != "unsafe" || p.arms.count() != 1 {
		t.Fatalf("probe = %+v arms %d", got, p.arms.count())
	}
}

func TestStoredArtifacts_Redacted(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	p.arms.behave = func(armJob, int) fakeArm {
		return fakeArm{cost: cost(0.05), writes: map[string]string{"notes.txt": "key=SECRET-TOKEN-123\n"}, final: "done, token SECRET-TOKEN-123"}
	}
	pr := p.start(false)
	p.wait(pr.ID)
	rows, _ := p.st.ListProbeArms(pr.ID)
	var stored []string
	for _, a := range rows {
		stored = append(stored, a.Diff, a.FinalMessage, a.Metrics, a.CheckResult)
		if !strings.Contains(a.Diff, "[redacted]") || !strings.Contains(a.FinalMessage, "[redacted]") {
			t.Fatalf("arm %s not redacted: %+v", a.Role, a)
		}
	}
	pb, _ := p.st.GetProbe(pr.ID)
	stored = append(stored, pb.Contrast, pb.Error, pb.VerdictReason)
	ledger, _ := p.st.ListLedger(p.ws.ID, 0, 0)
	for _, l := range ledger {
		stored = append(stored, l.Reason, l.Evidence)
	}
	for _, s := range stored {
		if strings.Contains(s, "SECRET-TOKEN-123") {
			t.Fatalf("secret stored: %q", s)
		}
	}
	v, _, _ := p.svc.Get(p.prop.ID)
	if strings.Contains(mustJSON(v), "SECRET-TOKEN-123") {
		t.Fatal("secret in the view")
	}
}

func TestVerdict_NeverChangesProposalStatus(t *testing.T) {
	t.Parallel()
	for name, behave := range map[string]func(armJob, int) fakeArm{
		"normal": func(armJob, int) fakeArm { return fakeArm{cost: cost(0.05)} },
		"unsafe": func(armJob, int) fakeArm { return fakeArm{cost: cost(0.05), trip: true} },
		"failed": func(armJob, int) fakeArm { return fakeArm{end: arm.EndError} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := newPfx(t, nil)
			p.arms.behave = behave
			pr := p.start(false)
			p.wait(pr.ID)
			got, err := p.st.GetProposal(p.prop.ID)
			if err != nil || got.Status != "pending" || got.DecidedAt != nil {
				t.Fatalf("proposal = %+v %v", got, err)
			}
		})
	}
}

func TestProbe_FailedArmIsProbeFailed(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	p.arms.behave = func(armJob, int) fakeArm { return fakeArm{noRun: true} }
	pr := p.start(false)
	got := p.wait(pr.ID)
	if got.Status != "failed" || got.Verdict != "probe_failed" || p.arms.count() != 1 {
		t.Fatalf("probe = %+v arms %d", got, p.arms.count())
	}
	p.noLeftovers()
}

func TestLedger_ProbedRow(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	pr := p.start(false)
	got := p.wait(pr.ID)
	ledger, err := p.st.ListLedger(p.ws.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var probed []store.LearnedLedgerEntry
	for _, l := range ledger {
		if l.Action == "probed" {
			probed = append(probed, l)
		}
	}
	if len(probed) != 1 {
		t.Fatalf("probed rows = %d, want exactly 1", len(probed))
	}
	l := probed[0]
	if l.Reason != got.Verdict || l.Actor != "system" || l.SkillName != p.prop.Name || l.RunID != fmt.Sprintf("probe-run-%d-B", pr.ID) {
		t.Fatalf("ledger row = %+v", l)
	}
}

func TestEvents_ProbeLifecycle(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	pr := p.start(true)
	got := p.wait(pr.ID)
	want := []string{"started", "arm:A:running", "arm:A:done", "arm:B:running", "arm:B:done", "finished:done:" + got.Verdict}
	if ev := p.notify.events(); strings.Join(ev, "|") != strings.Join(want, "|") {
		t.Fatalf("events = %v\nwant     %v", ev, want)
	}
	// A failing canary: started, then failed; no arm starts, canary cost kept.
	q := newPfx(t, nil)
	q.att = func() (sandbox.Attestation, float64, error) {
		return sandbox.Attestation{}, 0.003, fmt.Errorf("%w: outside write landed", sandbox.ErrCanaryFailed)
	}
	pq := q.start(false)
	gq := q.wait(pq.ID)
	if ev := q.notify.events(); strings.Join(ev, "|") != "started|failed" {
		t.Fatalf("events = %v", ev)
	}
	if gq.Status != "failed" || gq.Verdict != "probe_failed" || !strings.Contains(gq.VerdictReason, "sandbox could not be attested") ||
		q.arms.count() != 0 || gq.CanaryCostUSD != 0.003 {
		t.Fatalf("probe = %+v arms %d", gq, q.arms.count())
	}
	q.noLeftovers()
}

func TestProbe_StaleAttestationRecanariesBeforeArm(t *testing.T) {
	t.Parallel()
	now := time.Now()
	clock := &now
	var mu sync.Mutex
	p := newPfx(t, func(d *Deps) { d.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return *clock } })
	p.arms.behave = func(j armJob, n int) fakeArm {
		mu.Lock()
		*clock = clock.Add(sandbox.AttestationTTL + time.Minute) // each arm outlives the attestation
		mu.Unlock()
		return fakeArm{cost: cost(0.05)}
	}
	pr := p.start(false)
	got := p.wait(pr.ID)
	if p.canary != 3 {
		t.Fatalf("canaries = %d, want 3 (initial + before A2 and B)", p.canary)
	}
	if got.CanaryCostUSD < 0.0059 {
		t.Fatalf("canary cost = %v, want all three charged", got.CanaryCostUSD)
	}
	// A stale attestation that cannot be renewed starts no further arm.
	q := newPfx(t, func(d *Deps) { d.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return *clock } })
	calls := 0
	q.att = func() (sandbox.Attestation, float64, error) {
		calls++
		if calls > 1 {
			return sandbox.Attestation{}, 0, fmt.Errorf("%w: no", sandbox.ErrCanaryFailed)
		}
		return sandbox.AttestationForTest(t, q.svc.d.Now()), 0.002, nil
	}
	q.arms.behave = p.arms.behave
	pq := q.start(false)
	gq := q.wait(pq.ID)
	if q.arms.count() != 1 || gq.Status != "failed" {
		t.Fatalf("arms %d probe %+v", q.arms.count(), gq)
	}
}

func TestRetention_PurgesFourteenDaysAfterDecision(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	pr := p.start(false)
	p.wait(pr.ID)
	if err := p.st.DecideProposal(p.prop.ID, "rejected", "", "human"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	p.svc.purgeNow(now.AddDate(0, 0, 13))
	if _, err := p.st.GetProbe(pr.ID); err != nil {
		t.Fatalf("probe purged 13 days after the decision: %v", err)
	}
	p.svc.purgeNow(now.AddDate(0, 0, 15))
	if _, err := p.st.GetProbe(pr.ID); err == nil {
		t.Fatal("probe kept 15 days after the decision")
	}
	if _, _, runsLeft := p.countRows(); runsLeft != 1 { // only the source run
		t.Fatalf("runs left = %d, want the source run only", runsLeft)
	}
}

func TestRetention_PendingKeepsProbe(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	pr := p.start(false)
	p.wait(pr.ID)
	p.svc.purgeNow(time.Now().AddDate(0, 0, 400))
	if _, err := p.st.GetProbe(pr.ID); err != nil {
		t.Fatalf("a pending proposal's probe was purged: %v", err)
	}
}

func TestRetention_JanitorRunsPurge(t *testing.T) {
	t.Parallel()
	p := newPfx(t, func(d *Deps) {
		d.Config = func() config.LearningConfig { c := config.DefaultLearning(); c.Probe.RetentionDays = 1; return c }
		d.Now = func() time.Time { return time.Now().AddDate(0, 0, 5) }
	})
	p.svc.janitorEvery = 10 * time.Millisecond
	pr := p.start(false)
	p.wait(pr.ID)
	if err := p.st.DecideProposal(p.prop.ID, "rejected", "", "human"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.svc.Janitor(ctx); close(done) }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := p.st.GetProbe(pr.ID); err != nil {
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("Janitor never purged the decided probe")
}

func TestRecover_FailsInterruptedAndSweeps(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	s := f.svc(nil)
	pr, err := f.st.CreateProbe(store.LearningProbe{WorkspaceID: f.ws.ID, ProposalID: f.prop.ID, Status: "running", Contrast: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.UpsertProbeArm(store.LearningProbeArm{ProbeID: pr.ID, Role: "A", Status: "running", Metrics: "{}"}); err != nil {
		t.Fatal(err)
	}
	orphans := []string{
		filepath.Join(s.armsRoot(), "arm-99-A"),
		filepath.Join(s.launchRoot(), "99-A"),
	}
	for _, d := range orphans {
		if err := os.MkdirAll(filepath.Join(d, "sub"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	proj := filepath.Join(f.home, "learning", "probe-projections", "1", "abc")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	s.Recover(context.Background())
	got, _ := f.st.GetProbe(pr.ID)
	if got.Status != "failed" || got.Error != "interrupted by restart" {
		t.Fatalf("probe = %+v", got)
	}
	arms, _ := f.st.ListProbeArms(pr.ID)
	if arms[0].Status != "killed" {
		t.Fatalf("arm = %+v", arms[0])
	}
	for _, d := range append(orphans, proj) {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("%s survived Recover (%v)", d, err)
		}
	}
	// With the stale probe failed, the service is no longer busy.
	if _, ok, _ := f.st.ActiveProbe(); ok {
		t.Fatal("probe still active after Recover")
	}
}
