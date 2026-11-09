package runs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// genHistory builds a deterministic history of about size bytes: one text, then
// tool calls (input + result pairs, some with a heredoc command), then done.
func genHistory(seed int, size int) []Event {
	h := []Event{{Type: taskrunner.EventTypeUserMessage, Text: fmt.Sprintf("prompt %d", seed)}}
	chunk := strings.Repeat("x", 4096)
	for n := 0; size > 0; n++ {
		id := fmt.Sprintf("call-%d-%d", seed, n)
		cmd := fmt.Sprintf("echo %d", n)
		if n%3 == 0 {
			cmd = fmt.Sprintf("cat > f%d <<'EOF'\nbody %d\nEOF\necho done", n, n)
		}
		in, _ := json.Marshal(map[string]string{"command": cmd})
		res, _ := json.Marshal(chunk + fmt.Sprintf("#%d", n))
		h = append(h,
			Event{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolName: "Bash", ToolStatus: taskrunner.ToolStatusRunning, ToolInput: in},
			Event{Type: taskrunner.EventTypeToolCall, ToolCallID: id, ToolName: "Bash", ToolStatus: taskrunner.ToolStatusSuccess, ToolResult: res},
			Event{Type: taskrunner.EventTypeText, Text: fmt.Sprintf("step %d ", n)},
		)
		size -= len(chunk) + 200
	}
	return append(h,
		Event{Type: taskrunner.EventTypeText, Text: "all "},
		Event{Type: taskrunner.EventTypeText, Text: "done"},
		Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
}

// seedFinished stores a finished user run with hist as its recorded events.
func seedFinished(t testing.TB, st *store.Store, taskID int64, id string, started time.Time, hist []Event) {
	t.Helper()
	fin := started.Add(time.Second)
	if _, err := st.CreateRun(store.Run{ID: id, TaskID: taskID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: started, FinishedAt: &fin}); err != nil {
		t.Fatalf("CreateRun(%s): %v", id, err)
	}
	for i, e := range hist {
		data, err := encodeEvent(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AppendRunEvent(id, int64(i), data); err != nil {
			t.Fatalf("AppendRunEvent(%s,%d): %v", id, i, err)
		}
	}
}

// roundTrip is what the store hands back for hist: the restart view.
func roundTrip(t testing.TB, hist []Event) []Event {
	t.Helper()
	out := make([]Event, len(hist))
	for i, e := range hist {
		data, err := encodeEvent(e)
		if err != nil {
			t.Fatal(err)
		}
		if out[i], err = decodeEvent(data); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

type histEnv struct {
	st   *store.Store
	task store.Task
}

func newHistEnv(t *testing.T) *histEnv {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	return &histEnv{st: st, task: newTestTask(t, wm, "")}
}

func (e *histEnv) seed(t testing.TB, n, size int) map[string][]Event {
	t.Helper()
	want := map[string][]Event{}
	base := time.Now().Add(-time.Hour).UTC()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("seed-%03d", i)
		h := genHistory(i, size)
		seedFinished(t, e.st, e.task.ID, id, base.Add(time.Duration(i)*time.Second), h)
		want[id] = roundTrip(t, h)
	}
	return want
}

func residentRuns(reg *Registry) (n int) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, r := range reg.runs {
		if !r.histEvicted {
			n++
		}
	}
	return n
}

func heapAlloc() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestNewRehydratesMetadataOnly_HeapBounded(t *testing.T) {
	e := newHistEnv(t)
	// 200 runs x ~1 MB: a full load would add ~200 MB; metadata must stay far below.
	e.seed(t, 200, 1<<20)
	before := heapAlloc()
	reg, err := New(e.st)
	if err != nil {
		t.Fatal(err)
	}
	after := heapAlloc()
	var delta uint64
	if after > before {
		delta = after - before
	}
	if delta > 24<<20 {
		t.Fatalf("heap grew %d MB after New with 200 x 1 MB seeded runs, want < 24 MB (history must stay in the store)", delta>>20)
	}
	if n := len(reg.List(0)); n != 200 {
		t.Fatalf("runs listed = %d, want 200", n)
	}
	if n := residentRuns(reg); n != 0 {
		t.Fatalf("%d runs resident after New, want 0", n)
	}
	runtime.KeepAlive(reg)
}

func TestEvictedRunHistoryComplete(t *testing.T) {
	e := newHistEnv(t)
	want := e.seed(t, 6, 300<<10)
	reg := newTestRegistry(t, e.st)
	reg.SetHistoryBudget(1) // every history is "oversized": nothing stays resident
	for id, w := range want {
		got, _, err := reg.History(id)
		if err != nil {
			t.Fatalf("History(%s): %v", id, err)
		}
		if !reflect.DeepEqual(got, w) {
			t.Fatalf("History(%s) differs from the recorded events (%d vs %d events)", id, len(got), len(w))
		}
	}
	if n := residentRuns(reg); n != 0 {
		t.Fatalf("resident = %d with a 1-byte budget", n)
	}
}

func drain(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var out []Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, e)
		case <-timeout:
			t.Fatal("subscribe channel did not close")
		}
	}
}

func TestSubscribeEvictedRunBackfillsAll(t *testing.T) {
	e := newHistEnv(t)
	want := e.seed(t, 3, 200<<10)
	reg := newTestRegistry(t, e.st)
	for id, w := range want {
		ch, unsub, err := reg.Subscribe(id)
		if err != nil {
			t.Fatalf("Subscribe(%s): %v", id, err)
		}
		got := drain(t, ch)
		unsub()
		if !reflect.DeepEqual(got, w) {
			t.Fatalf("Subscribe(%s) delivered %d events, want %d identical", id, len(got), len(w))
		}
	}
	// A run past the metadata cap (not in reg.runs at all) is served too.
	reg.mu.Lock()
	delete(reg.runs, "seed-000")
	reg.mu.Unlock()
	ch, unsub, err := reg.Subscribe("seed-000")
	if err != nil {
		t.Fatalf("Subscribe(unlisted): %v", err)
	}
	defer unsub()
	if got := drain(t, ch); !reflect.DeepEqual(got, want["seed-000"]) {
		t.Fatalf("unlisted run backfill differs")
	}
}

func TestHistoryBudget_OldestFirstAndBounded(t *testing.T) {
	e := newHistEnv(t)
	want := e.seed(t, 8, 100<<10)
	reg := newTestRegistry(t, e.st)
	one := historyBytes(want["seed-000"])
	reg.SetHistoryBudget(one*3 + one/2) // room for 3 runs

	assertBounded := func() {
		t.Helper()
		st := reg.HistoryStats()
		if st.ResidentBytes > st.BudgetBytes {
			t.Fatalf("resident %d > budget %d", st.ResidentBytes, st.BudgetBytes)
		}
	}
	for i := 0; i < 8; i++ { // load oldest to newest: seed-000 is least recently used
		if _, _, err := reg.History(fmt.Sprintf("seed-%03d", i)); err != nil {
			t.Fatal(err)
		}
		assertBounded()
	}
	resident := func(id string) bool {
		reg.mu.Lock()
		defer reg.mu.Unlock()
		return !reg.runs[id].histEvicted
	}
	for i, wantRes := range []bool{false, false, false, false, false, true, true, true} {
		if got := resident(fmt.Sprintf("seed-%03d", i)); got != wantRes {
			t.Fatalf("seed-%03d resident = %v, want %v (oldest-used must go first)", i, got, wantRes)
		}
	}
	// Touching seed-005 makes it the most recent: the next load evicts seed-006.
	if _, _, err := reg.History("seed-005"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.History("seed-000"); err != nil {
		t.Fatal(err)
	}
	assertBounded()
	if resident("seed-006") || !resident("seed-005") || !resident("seed-000") {
		t.Fatalf("LRU order wrong after touching seed-005 and loading seed-000")
	}
}

func TestBudgetNeverEvictsRunningRun(t *testing.T) {
	e := newHistEnv(t)
	e.seed(t, 4, 50<<10)
	reg := newTestRegistry(t, e.st)

	now := time.Now().UTC()
	if _, err := e.st.CreateRun(store.Run{ID: "live", TaskID: e.task.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	live := &run{id: "live", taskID: e.task.ID, status: StatusRunning, startedAt: now, subscribers: map[int]*subQueue{}, pendingPermissions: map[string]*pendingPermission{}, closedCh: make(chan struct{})}
	reg.mu.Lock()
	reg.runs[live.id] = live
	reg.mu.Unlock()

	reg.SetHistoryBudget(150 << 10) // two seeded runs fit; the live run's history alone is far over it
	hist := genHistory(99, 300<<10)
	for _, ev := range hist {
		reg.record(live, ev)
	}
	for i := 0; i < 4; i++ { // churn the cache around it
		if _, _, err := reg.History(fmt.Sprintf("seed-%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := reg.History("live")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(hist) || live.histEvicted {
		t.Fatalf("running run lost history: %d of %d events, evicted=%v", len(got), len(hist), live.histEvicted)
	}
	if st := reg.HistoryStats(); st.RunningRuns != 1 || st.RunningHistBytes < 300<<10 {
		t.Fatalf("stats do not report the running run: %+v", st)
	}
	// Once it finishes it is subject to the budget like any other run.
	reg.flushPersist(live) // what finish does
	live.mu.Lock()
	live.status = StatusDone
	live.mu.Unlock()
	reg.retain("live")
	if !live.histEvicted {
		t.Fatal("a finished run over the budget stayed resident")
	}
	// The store holds the coalesced rows (persistbuf.go): same content, fewer events.
	if got, _, err := reg.History("live"); err != nil || fmt.Sprint(canon(t, got)) != fmt.Sprint(canon(t, hist)) {
		t.Fatalf("finished run history from the store: %d events, err %v", len(got), err)
	}
}

func TestOversizedRunServedFromStoreNotTruncated(t *testing.T) {
	e := newHistEnv(t)
	want := e.seed(t, 2, 400<<10)
	reg := newTestRegistry(t, e.st)
	reg.SetHistoryBudget(100 << 10)
	for i := 0; i < 3; i++ {
		got, _, err := reg.History("seed-001")
		if err != nil || !reflect.DeepEqual(got, want["seed-001"]) {
			t.Fatalf("read %d: err %v, equal=%v", i, err, reflect.DeepEqual(got, want["seed-001"]))
		}
	}
	if st := reg.HistoryStats(); st.ResidentRuns != 0 || st.ResidentBytes != 0 || st.StoreReads < 3 {
		t.Fatalf("oversized run should be re-read from the store each time: %+v", st)
	}
}

func TestConcurrentLoadsDeduped(t *testing.T) {
	e := newHistEnv(t)
	want := e.seed(t, 1, 200<<10)
	reg := newTestRegistry(t, e.st)
	reg.histLoadHook = func() { time.Sleep(100 * time.Millisecond) } // let every caller pile up behind one load
	var wg sync.WaitGroup
	errs := make(chan string, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, _, err := reg.History("seed-000")
			if err != nil || !reflect.DeepEqual(got, want["seed-000"]) {
				errs <- fmt.Sprintf("err %v", err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for m := range errs {
		t.Fatal(m)
	}
	st := reg.HistoryStats()
	if st.Loads != 1 || st.StoreReads != 1 {
		t.Fatalf("loads = %d, store reads = %d, want 1 and 1 (one store read shared by every caller)", st.Loads, st.StoreReads)
	}
	if st.ResidentBytes != historyBytes(want["seed-000"]) {
		t.Fatalf("resident bytes %d double counted, want %d", st.ResidentBytes, historyBytes(want["seed-000"]))
	}
}

func TestTrailingTextEvictedEqualsResident(t *testing.T) {
	e := newHistEnv(t)
	e.seed(t, 3, 300<<10)
	// A run whose last text run is longer than the first tail window.
	h := []Event{{Type: taskrunner.EventTypeUserMessage, Text: "go"}}
	for i := 0; i < 300; i++ {
		h = append(h, Event{Type: taskrunner.EventTypeText, Text: fmt.Sprintf("t%d ", i)})
	}
	h = append(h, Event{Type: taskrunner.EventTypeUsage}, Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
	seedFinished(t, e.st, e.task.ID, "long-text", time.Now().UTC(), h)

	resident := newTestRegistry(t, e.st)
	evicted := newTestRegistry(t, e.st)
	evicted.SetHistoryBudget(1)
	for _, id := range []string{"seed-000", "seed-002", "long-text"} {
		if _, _, err := resident.History(id); err != nil {
			t.Fatal(err)
		}
		a, err1 := resident.LastAgentText(id)
		b, err2 := evicted.LastAgentText(id)
		if err1 != nil || err2 != nil || a != b || a == "" {
			t.Fatalf("%s: LastAgentText resident %q (%v) vs evicted %q (%v)", id, a, err1, b, err2)
		}
		c, _ := resident.FinalAgentText(id)
		d, _ := evicted.FinalAgentText(id)
		if c != d || c == "" {
			t.Fatalf("%s: FinalAgentText resident %q vs evicted %q", id, c, d)
		}
	}
	if n := residentRuns(evicted); n != 0 {
		t.Fatalf("evicted registry holds %d runs", n)
	}
}

func TestHeredocRevealEquivalentResidentAndEvicted(t *testing.T) {
	e := newHistEnv(t)
	want := e.seed(t, 1, 200<<10)
	resident := newTestRegistry(t, e.st)
	evicted := newTestRegistry(t, e.st)
	evicted.SetHistoryBudget(1)
	if _, _, err := resident.History("seed-000"); err != nil {
		t.Fatal(err)
	}
	type key struct {
		call  string
		index int
	}
	var table []key
	for _, ev := range want["seed-000"] {
		if ev.Type == taskrunner.EventTypeToolCall {
			for _, idx := range []int{0, 1, -1} {
				table = append(table, key{ev.ToolCallID, idx})
			}
		}
	}
	table = append(table, key{"nope", 0})
	revealed := 0
	for _, k := range table {
		a, errA := resident.ToolCallHeredoc("seed-000", k.call, k.index)
		b, errB := evicted.ToolCallHeredoc("seed-000", k.call, k.index)
		if a != b || fmt.Sprint(errA) != fmt.Sprint(errB) {
			t.Fatalf("%v: resident (%+v, %v) != evicted (%+v, %v)", k, a, errA, b, errB)
		}
		if errA == nil && a.Text != "" {
			revealed++
		}
	}
	if revealed == 0 {
		t.Fatal("table never revealed a body: the equivalence check proved nothing")
	}
	if _, err := evicted.ToolCallHeredoc("missing-run", "x", 0); err == nil || err.Error() != ErrNoHeredoc.Error() {
		t.Fatalf("unknown run = %v, want ErrNoHeredoc", err)
	}
}

func TestRaceRecordFinishHistoryEvict(t *testing.T) {
	e := newHistEnv(t)
	e.seed(t, 6, 60<<10)
	reg := newTestRegistry(t, e.st)
	reg.SetHistoryBudget(150 << 10)
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				id := fmt.Sprintf("seed-%03d", (g+i)%6)
				if _, _, err := reg.History(id); err != nil {
					t.Error(err)
					return
				}
				if ch, unsub, err := reg.Subscribe(id); err == nil {
					drain(t, ch)
					unsub()
				}
				_, _ = reg.Tail(id, 10)
				_ = reg.HistoryStats()
			}
		}(g)
	}
	wg.Wait()
	if st := reg.HistoryStats(); st.ResidentBytes > st.BudgetBytes {
		t.Fatalf("resident %d > budget %d", st.ResidentBytes, st.BudgetBytes)
	}
}

func TestEventBytesTracksHeap(t *testing.T) {
	hist := genHistory(1, 4<<20)
	est := historyBytes(hist)
	if est < 4<<20 || est > 3*(4<<20) {
		t.Fatalf("estimate %d for a ~4 MB history is outside [1x, 3x]", est)
	}
}

// TestGenerateSyntheticDB writes the dogfood database used for the memory
// measurements (docs/plans/active/daemon-memory.md): SMIND_GEN_SYNTH_DB=<dir>
// SMIND_GEN_RUNS=200 SMIND_GEN_RUN_MB=4 go test ./internal/runs -run GenerateSyntheticDB.
// Skipped otherwise; it never touches a real home.
func TestGenerateSyntheticDB(t *testing.T) {
	dir := os.Getenv("SMIND_GEN_SYNTH_DB")
	if dir == "" {
		t.Skip("SMIND_GEN_SYNTH_DB not set")
	}
	runsN, _ := strconv.Atoi(os.Getenv("SMIND_GEN_RUNS"))
	mb, _ := strconv.Atoi(os.Getenv("SMIND_GEN_RUN_MB"))
	if runsN <= 0 || mb <= 0 {
		t.Fatal("set SMIND_GEN_RUNS and SMIND_GEN_RUN_MB")
	}
	st, err := store.Open(filepath.Join(dir, "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/synthetic/repo", Title: "synthetic", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "synthetic", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-48 * time.Hour).UTC()
	// SMIND_GEN_OLD_PCT: this percent of the runs are 60 days old and belong to
	// an archived task (retention measurements); the rest stay recent on an open task.
	oldPct, _ := strconv.Atoi(os.Getenv("SMIND_GEN_OLD_PCT"))
	var archived store.Task
	if oldPct > 0 {
		if archived, err = st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "archived", Status: "created"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ArchiveTask(archived.ID); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < runsN; i++ {
		taskID, start := tk.ID, base.Add(time.Duration(i)*time.Minute)
		if i*100 < oldPct*runsN {
			taskID, start = archived.ID, time.Now().Add(-60*24*time.Hour).UTC().Add(time.Duration(i)*time.Minute)
		}
		seedFinished(t, st, taskID, fmt.Sprintf("synth-%04d", i), start, genHistory(i, mb<<20))
	}
}
