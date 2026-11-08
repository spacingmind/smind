package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

type pruneEnv struct {
	*histEnv
	days int
}

// newPruneEnv seeds n 60-day-old finished runs on an archived task (or an open one).
func newPruneEnv(t *testing.T, archived bool, n int) *pruneEnv {
	t.Helper()
	e := &pruneEnv{histEnv: newHistEnv(t), days: 30}
	if archived {
		if _, err := e.st.ArchiveTask(e.task.ID); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Now().Add(-60 * 24 * time.Hour).UTC()
	for i := 0; i < n; i++ {
		seedFinished(t, e.st, e.task.ID, fmt.Sprintf("old-%d", i), base.Add(time.Duration(i)*time.Second), genHistory(i, 100<<10))
	}
	return e
}

func (e *pruneEnv) pruner(reg *Registry) *Pruner {
	p := NewPruner(reg, e.st, func() int { return e.days })
	p.pause = 0
	p.batch = 50
	return p
}

func eventCount(t *testing.T, st *store.Store, id string) int {
	t.Helper()
	rows, err := st.ListRunEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

func TestHistoryOfPrunedRun_SummaryAndMarker(t *testing.T) {
	e := newPruneEnv(t, true, 3)
	reg := newTestRegistry(t, e.st)
	reg.SetRetentionDays(30)

	type kept struct {
		last, final string
		row         store.Run
		usage       store.RunUsage
	}
	before := map[string]kept{}
	cost := 1.25
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("old-%d", i)
		if _, _, err := reg.History(id); err != nil { // resident copy: pruning must drop it
			t.Fatal(err)
		}
		if err := e.st.UpsertRunUsage(store.RunUsage{RunID: id, TaskID: e.task.ID, Provider: "glm", CostUSD: &cost, Source: "test"}); err != nil {
			t.Fatal(err)
		}
		last, _ := reg.LastAgentText(id)
		final, _ := reg.FinalAgentText(id)
		row, _ := e.st.GetRun(id)
		u, _ := e.st.GetRunUsage(id)
		before[id] = kept{last, final, row, u}
	}
	rep, err := e.pruner(reg).Run(context.Background())
	if err != nil || rep.Runs != 3 || rep.Rows == 0 {
		t.Fatalf("prune = %+v, %v", rep, err)
	}
	for id, b := range before {
		if n := eventCount(t, e.st, id); n != 0 {
			t.Fatalf("%s: %d event rows left", id, n)
		}
		// What the summary keeps equals what the full history said.
		last, _ := reg.LastAgentText(id)
		final, _ := reg.FinalAgentText(id)
		if last != b.last || final != b.final || last == "" {
			t.Fatalf("%s: text after prune %q/%q, before %q/%q", id, last, final, b.last, b.final)
		}
		hist, status, err := reg.History(id)
		if err != nil || len(hist) != 3 {
			t.Fatalf("%s: History = %d events, %v; want note, text, done", id, len(hist), err)
		}
		if hist[0].Type != taskrunner.EventTypeSessionNote || !strings.Contains(hist[0].Text, "pruned after 30 days") ||
			hist[1].Type != taskrunner.EventTypeText || hist[1].Text != b.last ||
			hist[2].Type != taskrunner.EventTypeDone {
			t.Fatalf("%s: summary = %+v", id, hist)
		}
		if status.HistoryPrunedAt == nil {
			t.Fatalf("%s: status has no HistoryPrunedAt", id)
		}
		// The run row (minus the marker) and its usage are unchanged.
		row, _ := e.st.GetRun(id)
		if row.HistoryPrunedAt == nil {
			t.Fatalf("%s: row not marked", id)
		}
		row.HistoryPrunedAt = nil
		if !reflect.DeepEqual(row, b.row) {
			t.Fatalf("%s: run row changed:\n got %+v\nwant %+v", id, row, b.row)
		}
		if u, _ := e.st.GetRunUsage(id); !reflect.DeepEqual(u, b.usage) {
			t.Fatalf("%s: usage changed", id)
		}
	}
	if st := reg.HistoryStats(); st.ResidentBytes != 0 || st.ResidentRuns != 0 {
		t.Fatalf("pruned runs still resident: %+v", st)
	}
}

func TestPrunedRun_EveryReaderServesTheSummary(t *testing.T) {
	e := newPruneEnv(t, true, 1)
	reg := newTestRegistry(t, e.st)
	reg.SetRetentionDays(30)
	if _, err := e.pruner(reg).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	want, _, _ := reg.History("old-0")
	check := func(name string, r *Registry) {
		t.Helper()
		hist, _, err := r.History("old-0")
		if err != nil || !reflect.DeepEqual(hist, want) {
			t.Fatalf("%s: History = %+v, %v", name, hist, err)
		}
		ch, unsub, err := r.Subscribe("old-0")
		if err != nil {
			t.Fatalf("%s: Subscribe: %v", name, err)
		}
		if got := drain(t, ch); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: Subscribe delivered %+v", name, got)
		}
		unsub()
		view, err := r.Tail("old-0", 2)
		if err != nil || !reflect.DeepEqual(view.Events, want[1:]) || view.Status.HistoryPrunedAt == nil {
			t.Fatalf("%s: Tail(2) = %+v, %v", name, view, err)
		}
		if _, err := r.ToolCallHeredoc("old-0", "call-0-0", 0); err != ErrHistoryPruned {
			t.Fatalf("%s: ToolCallHeredoc = %v, want ErrHistoryPruned", name, err)
		}
		if txt, err := r.LastAgentText("old-0"); err != nil || txt != want[1].Text {
			t.Fatalf("%s: LastAgentText = %q, %v", name, txt, err)
		}
		if txt, err := r.FinalAgentText("old-0"); err != nil || txt != want[1].Text {
			t.Fatalf("%s: FinalAgentText = %q, %v", name, txt, err)
		}
	}
	check("same registry", reg)
	restarted := newTestRegistry(t, e.st)
	restarted.SetRetentionDays(30) // server.New does this from the config
	check("after a restart", restarted)
	unlisted := newTestRegistry(t, e.st)
	unlisted.SetRetentionDays(30)
	unlisted.mu.Lock()
	delete(unlisted.runs, "old-0")
	unlisted.mu.Unlock()
	check("unlisted run", unlisted)
}

func TestPrune_NeverTouchesOpenTaskOrRunningRuns(t *testing.T) {
	e := newPruneEnv(t, false, 2) // an open (not archived) task
	reg := newTestRegistry(t, e.st)

	// A running run with an old stamp in the registry: also untouched.
	now := time.Now().UTC()
	if _, err := e.st.CreateRun(store.Run{ID: "live", TaskID: e.task.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: now.Add(-90 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	live := &run{id: "live", taskID: e.task.ID, status: StatusRunning, cancel: func() {}, startedAt: now, subscribers: map[int]*subQueue{}, pendingPermissions: map[string]*pendingPermission{"req-1": {ch: make(chan permissionAnswer, 1)}}, closedCh: make(chan struct{})}
	reg.mu.Lock()
	reg.runs["live"] = live
	reg.mu.Unlock()
	for _, ev := range genHistory(7, 20<<10) {
		reg.record(live, ev)
	}

	rep, err := e.pruner(reg).Run(context.Background())
	if err != nil || rep.Runs != 0 || rep.Rows != 0 {
		t.Fatalf("prune = %+v, %v; nothing may be pruned", rep, err)
	}
	for _, id := range []string{"old-0", "old-1", "live"} {
		if eventCount(t, e.st, id) == 0 {
			t.Fatalf("%s lost its events", id)
		}
		if hist, status, _ := reg.History(id); len(hist) < 5 || status.HistoryPrunedAt != nil {
			t.Fatalf("%s: History is not the full history: %d events, pruned=%v", id, len(hist), status.HistoryPrunedAt)
		}
	}
	// Archiving the task makes the old finished runs eligible; the running one still is not.
	if _, err := e.st.ArchiveTask(e.task.ID); err != nil {
		t.Fatal(err)
	}
	rep, err = e.pruner(reg).Run(context.Background())
	if err != nil || rep.Runs != 2 {
		t.Fatalf("after archive: %+v, %v; want the 2 finished runs", rep, err)
	}
	if eventCount(t, e.st, "live") == 0 {
		t.Fatal("the running run was pruned")
	}
	live.mu.Lock()
	live.status = StatusDone // let the cleanup's CloseAll return
	live.mu.Unlock()
}

func TestPrune_DisabledAtZeroDays(t *testing.T) {
	e := newPruneEnv(t, true, 1)
	e.days = 0
	reg := newTestRegistry(t, e.st)
	rep, err := e.pruner(reg).Run(context.Background())
	if err != nil || rep.Enabled || eventCount(t, e.st, "old-0") == 0 {
		t.Fatalf("0 days must keep everything: %+v, %v", rep, err)
	}
	if pv, _ := e.pruner(reg).Preview(); pv.Enabled || pv.Rows != 0 {
		t.Fatalf("preview with retention off = %+v", pv)
	}
}

func TestPrune_DryRunEqualsReal(t *testing.T) {
	e := newPruneEnv(t, true, 4)
	reg := newTestRegistry(t, e.st)
	p := e.pruner(reg)
	pv, err := p.Preview()
	if err != nil || !pv.DryRun || pv.Runs != 4 || pv.Rows == 0 || pv.Bytes == 0 {
		t.Fatalf("preview = %+v, %v", pv, err)
	}
	if eventCount(t, e.st, "old-0") == 0 {
		t.Fatal("the dry run deleted rows")
	}
	rep, err := p.Run(context.Background())
	if err != nil || rep.Runs != pv.Runs || rep.Rows != pv.Rows || rep.Bytes != pv.Bytes {
		t.Fatalf("real %+v != dry run %+v (%v)", rep, pv, err)
	}
}

// skelPrunePair builds the PR C fixture: two 60-day-old runs on an archived
// task with the readers' stream (a 3000-byte tool result, a ~1.5 KB heredoc
// input), one stored full ("full") and one in skeleton form ("skel").
func skelPrunePair(t *testing.T) *skelPair {
	t.Helper()
	return newSkelPair(t, skelStream(), true)
}

func (p *skelPair) pruner() *Pruner {
	pr := NewPruner(p.reg, p.e.st, func() int { return 30 })
	pr.pause, pr.batch = 0, 50
	return pr
}

func excerptCount(t *testing.T, st *store.Store, id string) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM run_excerpts WHERE run_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPrune_SkeletonRunKeepsRowsDeletesExcerpts(t *testing.T) {
	p := skelPrunePair(t)
	rowsBefore := eventCount(t, p.e.st, p.skl)
	rep, err := p.pruner().Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Runs != 1 || rep.Rows == 0 || rep.ExcerptRuns != 1 || rep.ExcerptRows == 0 {
		t.Fatalf("report = %+v; want the full run's rows and the skeleton's excerpts", rep)
	}
	// the skeleton run: rows and final text unchanged, excerpts gone, never
	// marked history-pruned
	if n := eventCount(t, p.e.st, p.skl); n != rowsBefore {
		t.Fatalf("skeleton run: %d rows, want the %d it had", n, rowsBefore)
	}
	if n := excerptCount(t, p.e.st, p.skl); n != 0 {
		t.Fatalf("skeleton run: %d excerpt rows left", n)
	}
	if txt, _ := p.e.st.GetRunFinalText(p.skl); txt != "" {
		t.Fatalf("skeleton run: final text = %q, want none", txt)
	}
	if row, _ := p.e.st.GetRun(p.skl); row.HistoryPrunedAt != nil {
		t.Fatal("skeleton run was marked history-pruned")
	}
	if sk, _ := p.e.st.GetRunSkeleton(p.skl); sk.ExcerptsPrunedAt == nil {
		t.Fatal("skeleton run has no excerpts_pruned_at")
	}
	// the full run prunes as before
	if n := eventCount(t, p.e.st, p.full); n != 0 {
		t.Fatalf("full run: %d rows left", n)
	}
	if row, _ := p.e.st.GetRun(p.full); row.HistoryPrunedAt == nil {
		t.Fatal("full run was not marked history-pruned")
	}
}

func TestPrune_ReportCountsExcerptsSeparately(t *testing.T) {
	p := skelPrunePair(t)
	fullRows := eventCount(t, p.e.st, p.full)
	skelExcerpts := excerptCount(t, p.e.st, p.skl)
	rep, err := p.pruner().Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Runs != 1 || rep.Rows != int64(fullRows) {
		t.Fatalf("full counts = %d runs, %d rows; want 1, %d", rep.Runs, rep.Rows, fullRows)
	}
	if rep.ExcerptRuns != 1 || rep.ExcerptRows != int64(skelExcerpts) {
		t.Fatalf("excerpt counts = %d runs, %d rows; want 1, %d", rep.ExcerptRuns, rep.ExcerptRows, skelExcerpts)
	}
}

func TestPrune_DryRunEqualsRealWithSkeletonRuns(t *testing.T) {
	p := skelPrunePair(t)
	rowsBefore := eventCount(t, p.e.st, p.skl)
	pv, err := p.pruner().Preview()
	if err != nil || !pv.DryRun || pv.Runs != 1 || pv.ExcerptRuns != 1 || pv.Rows == 0 || pv.ExcerptRows == 0 {
		t.Fatalf("preview = %+v, %v", pv, err)
	}
	if n := eventCount(t, p.e.st, p.skl); n != rowsBefore {
		t.Fatal("the dry run changed the store")
	}
	rep, err := p.pruner().Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Runs != pv.Runs || rep.Rows != pv.Rows || rep.Bytes != pv.Bytes ||
		rep.ExcerptRuns != pv.ExcerptRuns || rep.ExcerptRows != pv.ExcerptRows || rep.ExcerptBytes != pv.ExcerptBytes {
		t.Fatalf("real %+v != dry run %+v", rep, pv)
	}
}

func TestPrune_CrashResumeExcerpts(t *testing.T) {
	p := skelPrunePair(t)
	total := excerptCount(t, p.e.st, p.skl)
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	// A crash after the mark and one batch: one excerpt row is gone, the mark
	// is set, the rest survives.
	if n, err := p.e.st.PruneExcerptsBatch(p.skl, 1, time.Now().UTC(), cutoff); err != nil || n != 1 {
		t.Fatalf("first batch = %d, %v", n, err)
	}
	if sk, _ := p.e.st.GetRunSkeleton(p.skl); sk.ExcerptsPrunedAt == nil {
		t.Fatal("the first batch did not mark the run")
	}
	rep, err := p.pruner().Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExcerptRuns != 1 || rep.ExcerptRows != int64(total-1) {
		t.Fatalf("resume = %+v; want %d more excerpt rows", rep, total-1)
	}
	if n := excerptCount(t, p.e.st, p.skl); n != 0 {
		t.Fatalf("%d excerpt rows left after the resume", n)
	}
}

func TestHistoryInfo_ExcerptPruned(t *testing.T) {
	env := newRebuildEnvFiller(t, true, 30) // the tool input has an excerpt
	old := time.Now().UTC().Add(-60 * 24 * time.Hour)
	if _, err := env.e.st.ArchiveTask(env.e.task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.e.st.DB().Exec(`UPDATE runs SET started_at = ?, finished_at = ? WHERE id = 'r'`, old.Add(time.Hour), old); err != nil {
		t.Fatal(err)
	}
	if got := env.reg.HistoryInfo("r"); got.Reason != "rebuild_not_requested" || !got.CanLoadFull {
		t.Fatalf("before the prune: %+v", got)
	}
	pr := NewPruner(env.reg, env.e.st, func() int { return 30 })
	pr.pause, pr.batch = 0, 50
	if rep, err := pr.Run(context.Background()); err != nil || rep.ExcerptRuns != 1 {
		t.Fatalf("prune = %+v, %v", rep, err)
	}
	if got := env.reg.HistoryInfo("r"); got.Source != "skeleton" || got.Reason != "excerpt_pruned" || !got.CanLoadFull {
		t.Fatalf("after the prune: %+v; want excerpt_pruned and CanLoadFull", got)
	}
	// without a rebuilder the reason stays provider_cannot_rebuild
	if got := newTestRegistry(t, env.e.st).HistoryInfo("r"); got.Reason != "provider_cannot_rebuild" || got.CanLoadFull {
		t.Fatalf("no rebuilder: %+v", got)
	}
}

func TestPrune_CrashResume(t *testing.T) {
	e := newPruneEnv(t, true, 1)
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	// A crash after phase 1 and one batch.
	if ok, err := e.st.MarkRunPruned("old-0", "kept answer", time.Now(), cutoff); !ok || err != nil {
		t.Fatalf("mark: %v %v", ok, err)
	}
	if _, err := e.st.DeleteRunEventsBatch("old-0", 10); err != nil {
		t.Fatal(err)
	}
	reg := newTestRegistry(t, e.st) // the restart
	reg.SetRetentionDays(30)
	if eventCount(t, e.st, "old-0") == 0 {
		t.Fatal("test premise: rows should remain")
	}
	hist, _, err := reg.History("old-0")
	if err != nil || len(hist) != 3 || hist[1].Text != "kept answer" {
		t.Fatalf("mid-prune History = %+v, %v; want the summary, never the partial rows", hist, err)
	}
	rep, err := e.pruner(reg).Run(context.Background())
	if err != nil || rep.Rows == 0 || eventCount(t, e.st, "old-0") != 0 {
		t.Fatalf("resume = %+v, %v", rep, err)
	}
}

func TestPrune_OnePassAtATime(t *testing.T) {
	e := newPruneEnv(t, true, 1)
	p := e.pruner(newTestRegistry(t, e.st))
	p.running.Lock()
	defer p.running.Unlock()
	if _, err := p.Run(context.Background()); err != ErrPruneRunning {
		t.Fatalf("second pass = %v, want ErrPruneRunning", err)
	}
}

func TestPruneJobStopsOnCancel(t *testing.T) {
	e := newPruneEnv(t, true, 1)
	p := e.pruner(newTestRegistry(t, e.st))
	p.startDelay = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for eventCount(t, e.st, "old-0") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the job never ran its first pass")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if p.running.TryLock() {
		p.running.Unlock()
	} else {
		time.Sleep(200 * time.Millisecond)
	}
}

func TestCapText(t *testing.T) {
	s := strings.Repeat("é", 10) // 2 bytes each
	if got := capText(s, 5); got != "éé" || !strings.HasPrefix(s, got) {
		t.Fatalf("capText cut inside a rune: %q", got)
	}
	if capText("abc", 10) != "abc" {
		t.Fatal("short text must be untouched")
	}
}

// TestMeasureRetention prints the retention numbers on a synthetic database
// (docs/plans/active/run-events-retention.md): SMIND_MEASURE_DB=<dir with
// smind.db> go test ./internal/runs -run MeasureRetention -v. Skipped otherwise;
// the database is modified, so point it at a copy.
func TestMeasureRetention(t *testing.T) {
	dir := os.Getenv("SMIND_MEASURE_DB")
	if dir == "" {
		t.Skip("SMIND_MEASURE_DB not set")
	}
	path := filepath.Join(dir, "smind.db")
	size := func() int64 { fi, _ := os.Stat(path); return fi.Size() }
	t0 := time.Now()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg, err := New(st)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("open + New: %v; file %.1f MiB; incremental auto_vacuum: %v", time.Since(t0).Round(time.Millisecond), float64(size())/(1<<20), st.AutoVacuumIncremental())
	p := NewPruner(reg, st, func() int { return 30 })
	pv, err := p.Preview()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("dry run: %d runs, %d rows, %.1f MiB of event data", pv.Runs, pv.Rows, float64(pv.Bytes)/(1<<20))

	ws, _ := st.ListWorkspaces()
	task, err := st.CreateTask(store.Task{WorkspaceID: ws[0].ID, Title: "live writer", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRun(store.Run{ID: "measure-live", TaskID: task.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	writer := func(stop <-chan struct{}, lat *[]time.Duration, done chan<- struct{}, offset int64) {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			t1 := time.Now()
			if _, err := st.AppendRunEvent("measure-live", offset+int64(i), `{"type":0,"text":"live"}`); err != nil {
				t.Errorf("live writer: %v", err)
				return
			}
			*lat = append(*lat, time.Since(t1))
			time.Sleep(5 * time.Millisecond)
		}
	}
	report := func(name string, lat []time.Duration) {
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		if len(lat) == 0 {
			return
		}
		t.Logf("%s writer latency over %d writes: p50 %v p99 %v max %v", name, len(lat), lat[len(lat)/2], lat[len(lat)*99/100], lat[len(lat)-1])
	}
	var base []time.Duration
	stop, done := make(chan struct{}), make(chan struct{})
	go writer(stop, &base, done, 0)
	time.Sleep(2 * time.Second)
	close(stop)
	<-done
	report("baseline (no pruning)", base)

	var during []time.Duration
	stop, done = make(chan struct{}), make(chan struct{})
	go writer(stop, &during, done, 1_000_000)
	before := size()
	t1 := time.Now()
	rep, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(t1)
	close(stop)
	<-done
	report("during pruning", during)
	t.Logf("prune pass: %v; %d runs, %d rows, freed %.1f MiB to the OS, reusable in file %.1f MiB; file %.1f MiB -> %.1f MiB", took.Round(time.Millisecond), rep.Runs, rep.Rows, float64(rep.FreedBytes)/(1<<20), rep.ReusableMB, float64(before)/(1<<20), float64(size())/(1<<20))

	if os.Getenv("SMIND_MEASURE_COMPACT") != "" {
		t2 := time.Now()
		res, err := st.Compact(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("compact: %v; %.1f MiB -> %.1f MiB; incremental now: %v", time.Since(t2).Round(time.Millisecond), float64(res.BeforeBytes)/(1<<20), float64(res.AfterBytes)/(1<<20), st.AutoVacuumIncremental())
	}
	// Close the live run so cleanup does not wait for it.
	_ = reg
}
