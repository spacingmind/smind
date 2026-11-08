package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// insertPassReview opens one review and returns it, so each pass test has a
// review row to hang pass rows off.
func insertPassReview(t *testing.T, s *Store) StewardReview {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	r, err := s.InsertStewardReview(StewardReview{TaskID: 1, TriggerKind: "auto", HeadSHA: "h1", Status: "running"}, now)
	if err != nil {
		t.Fatalf("InsertStewardReview() error = %v", err)
	}
	return r
}

func TestStore_PassRowsUniquePerAxis(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	r := insertPassReview(t, s)
	now := time.Now().UTC().Truncate(time.Second)

	got, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: StewardAxisStandards, Profile: "steward-standards", Model: "claude-haiku-5-5", Status: "pending"}, now)
	if err != nil {
		t.Fatalf("InsertStewardReviewPass(standards) error = %v", err)
	}
	if got.ID == 0 || got.ReviewID != r.ID || got.Axis != StewardAxisStandards || got.Status != "pending" ||
		got.Profile != "steward-standards" || got.Model != "claude-haiku-5-5" {
		t.Fatalf("stored pass = %+v, want the inserted fields", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}

	if _, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: StewardAxisStandards, Status: "pending"}, now); err == nil ||
		!strings.Contains(err.Error(), "insert steward review pass") {
		t.Fatalf("second insert for the same axis error = %v, want the wrapped UNIQUE failure", err)
	}

	spec, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: StewardAxisSpec, Profile: "steward", Model: "claude-sonnet-5-5", Status: "pending"}, now)
	if err != nil {
		t.Fatalf("InsertStewardReviewPass(spec) error = %v", err)
	}
	if spec.ID == got.ID {
		t.Fatal("the spec pass reused the standards row's id")
	}

	fetched, err := s.GetStewardReviewPass(r.ID, StewardAxisSpec)
	if err != nil || fetched != spec {
		t.Fatalf("GetStewardReviewPass() = %+v, %v; want %+v", fetched, err, spec)
	}
	if _, err := s.GetStewardReviewPass(r.ID+1, StewardAxisSpec); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetStewardReviewPass(missing) error = %v, want sql.ErrNoRows", err)
	}
}

func TestStore_ListPassesStandardsFirst(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	r := insertPassReview(t, s)
	now := time.Now().UTC().Truncate(time.Second)

	if got, err := s.ListStewardReviewPasses(r.ID); err != nil || len(got) != 0 || got == nil {
		t.Fatalf("ListStewardReviewPasses(no passes) = %v (nil %t), %v; want an empty, non-nil slice", got, got == nil, err)
	}

	if _, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: StewardAxisSpec, Status: "pending"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: StewardAxisStandards, Status: "pending"}, now); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListStewardReviewPasses(r.ID)
	if err != nil {
		t.Fatalf("ListStewardReviewPasses() error = %v", err)
	}
	if len(got) != 2 || got[0].Axis != StewardAxisStandards || got[1].Axis != StewardAxisSpec {
		t.Fatalf("passes = %v, want standards first then spec", got)
	}
}

func TestStore_PassVerdictWriteOnce(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	r := insertPassReview(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: StewardAxisSpec, Status: "running"}, now); err != nil {
		t.Fatal(err)
	}

	if err := s.SetStewardPassVerdict(r.ID, StewardAxisSpec, "CHANGES", "needs work", `[{"f":1}]`, `[{"id":"AC1"}]`, 0.75, now); err != nil {
		t.Fatalf("SetStewardPassVerdict() error = %v", err)
	}
	got, err := s.GetStewardReviewPass(r.ID, StewardAxisSpec)
	if err != nil {
		t.Fatal(err)
	}
	if got.SeatVerdict != "CHANGES" || got.Summary != "needs work" || got.FindingsJSON != `[{"f":1}]` || got.CriteriaJSON != `[{"id":"AC1"}]` || got.CostUSD != 0.75 {
		t.Fatalf("verdict fields = %+v", got)
	}
	if got.Status != "done" || got.FinishedAt == nil || !got.FinishedAt.Equal(now) {
		t.Fatalf("finished pass = status %q finished %v, want done at %v", got.Status, got.FinishedAt, now)
	}

	if err := s.SetStewardPassVerdict(r.ID, StewardAxisSpec, "ACCEPTED", "second", "[]", "[]", 1, now); !errors.Is(err, ErrStewardPassClosed) {
		t.Fatalf("second verdict error = %v, want ErrStewardPassClosed", err)
	}
	if err := s.SetStewardPassBlocked(r.ID, StewardAxisSpec, "timeout", "late", 1, now); !errors.Is(err, ErrStewardPassClosed) {
		t.Fatalf("blocked after verdict error = %v, want ErrStewardPassClosed", err)
	}
	if err := s.SetStewardPassSkipped(r.ID, StewardAxisSpec, "late", now); !errors.Is(err, ErrStewardPassClosed) {
		t.Fatalf("skipped after verdict error = %v, want ErrStewardPassClosed", err)
	}
	for name, err := range map[string]error{
		"run":    s.SetStewardPassRun(r.ID, StewardAxisSpec, "run-2", 7, "/s"),
		"status": s.SetStewardPassStatus(r.ID, StewardAxisSpec, "running"),
		"token":  s.SetStewardPassToken(r.ID, StewardAxisSpec, "tok"),
	} {
		if !errors.Is(err, ErrStewardPassClosed) {
			t.Errorf("%s after done = %v, want ErrStewardPassClosed", name, err)
		}
	}
	got, err = s.GetStewardReviewPass(r.ID, StewardAxisSpec)
	if err != nil {
		t.Fatal(err)
	}
	if got.SeatVerdict != "CHANGES" || got.Status != "done" {
		t.Fatalf("closed row was mutated: %+v", got)
	}
}

func TestStore_PassTokenSingleUse(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	r := insertPassReview(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	for _, axis := range []string{StewardAxisStandards, StewardAxisSpec} {
		if _, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: axis, Status: "pending"}, now); err != nil {
			t.Fatal(err)
		}
		if err := s.SetStewardPassToken(r.ID, axis, "sha256:"+axis); err != nil {
			t.Fatalf("SetStewardPassToken(%s) error = %v", axis, err)
		}
	}

	if err := s.ConsumeStewardPassToken(r.ID, StewardAxisStandards); err != nil {
		t.Fatalf("first ConsumeStewardPassToken(standards) error = %v", err)
	}
	if err := s.ConsumeStewardPassToken(r.ID, StewardAxisStandards); !errors.Is(err, ErrStewardTokenUsed) {
		t.Fatalf("second ConsumeStewardPassToken(standards) error = %v, want ErrStewardTokenUsed", err)
	}

	spec, err := s.GetStewardReviewPass(r.ID, StewardAxisSpec)
	if err != nil {
		t.Fatal(err)
	}
	if spec.TokenUsed || spec.TokenHash != "sha256:"+StewardAxisSpec {
		t.Fatalf("spec token = used %v hash %q, want the spec pass untouched by the standards consume", spec.TokenUsed, spec.TokenHash)
	}
	if err := s.ConsumeStewardPassToken(r.ID, StewardAxisSpec); err != nil {
		t.Fatalf("ConsumeStewardPassToken(spec) error = %v, want the spec token still usable", err)
	}
}

func TestStore_PassBlockedAndSkipped(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	r := insertPassReview(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: StewardAxisStandards, Status: "running"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: StewardAxisSpec, Status: "pending"}, now); err != nil {
		t.Fatal(err)
	}

	if err := s.SetStewardPassRun(r.ID, StewardAxisStandards, "run-1", 42, "/scratch/r1"); err != nil {
		t.Fatalf("SetStewardPassRun() error = %v", err)
	}
	if err := s.SetStewardPassStatus(r.ID, StewardAxisStandards, "running"); err != nil {
		t.Fatalf("SetStewardPassStatus() error = %v", err)
	}
	if err := s.SetStewardPassBlocked(r.ID, StewardAxisStandards, "timeout", "took too long", 0.4, now); err != nil {
		t.Fatalf("SetStewardPassBlocked() error = %v", err)
	}
	blocked, err := s.GetStewardReviewPass(r.ID, StewardAxisStandards)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != "blocked" || blocked.BlockedReason != "timeout" || blocked.Summary != "took too long" || blocked.SeatVerdict != "" || blocked.CostUSD != 0.4 {
		t.Fatalf("blocked pass = %+v", blocked)
	}
	if blocked.RunID != "run-1" || blocked.StewardTaskID != 42 || blocked.ScratchDir != "/scratch/r1" || blocked.Status != "blocked" {
		t.Fatalf("run fields lost on block: %+v", blocked)
	}
	if blocked.FinishedAt == nil || !blocked.FinishedAt.Equal(now) {
		t.Fatalf("finished = %v, want %v", blocked.FinishedAt, now)
	}

	if err := s.SetStewardPassSkipped(r.ID, StewardAxisSpec, "no spec available", now); err != nil {
		t.Fatalf("SetStewardPassSkipped() error = %v", err)
	}
	skipped, err := s.GetStewardReviewPass(r.ID, StewardAxisSpec)
	if err != nil {
		t.Fatal(err)
	}
	if skipped.Status != "skipped" || skipped.BlockedReason != "no_spec" || skipped.Summary != "no spec available" ||
		skipped.SeatVerdict != "" || skipped.CostUSD != 0 {
		t.Fatalf("skipped pass = %+v", skipped)
	}
	if skipped.FinishedAt == nil {
		t.Fatal("skipped pass has no finished_at")
	}
}

func TestStore_PassSummaryCapped(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	r := insertPassReview(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: StewardAxisSpec, Status: "pending"}, now); err != nil {
		t.Fatal(err)
	}

	long := strings.Repeat("é", 450) // 900 bytes, must cut to 800 rune-safe
	if err := s.SetStewardPassVerdict(r.ID, StewardAxisSpec, "CHANGES", long, "[]", "[]", 0, now); err != nil {
		t.Fatalf("SetStewardPassVerdict() error = %v", err)
	}
	got, err := s.GetStewardReviewPass(r.ID, StewardAxisSpec)
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

func TestStore_PassCostOnlyRises(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	seedStewardTasks(t, s, 1)
	r := insertPassReview(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := s.InsertStewardReviewPass(StewardReviewPass{ReviewID: r.ID, Axis: StewardAxisStandards, Status: "pending"}, now); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		cost float64
		want float64
	}{
		{0.5, 0.5},
		{0.2, 0.5}, // never falls
		{0.9, 0.9},
	} {
		if err := s.RaiseStewardPassCost(r.ID, StewardAxisStandards, c.cost); err != nil {
			t.Fatalf("RaiseStewardPassCost(%v) error = %v", c.cost, err)
		}
		got, err := s.GetStewardReviewPass(r.ID, StewardAxisStandards)
		if err != nil {
			t.Fatal(err)
		}
		if got.CostUSD != c.want {
			t.Fatalf("cost after raise to %v = %v, want %v", c.cost, got.CostUSD, c.want)
		}
	}

	// Any status: the usage row lands after the pass is done.
	if err := s.SetStewardPassVerdict(r.ID, StewardAxisStandards, "ACCEPTED", "ok", "[]", "[]", 0.9, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RaiseStewardPassCost(r.ID, StewardAxisStandards, 0.95); err != nil {
		t.Fatalf("RaiseStewardPassCost(after done) error = %v", err)
	}
	got, err := s.GetStewardReviewPass(r.ID, StewardAxisStandards)
	if err != nil {
		t.Fatal(err)
	}
	if got.CostUSD != 0.95 {
		t.Fatalf("cost after done = %v, want 0.95", got.CostUSD)
	}
}
