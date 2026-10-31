package steward

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/validation"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return &Service{DB: s, Now: func() time.Time { return time.Now().UTC() }}
}

func seedTask(t *testing.T, svc *Service) int64 {
	t.Helper()
	ws, err := svc.DB.CreateWorkspace(store.Workspace{Path: t.TempDir(), Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := svc.DB.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "task"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return task.ID
}

// openReview inserts a running review bound to a seat run, with its digest
// and a fresh token, and returns the row and the token.
func openReview(t *testing.T, svc *Service, digest string) (store.StewardReview, string) {
	t.Helper()
	taskID := seedTask(t, svc)
	token, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}
	row, err := svc.DB.InsertStewardReview(store.StewardReview{
		TaskID: taskID, TriggerKind: TriggerHuman, HeadSHA: "head1", ContractSha256: "c1",
		CIState: "green", DigestJSON: digest, Status: "running", TokenHash: HashToken(token),
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("InsertStewardReview() error = %v", err)
	}
	if err := svc.DB.SetStewardReviewRun(row.ID, "seat-run-1", taskID, "/scratch/s1"); err != nil {
		t.Fatalf("SetStewardReviewRun() error = %v", err)
	}
	row, err = svc.DB.GetStewardReview(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Fresh by default: the seeded head and contract are still current.
	svc.Head = func(int64) (string, error) { return row.HeadSHA, nil }
	svc.ContractSha = func(int64) string { return row.ContractSha256 }
	return row, token
}

func digestWithGates(gates ...Gate) string {
	b, err := json.Marshal(Digest{V: 1, Gates: gates})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func verdictRaw(reviewID int64, verdict string, ackIDs ...string) []byte {
	acks := make([]GateAck, len(ackIDs))
	for i, id := range ackIDs {
		acks[i] = GateAck{ID: id, Ack: true}
	}
	b, err := json.Marshal(SeatVerdict{V: 1, ReviewID: reviewID, Verdict: verdict, Summary: "judged from the digest", Gates: acks})
	if err != nil {
		panic(err)
	}
	return b
}

func mustBeOpen(t *testing.T, svc *Service, id int64, why string) {
	t.Helper()
	row, err := svc.DB.GetStewardReview(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "pending" && row.Status != "running" {
		t.Fatalf("%s: status = %q, want an open review", why, row.Status)
	}
	if row.Verdict != "" {
		t.Fatalf("%s: verdict = %q, want none stored", why, row.Verdict)
	}
	if row.TokenUsed {
		t.Fatalf("%s: token already consumed", why)
	}
}

func TestVerdictSubmit_RequiresToken(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	row, _ := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
	raw := verdictRaw(row.ID, VerdictAccepted, "ci")

	if _, err := svc.SubmitVerdict(row.ID, "", row.RunID, raw); !errors.Is(err, ErrBadToken) {
		t.Fatalf("SubmitVerdict(empty token) error = %v, want ErrBadToken", err)
	}
	if _, err := svc.SubmitVerdict(row.ID, "not-the-token", row.RunID, raw); !errors.Is(err, ErrBadToken) {
		t.Fatalf("SubmitVerdict(wrong token) error = %v, want ErrBadToken", err)
	}
	mustBeOpen(t, svc, row.ID, "after bad tokens")

	got, err := svc.SubmitVerdict(row.ID, "not-the-token", row.RunID, raw)
	if err == nil || got.Verdict != "" {
		t.Fatalf("SubmitVerdict(wrong token) = %+v, %v; want no view", got, err)
	}
}

func TestVerdictSubmit_TokenOnceOnly(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	var published []string
	svc.Publish = func(taskID, reviewID int64, status string) { published = append(published, status) }
	row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))

	v, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci"))
	if err != nil {
		t.Fatalf("SubmitVerdict() error = %v", err)
	}
	if v.Verdict != VerdictAccepted || v.Status != "done" || v.SeatVerdict != VerdictAccepted || !v.MergeReady {
		t.Fatalf("first submit = %+v, want a fresh ACCEPTED done review", v)
	}
	if len(published) == 0 || published[len(published)-1] != "done" {
		t.Fatalf("published = %v, want a done event", published)
	}

	if _, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci")); !errors.Is(err, ErrVerdictExists) {
		t.Fatalf("second SubmitVerdict() error = %v, want ErrVerdictExists", err)
	}
	if err := svc.DB.ConsumeStewardToken(row.ID); !errors.Is(err, store.ErrStewardTokenUsed) {
		t.Fatalf("ConsumeStewardToken() again error = %v, want ErrStewardTokenUsed", err)
	}
	stored, err := svc.DB.GetStewardReview(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Verdict != VerdictAccepted || stored.Status != "done" || !stored.TokenUsed {
		t.Fatalf("stored row = %+v, want a done ACCEPTED review with a used token", stored)
	}
}

func TestVerdictSubmit_WrongRunRefused(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
	raw := verdictRaw(row.ID, VerdictAccepted, "ci")

	if _, err := svc.SubmitVerdict(row.ID, token, "someone-elses-run", raw); !errors.Is(err, ErrWrongRun) {
		t.Fatalf("SubmitVerdict(other run) error = %v, want ErrWrongRun", err)
	}
	if _, err := svc.SubmitVerdict(row.ID, token, "", raw); !errors.Is(err, ErrWrongRun) {
		t.Fatalf("SubmitVerdict(no run) error = %v, want ErrWrongRun", err)
	}
	mustBeOpen(t, svc, row.ID, "after a refused run")

	if _, err := svc.SubmitVerdict(row.ID, token, row.RunID, raw); err != nil {
		t.Fatalf("SubmitVerdict(bound run) error = %v", err)
	}

	// A different run of the same steward task is still the seat (a
	// rate-limit wait re-queues the prompt as a new run).
	requed, requedToken := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
	if _, err := svc.DB.CreateRun(store.Run{ID: "seat-run-requeued", TaskID: requed.StewardTaskID, Status: "completed", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if _, err := svc.SubmitVerdict(requed.ID, requedToken, "seat-run-requeued", verdictRaw(requed.ID, VerdictAccepted, "ci")); err != nil {
		t.Fatalf("SubmitVerdict(re-queued run of the steward task) error = %v", err)
	}

	other, otherToken := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
	if other.StewardTaskID == row.StewardTaskID || other.StewardTaskID == requed.StewardTaskID {
		t.Fatal("the reviews share a steward task; the test needs distinct ones")
	}
	// A run of another review's task is not this review's seat.
	if _, err := svc.DB.CreateRun(store.Run{ID: "first-tasks-run", TaskID: row.StewardTaskID, Status: "completed", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if _, err := svc.SubmitVerdict(other.ID, otherToken, "first-tasks-run", verdictRaw(other.ID, VerdictAccepted, "ci")); !errors.Is(err, ErrWrongRun) {
		t.Fatalf("SubmitVerdict(run of another task) error = %v, want ErrWrongRun", err)
	}
}

func TestVerdictSubmit_IncompleteDoesNotConsumeToken(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))

	_, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted))
	if err == nil || !strings.Contains(err.Error(), "gate ci not acknowledged") {
		t.Fatalf("SubmitVerdict(missing ack) error = %v, want a VerdictError naming gate ci", err)
	}
	var ve VerdictError
	if !errors.As(err, &ve) || ve.Field != "gates" {
		t.Fatalf("SubmitVerdict(missing ack) error = %v, want a VerdictError on gates", err)
	}
	mustBeOpen(t, svc, row.ID, "after an incomplete verdict")

	if _, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci")); err != nil {
		t.Fatalf("SubmitVerdict(complete) error = %v", err)
	}
	stored, err := svc.DB.GetStewardReview(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Verdict != VerdictAccepted || stored.Status != "done" {
		t.Fatalf("stored row = %+v, want the resubmitted verdict stored", stored)
	}
}

func TestVerdictSubmit_ClampStoresMinimum(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GateFail, Detail: "ci red"}))

	v, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci"))
	if err != nil {
		t.Fatalf("SubmitVerdict() error = %v", err)
	}
	if v.SeatVerdict != VerdictAccepted {
		t.Fatalf("seatVerdict = %q, want ACCEPTED as submitted", v.SeatVerdict)
	}
	if v.Verdict != VerdictChanges {
		t.Fatalf("verdict = %q, want CHANGES (the failing ci gate clamps it down)", v.Verdict)
	}
	if !strings.Contains(v.Summary, "clamped to "+VerdictChanges) {
		t.Fatalf("summary = %q, want it to mention the clamp", v.Summary)
	}
	if v.MergeReady || v.NotReadyReason != "not accepted" {
		t.Fatalf("mergeReady = %v (%q), want false with the verdict reason", v.MergeReady, v.NotReadyReason)
	}
	if len(v.Gates) != 1 || v.Gates[0].ID != "ci" || v.Gates[0].State != GateFail {
		t.Fatalf("gates = %+v, want the digest's ci gate", v.Gates)
	}
}

func TestVerdictSubmit_HoldWhenGateUnknown(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GateUnknown}))

	_, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci"))
	if err == nil || !strings.Contains(err.Error(), "a gate is still unknown") {
		t.Fatalf("SubmitVerdict(unknown gate) error = %v, want the hold error", err)
	}
	mustBeOpen(t, svc, row.ID, "while a gate is unknown")
}

func TestVerdictSubmit_ReviewIDMismatch(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))

	_, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID+1, VerdictAccepted, "ci"))
	var ve VerdictError
	if !errors.As(err, &ve) || ve.Field != "reviewId" {
		t.Fatalf("SubmitVerdict(other review) error = %v, want a VerdictError on reviewId", err)
	}
	if !strings.Contains(err.Error(), "must judge review") {
		t.Fatalf("SubmitVerdict(other review) error = %v, want it to name the review", err)
	}
	mustBeOpen(t, svc, row.ID, "after a reviewId mismatch")
}

func TestWriteBlocked_OnlyDaemonReasons(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	for _, reason := range DaemonBlockedReasons {
		row, _ := openReview(t, svc, digestWithGates())
		if err := svc.WriteBlocked(row.ID, reason, "daemon ended it", 0.5); err != nil {
			t.Fatalf("WriteBlocked(%q) error = %v", reason, err)
		}
		stored, err := svc.DB.GetStewardReview(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Verdict != VerdictBlocked || stored.Status != "blocked" || stored.BlockedReason != reason {
			t.Fatalf("WriteBlocked(%q) row = %+v, want BLOCKED/blocked/%q", reason, stored, reason)
		}
		if err := svc.WriteBlocked(row.ID, reason, "again", 1); !errors.Is(err, ErrVerdictExists) {
			t.Fatalf("second WriteBlocked(%q) error = %v, want ErrVerdictExists", reason, err)
		}
	}

	for _, reason := range []string{"ACCEPTED", "stale", ""} {
		row, _ := openReview(t, svc, digestWithGates())
		err := svc.WriteBlocked(row.ID, reason, "nope", 0)
		if err == nil || !strings.Contains(err.Error(), "may only write blocked reviews") {
			t.Fatalf("WriteBlocked(%q) error = %v, want the daemon-reasons refusal", reason, err)
		}
		mustBeOpen(t, svc, row.ID, "after a refused reason "+reason)
	}
}

func TestTimeoutBudgetCrashWriteBlocked(t *testing.T) {
	t.Parallel()

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		svc := newTestService(t)
		row, _ := openReview(t, svc, digestWithGates())
		w := &Watch{Svc: svc, ReviewID: row.ID, StewardTaskID: row.StewardTaskID, MaxMinutes: 20,
			Probe: func(int64) validation.ProbeState {
				return validation.ProbeState{Running: true, Started: true, CostUSD: 0.25}
			}}
		if w.Tick(10 * time.Minute) {
			t.Fatal("Tick(10m of 20m) = done, want still running")
		}
		if !w.Tick(10 * time.Minute) {
			t.Fatal("Tick(20m of 20m) = not done, want the timeout write")
		}
		stored, err := svc.DB.GetStewardReview(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != "blocked" || stored.BlockedReason != "timeout" || stored.Verdict != VerdictBlocked {
			t.Fatalf("row = %+v, want blocked/timeout/BLOCKED", stored)
		}
		if stored.CostUSD != 0.25 {
			t.Fatalf("cost = %v, want the probed cost recorded", stored.CostUSD)
		}
	})

	t.Run("budget", func(t *testing.T) {
		t.Parallel()
		svc := newTestService(t)
		row, _ := openReview(t, svc, digestWithGates())
		w := &Watch{Svc: svc, ReviewID: row.ID, StewardTaskID: row.StewardTaskID, MaxMinutes: 60, BudgetUSD: 1,
			Probe: func(int64) validation.ProbeState {
				return validation.ProbeState{Running: true, Started: true, CostUSD: 1.5}
			}}
		if !w.Tick(time.Minute) {
			t.Fatal("Tick(over budget) = not done, want the budget write")
		}
		stored, err := svc.DB.GetStewardReview(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != "blocked" || stored.BlockedReason != "budget" || stored.Verdict != VerdictBlocked {
			t.Fatalf("row = %+v, want blocked/budget/BLOCKED", stored)
		}
	})

	t.Run("crashed", func(t *testing.T) {
		t.Parallel()
		svc := newTestService(t)
		row, _ := openReview(t, svc, digestWithGates())
		ps := validation.ProbeState{Running: true, Started: true}
		w := &Watch{Svc: svc, ReviewID: row.ID, StewardTaskID: row.StewardTaskID, MaxMinutes: 60,
			Probe: func(int64) validation.ProbeState { return ps }}
		if w.Tick(time.Minute) {
			t.Fatal("Tick(running) = done, want still running")
		}
		ps = validation.ProbeState{Started: true}
		if !w.Tick(time.Minute) {
			t.Fatal("Tick(run ended, no verdict) = not done, want the crashed write")
		}
		stored, err := svc.DB.GetStewardReview(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != "blocked" || stored.BlockedReason != "crashed" || stored.Verdict != VerdictBlocked {
			t.Fatalf("row = %+v, want blocked/crashed/BLOCKED", stored)
		}
	})
}

func TestRateLimitWaitDoesNotConsumeWallClock(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	row, _ := openReview(t, svc, digestWithGates())
	w := &Watch{Svc: svc, ReviewID: row.ID, StewardTaskID: row.StewardTaskID, MaxMinutes: 20,
		Probe: func(int64) validation.ProbeState { return validation.ProbeState{Queued: true} }}
	for i := 0; i < 6; i++ {
		if w.Tick(10 * time.Minute) {
			t.Fatalf("queued tick %d = done, want the wait to keep the review open", i)
		}
	}
	mustBeOpen(t, svc, row.ID, "after an hour of queued ticks")

	probe := validation.ProbeState{Running: true, Started: true}
	w.Probe = func(int64) validation.ProbeState { return probe }
	for i := 1; i <= 3; i++ {
		if w.Tick(5 * time.Minute) {
			t.Fatalf("running tick %d = done, want only 15m of 20m counted", i)
		}
	}
	mustBeOpen(t, svc, row.ID, "after 15m of running")
	if !w.Tick(5 * time.Minute) {
		t.Fatal("Tick(20m running) = not done, want the timeout write")
	}
	stored, err := svc.DB.GetStewardReview(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.BlockedReason != "timeout" {
		t.Fatalf("blockedReason = %q, want timeout", stored.BlockedReason)
	}
}

func TestPostCheck_LeadWorktreeChangedIsScopeViolation(t *testing.T) {
	t.Parallel()

	before := validation.Snapshot{Head: "h1", Status: ""}
	after := func() (validation.Snapshot, error) {
		return validation.Snapshot{Head: "h2", Status: ""}, nil
	}

	t.Run("while open", func(t *testing.T) {
		t.Parallel()
		svc := newTestService(t)
		row, _ := openReview(t, svc, digestWithGates())
		ps := validation.ProbeState{Running: true, Started: true}
		w := &Watch{Svc: svc, ReviewID: row.ID, StewardTaskID: row.StewardTaskID, MaxMinutes: 60,
			Probe: func(int64) validation.ProbeState { return ps }, LeadSnapshot: after, Before: before}
		if w.Tick(time.Minute) {
			t.Fatal("Tick(running) = done, want still running")
		}
		ps = validation.ProbeState{Started: true}
		if !w.Tick(time.Minute) {
			t.Fatal("Tick(run ended, lead changed) = not done, want the scope-violation write")
		}
		stored, err := svc.DB.GetStewardReview(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != "blocked" || stored.BlockedReason != "scope_violation" || stored.Verdict != VerdictBlocked {
			t.Fatalf("row = %+v, want blocked/scope_violation/BLOCKED", stored)
		}
	})

	t.Run("after a done verdict", func(t *testing.T) {
		t.Parallel()
		svc := newTestService(t)
		row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
		svc.Head = func(int64) (string, error) { return row.HeadSHA, nil }
		svc.ContractSha = func(int64) string { return row.ContractSha256 }
		if _, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci")); err != nil {
			t.Fatalf("SubmitVerdict() error = %v", err)
		}
		if v, err := svc.View(row.ID); err != nil || !v.MergeReady {
			t.Fatalf("View(before post-check) = %+v, %v; want merge ready", v, err)
		}
		w := &Watch{Svc: svc, ReviewID: row.ID, StewardTaskID: row.StewardTaskID, MaxMinutes: 60,
			Probe:        func(int64) validation.ProbeState { return validation.ProbeState{Started: true} },
			LeadSnapshot: after, Before: before}
		if !w.Tick(time.Minute) {
			t.Fatal("Tick(done review, run ended) = not done, want the post-check to finish it")
		}
		v, err := svc.View(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !v.ScopeViolation {
			t.Fatal("scope violation not flagged on the done review")
		}
		if v.MergeReady || v.NotReadyReason != "scope violation" {
			t.Fatalf("mergeReady = %v (%q), want false with the scope-violation reason", v.MergeReady, v.NotReadyReason)
		}
	})
}

// The clamp_test.go test of the same name covers Clamp's purity; this one
// covers the service's write paths.
func TestServiceDaemonNeverWritesAccepted(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	for _, reason := range DaemonBlockedReasons {
		row, _ := openReview(t, svc, digestWithGates())
		if err := svc.WriteBlocked(row.ID, reason, reason, 0.25); err != nil {
			t.Fatalf("WriteBlocked(%q) error = %v", reason, err)
		}
		if err := svc.FlagScopeViolation(row.ID); err != nil {
			t.Fatalf("FlagScopeViolation() error = %v", err)
		}
		v, err := svc.View(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if v.Verdict == VerdictAccepted || v.MergeReady {
			t.Fatalf("WriteBlocked(%q) view = %+v, want a review the daemon can never make merge-ready", reason, v)
		}
		if v.Verdict != VerdictBlocked {
			t.Fatalf("WriteBlocked(%q) verdict = %q, want BLOCKED", reason, v.Verdict)
		}
	}
}

func TestView_StaleAndMergeReady(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
	if _, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci")); err != nil {
		t.Fatalf("SubmitVerdict() error = %v", err)
	}

	svc.Head = func(int64) (string, error) { return row.HeadSHA, nil }
	svc.ContractSha = func(int64) string { return row.ContractSha256 }
	v, err := svc.View(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Stale != "" || !v.MergeReady || v.NotReadyReason != "" {
		t.Fatalf("fresh view = stale %q mergeReady %v (%q), want fresh and merge ready", v.Stale, v.MergeReady, v.NotReadyReason)
	}

	svc.Head = func(int64) (string, error) { return "moved-on", nil }
	if v, _ := svc.View(row.ID); v.Stale != StaleHeadMoved || v.MergeReady || v.NotReadyReason != StaleHeadMoved {
		t.Fatalf("moved head view = stale %q mergeReady %v (%q), want head moved and not ready", v.Stale, v.MergeReady, v.NotReadyReason)
	}

	svc.Head = func(int64) (string, error) { return "", errors.New("unknown") }
	svc.ContractSha = func(int64) string { return "c2" }
	if v, _ := svc.View(row.ID); v.Stale != StaleContractChanged || v.MergeReady {
		t.Fatalf("changed contract view = stale %q mergeReady %v, want contract changed and not ready", v.Stale, v.MergeReady)
	}

	svc.Head = nil
	svc.ContractSha = func(int64) string { return row.ContractSha256 }
	newer, err := svc.DB.InsertStewardReview(store.StewardReview{
		TaskID: row.TaskID, TriggerKind: TriggerAuto, HeadSHA: row.HeadSHA, ContractSha256: row.ContractSha256, Status: "pending",
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("InsertStewardReview(newer) error = %v", err)
	}
	if v, _ := svc.View(row.ID); v.Stale != StaleSuperseded || v.MergeReady || v.NotReadyReason != StaleSuperseded {
		t.Fatalf("superseded view = stale %q mergeReady %v (%q), want superseded and not ready", v.Stale, v.MergeReady, v.NotReadyReason)
	}
	list, err := svc.List(row.TaskID, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != newer.ID || list[1].ID != row.ID {
		t.Fatalf("List() = %d rows (%v), want the newer review first", len(list), list)
	}
	if list[0].Stale != "" || list[1].Stale != StaleSuperseded {
		t.Fatalf("List staleness = %q / %q, want fresh newest and superseded oldest", list[0].Stale, list[1].Stale)
	}
}

func TestView_HidesTokenAndDigest(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	const marker = "SECRETDIGESTMARKER"
	row, token := openReview(t, svc, `{"v":1,"task":{"title":"`+marker+`"},"gates":[{"id":"ci","state":"pass"}]}`)
	if _, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci")); err != nil {
		t.Fatalf("SubmitVerdict() error = %v", err)
	}
	v, err := svc.View(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, leak := range []string{row.TokenHash, marker, "digestJson", token} {
		if strings.Contains(text, leak) {
			t.Errorf("view JSON leaks %q: %s", leak, text)
		}
	}
	digest, err := svc.DigestJSON(row.ID)
	if err != nil {
		t.Fatalf("DigestJSON() error = %v", err)
	}
	if !strings.Contains(string(digest), marker) {
		t.Fatalf("DigestJSON() = %s, want the stored digest verbatim", digest)
	}
	if _, err := svc.DigestJSON(9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DigestJSON(missing) error = %v, want ErrNotFound", err)
	}
}
