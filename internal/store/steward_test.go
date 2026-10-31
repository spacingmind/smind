package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// seedStewardTasks inserts bare workspace/task rows so steward_reviews'
// task_id foreign key has something to point at.
func seedStewardTasks(t *testing.T, s *Store, ids ...int64) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := s.db.Exec(`INSERT INTO workspaces (id, path, title, routing_policy, created_at, updated_at) VALUES (1, '/repo', 'repo', 'hard', ?, ?)`, now, now); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	for _, id := range ids {
		if _, err := s.db.Exec(`INSERT INTO tasks (id, workspace_id, title, status, created_at, updated_at) VALUES (?, 1, ?, 'done', ?, ?)`,
			id, fmt.Sprintf("task %d", id), now, now); err != nil {
			t.Fatalf("seed task %d: %v", id, err)
		}
	}
}

func TestStewardReviews_InsertAndGet(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	now := time.Now().UTC().Truncate(time.Second)

	got, err := s.InsertStewardReview(StewardReview{
		TaskID: 1, TriggerKind: "human", HeadSHA: "abc123", ContractSha256: "c1",
		ValidationID: 7, CIState: "green", Status: "pending",
	}, now)
	if err != nil {
		t.Fatalf("InsertStewardReview() error = %v", err)
	}
	if got.ID == 0 {
		t.Fatal("InsertStewardReview() did not assign an id")
	}
	if got.TaskID != 1 || got.TriggerKind != "human" || got.HeadSHA != "abc123" || got.ContractSha256 != "c1" ||
		got.ValidationID != 7 || got.CIState != "green" {
		t.Fatalf("stored review = %+v, want the inserted fields", got)
	}
	if got.Status != "pending" || got.Verdict != "" || got.BlockedReason != "" || got.FinishedAt != nil {
		t.Fatalf("open review = status %q verdict %q blocked %q finished %v, want pending/empty/nil", got.Status, got.Verdict, got.BlockedReason, got.FinishedAt)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}

	fetched, err := s.GetStewardReview(got.ID)
	if err != nil {
		t.Fatalf("GetStewardReview() error = %v", err)
	}
	if fetched != got {
		t.Fatalf("GetStewardReview() = %+v, want %+v", fetched, got)
	}
	if _, err := s.GetStewardReview(9999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetStewardReview(missing) error = %v, want sql.ErrNoRows", err)
	}

	if err := s.SetStewardReviewStatus(got.ID, "running"); err != nil {
		t.Fatalf("SetStewardReviewStatus(running) error = %v", err)
	}
	if err := s.SetStewardReviewRun(got.ID, "run-1", 42, "/scratch/r1"); err != nil {
		t.Fatalf("SetStewardReviewRun() error = %v", err)
	}
	if err := s.SetStewardReviewDigest(got.ID, `{"d":1}`, "dsha", "red", "psha", "c2", 9); err != nil {
		t.Fatalf("SetStewardReviewDigest() error = %v", err)
	}
	if err := s.SetStewardReviewCost(got.ID, 0.25); err != nil {
		t.Fatalf("SetStewardReviewCost() error = %v", err)
	}
	got, err = s.GetStewardReview(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "running" || got.RunID != "run-1" || got.StewardTaskID != 42 || got.ScratchDir != "/scratch/r1" {
		t.Fatalf("run fields = %+v", got)
	}
	if got.DigestJSON != `{"d":1}` || got.DigestSha256 != "dsha" || got.CIState != "red" || got.PolicySha != "psha" ||
		got.ContractSha256 != "c2" || got.ValidationID != 9 || got.CostUSD != 0.25 {
		t.Fatalf("digest/cost fields = %+v", got)
	}

	latest, err := s.LatestStewardReview(1)
	if err != nil || latest.ID != got.ID {
		t.Fatalf("LatestStewardReview() = id %d, %v; want %d", latest.ID, err, got.ID)
	}
	stale, err := s.ListStaleOpenStewardReviews()
	if err != nil || len(stale) != 1 || stale[0].ID != got.ID {
		t.Fatalf("ListStaleOpenStewardReviews() = %d rows, %v; want the one open row", len(stale), err)
	}

	if err := s.SetStewardReviewScopeViolation(got.ID); err != nil {
		t.Fatalf("SetStewardReviewScopeViolation() error = %v", err)
	}
	got, err = s.GetStewardReview(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ScopeViolation {
		t.Fatal("scope violation flag not stored")
	}

	long := strings.Repeat("é", 600) // 1200 bytes, must cut to 800 rune-safe
	if err := s.SetStewardReviewVerdict(got.ID, "ACCEPTED", "ACCEPTED", "[]", "[]", long, 1.5, now); err != nil {
		t.Fatalf("SetStewardReviewVerdict() error = %v", err)
	}
	got, err = s.GetStewardReview(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Summary) > stewardSummaryMax {
		t.Fatalf("summary length = %d bytes, want <= %d", len(got.Summary), stewardSummaryMax)
	}
	for _, r := range got.Summary {
		if r == 0xFFFD {
			t.Fatal("summary was cut mid-rune")
		}
	}
}

func TestStewardReviews_OneOpenPerTask(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1, 2)
	now := time.Now().UTC().Truncate(time.Second)

	first, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "auto", HeadSHA: "h1", Status: "running"}, now)
	if err != nil {
		t.Fatalf("first insert error = %v", err)
	}
	if _, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "human", HeadSHA: "h2", Status: "pending"}, now); !errors.Is(err, ErrStewardReviewOpen) {
		t.Fatalf("second insert error = %v, want ErrStewardReviewOpen", err)
	}
	other, err := s.InsertStewardReview(StewardReview{TaskID: 2, TriggerKind: "human", HeadSHA: "h1", Status: "pending"}, now)
	if err != nil {
		t.Fatalf("insert for another task error = %v, err = %v", other.ID, err)
	}

	if err := s.SetStewardReviewBlocked(first.ID, "timeout", "took too long", 0.5, now); err != nil {
		t.Fatalf("SetStewardReviewBlocked() error = %v", err)
	}
	again, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "auto", HeadSHA: "h3", Status: "pending"}, now)
	if err != nil {
		t.Fatalf("insert after finish error = %v", err)
	}
	if again.ID == first.ID {
		t.Fatal("insert after finish reused the finished row's id")
	}
}

func TestStewardReviews_VerdictWriteOnce(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	now := time.Now().UTC().Truncate(time.Second)

	r, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "auto", HeadSHA: "h1", Status: "pending"}, now)
	if err != nil {
		t.Fatalf("insert error = %v", err)
	}
	if err := s.SetStewardReviewVerdict(r.ID, "CHANGES", "CHANGES", `[{"f":1}]`, `[{"g":"ci"}]`, "needs work", 0.75, now); err != nil {
		t.Fatalf("SetStewardReviewVerdict() error = %v", err)
	}
	got, err := s.GetStewardReview(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SeatVerdict != "CHANGES" || got.Verdict != "CHANGES" || got.FindingsJSON != `[{"f":1}]` || got.GatesJSON != `[{"g":"ci"}]` ||
		got.Summary != "needs work" || got.CostUSD != 0.75 {
		t.Fatalf("verdict fields = %+v", got)
	}
	if got.Status != "done" || got.FinishedAt == nil || !got.FinishedAt.Equal(now) {
		t.Fatalf("finished review = status %q finished %v, want done at %v", got.Status, got.FinishedAt, now)
	}

	if err := s.SetStewardReviewVerdict(r.ID, "ACCEPTED", "ACCEPTED", "[]", "[]", "second", 1, now); !errors.Is(err, ErrStewardReviewClosed) {
		t.Fatalf("second verdict error = %v, want ErrStewardReviewClosed", err)
	}
	if err := s.SetStewardReviewBlocked(r.ID, "timeout", "late", 1, now); !errors.Is(err, ErrStewardReviewClosed) {
		t.Fatalf("blocked after done error = %v, want ErrStewardReviewClosed", err)
	}
	for name, err := range map[string]error{
		"run":    s.SetStewardReviewRun(r.ID, "run-2", 7, "/s"),
		"digest": s.SetStewardReviewDigest(r.ID, "{}", "d", "green", "p", "c", 1),
		"status": s.SetStewardReviewStatus(r.ID, "running"),
		"token":  s.SetStewardReviewToken(r.ID, "tok"),
		"cost":   s.SetStewardReviewCost(r.ID, 2),
	} {
		if !errors.Is(err, ErrStewardReviewClosed) {
			t.Errorf("%s after done = %v, want ErrStewardReviewClosed", name, err)
		}
	}
	got, err = s.GetStewardReview(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != "CHANGES" || got.Status != "done" {
		t.Fatalf("closed row was mutated: %+v", got)
	}
}

func TestStewardReviews_TokenSingleUse(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	now := time.Now().UTC().Truncate(time.Second)

	r, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "auto", HeadSHA: "h1", Status: "pending"}, now)
	if err != nil {
		t.Fatalf("insert error = %v", err)
	}
	if err := s.SetStewardReviewToken(r.ID, "sha256:abc"); err != nil {
		t.Fatalf("SetStewardReviewToken() error = %v", err)
	}
	if err := s.ConsumeStewardToken(r.ID); err != nil {
		t.Fatalf("first ConsumeStewardToken() error = %v", err)
	}
	if err := s.ConsumeStewardToken(r.ID); !errors.Is(err, ErrStewardTokenUsed) {
		t.Fatalf("second ConsumeStewardToken() error = %v, want ErrStewardTokenUsed", err)
	}
	got, err := s.GetStewardReview(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.TokenUsed || got.TokenHash != "sha256:abc" {
		t.Fatalf("token state = used %v hash %q, want true and the stored hash", got.TokenUsed, got.TokenHash)
	}
}

func TestStewardReviews_FindByKeyIgnoresBlocked(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1, 2)
	now := time.Now().UTC().Truncate(time.Second)

	crashed, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "auto", HeadSHA: "h1", Status: "pending"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStewardReviewDigest(crashed.ID, "{}", "d", "green", "p", "c1", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStewardReviewBlocked(crashed.ID, "crashed", "seat died", 0, now); err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "auto", HeadSHA: "h1", Status: "pending"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStewardReviewDigest(cancelled.ID, "{}", "d", "green", "p", "c1", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStewardReviewBlocked(cancelled.ID, "cancelled", "human backed out", 0, now); err != nil {
		t.Fatal(err)
	}
	kept, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "auto", HeadSHA: "h1", Status: "pending"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStewardReviewDigest(kept.ID, "{}", "d", "green", "p", "c1", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStewardReviewVerdict(kept.ID, "ACCEPTED", "ACCEPTED", "[]", "[]", "fine", 0.1, now); err != nil {
		t.Fatal(err)
	}

	got, err := s.FindStewardReviewByKey(1, "h1", "c1", "green")
	if err != nil {
		t.Fatalf("FindStewardReviewByKey() error = %v", err)
	}
	if got.ID != kept.ID {
		t.Fatalf("FindStewardReviewByKey() = id %d, want the finished row %d (the blocked ones must not match)", got.ID, kept.ID)
	}
	if _, err := s.FindStewardReviewByKey(1, "h1", "c1", "red"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("FindStewardReviewByKey(no match) error = %v, want sql.ErrNoRows", err)
	}

	// A task whose only review of the key is blocked (crashed) must not
	// block a retry of the same head: the lookup finds nothing.
	onlyBlocked, err := s.InsertStewardReview(StewardReview{TaskID: 2, TriggerKind: "auto", HeadSHA: "h2", Status: "pending"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStewardReviewDigest(onlyBlocked.ID, "{}", "d", "green", "p", "c1", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStewardReviewBlocked(onlyBlocked.ID, "crashed", "seat died again", 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindStewardReviewByKey(2, "h2", "c1", "green"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("FindStewardReviewByKey(only blocked rows) error = %v, want sql.ErrNoRows", err)
	}
}

func TestStewardReviews_CountsSinceAndPerTask(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1, 2)
	now := time.Now().UTC().Truncate(time.Second)

	old, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "auto", HeadSHA: "h1", Status: "pending"}, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStewardReviewVerdict(old.ID, "ACCEPTED", "ACCEPTED", "[]", "[]", "ok", 0.1, now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.InsertStewardReview(StewardReview{TaskID: 2, TriggerKind: "auto", HeadSHA: "h2", Status: "pending"}, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStewardReviewBlocked(cancelled.ID, "cancelled", "nope", 0, now.Add(-1*time.Hour)); err != nil {
		t.Fatal(err)
	}

	if n, err := s.CountStewardReviewsSince(now.Add(-90 * time.Minute)); err != nil || n != 0 {
		t.Fatalf("CountStewardReviewsSince(90m ago) = %d, %v; want 0 (only the cancelled row)", n, err)
	}
	if n, err := s.CountStewardReviewsSince(now.Add(-3 * time.Hour)); err != nil || n != 1 {
		t.Fatalf("CountStewardReviewsSince(3h ago) = %d, %v; want 1", n, err)
	}
	if n, err := s.CountStewardReviewsForTask(1); err != nil || n != 1 {
		t.Fatalf("CountStewardReviewsForTask(1) = %d, %v; want 1", n, err)
	}
	if n, err := s.CountStewardReviewsForTask(2); err != nil || n != 0 {
		t.Fatalf("CountStewardReviewsForTask(2) = %d, %v; want 0 (cancelled excluded)", n, err)
	}
}

// finishStewardRow appends one review in the given end state, oldest first,
// each on its own task so open rows never block each other. State is done or
// a blocked_reason.
func finishStewardRow(t *testing.T, s *Store, taskID int64, state string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	r, err := s.InsertStewardReview(StewardReview{TaskID: taskID, TriggerKind: "auto", HeadSHA: "h", Status: "pending"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if state == "done" {
		if err := s.SetStewardReviewVerdict(r.ID, "ACCEPTED", "ACCEPTED", "[]", "[]", "ok", 0, now); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := s.SetStewardReviewBlocked(r.ID, state, state, 0, now); err != nil {
		t.Fatal(err)
	}
}

func TestStewardReviews_ConsecutiveFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rows []string // end states, oldest first: done | crashed | timeout | budget | cancelled
		want int
	}{
		{"done then two crashes", []string{"done", "crashed", "timeout"}, 2},
		{"done in the middle", []string{"crashed", "done", "timeout"}, 1},
		{"done newest", []string{"crashed", "timeout", "done"}, 0},
		{"all failing", []string{"crashed", "timeout", "crashed"}, 3},
		{"cancelled skipped", []string{"cancelled", "crashed"}, 1},
		{"budget blocked stops the run", []string{"budget", "crashed"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			ids := make([]int64, len(tt.rows))
			for i := range ids {
				ids[i] = int64(i + 1)
			}
			seedStewardTasks(t, s, ids...)
			for i, state := range tt.rows {
				finishStewardRow(t, s, int64(i+1), state)
			}
			got, err := s.ConsecutiveStewardFailures()
			if err != nil {
				t.Fatalf("ConsecutiveStewardFailures() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ConsecutiveStewardFailures() = %d, want %d", got, tt.want)
			}
		})
	}

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	if n, err := s.ConsecutiveStewardFailures(); err != nil || n != 0 {
		t.Fatalf("empty table = %d, %v; want 0", n, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "auto", HeadSHA: "h", Status: "pending"}, now); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ConsecutiveStewardFailures(); err != nil || n != 0 {
		t.Fatalf("only an open row = %d, %v; want 0", n, err)
	}
}

func TestStewardReviews_ListNewestFirstAndLimit(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1, 2)
	now := time.Now().UTC().Truncate(time.Second)

	var ids []int64
	for i, task := range []int64{1, 2, 1, 1, 2} {
		r, err := s.InsertStewardReview(StewardReview{TaskID: task, TriggerKind: "auto", HeadSHA: fmt.Sprintf("h%d", i), Status: "pending"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetStewardReviewVerdict(r.ID, "ACCEPTED", "ACCEPTED", "[]", "[]", "ok", 0, now); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.ID)
	}

	got, err := s.ListStewardReviews(0, 0)
	if err != nil {
		t.Fatalf("ListStewardReviews(all, default) error = %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("ListStewardReviews(all, default) = %d rows, want 5", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].ID <= got[i].ID {
			t.Fatalf("rows not newest first: %d then %d", got[i-1].ID, got[i].ID)
		}
	}

	only1, err := s.ListStewardReviews(1, 2)
	if err != nil {
		t.Fatalf("ListStewardReviews(1, 2) error = %v", err)
	}
	if len(only1) != 2 || only1[0].ID != ids[3] || only1[1].ID != ids[2] {
		t.Fatalf("ListStewardReviews(1, 2) = %v, want the two newest of task 1", only1)
	}
	for _, r := range only1 {
		if r.TaskID != 1 {
			t.Fatalf("task filter leaked task %d", r.TaskID)
		}
	}

	if _, err := s.LatestStewardReview(2); err != nil {
		t.Fatalf("LatestStewardReview(2) error = %v", err)
	}
	if _, err := s.LatestStewardReview(999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("LatestStewardReview(missing) error = %v, want sql.ErrNoRows", err)
	}
}
