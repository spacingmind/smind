package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type retEnv struct {
	s      *Store
	wsID   int64
	cutoff time.Time
}

func newRetEnv(t *testing.T) *retEnv {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ws, err := s.CreateWorkspace(Workspace{Path: "/r/repo", Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	return &retEnv{s: s, wsID: ws.ID, cutoff: time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Second)}
}

// task makes a task: archived and/or system as asked.
func (e *retEnv) task(t *testing.T, archived, system bool) int64 {
	t.Helper()
	tk := Task{WorkspaceID: e.wsID, Title: "t", Status: "created", Kind: "task"}
	if system {
		tk.Kind = "system"
	}
	if archived {
		now := time.Now().UTC()
		tk.Status, tk.ArchivedAt = "archived", &now
	}
	out, err := e.s.CreateTask(tk)
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

// run makes a run finished at fin (nil = unfinished) with n events.
func (e *retEnv) run(t *testing.T, id string, taskID int64, status, purpose string, fin *time.Time, n int) {
	t.Helper()
	if _, err := e.s.CreateRun(Run{ID: id, TaskID: taskID, Provider: "glm", Prompt: "p", Status: status, Purpose: purpose, StartedAt: time.Now().UTC().Add(-90 * 24 * time.Hour), FinishedAt: fin}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, err := e.s.AppendRunEvent(id, int64(i), fmt.Sprintf(`{"type":0,"text":"e%d"}`, i)); err != nil {
			t.Fatal(err)
		}
	}
}

func at(t time.Time) *time.Time { return &t }

func (e *retEnv) events(t *testing.T, id string) int {
	t.Helper()
	rows, err := e.s.ListRunEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

// pruneAll runs both phases like the daemon job does and returns the counts.
func (e *retEnv) pruneAll(t *testing.T, batch int) PruneCounts {
	t.Helper()
	var c PruneCounts
	for {
		ids, err := e.s.PruneCandidates(e.cutoff, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) == 0 {
			return c
		}
		for _, id := range ids {
			if _, err := e.s.MarkRunPruned(id, "final of "+id, time.Now(), e.cutoff); err != nil {
				t.Fatal(err)
			}
			var rows int64
			for {
				n, err := e.s.DeleteRunEventsBatch(id, batch)
				if err != nil {
					t.Fatal(err)
				}
				if n > int64(batch) {
					t.Fatalf("batch deleted %d rows, limit %d", n, batch)
				}
				rows += n
				if n == 0 {
					break
				}
			}
			if rows > 0 {
				c.Runs++
				c.Rows += rows
			}
		}
	}
}

func TestPruneRunEvents_CutoffBoundary(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	e.run(t, "old", task, "done", "user", at(e.cutoff.Add(-time.Nanosecond)), 3)
	e.run(t, "edge", task, "done", "user", at(e.cutoff), 3)
	e.run(t, "young", task, "done", "user", at(e.cutoff.Add(time.Hour)), 3)
	e.pruneAll(t, 100)
	if e.events(t, "old") != 0 || e.events(t, "edge") != 3 || e.events(t, "young") != 3 {
		t.Fatalf("events old/edge/young = %d/%d/%d, want 0/3/3 (a run exactly at the cutoff stays)", e.events(t, "old"), e.events(t, "edge"), e.events(t, "young"))
	}
	row, _ := e.s.GetRun("old")
	if row.HistoryPrunedAt == nil {
		t.Fatal("pruned run has no history_pruned_at")
	}
	if txt, _ := e.s.GetRunFinalText("old"); txt != "final of old" {
		t.Fatalf("final text = %q", txt)
	}
}

func TestPruneRunEvents_NeverPrunesRunningOrOpenTask(t *testing.T) {
	e := newRetEnv(t)
	open, archived := e.task(t, false, false), e.task(t, true, false)
	old := at(e.cutoff.Add(-48 * time.Hour))
	e.run(t, "open-task", open, "done", "user", old, 4)
	e.run(t, "running", archived, "running", "user", old, 4)
	e.run(t, "unfinished", archived, "done", "user", nil, 4)
	e.run(t, "running-with-finished-at", archived, "running", "user", old, 4) // a stale stamp must not matter
	e.run(t, "archived-ok", archived, "done", "user", old, 4)
	c := e.pruneAll(t, 100)
	for _, id := range []string{"open-task", "running", "unfinished", "running-with-finished-at"} {
		if e.events(t, id) != 4 {
			t.Errorf("%s lost its events", id)
		}
		if row, _ := e.s.GetRun(id); row.HistoryPrunedAt != nil {
			t.Errorf("%s was marked pruned", id)
		}
	}
	if e.events(t, "archived-ok") != 0 || c.Runs != 1 || c.Rows != 4 {
		t.Fatalf("archived run: events %d, counts %+v", e.events(t, "archived-ok"), c)
	}
}

func TestPruneRunEvents_SystemRunsPrunedWhenTaskOpen(t *testing.T) {
	e := newRetEnv(t)
	open, sys := e.task(t, false, false), e.task(t, false, true)
	old := at(e.cutoff.Add(-time.Hour))
	e.run(t, "probe-in-open", open, "done", "probe", old, 2)
	e.run(t, "user-in-system", sys, "done", "user", old, 2)
	e.run(t, "user-in-open", open, "done", "user", old, 2)
	e.pruneAll(t, 100)
	if e.events(t, "probe-in-open") != 0 || e.events(t, "user-in-system") != 0 || e.events(t, "user-in-open") != 2 {
		t.Fatal("system/probe runs must be pruned by age, a user run of an open task must not")
	}
}

func TestPruneRunEvents_BatchBound(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	e.run(t, "r", task, "done", "user", at(e.cutoff.Add(-time.Hour)), 25)
	// An unmarked run is never deleted from, whatever the limit.
	if n, err := e.s.DeleteRunEventsBatch("r", 100); err != nil || n != 0 {
		t.Fatalf("unmarked run: deleted %d, %v", n, err)
	}
	if ok, err := e.s.MarkRunPruned("r", "x", time.Now(), e.cutoff); !ok || err != nil {
		t.Fatalf("mark = %v, %v", ok, err)
	}
	if again, _ := e.s.MarkRunPruned("r", "y", time.Now(), e.cutoff); again {
		t.Fatal("a second mark must not overwrite the first")
	}
	var got []int64
	for {
		n, err := e.s.DeleteRunEventsBatch("r", 7)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
		got = append(got, n)
	}
	if fmt.Sprint(got) != "[7 7 7 4]" {
		t.Fatalf("batches = %v, want [7 7 7 4]", got)
	}
}

func TestPruneRunEvents_TwoPhaseCrash(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	e.run(t, "r", task, "done", "user", at(e.cutoff.Add(-time.Hour)), 20)
	// Crash after phase 1 and one batch: marked, rows left.
	if ok, _ := e.s.MarkRunPruned("r", "kept", time.Now(), e.cutoff); !ok {
		t.Fatal("not marked")
	}
	if _, err := e.s.DeleteRunEventsBatch("r", 5); err != nil {
		t.Fatal(err)
	}
	row, _ := e.s.GetRun("r")
	if row.HistoryPrunedAt == nil || e.events(t, "r") != 15 {
		t.Fatalf("mid-prune state: marked=%v events=%d", row.HistoryPrunedAt != nil, e.events(t, "r"))
	}
	// The next pass resumes: the run is still a candidate, even if its task became live.
	if _, err := e.s.db.Exec(`UPDATE tasks SET archived_at = NULL, status = 'created'`); err != nil {
		t.Fatal(err)
	}
	if c := e.pruneAll(t, 6); c.Rows != 15 {
		t.Fatalf("resume deleted %d rows, want 15", c.Rows)
	}
	if e.events(t, "r") != 0 {
		t.Fatal("events remain after resume")
	}
	if txt, _ := e.s.GetRunFinalText("r"); txt != "kept" {
		t.Fatalf("final text changed to %q", txt)
	}
}

func TestMarkRunPruned_RechecksEligibility(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	e.run(t, "r", task, "done", "user", at(e.cutoff.Add(-time.Hour)), 2)
	ids, _ := e.s.PruneCandidates(e.cutoff, 10)
	if len(ids) != 1 {
		t.Fatalf("candidates %v", ids)
	}
	// The task is un-archived between selection and mark.
	if _, err := e.s.db.Exec(`UPDATE tasks SET archived_at = NULL, status = 'created'`); err != nil {
		t.Fatal(err)
	}
	if ok, _ := e.s.MarkRunPruned("r", "x", time.Now(), e.cutoff); ok {
		t.Fatal("a run of a task that became live was marked")
	}
}

func TestPrunePreviewEqualsReal(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	for i := 0; i < 6; i++ {
		e.run(t, fmt.Sprintf("r%d", i), task, "done", "user", at(e.cutoff.Add(-time.Duration(i+1)*time.Hour)), 3+i)
	}
	e.run(t, "young", task, "done", "user", at(time.Now()), 9)
	want, err := e.s.PrunePreview(e.cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if want.Bytes <= 0 {
		t.Fatalf("preview bytes = %d", want.Bytes)
	}
	got := e.pruneAll(t, 4)
	if got.Runs != want.Runs || got.Rows != want.Rows {
		t.Fatalf("real %+v != dry run %+v", got, want)
	}
	if after, _ := e.s.PrunePreview(e.cutoff); after.Rows != 0 {
		t.Fatalf("preview after prune = %+v", after)
	}
}

func TestPruneRunEvents_WriterNotBlocked(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	live := e.task(t, false, false)
	for i := 0; i < 20; i++ {
		e.run(t, fmt.Sprintf("old%d", i), task, "done", "user", at(e.cutoff.Add(-time.Hour)), 400)
	}
	e.run(t, "live", live, "running", "user", nil, 0)

	var wg sync.WaitGroup
	var maxWait time.Duration
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			t0 := time.Now()
			if _, err := e.s.AppendRunEvent("live", int64(i), `{"type":0,"text":"x"}`); err != nil {
				t.Errorf("live writer: %v", err)
				return
			}
			if d := time.Since(t0); d > maxWait {
				maxWait = d
			}
			time.Sleep(time.Millisecond)
		}
	}()
	for _, id := range mustCandidates(t, e) {
		if _, err := e.s.MarkRunPruned(id, "x", time.Now(), e.cutoff); err != nil {
			t.Fatal(err)
		}
		for {
			n, err := e.s.DeleteRunEventsBatch(id, 200)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	close(stop)
	wg.Wait()
	if maxWait > time.Second {
		t.Fatalf("a live writer waited %v during pruning", maxWait)
	}
	t.Logf("max live writer wait during pruning: %v", maxWait)
}

func mustCandidates(t *testing.T, e *retEnv) []string {
	t.Helper()
	ids, err := e.s.PruneCandidates(e.cutoff, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// skelRun makes a run in skeleton form: two event rows and two excerpt rows,
// the shape ApplySkeleton leaves behind.
func (e *retEnv) skelRun(t *testing.T, id string, taskID int64, status string, fin *time.Time) {
	t.Helper()
	e.run(t, id, taskID, status, "user", fin, 2)
	if _, err := e.s.db.Exec(`UPDATE runs SET skeleton_at = ?, skeleton_rev = 1 WHERE id = ?`, time.Now().UTC(), id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := e.s.db.Exec(`INSERT INTO run_excerpts (run_id, seq, field, head, tail, enc, bytes, sha256) VALUES (?, ?, 'out', ?, '', 'raw', 40, 'x')`,
			id, i, fmt.Sprintf(`"excerpt-%s-%d"`, id, i)); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *retEnv) excerpts(t *testing.T, id string) int {
	t.Helper()
	var n int
	if err := e.s.db.QueryRow(`SELECT COUNT(*) FROM run_excerpts WHERE run_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// pruneExcerptsAll drives PruneExcerptsBatch to completion, as the pruner does.
func (e *retEnv) pruneExcerptsAll(t *testing.T, id string, batch int) int64 {
	t.Helper()
	var total int64
	for {
		n, err := e.s.PruneExcerptsBatch(id, batch, time.Now().UTC(), e.cutoff)
		if err != nil {
			t.Fatal(err)
		}
		total += n
		if n == 0 {
			return total
		}
	}
}

// pruneAllMixed drives both kinds of prune as the pruner does: a skeleton run
// loses only its excerpts, a full run its rows.
func (e *retEnv) pruneAllMixed(t *testing.T, batch int) PruneCounts {
	t.Helper()
	var c PruneCounts
	for {
		ids, err := e.s.PruneCandidates(e.cutoff, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) == 0 {
			return c
		}
		for _, id := range ids {
			if sk, _ := e.s.GetRunSkeleton(id); sk.SkeletonAt != nil {
				if rows := e.pruneExcerptsAll(t, id, batch); rows > 0 {
					c.ExcerptRuns++
					c.ExcerptRows += rows
				}
				continue
			}
			if _, err := e.s.MarkRunPruned(id, "final of "+id, time.Now(), e.cutoff); err != nil {
				t.Fatal(err)
			}
			var rows int64
			for {
				n, err := e.s.DeleteRunEventsBatch(id, batch)
				if err != nil {
					t.Fatal(err)
				}
				rows += n
				if n == 0 {
					break
				}
			}
			if rows > 0 {
				c.Runs++
				c.Rows += rows
			}
		}
	}
}

// A skeleton run's prune deletes only its excerpt tier: the rows, the run row
// and its final text stay, and it is never marked history-pruned.
func TestPruneExcerptsOnly_KeepsSkeleton(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	e.skelRun(t, "sk", task, "done", at(e.cutoff.Add(-time.Hour)))
	if n := e.pruneExcerptsAll(t, "sk", 1); n != 2 {
		t.Fatalf("deleted %d excerpt rows, want 2", n)
	}
	if e.excerpts(t, "sk") != 0 {
		t.Fatal("excerpt rows left")
	}
	if n := e.events(t, "sk"); n != 2 {
		t.Fatalf("%d event rows left, want the 2 stored ones untouched", n)
	}
	row, _ := e.s.GetRun("sk")
	if row.HistoryPrunedAt != nil {
		t.Fatal("a skeleton run must never be marked history-pruned")
	}
	if sk, _ := e.s.GetRunSkeleton("sk"); sk.ExcerptsPrunedAt == nil {
		t.Fatal("excerpts_pruned_at not stamped")
	}
	if txt, _ := e.s.GetRunFinalText("sk"); txt != "" {
		t.Fatalf("final text = %q, want the unchanged empty default", txt)
	}
}

func TestMarkRunPrunedRefusesSkeletonRun(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	e.skelRun(t, "sk", task, "done", at(e.cutoff.Add(-time.Hour)))
	ok, err := e.s.MarkRunPruned("sk", "kept", time.Now(), e.cutoff)
	if err != nil || ok {
		t.Fatalf("MarkRunPruned = %v, %v; a skeleton run must be refused", ok, err)
	}
	row, _ := e.s.GetRun("sk")
	if row.HistoryPrunedAt != nil {
		t.Fatal("the skeleton run was marked")
	}
	if txt, _ := e.s.GetRunFinalText("sk"); txt != "" {
		t.Fatalf("final text = %q, want the unchanged empty default", txt)
	}
	if n := e.pruneExcerptsAll(t, "sk", 10); n != 2 {
		t.Fatalf("its excerpts are still prunable, deleted %d", n)
	}
}

func TestPruneCandidatesIncludeSkeletonRunsWithExcerpts(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	old := at(e.cutoff.Add(-time.Hour))
	// a full run with no events and a skeleton run with no excerpts are not candidates
	e.run(t, "full-noevents", task, "done", "user", old, 0)
	e.run(t, "sk-noexcerpts", task, "done", "user", old, 2)
	if _, err := e.s.db.Exec(`UPDATE runs SET skeleton_at = ?, skeleton_rev = 1 WHERE id = 'sk-noexcerpts'`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// both kinds with data are
	e.run(t, "full", task, "done", "user", at(e.cutoff.Add(-2*time.Hour)), 1)
	e.skelRun(t, "sk", task, "done", at(e.cutoff.Add(-3*time.Hour)))
	ids, err := e.s.PruneCandidates(e.cutoff, 10)
	if err != nil || fmt.Sprint(ids) != "[sk full]" {
		t.Fatalf("candidates = %v, %v; want [sk full]", ids, err)
	}
}

func TestPruneExcerptsNeverTouchesLiveTaskOrRunning(t *testing.T) {
	e := newRetEnv(t)
	open, archived := e.task(t, false, false), e.task(t, true, false)
	old := at(e.cutoff.Add(-48 * time.Hour))
	e.skelRun(t, "open-task", open, "done", old)
	e.skelRun(t, "running", archived, "running", old)
	e.skelRun(t, "young", archived, "done", at(e.cutoff.Add(time.Hour)))
	for _, id := range []string{"open-task", "running", "young"} {
		if n, err := e.s.PruneExcerptsBatch(id, 10, time.Now().UTC(), e.cutoff); err != nil || n != 0 {
			t.Fatalf("%s: deleted %d excerpt rows, %v", id, n, err)
		}
		if e.excerpts(t, id) != 2 {
			t.Errorf("%s lost its excerpts", id)
		}
		if sk, _ := e.s.GetRunSkeleton(id); sk.ExcerptsPrunedAt != nil {
			t.Errorf("%s was stamped excerpts_pruned_at", id)
		}
	}
	if ids, _ := e.s.PruneCandidates(e.cutoff, 10); len(ids) != 0 {
		t.Fatalf("candidates = %v, want none", ids)
	}
}

// The excerpt prune is for skeleton runs only: on a run in full form (even an
// eligible one that somehow has an excerpt row) it neither deletes anything nor
// stamps excerpts_pruned_at, which would later read as "its excerpts were pruned".
func TestPruneExcerptsBatchRefusesAFullFormRun(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	e.run(t, "full", task, "done", "user", at(e.cutoff.Add(-2*time.Hour)), 3)
	if _, err := e.s.db.Exec(`INSERT INTO run_excerpts (run_id, seq, field, head, tail, bytes, sha256) VALUES ('full', 1, 'out', '"x"', '', 3, 'x')`); err != nil {
		t.Fatal(err)
	}
	if n, err := e.s.PruneExcerptsBatch("full", 10, time.Now().UTC(), e.cutoff); err != nil || n != 0 {
		t.Fatalf("deleted %d excerpt rows of a full-form run, %v", n, err)
	}
	if sk, _ := e.s.GetRunSkeleton("full"); sk.ExcerptsPrunedAt != nil {
		t.Fatal("a full-form run was stamped excerpts_pruned_at")
	}
	if e.excerpts(t, "full") != 1 {
		t.Fatal("an excerpt row of a full-form run was deleted")
	}
}

func TestPrunePreviewCountsExcerptsOfSkeletonRuns(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	e.run(t, "full", task, "done", "user", at(e.cutoff.Add(-2*time.Hour)), 3)
	e.skelRun(t, "sk", task, "done", at(e.cutoff.Add(-time.Hour)))
	c, err := e.s.PrunePreview(e.cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if c.Runs != 1 || c.Rows != 3 || c.Bytes <= 0 {
		t.Fatalf("full counts = %+v, want 1 run / 3 rows / bytes", c)
	}
	if c.ExcerptRuns != 1 || c.ExcerptRows != 2 || c.ExcerptBytes <= 0 {
		t.Fatalf("excerpt counts = %+v, want 1 run / 2 rows / bytes", c)
	}
}

func TestPrunePreviewDryRunEqualsReal(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	e.run(t, "full", task, "done", "user", at(e.cutoff.Add(-2*time.Hour)), 5)
	e.skelRun(t, "sk", task, "done", at(e.cutoff.Add(-time.Hour)))
	want, err := e.s.PrunePreview(e.cutoff)
	if err != nil {
		t.Fatal(err)
	}
	got := e.pruneAllMixed(t, 2)
	if got.Runs != want.Runs || got.Rows != want.Rows || got.ExcerptRuns != want.ExcerptRuns || got.ExcerptRows != want.ExcerptRows {
		t.Fatalf("real %+v != dry run %+v", got, want)
	}
	if after, _ := e.s.PrunePreview(e.cutoff); after.Rows != 0 || after.ExcerptRows != 0 {
		t.Fatalf("preview after the prune = %+v", after)
	}
}

func TestNewDBIsIncrementalVacuum(t *testing.T) {
	e := newRetEnv(t)
	if !e.s.AutoVacuumIncremental() {
		t.Fatal("a new database must be created with auto_vacuum=INCREMENTAL")
	}
}

func TestCompactConvertsAndShrinks(t *testing.T) {
	e := newRetEnv(t)
	// Make it a legacy file: auto_vacuum NONE.
	if _, err := e.s.db.Exec(`PRAGMA auto_vacuum = NONE`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.db.Exec(`VACUUM`); err != nil {
		t.Fatal(err)
	}
	if e.s.AutoVacuumIncremental() {
		t.Skip("could not downgrade auto_vacuum on this connection pool")
	}
	task := e.task(t, true, false)
	for i := 0; i < 10; i++ {
		e.run(t, fmt.Sprintf("r%d", i), task, "done", "user", at(e.cutoff.Add(-time.Hour)), 300)
	}
	e.pruneAll(t, 1000)
	res, err := e.s.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.AfterBytes >= res.BeforeBytes {
		t.Fatalf("compact did not shrink: %+v", res)
	}
	if !e.s.AutoVacuumIncremental() {
		t.Fatal("compact must switch the file to incremental auto_vacuum")
	}
}

func TestCompactGuards(t *testing.T) {
	mem, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	if _, err := mem.Compact(context.Background()); err == nil {
		t.Fatal("compacting an in-memory database must be refused")
	}
}

func TestMigrationAddsPruneColumns(t *testing.T) {
	e := newRetEnv(t)
	for _, col := range []string{"history_pruned_at", "final_text"} {
		if _, err := e.s.db.Exec(`ALTER TABLE runs DROP COLUMN ` + col); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(e.s.db); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.GetRunFinalText("nope"); err == nil {
		t.Fatal("expected not found")
	}
	if _, err := e.s.db.Exec(`SELECT history_pruned_at, final_text FROM runs`); err != nil {
		t.Fatalf("columns missing after migrate: %v", err)
	}
}

// A marked run (even with rows left mid-prune) is invisible to the learning
// readers: they must not collect half a run, and a pruned run has nothing.
func TestLearningReadersSkipPrunedRuns(t *testing.T) {
	e := newRetEnv(t)
	task := e.task(t, true, false)
	chat, err := e.s.CreateChat(task, "c")
	if err != nil {
		t.Fatal(err)
	}
	mk := func(id string) {
		if _, err := e.s.CreateRun(Run{ID: id, TaskID: task, ChatID: chat.ID, Provider: "glm", Prompt: "p", Status: "done", Purpose: "user", StartedAt: time.Now().UTC().Add(-90 * 24 * time.Hour), FinishedAt: at(e.cutoff.Add(-time.Hour))}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if _, err := e.s.AppendRunEvent(id, int64(i), `{"type":0,"text":"x"}`); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("full")
	mk("mid-prune")
	if ok, err := e.s.MarkRunPruned("mid-prune", "t", time.Now(), e.cutoff); !ok || err != nil {
		t.Fatalf("mark: %v %v", ok, err)
	}
	if _, err := e.s.DeleteRunEventsBatch("mid-prune", 1); err != nil { // two rows left
		t.Fatal(err)
	}
	got, err := e.s.ListChatRunsWithEventsAfter(chat.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Run.ID != "full" {
		t.Fatalf("window runs = %v, want only the unpruned run", got)
	}
	rows, _, err := e.s.ListWorkspaceUserEvents(e.wsID, time.Now().Add(-365*24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.RunID == "mid-prune" {
			t.Fatal("events of a pruned run reached the prevalence scan")
		}
	}
}
