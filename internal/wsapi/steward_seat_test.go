package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/store"
)

// stewardTestInputs is a minimal deterministic Inputs for the fake Collect;
// only the CI state varies between scenarios.
func stewardTestInputs(ci string) steward.Inputs {
	sha := strings.Repeat("a", 40)
	return steward.Inputs{
		Task:    steward.TaskInfo{ID: 1, Title: "task", Branch: "b", HeadSHA: sha, BaseSHA: strings.Repeat("0", 40)},
		Numstat: "1\t1\tmain.go",
		Commits: []steward.CommitInfo{{SHA: sha, Subject: "feat: one", HasAttribution: true}},
		CI:      steward.CIInfo{State: ci},
	}
}

// fakeStewardCollect is the host's Collect: it returns whatever Inputs it
// currently holds, so a test can flip the CI state under a live recheck.
type fakeStewardCollect struct {
	mu     sync.Mutex
	inputs steward.Inputs
}

func (f *fakeStewardCollect) set(ci string) {
	f.mu.Lock()
	f.inputs = stewardTestInputs(ci)
	f.mu.Unlock()
}

func (f *fakeStewardCollect) fn(ctx context.Context, taskID int64, sha string) (steward.Inputs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inputs, nil
}

// stewardHostEnv is a Lead task with a committed "hang" scenario (the seat's
// scratch worktree at that sha hangs too), a "steward" profile, live rails
// over the real store and a host wired to a fake Collect.
type stewardHostEnv struct {
	*identEnv
	host  *stewardHost
	svc   *steward.Service
	rails *steward.Rails
	coll  *fakeStewardCollect
	cfg   config.StewardConfig
	head  string
}

func newStewardHostEnv(t *testing.T, ci string) *stewardHostEnv {
	t.Helper()
	e := &stewardHostEnv{identEnv: newIdentEnv(t, "hang"), cfg: config.StewardConfig{Enabled: true}}
	wt := *e.seatTask.WorktreePath
	waitForFile(t, filepath.Join(wt, "session-init-method"))
	gitIn(t, wt, "add", "-f", "scenario")
	gitIn(t, wt, "commit", "-qm", "scenario")
	e.head = gitIn(t, wt, "rev-parse", "HEAD")
	if _, err := e.db.CreateAgentProfile(store.AgentProfile{Name: steward.SeatProfileName, Provider: "glm"}); err != nil {
		t.Fatal(err)
	}
	e.svc = &steward.Service{DB: e.db, ContractSha: func(int64) string { return "cs" }}
	e.rails = &steward.Rails{Store: e.db, Config: func() (config.StewardConfig, error) { return e.cfg, nil }}
	e.coll = &fakeStewardCollect{inputs: stewardTestInputs(ci)}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.host = newStewardHost(stewardHostOptions{
		Svc: e.svc, Rails: e.rails, Collect: e.coll.fn,
		WM: e.wm, DB: e.db, Reg: e.api.Runs, Runner: newTestRunner(e.wm), ProfReg: profiles.New(e.db),
		Ctx: ctx, WatchPeriod: 40 * time.Millisecond, RecheckPeriod: 20 * time.Millisecond,
	})
	return e
}

func (e *stewardHostEnv) start(t *testing.T, sha string) (reviewResult, error) {
	t.Helper()
	return e.host.startReview(context.Background(), reviewParams{TaskID: e.seatTask.ID, CommitSHA: sha, Trigger: steward.TriggerHuman})
}

func (e *stewardHostEnv) row(t *testing.T, id int64) store.StewardReview {
	t.Helper()
	row, err := e.db.GetStewardReview(id)
	if err != nil {
		t.Fatalf("get review %d: %v", id, err)
	}
	return row
}

func (e *stewardHostEnv) waitStatus(t *testing.T, id int64, status string) store.StewardReview {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if row, err := e.db.GetStewardReview(id); err == nil && row.Status == status {
			return row
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("review %d did not reach %q: %+v", id, status, e.row(t, id))
	return store.StewardReview{}
}

func (e *stewardHostEnv) waitSeatStarted(t *testing.T, id int64) store.StewardReview {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if row, err := e.db.GetStewardReview(id); err == nil && row.Status == "running" && row.RunID != "" && row.StewardTaskID != 0 {
			return row
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("review %d's seat did not start: %+v", id, e.row(t, id))
	return store.StewardReview{}
}

func (e *stewardHostEnv) endReview(t *testing.T, id int64) {
	t.Helper()
	if err := e.host.cancel(id); err != nil {
		t.Fatalf("cancel review %d: %v", id, err)
	}
}

func TestReview_CreatesScratchWorktreeAtSha(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	r, err := e.start(t, e.head)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "running" || r.Existing || r.RunID == "" || r.StewardTaskID == 0 || r.ChatID == 0 {
		t.Fatalf("result = %+v", r)
	}
	row := e.waitStatus(t, r.ReviewID, "running")
	st, err := e.db.GetTask(row.StewardTaskID)
	if err != nil || st.WorktreePath == nil {
		t.Fatalf("seat task = %+v (%v)", st, err)
	}
	if *st.WorktreePath == *e.seatTask.WorktreePath {
		t.Fatalf("the scratch worktree is the lead's own: %s", *st.WorktreePath)
	}
	if got := gitIn(t, *st.WorktreePath, "rev-parse", "HEAD"); got != e.head {
		t.Fatalf("scratch worktree at %s, want %s", got, e.head)
	}
	if st.ParentTaskID == nil || *st.ParentTaskID != e.seatTask.ID {
		t.Fatalf("seat task parent = %v, want the lead %d", st.ParentTaskID, e.seatTask.ID)
	}
	if row.TokenHash == "" || row.ScratchDir != *st.WorktreePath {
		t.Fatalf("row = %+v", row)
	}
	spec, ok := e.host.seatSource(st.ID)
	if !ok {
		t.Fatal("no seat spec for the seat task")
	}
	token := spec.Env[steward.EnvToken]
	if token == "" || steward.HashToken(token) != row.TokenHash {
		t.Fatalf("token does not match the stored hash: %q", token)
	}
	if strings.Contains(row.DigestJSON, token) || strings.Contains(row.Summary, token) {
		t.Fatal("the token appears in the review row")
	}
	e.endReview(t, r.ReviewID)
}

func TestReview_RefusesNonAncestorSha(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	_, err := e.start(t, strings.Repeat("0", 40))
	if r := wantErrCode(t, err, codeInvalid); !strings.Contains(r.Message, "not on the task branch") {
		t.Fatalf("message = %q", r.Message)
	}
	wantErrCode(t, func() error { _, err := e.start(t, "HEAD"); return err }(), codeInvalid)
	_, err = e.host.startReview(context.Background(), reviewParams{TaskID: e.seatTask.ID + 1000, Trigger: steward.TriggerHuman})
	wantErrCode(t, err, codeNotFound)
	if rows, _ := e.db.ListStewardReviews(e.seatTask.ID, 10); len(rows) != 0 {
		t.Fatalf("a refused request left reviews: %+v", rows)
	}
}

func TestReview_DefaultsToBranchHead(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	r, err := e.start(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.CommitSHA != e.head {
		t.Fatalf("commitSha = %s, want the head %s", r.CommitSHA, e.head)
	}
	row := e.waitStatus(t, r.ReviewID, "running")
	if row.HeadSHA != e.head {
		t.Fatalf("row head = %s, want %s", row.HeadSHA, e.head)
	}
	st, _ := e.db.GetTask(row.StewardTaskID)
	if got := gitIn(t, *st.WorktreePath, "rev-parse", "HEAD"); got != e.head {
		t.Fatalf("scratch worktree at %s, want %s", got, e.head)
	}
	e.endReview(t, r.ReviewID)
}

func TestReview_SingleFlightPerTask(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	r1, err := e.start(t, e.head)
	if err != nil {
		t.Fatal(err)
	}
	// A different CI state makes a different review key, so the request
	// reaches the insert and its one-open-review-per-task guard.
	e.coll.set("failure")
	_, err = e.start(t, e.head)
	if r := wantErrCode(t, err, codeInvalid); !strings.Contains(r.Message, "already has an open steward review") {
		t.Fatalf("message = %q", r.Message)
	}
	e.endReview(t, r1.ReviewID)
	r3, err := e.start(t, e.head)
	if err != nil {
		t.Fatalf("after cancel a new review must start: %v", err)
	}
	if r3.ReviewID == r1.ReviewID || r3.Status != "running" {
		t.Fatalf("third review = %+v", r3)
	}
	e.endReview(t, r3.ReviewID)
}

func TestReview_DuplicateReturnsExisting(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	r1, err := e.start(t, e.head)
	if err != nil {
		t.Fatal(err)
	}
	row := e.waitStatus(t, r1.ReviewID, "running")
	// End it with a verdict (the only way a review ends ACCEPTED).
	if err := e.db.SetStewardReviewVerdict(r1.ReviewID, "ACCEPTED", "ACCEPTED", "[]", "[]", "ok", row.CostUSD, time.Now()); err != nil {
		t.Fatal(err)
	}
	runsBefore, _ := e.db.ListRunsByTask(r1.StewardTaskID, 50)
	r2, err := e.start(t, e.head)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Existing || r2.ReviewID != r1.ReviewID || r2.Status != "done" {
		t.Fatalf("second request = %+v, want the existing done review %d", r2, r1.ReviewID)
	}
	if r2.RunID != "" || r2.StewardTaskID != 0 || r2.ChatID != 0 {
		t.Fatalf("a duplicate must start no seat: %+v", r2)
	}
	if rows, _ := e.db.ListStewardReviews(e.seatTask.ID, 10); len(rows) != 1 {
		t.Fatalf("a duplicate must open no row: %+v", rows)
	}
	runsAfter, _ := e.db.ListRunsByTask(r1.StewardTaskID, 50)
	if len(runsAfter) != len(runsBefore) {
		t.Fatalf("a duplicate started a run: %d runs, want %d", len(runsAfter), len(runsBefore))
	}
	_ = e.api.Runs.Stop(r1.RunID)
	waitRunEnd(t, e.api.Runs, r1.RunID)
}

func TestReview_RailsRefuse(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.cfg.Enabled = false
	if r := wantErrCode(t, func() error { _, err := e.start(t, e.head); return err }(), codeForbidden); !strings.Contains(r.Message, "off") {
		t.Fatalf("message = %q", r.Message)
	}
	e.cfg.Enabled = true
	e.cfg.MaxReviewsPerHour = 1
	// One done review of another key already spent the hourly cap.
	if _, err := e.db.InsertStewardReview(store.StewardReview{TaskID: e.seatTask.ID, TriggerKind: steward.TriggerHuman,
		HeadSHA: strings.Repeat("c", 40), ContractSha256: "cs", CIState: "success", Status: "done"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if r := wantErrCode(t, func() error { _, err := e.start(t, e.head); return err }(), codeInvalid); !strings.Contains(r.Message, "hourly") {
		t.Fatalf("message = %q", r.Message)
	}
	if rows, _ := e.db.ListStewardReviews(e.seatTask.ID, 10); len(rows) != 1 {
		t.Fatalf("a refused request opened a review: %+v", rows)
	}
}

func TestReview_CIPendingHoldsPendingAndStartsNoSeat(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "pending")
	r, err := e.start(t, e.head)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "pending" || r.RunID != "" || r.StewardTaskID != 0 || r.ChatID != 0 {
		t.Fatalf("result = %+v", r)
	}
	row := e.row(t, r.ReviewID)
	if row.Status != "pending" || row.StewardTaskID != 0 || row.RunID != "" || row.TokenHash != "" {
		t.Fatalf("row = %+v", row)
	}
	// CI turns green; the recheck starts the seat on its own.
	e.coll.set("success")
	row = e.waitSeatStarted(t, r.ReviewID)
	if row.TokenHash == "" {
		t.Fatalf("the seat did not start: %+v", row)
	}
	st, _ := e.db.GetTask(row.StewardTaskID)
	if got := gitIn(t, *st.WorktreePath, "rev-parse", "HEAD"); got != e.head {
		t.Fatalf("scratch worktree at %s, want %s", got, e.head)
	}
	if row.CIState != "success" {
		t.Fatalf("ciState = %q, want the rechecked success", row.CIState)
	}
	e.endReview(t, r.ReviewID)
}

func TestReview_PendingCancelledWhenHeadMoves(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "pending")
	r, err := e.start(t, e.head)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, *e.seatTask.WorktreePath, "commit", "-q", "--allow-empty", "-m", "moved")
	row := e.waitStatus(t, r.ReviewID, "blocked")
	if row.BlockedReason != "cancelled" || !strings.Contains(row.Summary, "head moved") {
		t.Fatalf("row = %+v", row)
	}
	if row.StewardTaskID != 0 || row.RunID != "" {
		t.Fatalf("a cancelled pending review must never have started a seat: %+v", row)
	}
}

func TestStewardSeatSource_EnvOnlyInSeatTask(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	r, err := e.start(t, e.head)
	if err != nil {
		t.Fatal(err)
	}
	row := e.waitStatus(t, r.ReviewID, "running")
	if _, ok := e.host.seatSource(e.seatTask.ID); ok {
		t.Fatal("the lead task must never get a seat spec")
	}
	if _, ok := e.host.seatSource(e.seatTask.ID + 1000); ok {
		t.Fatal("an unknown task must get no seat spec")
	}
	spec, ok := e.host.seatSource(row.StewardTaskID)
	if !ok {
		t.Fatal("no seat spec for the seat task")
	}
	if spec.Env[steward.EnvReviewID] != fmt.Sprint(r.ReviewID) || spec.Env[steward.EnvToken] == "" || len(spec.Env) != 2 {
		t.Fatalf("seat env = %v", spec.Env)
	}
	e.endReview(t, r.ReviewID)
	if _, ok := e.host.seatSource(row.StewardTaskID); ok {
		t.Fatal("a finished review must hand out no seat spec")
	}
}

func TestStewardSeatSource_FloorAndDisallowedTools(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	r, err := e.start(t, e.head)
	if err != nil {
		t.Fatal(err)
	}
	row := e.waitStatus(t, r.ReviewID, "running")
	spec, ok := e.host.seatSource(row.StewardTaskID)
	if !ok {
		t.Fatal("no seat spec")
	}
	if allow, _ := spec.Floor("Bash", nil); allow {
		t.Fatal("the floor must deny Bash")
	}
	if allow, reason := spec.Floor("Read", []string{filepath.Join(row.ScratchDir, "main.go")}); !allow {
		t.Fatalf("the floor must allow a read inside the scratch tree: %s", reason)
	}
	if allow, _ := spec.Floor("Read", []string{"/etc/passwd"}); allow {
		t.Fatal("the floor must deny a read outside the scratch tree")
	}
	for _, tool := range []string{"Bash", "Edit", "Write"} {
		found := false
		for _, d := range spec.DisallowedTools {
			if d == tool {
				found = true
			}
		}
		if !found {
			t.Fatalf("DisallowedTools %v must contain %s", spec.DisallowedTools, tool)
		}
	}
	spec.DisallowedTools[0] = "Mutated"
	spec2, _ := e.host.seatSource(row.StewardTaskID)
	if !reflect.DeepEqual(spec2.DisallowedTools, steward.SeatDisallowedTools) {
		t.Fatalf("DisallowedTools must be a copy: %v", spec2.DisallowedTools)
	}
	e.endReview(t, r.ReviewID)
}

func TestStewardHost_StartupClosesStaleOpenReviews(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	row, err := e.db.InsertStewardReview(store.StewardReview{TaskID: e.seatTask.ID, TriggerKind: steward.TriggerHuman,
		HeadSHA: e.head, ContractSha256: "cs", CIState: "success", Status: "running"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	newStewardHost(stewardHostOptions{Svc: e.svc, Rails: e.rails, Collect: e.coll.fn,
		WM: e.wm, DB: e.db, Reg: e.api.Runs, Runner: newTestRunner(e.wm), ProfReg: profiles.New(e.db),
		Ctx: context.Background(), WatchPeriod: 40 * time.Millisecond, RecheckPeriod: 20 * time.Millisecond})
	after := e.row(t, row.ID)
	if after.Status != "blocked" || after.BlockedReason != "crashed" || !strings.Contains(after.Summary, "daemon restarted") {
		t.Fatalf("stale row = %+v", after)
	}
}

func TestStewardCancel_StopsAndWritesCancelled(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	r, err := e.start(t, e.head)
	if err != nil {
		t.Fatal(err)
	}
	row := e.waitStatus(t, r.ReviewID, "running")
	if err := e.host.cancel(r.ReviewID); err != nil {
		t.Fatal(err)
	}
	row = e.waitStatus(t, r.ReviewID, "blocked")
	if row.BlockedReason != "cancelled" || !strings.Contains(row.Summary, "cancelled by the Human") {
		t.Fatalf("row = %+v", row)
	}
	waitRunEnd(t, e.api.Runs, r.RunID)
	if err := e.host.cancel(r.ReviewID); err == nil {
		t.Fatal("a second cancel must fail")
	}
	wantErrCode(t, e.host.cancel(r.ReviewID), codeInvalid)
	wantErrCode(t, e.host.cancel(99999), codeNotFound)
}

func TestStewardTokenNeverInPromptOrDigest(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	r, err := e.start(t, e.head)
	if err != nil {
		t.Fatal(err)
	}
	row := e.waitStatus(t, r.ReviewID, "running")
	spec, ok := e.host.seatSource(row.StewardTaskID)
	if !ok {
		t.Fatal("no seat spec")
	}
	token := spec.Env[steward.EnvToken]
	if token == "" {
		t.Fatal("the seat must carry a token")
	}
	run, err := e.db.GetRun(r.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(run.Prompt, token) {
		t.Fatal("the token appears in the seat's prompt")
	}
	if strings.Contains(row.DigestJSON, token) || strings.Contains(row.Summary, token) {
		t.Fatal("the token appears in the review row")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("the token appears in the review result")
	}
	if steward.HashToken(token) != row.TokenHash {
		t.Fatal("the stored hash does not match the token")
	}
	e.endReview(t, r.ReviewID)
}
