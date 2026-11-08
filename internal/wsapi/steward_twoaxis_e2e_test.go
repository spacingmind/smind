package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/validation"
)

// The two-axis review's end-to-end tests (ADR-0054 P2), with a fake LLM: the
// seats are the test code calling the seat RPCs as their own runs. Both seats
// run on the "hang" fake provider, so each pass's run stays open until a test
// ends it.

const (
	twoAxisBaseRules = "# Rules\n\nAlways run task lint.\n"
	twoAxisHeadRules = "# Rules\n\nNo rules apply to this change.\n"
)

// twoAxisOpts selects what the scenario's inputs carry.
type twoAxisOpts struct {
	ci          string
	withSpec    bool
	baseMissing bool // the merge-base could not be resolved
	noStdProf   bool // the steward-standards profile was never seeded
}

// enableTwoAxis turns steward.twoAxis on for the env: it commits a base and a
// head version of AGENTS.md in the Lead's worktree, builds the two-axis
// inputs from them with the real git readers and seeds the profiles.
func (e *stewardHostEnv) enableTwoAxis(t *testing.T, o twoAxisOpts) {
	t.Helper()
	if o.ci == "" {
		o.ci = "success"
	}
	wt := *e.seatTask.WorktreePath
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(wt, "AGENTS.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, wt, "add", "AGENTS.md")
		gitIn(t, wt, "commit", "-qm", "feat: edit the standards")
	}
	write(twoAxisBaseRules)
	base := gitIn(t, wt, "rev-parse", "HEAD")
	write(twoAxisHeadRules)
	e.head = gitIn(t, wt, "rev-parse", "HEAD")

	files, err := steward.StandardsAtBase(context.Background(), wt, base, nil)
	if err != nil || len(files) != 1 {
		t.Fatalf("StandardsAtBase() = %+v, %v", files, err)
	}
	diff, err := steward.StandardsHeadDiff(context.Background(), wt, base, e.head, files)
	if err != nil {
		t.Fatal(err)
	}
	in := greenInputs(o.ci)
	in.TwoAxis = &steward.TwoAxisInputs{
		Standards: steward.StandardsInput{BaseRef: base, Files: files, HeadDiff: diff, Rules: []string{"Keep comments short."}},
		Spec:      steward.SpecInput{ContractCriteria: []steward.SpecCriterionInput{}, ADRFiles: []string{}, TestsInTree: map[string]bool{}},
	}
	if o.baseMissing {
		in.TwoAxis.Standards.BaseRef = ""
	}
	if o.withSpec {
		in.TwoAxis.Spec.PlanPath = "docs/plans/active/x.md"
		in.TwoAxis.Spec.PlanText = "# X\n\n## Acceptance Criteria\n- AC1 the change does the thing\n- AC2 the named test TestThing passes\n\n## Test Scenarios\n"
	}
	e.setInputs(in)
	e.cfg.TwoAxis = true
	if !o.noStdProf {
		if _, err := e.db.CreateAgentProfile(store.AgentProfile{Name: "steward-standards", Provider: "glm"}); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *stewardHostEnv) pass(t *testing.T, id int64, axis steward.Axis) store.StewardReviewPass {
	t.Helper()
	p, err := e.db.GetStewardReviewPass(id, string(axis))
	if err != nil {
		t.Fatalf("pass %s of review %d: %v", axis, id, err)
	}
	return p
}

// waitPass waits until pass axis has the wanted status (and, for running, a
// started run).
func (e *stewardHostEnv) waitPass(t *testing.T, id int64, axis steward.Axis, status string) store.StewardReviewPass {
	t.Helper()
	poll(t, 10*time.Second, "the "+string(axis)+" pass to be "+status, func() bool {
		p, err := e.db.GetStewardReviewPass(id, string(axis))
		return err == nil && p.Status == status && (status != steward.PassRunning || (p.RunID != "" && p.StewardTaskID != 0))
	})
	return e.pass(t, id, axis)
}

func passRC(p store.StewardReviewPass) *requestContext {
	return &requestContext{runScoped: true, ident: runIdentity{runID: p.RunID, taskID: p.StewardTaskID}}
}

func (e *stewardHostEnv) passToken(t *testing.T, p store.StewardReviewPass) string {
	t.Helper()
	spec, ok := e.host.seatSource(p.StewardTaskID)
	if !ok {
		t.Fatalf("no seat spec for the %s pass", p.Axis)
	}
	if spec.Env[steward.EnvAxis] != p.Axis {
		t.Fatalf("seat env axis = %q, want %q", spec.Env[steward.EnvAxis], p.Axis)
	}
	return spec.Env[steward.EnvToken]
}

// passDocs builds each axis's complete ACCEPTED verdict from the review's
// stored digest, as a seat that read the digest would.
func (e *stewardHostEnv) standardsDoc(reviewID int64, verdict string, findings ...steward.AxisFinding) json.RawMessage {
	raw, _ := json.Marshal(steward.AxisVerdict{V: 1, ReviewID: reviewID, Axis: steward.AxisStandards, Verdict: verdict,
		Summary: "judged against the base standards", Findings: findings})
	return raw
}

func (e *stewardHostEnv) specDoc(t *testing.T, reviewID int64, verdict string) json.RawMessage {
	t.Helper()
	var d steward.Digest
	if err := json.Unmarshal([]byte(e.row(t, reviewID).DigestJSON), &d); err != nil {
		t.Fatal(err)
	}
	doc := steward.AxisVerdict{V: 1, ReviewID: reviewID, Axis: steward.AxisSpec, Verdict: verdict, Summary: "judged against the spec",
		Gates: []steward.GateAck{}, Frames: []steward.FrameObservation{}, Criteria: []steward.CriterionResult{}}
	for _, g := range d.Gates {
		doc.Gates = append(doc.Gates, steward.GateAck{ID: g.ID, Ack: true})
	}
	if d.Spec != nil {
		for _, c := range d.Spec.Criteria {
			doc.Criteria = append(doc.Criteria, steward.CriterionResult{ID: c.ID, Status: steward.CriterionMet, Evidence: "the diff and its tests cover it"})
		}
	}
	if verdict == steward.VerdictChanges {
		doc.Findings = []steward.AxisFinding{{SeatFinding: steward.SeatFinding{File: "main.go", Line: 1, Why: "wrong", Check: "go test"}}}
	}
	raw, _ := json.Marshal(doc)
	return raw
}

func (e *stewardHostEnv) submitPass(t *testing.T, p store.StewardReviewPass, doc json.RawMessage) (steward.View, error) {
	t.Helper()
	res, err := budgetCall(t, e.api, passRC(p), "steward.verdict.submit",
		map[string]any{"reviewId": p.ReviewID, "token": e.passToken(t, p), "submission": doc})
	if err != nil {
		return steward.View{}, err
	}
	var v steward.View
	if err := json.Unmarshal(res, &v); err != nil {
		t.Fatal(err)
	}
	return v, nil
}

// startTwoAxis opens a review with both passes running.
func (e *stewardHostEnv) startTwoAxis(t *testing.T, o twoAxisOpts) (int64, store.StewardReviewPass, store.StewardReviewPass) {
	t.Helper()
	e.enableTwoAxis(t, o)
	e.wireHeadAndContract()
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	std := e.waitPass(t, r.ReviewID, steward.AxisStandards, steward.PassRunning)
	spec := e.waitPass(t, r.ReviewID, steward.AxisSpec, steward.PassRunning)
	return r.ReviewID, std, spec
}

func TestTwoAxisE2E_BothAcceptedStoresAccepted(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	id, std, spec := e.startTwoAxis(t, twoAxisOpts{withSpec: true})

	// Two seats: own tasks, runs, tokens, scratch trees; the review row is the aggregate.
	if std.StewardTaskID == spec.StewardTaskID || std.RunID == spec.RunID || std.ScratchDir == spec.ScratchDir || std.TokenHash == spec.TokenHash || std.TokenHash == "" {
		t.Fatalf("passes share a task, run, scratch or token: %+v / %+v", std, spec)
	}
	if std.Profile != "steward-standards" || spec.Profile != steward.SeatProfileName {
		t.Fatalf("pass profiles = %q/%q, want steward-standards and steward", std.Profile, spec.Profile)
	}
	if row := e.row(t, id); row.Status != "running" || row.StewardTaskID != 0 || row.TokenHash != "" {
		t.Fatalf("review row = %+v, want the aggregate with no seat of its own", row)
	}
	// D11: the Standards seat's scratch tree holds the BASE text, never the head's.
	got, err := os.ReadFile(filepath.Join(std.ScratchDir, steward.StandardsDir, "AGENTS.md"))
	if err != nil || string(got) != twoAxisBaseRules {
		t.Fatalf("standards copy = %q, %v; want the base text", got, err)
	}
	if _, err := os.Stat(filepath.Join(spec.ScratchDir, steward.StandardsDir)); err == nil {
		t.Fatal("the spec seat's tree carries a standards copy")
	}
	// Neither tree carries a head AGENTS.md (the Standards seat reads only the
	// base copies, and the head's edit reaches the Spec seat through the
	// digest), and the ordinary files of the checkout are still there.
	for _, p := range []store.StewardReviewPass{std, spec} {
		if _, err := os.Stat(filepath.Join(p.ScratchDir, "AGENTS.md")); err == nil {
			t.Fatalf("the %s seat can read the head's AGENTS.md", p.Axis)
		}
		if _, err := os.Stat(filepath.Join(p.ScratchDir, "scenario")); err != nil {
			t.Fatalf("the %s seat's tree lost an ordinary file: %v", p.Axis, err)
		}
	}
	// Each seat reads the digest through its own run.
	for _, p := range []store.StewardReviewPass{std, spec} {
		raw, err := budgetCall(t, e.api, passRC(p), "steward.digest.get", map[string]any{"reviewId": id})
		if err != nil {
			t.Fatalf("digest.get as %s: %v", p.Axis, err)
		}
		var d steward.Digest
		if err := json.Unmarshal(raw, &d); err != nil || d.Standards == nil || !d.Standards.ChangedInDiff || d.Spec == nil || len(d.Spec.Criteria) != 2 {
			t.Fatalf("%s digest = %+v, %v", p.Axis, d, err)
		}
		if !strings.Contains(d.Untrusted.StandardsHeadDiff, "No rules apply") {
			t.Fatalf("the head edit of the standards is missing as data: %q", d.Untrusted.StandardsHeadDiff)
		}
	}

	if _, err := e.submitPass(t, std, e.standardsDoc(id, steward.VerdictAccepted)); err != nil {
		t.Fatal(err)
	}
	if row := e.row(t, id); row.Status != "running" || row.Verdict != "" {
		t.Fatalf("review after one pass = %+v, want it open", row)
	}
	v, err := e.submitPass(t, spec, e.specDoc(t, id, steward.VerdictAccepted))
	if err != nil {
		t.Fatal(err)
	}
	if v.Verdict != steward.VerdictAccepted || v.Status != "done" || len(v.Axes) != 2 || !v.MergeReady {
		t.Fatalf("view = %+v", v)
	}
	_ = e.api.Runs.Stop(std.RunID)
	_ = e.api.Runs.Stop(spec.RunID)
}

func TestTwoAxisE2E_StandardsChangesSpecAcceptedIsChanges(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	id, std, spec := e.startTwoAxis(t, twoAxisOpts{withSpec: true})
	f := steward.AxisFinding{SeatFinding: steward.SeatFinding{File: "main.go", Line: 3, Why: "no lint", Check: "task lint", Severity: "P2"}, Rule: ".smind-standards/AGENTS.md: Always run task lint."}
	if _, err := e.submitPass(t, std, e.standardsDoc(id, steward.VerdictChanges, f)); err != nil {
		t.Fatal(err)
	}
	v, err := e.submitPass(t, spec, e.specDoc(t, id, steward.VerdictAccepted))
	if err != nil {
		t.Fatal(err)
	}
	if v.Verdict != steward.VerdictChanges || v.MergeReady || len(v.Findings) != 1 || v.Findings[0].Axis != steward.AxisStandards {
		t.Fatalf("view = %+v, want CHANGES with the standards finding tagged", v)
	}
	_ = e.api.Runs.Stop(std.RunID)
	_ = e.api.Runs.Stop(spec.RunID)
}

func TestTwoAxisE2E_CIFailureClampsBothAccepted(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	id, std, spec := e.startTwoAxis(t, twoAxisOpts{withSpec: true, ci: "failure"})
	if _, err := e.submitPass(t, std, e.standardsDoc(id, steward.VerdictAccepted)); err != nil {
		t.Fatal(err)
	}
	v, err := e.submitPass(t, spec, e.specDoc(t, id, steward.VerdictAccepted))
	if err != nil {
		t.Fatal(err)
	}
	if v.SeatVerdict != steward.VerdictAccepted || v.Verdict != steward.VerdictChanges || v.MergeReady || !strings.Contains(v.Summary, "clamped") {
		t.Fatalf("view = %+v, want an ACCEPTED seat verdict clamped to CHANGES by the red ci gate", v)
	}
	_ = e.api.Runs.Stop(std.RunID)
	_ = e.api.Runs.Stop(spec.RunID)
}

func TestTwoAxisE2E_OnePassKilledNeverAccepted(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	id, std, spec := e.startTwoAxis(t, twoAxisOpts{withSpec: true})
	if _, err := e.submitPass(t, spec, e.specDoc(t, id, steward.VerdictAccepted)); err != nil {
		t.Fatal(err)
	}
	// The standards run dies without a verdict: its watch writes the pass's blocked row.
	if err := e.api.Runs.Stop(std.RunID); err != nil {
		t.Fatal(err)
	}
	p := e.waitPass(t, id, steward.AxisStandards, steward.PassBlocked)
	if p.BlockedReason != "crashed" {
		t.Fatalf("killed pass = %+v, want crashed", p)
	}
	row := e.waitStatus(t, id, "done")
	if row.Verdict != steward.VerdictChanges || row.Verdict == steward.VerdictAccepted {
		t.Fatalf("review = %+v, want CHANGES (a failed axis never lets it be ACCEPTED)", row)
	}
	// One review, counted once, and a half-failed review is not an auto-pause strike.
	if n, _ := e.db.CountStewardReviewsSince(time.Now().Add(-time.Hour)); n != 1 {
		t.Fatalf("reviews counted = %d, want 1 (not once per pass)", n)
	}
	if n, _ := e.db.ConsecutiveStewardFailures(); n != 0 {
		t.Fatalf("consecutive failures = %d, want 0", n)
	}
	_ = e.api.Runs.Stop(spec.RunID)
}

func TestTwoAxisE2E_BothPassesKilledBlocksAndCountsOneFailure(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	id, std, spec := e.startTwoAxis(t, twoAxisOpts{withSpec: true})
	_ = e.api.Runs.Stop(std.RunID)
	_ = e.api.Runs.Stop(spec.RunID)
	row := e.waitStatus(t, id, "blocked")
	if row.Verdict != steward.VerdictBlocked || row.BlockedReason != "crashed" {
		t.Fatalf("review = %+v", row)
	}
	if n, _ := e.db.ConsecutiveStewardFailures(); n != 1 {
		t.Fatalf("consecutive failures = %d, want 1 (a review, not a pass)", n)
	}
}

func TestTwoAxisE2E_NoSpecSkipsTheSpecPass(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.enableTwoAxis(t, twoAxisOpts{})
	e.wireHeadAndContract()
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	std := e.waitPass(t, r.ReviewID, steward.AxisStandards, steward.PassRunning)
	spec := e.waitPass(t, r.ReviewID, steward.AxisSpec, steward.PassSkipped)
	if spec.Summary != "no spec available" || spec.RunID != "" || spec.StewardTaskID != 0 || spec.CostUSD != 0 {
		t.Fatalf("spec pass = %+v, want skipped without a seat", spec)
	}
	v, err := e.submitPass(t, std, e.standardsDoc(r.ReviewID, steward.VerdictAccepted))
	if err != nil {
		t.Fatal(err)
	}
	if v.Verdict != steward.VerdictAccepted || len(v.Axes) != 2 || v.Axes[1].Status != steward.PassSkipped || !strings.Contains(v.Summary, "axis spec skipped") {
		t.Fatalf("view = %+v", v)
	}
	_ = e.api.Runs.Stop(std.RunID)
}

func TestTwoAxisE2E_UnresolvedBaseBlocksStandardsNeverJudgesTheHead(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.enableTwoAxis(t, twoAxisOpts{withSpec: true, baseMissing: true})
	e.wireHeadAndContract()
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	std := e.waitPass(t, r.ReviewID, steward.AxisStandards, steward.PassBlocked)
	if std.BlockedReason != "crashed" || std.RunID != "" || std.StewardTaskID != 0 {
		t.Fatalf("standards pass = %+v, want blocked before any seat", std)
	}
	spec := e.waitPass(t, r.ReviewID, steward.AxisSpec, steward.PassRunning)
	v, err := e.submitPass(t, spec, e.specDoc(t, r.ReviewID, steward.VerdictAccepted))
	if err != nil {
		t.Fatal(err)
	}
	if v.Verdict != steward.VerdictChanges {
		t.Fatalf("view = %+v, want CHANGES (the standards axis did not complete)", v)
	}
	_ = e.api.Runs.Stop(spec.RunID)
}

func TestTwoAxisE2E_StandardsProfileFallsBackAndRecordsTheModel(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.enableTwoAxis(t, twoAxisOpts{withSpec: true, noStdProf: true})
	e.wireHeadAndContract()
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	std := e.waitPass(t, r.ReviewID, steward.AxisStandards, steward.PassRunning)
	if std.Profile != steward.SeatProfileName {
		t.Fatalf("standards profile = %q, want the steward fallback", std.Profile)
	}
	e.endReview(t, r.ReviewID)
}

func TestTwoAxisE2E_CancelWritesBothRowsBeforeStoppingAndIsNotACrash(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	id, std, spec := e.startTwoAxis(t, twoAxisOpts{withSpec: true})
	e.endReview(t, id)
	for _, p := range []store.StewardReviewPass{std, spec} {
		got := e.pass(t, id, steward.Axis(p.Axis))
		if got.Status != steward.PassBlocked || got.BlockedReason != "cancelled" {
			t.Fatalf("%s pass after cancel = %+v", p.Axis, got)
		}
	}
	row := e.row(t, id)
	if row.Status != "blocked" || row.BlockedReason != "cancelled" {
		t.Fatalf("review = %+v", row)
	}
	// The watches see their runs end after the rows are written: never a crash.
	e.waitWatches(t)
	if n, _ := e.db.ConsecutiveStewardFailures(); n != 0 {
		t.Fatalf("consecutive failures = %d, want 0 (a cancel is not a crash)", n)
	}
	if got := e.row(t, id); got.BlockedReason != "cancelled" {
		t.Fatalf("review reason changed to %q", got.BlockedReason)
	}
}

func TestTwoAxisE2E_TokensAndSeatsAreBoundToTheirPass(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	id, std, spec := e.startTwoAxis(t, twoAxisOpts{withSpec: true})
	// The Lead's run, even with a pass's real token, is refused.
	lead := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}
	_, err := budgetCall(t, e.api, lead, "steward.verdict.submit", map[string]any{"reviewId": id, "token": e.passToken(t, std), "submission": e.standardsDoc(id, steward.VerdictAccepted)})
	wantErrCode(t, err, codeForbidden)
	// Nor can any run that is no pass's seat read the digest.
	_, err = budgetCall(t, e.api, lead, "steward.digest.get", map[string]any{"reviewId": id})
	wantErrCode(t, err, codeForbidden)
	// The spec seat with the standards seat's token: bad token, nothing consumed.
	_, err = budgetCall(t, e.api, passRC(spec), "steward.verdict.submit", map[string]any{"reviewId": id, "token": e.passToken(t, std), "submission": e.specDoc(t, id, steward.VerdictAccepted)})
	if err == nil {
		t.Fatal("the spec seat spent the standards token")
	}
	// The standards seat labelling its verdict as spec: refused.
	if _, err := e.submitPass(t, std, e.specDoc(t, id, steward.VerdictAccepted)); err == nil {
		t.Fatal("a standards seat submitted a spec-axis verdict")
	}
	for _, p := range []store.StewardReviewPass{std, spec} {
		if got := e.pass(t, id, steward.Axis(p.Axis)); got.TokenUsed || got.Status != steward.PassRunning {
			t.Fatalf("%s pass = %+v after refused submissions", p.Axis, got)
		}
	}
	e.endReview(t, id)
}

func TestTwoAxisOff_ReviewHasNoPassesAndOneSeat(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.setInputs(greenInputs("success"))
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	row := e.waitSeatStarted(t, r.ReviewID)
	if passes, _ := e.db.ListStewardReviewPasses(row.ID); len(passes) != 0 || row.TokenHash == "" {
		t.Fatalf("flag-off review: passes = %+v, token hash = %q; want the single seat path", passes, row.TokenHash)
	}
	spec, ok := e.host.seatSource(row.StewardTaskID)
	if !ok {
		t.Fatal("no seat spec")
	}
	if _, has := spec.Env[steward.EnvAxis]; has {
		t.Fatalf("flag-off seat env carries an axis: %v", spec.Env)
	}
	e.endReview(t, r.ReviewID)
}

// TestTwoAxisSeats_FloorAndCatalogueAreTheSingleSeatsForBothAxes: a pass's
// seat has no shell, write, web, task, permission or message power: the same
// disallowed tools and the same seat floor as the single seat (ADR-0054 §10).
func TestTwoAxisSeats_FloorAndCatalogueAreTheSingleSeatsForBothAxes(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	id, std, spec := e.startTwoAxis(t, twoAxisOpts{withSpec: true})
	for _, p := range []store.StewardReviewPass{std, spec} {
		seat, ok := e.host.seatSource(p.StewardTaskID)
		if !ok {
			t.Fatalf("no seat spec for the %s pass", p.Axis)
		}
		if got := append([]string(nil), seat.DisallowedTools...); strings.Join(got, ",") != strings.Join(steward.SeatDisallowedTools, ",") {
			t.Fatalf("%s DisallowedTools = %v", p.Axis, got)
		}
		for _, tool := range []string{"Bash", "Edit", "Write", "WebFetch", "Task"} {
			if allow, _ := seat.Floor(tool, nil); allow {
				t.Fatalf("%s floor allows %s", p.Axis, tool)
			}
		}
		if allow, reason := seat.Floor("Read", []string{filepath.Join(p.ScratchDir, "main.go")}); !allow {
			t.Fatalf("%s floor denies a read in its own scratch tree: %s", p.Axis, reason)
		}
		other := std.ScratchDir
		if p.Axis == string(steward.AxisStandards) {
			other = spec.ScratchDir
		}
		if allow, _ := seat.Floor("Read", []string{filepath.Join(other, "main.go")}); allow {
			t.Fatalf("%s floor allows a read in the other pass's scratch tree", p.Axis)
		}
		if _, hasSecret := seat.Env["SMIND_HOME"]; hasSecret || len(seat.Env) != 3 {
			t.Fatalf("%s seat env = %v, want exactly review id, token and axis", p.Axis, seat.Env)
		}
	}
	e.endReview(t, id)
}

// TestTwoAxisE2E_GateUnknownStartsNeitherPassUntilCIDecides: with CI pending
// the review holds, both pass rows exist and neither seat starts; when CI
// decides, the recheck starts both.
func TestTwoAxisE2E_GateUnknownStartsNeitherPassUntilCIDecides(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.enableTwoAxis(t, twoAxisOpts{withSpec: true, ci: "pending"})
	e.wireHeadAndContract()
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	if r.Status != "pending" {
		t.Fatalf("review = %+v, want it held pending", r)
	}
	for _, axis := range []steward.Axis{steward.AxisStandards, steward.AxisSpec} {
		if p := e.pass(t, r.ReviewID, axis); p.Status != steward.PassPending || p.StewardTaskID != 0 || p.RunID != "" {
			t.Fatalf("%s pass = %+v, want pending with no seat", axis, p)
		}
	}
	e.coll.mu.Lock()
	e.coll.inputs.CI = steward.CIInfo{State: "success"}
	e.coll.mu.Unlock()
	e.waitPass(t, r.ReviewID, steward.AxisStandards, steward.PassRunning)
	e.waitPass(t, r.ReviewID, steward.AxisSpec, steward.PassRunning)
	e.endReview(t, r.ReviewID)
}

// TestTwoAxisE2E_UnreadableConfigFailsClosed: the standards paths pruned from the
// head checkout come from the steward config, so a config that cannot be read at
// start blocks the review: no seat starts, no pass is left open.
func TestTwoAxisE2E_UnreadableConfigFailsClosed(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.enableTwoAxis(t, twoAxisOpts{withSpec: true})
	e.wireHeadAndContract()
	e.bindSteward(t)
	e.cfgErr = errors.New("config.yaml: unreadable")
	if _, err := budgetCall(t, e.api, &requestContext{local: true}, "steward.review", map[string]any{"taskId": e.seatTask.ID}); err == nil {
		t.Fatal("a review started with an unreadable config")
	}
	rows, err := e.db.ListStewardReviews(e.seatTask.ID, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("reviews = %+v, %v", rows, err)
	}
	if rows[0].Status != "blocked" || rows[0].BlockedReason != "crashed" {
		t.Fatalf("review = %+v, want blocked/crashed", rows[0])
	}
	passes, _ := e.db.ListStewardReviewPasses(rows[0].ID)
	for _, p := range passes {
		if p.Status != steward.PassBlocked || p.StewardTaskID != 0 {
			t.Fatalf("%s pass = %+v, want closed with no seat", p.Axis, p)
		}
	}
}

// TestTwoAxisE2E_SnapshotFailureBlocksTheReview: the scope check compares the
// Lead's worktree with a snapshot taken before the seats start; without one the
// comparison would be wrong, so nothing starts and the review is decided.
func TestTwoAxisE2E_SnapshotFailureBlocksTheReview(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.host.snapshot = func(context.Context, string) (validation.Snapshot, error) {
		return validation.Snapshot{}, errors.New("git status failed")
	}
	e.enableTwoAxis(t, twoAxisOpts{withSpec: true})
	e.wireHeadAndContract()
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	row := e.waitStatus(t, r.ReviewID, "blocked")
	if row.Verdict != steward.VerdictBlocked || row.BlockedReason != "crashed" {
		t.Fatalf("review = %+v, want BLOCKED/crashed", row)
	}
	for _, axis := range []steward.Axis{steward.AxisStandards, steward.AxisSpec} {
		if p := e.pass(t, r.ReviewID, axis); p.Status != steward.PassBlocked || p.StewardTaskID != 0 || p.RunID != "" {
			t.Fatalf("%s pass = %+v, want closed before any seat", axis, p)
		}
	}
}

// TestStewardHost_StartupClosesPassesLeftOpenUnderAClosedReview: a failed write
// in a watch can leave a pass running under a review that is already closed;
// the next start closes it.
func TestStewardHost_StartupClosesPassesLeftOpenUnderAClosedReview(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	row, err := e.db.InsertStewardReview(store.StewardReview{TaskID: e.seatTask.ID, TriggerKind: steward.TriggerHuman, HeadSHA: "h", Status: "pending"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, axis := range []steward.Axis{steward.AxisStandards, steward.AxisSpec} {
		if _, err := e.db.InsertStewardReviewPass(store.StewardReviewPass{ReviewID: row.ID, Axis: string(axis), Status: steward.PassRunning}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.db.SetStewardReviewBlocked(row.ID, "cancelled", "cancelled", 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if orphans, err := e.db.ListOpenPassesOfClosedReviews(); err != nil || len(orphans) != 2 {
		t.Fatalf("orphans = %+v, %v; want 2", orphans, err)
	}
	newStewardHost(stewardHostOptions{Svc: e.svc, Rails: e.rails, Collect: e.coll.fn, WM: e.wm, DB: e.db, Reg: e.api.Runs, Runner: newTestRunner(e.wm), ProfReg: profiles.New(e.db)})
	for _, axis := range []steward.Axis{steward.AxisStandards, steward.AxisSpec} {
		if p := e.pass(t, row.ID, axis); p.Status != steward.PassBlocked || p.BlockedReason != "crashed" {
			t.Fatalf("%s pass = %+v, want closed as crashed", axis, p)
		}
	}
	if orphans, _ := e.db.ListOpenPassesOfClosedReviews(); len(orphans) != 0 {
		t.Fatalf("orphans after the reconcile = %+v", orphans)
	}
}

// TestTwoAxisE2E_PruneFailureBlocksBothPasses: fail closed. A prune that fails
// leaves a head checkout that may still carry planted autoloads, so neither
// pass starts a run and the review is decided BLOCKED.
func TestTwoAxisE2E_PruneFailureBlocksBothPasses(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.host.prune = func(string, steward.PruneSpec) error { return errors.New("permission denied") }
	e.enableTwoAxis(t, twoAxisOpts{withSpec: true})
	e.wireHeadAndContract()
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	row := e.waitStatus(t, r.ReviewID, "blocked")
	if row.Verdict != steward.VerdictBlocked || row.BlockedReason != "crashed" {
		t.Fatalf("review = %+v, want BLOCKED/crashed", row)
	}
	for _, axis := range []steward.Axis{steward.AxisStandards, steward.AxisSpec} {
		if p := e.pass(t, r.ReviewID, axis); p.Status != steward.PassBlocked || p.RunID != "" {
			t.Fatalf("%s pass = %+v, want blocked before any run", axis, p)
		}
	}
}

// TestTwoAxisE2E_InboxItemCarriesAxes: through the real inbox.list, a finished
// two-axis review is one review item with each pass's axis, verdict and status.
func TestTwoAxisE2E_InboxItemCarriesAxes(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	id, std, spec := e.startTwoAxis(t, twoAxisOpts{withSpec: true})
	f := steward.AxisFinding{SeatFinding: steward.SeatFinding{File: "main.go", Line: 3, Why: "no lint", Check: "task lint", Severity: "P2"}, Rule: ".smind-standards/AGENTS.md: Always run task lint."}
	if _, err := e.submitPass(t, std, e.standardsDoc(id, steward.VerdictChanges, f)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.submitPass(t, spec, e.specDoc(t, id, steward.VerdictAccepted)); err != nil {
		t.Fatal(err)
	}
	raw, err := budgetCall(t, e.api, &requestContext{local: true}, "inbox.list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Items []struct {
			Key  string
			Kind string
			Axes []struct{ Axis, Verdict, Status string }
		}
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	for _, it := range listed.Items {
		if it.Kind != "review" {
			continue
		}
		if len(it.Axes) != 2 || it.Axes[0].Axis != "standards" || it.Axes[0].Verdict != "CHANGES" || it.Axes[0].Status != "done" ||
			it.Axes[1].Axis != "spec" || it.Axes[1].Verdict != "ACCEPTED" {
			t.Fatalf("review item %s axes = %+v", it.Key, it.Axes)
		}
		_ = e.api.Runs.Stop(std.RunID)
		_ = e.api.Runs.Stop(spec.RunID)
		return
	}
	t.Fatalf("no review item in the inbox: %s", raw)
}
