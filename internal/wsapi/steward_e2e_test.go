package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// The fake-LLM end-to-end steward tests (S4): a review opens through the
// human RPC, its seat reads the digest and submits its verdict through the
// seat RPCs, and the result lands in the view, the inbox and the rails.

// wireHeadAndContract points the env's service at the real branch head and
// the real contract sha of a contract-less task, as the daemon's own wiring
// does. The fake env ships neither, and its "cs" contract sha would read as
// a contract change through the API's real inbox service, hiding the review
// item these tests assert on.
func (e *stewardHostEnv) wireHeadAndContract() {
	e.svc.Head = func(taskID int64) (string, error) {
		task, err := e.db.GetTask(taskID)
		if err != nil || task.WorktreePath == nil {
			return "", err
		}
		return workspace.GitHead(context.Background(), *task.WorktreePath)
	}
	e.svc.ContractSha = func(int64) string { return "" }
}

// seatDigestGet is the fake seat's one read: its review's digest, fetched
// over steward.digest.get and decoded.
func seatDigestGet(t *testing.T, e *stewardHostEnv, row store.StewardReview) steward.Digest {
	t.Helper()
	raw, err := budgetCall(t, e.api, seatRC(row), "steward.digest.get", map[string]any{"reviewId": row.ID})
	if err != nil {
		t.Fatalf("steward.digest.get: %v", err)
	}
	var d steward.Digest
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// seatSubmit is the fake seat's one write: verdict.submit as its own run.
func seatSubmit(t *testing.T, e *stewardHostEnv, row store.StewardReview, verdict json.RawMessage) steward.View {
	t.Helper()
	res, err := budgetCall(t, e.api, seatRC(row), "steward.verdict.submit",
		map[string]any{"reviewId": row.ID, "token": e.seatToken(t, row.StewardTaskID), "verdict": verdict})
	if err != nil {
		t.Fatalf("steward.verdict.submit: %v", err)
	}
	var v steward.View
	if err := json.Unmarshal(res, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// inboxReviewKeys lists the review-kind keys of the Human's inbox.
func inboxReviewKeys(t *testing.T, e *stewardHostEnv) []string {
	t.Helper()
	raw, err := budgetCall(t, e.api, &requestContext{local: true}, "inbox.list", map[string]any{})
	if err != nil {
		t.Fatalf("inbox.list: %v", err)
	}
	var listed struct {
		Items []struct {
			Key, Kind string
		}
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, it := range listed.Items {
		if it.Kind == "review" {
			keys = append(keys, it.Key)
		}
	}
	return keys
}

// TestStewardE2E_FakeLLM_AcceptedThenStale proves the seat-driven happy
// path: digest.get, an ACCEPTED verdict built from that digest, submit, a
// merge-ready view and one inbox item — and that a head that moves afterwards
// turns the same stored verdict stale and drops the inbox item.
func TestStewardE2E_FakeLLM_AcceptedThenStale(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.setInputs(greenInputs("success"))
	e.wireHeadAndContract()
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	row := e.waitSeatStarted(t, r.ReviewID)

	// The seat builds its verdict from the digest the RPC handed it.
	d := seatDigestGet(t, e, row)
	gates := make([]steward.GateAck, 0, len(d.Gates))
	for _, g := range d.Gates {
		gates = append(gates, steward.GateAck{ID: g.ID, Ack: true})
	}
	frames := make([]steward.FrameObservation, 0, len(d.Frames.Items))
	for _, f := range d.Frames.Items {
		frames = append(frames, steward.FrameObservation{Path: f.Path, Observation: "the frame matches the diff and looks right"})
	}
	verdict, err := json.Marshal(steward.SeatVerdict{V: 1, ReviewID: r.ReviewID, Verdict: steward.VerdictAccepted,
		Summary: "every gate holds and the diff is sound", Gates: gates, Frames: frames})
	if err != nil {
		t.Fatal(err)
	}
	v := seatSubmit(t, e, row, verdict)
	if v.Status != "done" || v.Verdict != steward.VerdictAccepted || !v.MergeReady || v.Stale != "" {
		t.Fatalf("view = %+v", v)
	}

	// The Human's inbox carries exactly the finished review.
	wantKey := fmt.Sprintf("review:%d", r.ReviewID)
	if keys := inboxReviewKeys(t, e); len(keys) != 1 || keys[0] != wantKey {
		t.Fatalf("inbox review keys = %v, want [%s]", keys, wantKey)
	}

	// The branch moves on: the stored verdict stays, the view goes stale,
	// the merge-ready label drops and the inbox item disappears.
	gitIn(t, *e.seatTask.WorktreePath, "commit", "--allow-empty", "-qm", "feat: more")
	raw, err := budgetCall(t, e.api, &requestContext{local: true}, "steward.get", map[string]any{"reviewId": r.ReviewID})
	if err != nil {
		t.Fatal(err)
	}
	var after steward.View
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	if after.Stale == "" || after.MergeReady || after.Verdict != steward.VerdictAccepted {
		t.Fatalf("view after the head moved = %+v", after)
	}
	if keys := inboxReviewKeys(t, e); len(keys) != 0 {
		t.Fatalf("a stale review must leave the inbox: %v", keys)
	}
	_ = e.api.Runs.Stop(row.RunID)
}

// TestStewardE2E_FakeLLM_CIFailureClampsToChanges proves the clamp end to
// end: the seat says ACCEPTED over a digest whose ci gate is red, and the
// stored verdict is capped at CHANGES with the failing gate in the view.
func TestStewardE2E_FakeLLM_CIFailureClampsToChanges(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.setInputs(greenInputs("failure"))
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	row := e.waitSeatStarted(t, r.ReviewID)

	d := seatDigestGet(t, e, row)
	if g, ok := steward.GateByID(d.Gates, "ci"); !ok || g.State != steward.GateFail {
		t.Fatalf("digest ci gate = %+v, want failing", g)
	}
	v := seatSubmit(t, e, row, acceptedVerdict(t, e, r.ReviewID))
	if v.SeatVerdict != steward.VerdictAccepted || v.Verdict != steward.VerdictChanges || v.MergeReady {
		t.Fatalf("view = %+v", v)
	}
	if g, ok := steward.GateByID(v.Gates, "ci"); !ok || g.State != steward.GateFail {
		t.Fatalf("view gates = %+v, want the failing ci gate", v.Gates)
	}
	stored := e.row(t, r.ReviewID)
	if stored.Verdict == steward.VerdictAccepted || !strings.Contains(stored.Summary, "clamped") {
		t.Fatalf("row = %+v", stored)
	}
	_ = e.api.Runs.Stop(row.RunID)
}

// TestStewardE2E_SafetyRails proves the write path's rails end to end: only
// the seat's own run may submit (the Lead is refused even with the correct
// token), the token works exactly once, an identical re-review returns the
// existing one, and the hourly cap refuses a genuinely new key.
func TestStewardE2E_SafetyRails(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.setInputs(greenInputs("success"))
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	row := e.waitSeatStarted(t, r.ReviewID)
	token := e.seatToken(t, row.StewardTaskID)
	verdict := acceptedVerdict(t, e, r.ReviewID)

	// The Lead's run holds the correct token and is still refused.
	lead := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}
	_, err := budgetCall(t, e.api, lead, "steward.verdict.submit", map[string]any{"reviewId": r.ReviewID, "token": token, "verdict": verdict})
	wantErrCode(t, err, codeForbidden)
	if after := e.row(t, r.ReviewID); after.Status != "running" || after.TokenUsed {
		t.Fatalf("a refused submit left a mark: %+v", after)
	}

	// The seat submits once; the same token is dead the second time.
	if _, err := budgetCall(t, e.api, seatRC(row), "steward.verdict.submit", map[string]any{"reviewId": r.ReviewID, "token": token, "verdict": verdict}); err != nil {
		t.Fatal(err)
	}
	_, err = budgetCall(t, e.api, seatRC(row), "steward.verdict.submit", map[string]any{"reviewId": r.ReviewID, "token": token, "verdict": verdict})
	wantErrCode(t, err, codeInvalid)

	// A re-review of the same head and CI is the same review, not a new one.
	r2 := e.reviewViaRPC(t, &requestContext{local: true})
	if !r2.Existing || r2.ReviewID != r.ReviewID || r2.Status != "done" {
		t.Fatalf("second review = %+v, want the existing done review %d", r2, r.ReviewID)
	}
	if rows, _ := e.db.ListStewardReviews(e.seatTask.ID, 10); len(rows) != 1 {
		t.Fatalf("reviews = %+v, want the one row", rows)
	}

	// A new review key is refused once the hourly cap is spent.
	e.cfg.MaxReviewsPerHour = 1
	e.setInputs(greenInputs("neutral"))
	_, err = budgetCall(t, e.api, &requestContext{local: true}, "steward.review", map[string]any{"taskId": e.seatTask.ID})
	if res := wantErrCode(t, err, codeInvalid); !strings.Contains(res.Message, "hourly") {
		t.Fatalf("message = %q, want the hourly cap", res.Message)
	}
	if rows, _ := e.db.ListStewardReviews(e.seatTask.ID, 10); len(rows) != 1 {
		t.Fatalf("a refused review opened a row: %+v", rows)
	}
	_ = e.api.Runs.Stop(row.RunID)
}
