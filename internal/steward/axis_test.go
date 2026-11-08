package steward

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func TestCombine_MinOverAxes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		std, spec    string
		want         string
		wantNoteWith string
	}{
		{"both accepted", VerdictAccepted, VerdictAccepted, VerdictAccepted, ""},
		{"standards lowers", VerdictChanges, VerdictAccepted, VerdictChanges, "axis standards: CHANGES"},
		{"spec lowers", VerdictAccepted, VerdictChanges, VerdictChanges, "axis spec: CHANGES"},
		{"blocked beats changes", VerdictChanges, VerdictBlocked, VerdictBlocked, "axis spec: BLOCKED"},
		{"unrecognised fails closed", VerdictAccepted, "MAYBE", VerdictBlocked, "axis spec: BLOCKED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Combine([]PassResult{
				{Axis: AxisStandards, Status: PassDone, SeatVerdict: tt.std},
				{Axis: AxisSpec, Status: PassDone, SeatVerdict: tt.spec},
			})
			if err != nil || got.Blocked || got.SeatVerdict != tt.want {
				t.Fatalf("Combine() = %+v, %v; want seat verdict %s", got, err, tt.want)
			}
			if tt.wantNoteWith != "" && !strings.Contains(strings.Join(got.Notes, ";"), tt.wantNoteWith) {
				t.Fatalf("Notes = %v, want one with %q", got.Notes, tt.wantNoteWith)
			}
		})
	}
}

func TestCombine_AxisCannotRaiseAboveGates(t *testing.T) {
	t.Parallel()

	gates := []Gate{{ID: "ci", State: GateFail, Detail: "red"}, {ID: "scope", State: GatePass}}
	got, err := Combine([]PassResult{
		{Axis: AxisStandards, Status: PassDone, SeatVerdict: VerdictAccepted},
		{Axis: AxisSpec, Status: PassDone, SeatVerdict: VerdictAccepted},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res := Clamp(got.SeatVerdict, gates); res.Verdict != VerdictChanges || !res.Capped {
		t.Fatalf("Clamp(combined ACCEPTED, failing ci) = %+v, want CHANGES clamped", res)
	}
	stacked := []Gate{{ID: "not_stacked", State: GateFail}}
	if res := Clamp(got.SeatVerdict, stacked); res.Verdict != VerdictBlocked {
		t.Fatalf("Clamp(combined ACCEPTED, failing not_stacked) = %+v, want BLOCKED", res)
	}
}

func TestCombine_FailedAxisNeverAccepted(t *testing.T) {
	t.Parallel()

	for _, reason := range DaemonBlockedReasons {
		if reason == "scope_violation" {
			continue // blocks the whole review: TestCombine_ScopeViolationBlocksWholeReview
		}
		got, err := Combine([]PassResult{
			{Axis: AxisStandards, Status: PassBlocked, BlockedReason: reason},
			{Axis: AxisSpec, Status: PassDone, SeatVerdict: VerdictAccepted},
		})
		if err != nil || got.Blocked || got.SeatVerdict != VerdictChanges {
			t.Fatalf("Combine(standards %s, spec ACCEPTED) = %+v, %v; want CHANGES", reason, got, err)
		}
		if !strings.Contains(strings.Join(got.Notes, ";"), "axis standards did not complete ("+reason+")") {
			t.Fatalf("Notes = %v, want the failed axis named", got.Notes)
		}
	}
	// A failed axis never raises: the other axis was already BLOCKED.
	got, _ := Combine([]PassResult{
		{Axis: AxisStandards, Status: PassBlocked, BlockedReason: "timeout"},
		{Axis: AxisSpec, Status: PassDone, SeatVerdict: VerdictBlocked},
	})
	if got.SeatVerdict != VerdictBlocked {
		t.Fatalf("Combine(failed + BLOCKED) = %+v, want BLOCKED", got)
	}
}

func TestCombine_BothFailedIsBlocked(t *testing.T) {
	t.Parallel()

	got, err := Combine([]PassResult{
		{Axis: AxisStandards, Status: PassBlocked, BlockedReason: "budget"},
		{Axis: AxisSpec, Status: PassBlocked, BlockedReason: "timeout"},
	})
	if err != nil || !got.Blocked || got.BlockedReason != "budget" || got.SeatVerdict != "" {
		t.Fatalf("Combine(both blocked) = %+v, %v; want BLOCKED with the first reason", got, err)
	}
	if none, _ := Combine(nil); !none.Blocked {
		t.Fatalf("Combine(no passes) = %+v, want fail closed", none)
	}
}

func TestCombine_SkippedAndOpenPasses(t *testing.T) {
	t.Parallel()

	got, err := Combine([]PassResult{
		{Axis: AxisStandards, Status: PassDone, SeatVerdict: VerdictAccepted},
		{Axis: AxisSpec, Status: PassSkipped},
	})
	if err != nil || got.SeatVerdict != VerdictAccepted {
		t.Fatalf("Combine(standards ACCEPTED, spec skipped) = %+v, %v; want ACCEPTED (skipped is not a failure)", got, err)
	}
	for _, status := range []string{PassPending, PassRunning} {
		if _, err := Combine([]PassResult{{Axis: AxisStandards, Status: PassDone, SeatVerdict: VerdictAccepted}, {Axis: AxisSpec, Status: status}}); !errors.Is(err, ErrPassesOpen) {
			t.Fatalf("Combine(spec %s) error = %v, want ErrPassesOpen", status, err)
		}
	}
}

func TestClamp_UnchangedForSinglePass(t *testing.T) {
	t.Parallel()

	gateSets := [][]Gate{
		{{ID: "ci", State: GatePass}},
		{{ID: "ci", State: GateFail}},
		{{ID: "no_secrets", State: GateFail}},
		{{ID: "ci", State: GateUnknown}},
	}
	for _, gs := range gateSets {
		for _, v := range []string{VerdictAccepted, VerdictChanges, VerdictBlocked} {
			c, err := Combine([]PassResult{{Axis: AxisStandards, Status: PassDone, SeatVerdict: v}})
			if err != nil || !reflect.DeepEqual(Clamp(c.SeatVerdict, gs), Clamp(v, gs)) {
				t.Fatalf("Clamp(Combine(single %s)) differs from Clamp(%s) over %v", v, v, gs)
			}
		}
	}
}

func axisDigest(criteria ...string) Digest {
	d := Digest{V: 1, Gates: []Gate{{ID: "ci", State: GatePass}}}
	if len(criteria) > 0 {
		d.Spec = &SpecSection{}
		for _, id := range criteria {
			d.Spec.Criteria = append(d.Spec.Criteria, SpecCriterion{ID: id})
		}
	}
	return d
}

func standardsVerdict(verdict string, findings ...AxisFinding) AxisVerdict {
	return AxisVerdict{V: 1, ReviewID: 1, Axis: AxisStandards, Verdict: verdict, Summary: "judged against the base standards", Findings: findings}
}

func specVerdict(verdict string, criteria ...CriterionResult) AxisVerdict {
	return AxisVerdict{V: 1, ReviewID: 1, Axis: AxisSpec, Verdict: verdict, Summary: "judged against the spec", Criteria: criteria,
		Gates: []GateAck{{ID: "ci", Ack: true}}}
}

func TestSeatVerdict_AxisMustMatchToken(t *testing.T) {
	t.Parallel()

	v := standardsVerdict(VerdictAccepted)
	if err := CheckAxisVerdict(v, axisDigest(), AxisStandards, nil); err != nil {
		t.Fatalf("CheckAxisVerdict(own axis) error = %v", err)
	}
	err := CheckAxisVerdict(v, axisDigest(), AxisSpec, nil)
	var ve VerdictError
	if !errors.As(err, &ve) || ve.Field != "axis" {
		t.Fatalf("CheckAxisVerdict(standards verdict on the spec pass) error = %v, want an axis VerdictError", err)
	}
	f := AxisFinding{SeatFinding: SeatFinding{File: "a.go", Line: 1, Why: "w", Check: "c", Severity: "P2"}, Axis: AxisSpec, Rule: "AGENTS.md: x"}
	if err := CheckAxisVerdict(standardsVerdict(VerdictChanges, f), axisDigest(), AxisStandards, nil); err == nil {
		t.Fatal("CheckAxisVerdict(finding tagged with the other axis) = nil, want an error")
	}
}

func TestSeatVerdict_StandardsFindingNeedsRule(t *testing.T) {
	t.Parallel()

	f := AxisFinding{SeatFinding: SeatFinding{File: "a.go", Line: 4, Why: "naming", Check: "rename", Severity: "P2"}}
	err := CheckAxisVerdict(standardsVerdict(VerdictChanges, f), axisDigest(), AxisStandards, nil)
	var ve VerdictError
	if !errors.As(err, &ve) || ve.Field != "findings[0].rule" {
		t.Fatalf("error = %v, want findings[0].rule", err)
	}
	f.Rule = "AGENTS.md: run task lint before done"
	if err := CheckAxisVerdict(standardsVerdict(VerdictChanges, f), axisDigest(), AxisStandards, nil); err != nil {
		t.Fatalf("CHANGES with a cited rule error = %v", err)
	}
	if err := CheckAxisVerdict(standardsVerdict(VerdictChanges), axisDigest(), AxisStandards, nil); err == nil {
		t.Fatal("CHANGES without findings = nil, want an error")
	}
	if err := CheckAxisVerdict(standardsVerdict(VerdictAccepted, AxisFinding{SeatFinding: SeatFinding{File: "a.go", Line: 1, Why: "w", Check: "c", Severity: "P1"}, Rule: "r"}), axisDigest(), AxisStandards, nil); err == nil {
		t.Fatal("ACCEPTED with a P1 finding = nil, want an error")
	}
	std := standardsVerdict(VerdictAccepted)
	std.Criteria = []CriterionResult{{ID: "AC1", Status: CriterionMet, Evidence: "plenty of evidence here"}}
	if err := CheckAxisVerdict(std, axisDigest(), AxisStandards, nil); err == nil {
		t.Fatal("standards verdict with criteria = nil, want an error")
	}
}

func TestSeatVerdict_SpecAcceptedNeedsEveryCriterion(t *testing.T) {
	t.Parallel()

	d := axisDigest("AC1", "AC2")
	ids := []string{"AC1", "AC2"}
	met := func(id string) CriterionResult {
		return CriterionResult{ID: id, Status: CriterionMet, Evidence: "internal/steward/combine.go covers it"}
	}
	if err := CheckAxisVerdict(specVerdict(VerdictAccepted, met("AC1"), met("AC2")), d, AxisSpec, ids); err != nil {
		t.Fatalf("ACCEPTED with every criterion error = %v", err)
	}
	for name, v := range map[string]AxisVerdict{
		"missing criterion":   specVerdict(VerdictAccepted, met("AC1")),
		"unknown criterion":   specVerdict(VerdictAccepted, met("AC1"), met("AC2"), met("AC9")),
		"duplicate":           specVerdict(VerdictAccepted, met("AC1"), met("AC1"), met("AC2")),
		"no evidence":         specVerdict(VerdictAccepted, met("AC1"), CriterionResult{ID: "AC2", Status: CriterionMet, Evidence: " ok "}),
		"unmet in an ACCEPT":  specVerdict(VerdictAccepted, met("AC1"), CriterionResult{ID: "AC2", Status: CriterionUnmet}),
		"no gate ack":         {V: 1, ReviewID: 1, Axis: AxisSpec, Verdict: VerdictAccepted, Summary: "s", Criteria: []CriterionResult{met("AC1"), met("AC2")}},
		"unknown gate in ack": {V: 1, ReviewID: 1, Axis: AxisSpec, Verdict: VerdictAccepted, Summary: "s", Gates: []GateAck{{ID: "nope", Ack: true}}, Criteria: []CriterionResult{met("AC1"), met("AC2")}},
	} {
		if err := CheckAxisVerdict(v, d, AxisSpec, ids); err == nil {
			t.Fatalf("%s: CheckAxisVerdict = nil, want an error", name)
		}
	}
	// An unmet criterion alone justifies CHANGES; a spec verdict needs no rule.
	if err := CheckAxisVerdict(specVerdict(VerdictChanges, met("AC1"), CriterionResult{ID: "AC2", Status: CriterionUnmet}), d, AxisSpec, ids); err != nil {
		t.Fatalf("CHANGES with an unmet criterion error = %v", err)
	}
	// The Standards pass is shown neither gates nor images.
	if err := CheckAxisVerdict(standardsVerdict(VerdictAccepted), d, AxisStandards, nil); err != nil {
		t.Fatalf("standards ACCEPTED without gate acks error = %v", err)
	}
}

func TestSeatVerdict_UnknownFieldsStillRejected(t *testing.T) {
	t.Parallel()

	if _, err := ParseAxisVerdict([]byte(`{"v":1,"reviewId":1,"axis":"spec","verdict":"ACCEPTED","summary":"s","extra":1}`)); err == nil {
		t.Fatal("ParseAxisVerdict(unknown field) = nil, want an error")
	}
	if _, err := ParseAxisVerdict([]byte(`{"v":1,"reviewId":1,"axis":"other","verdict":"ACCEPTED","summary":"s"}`)); err == nil {
		t.Fatal("ParseAxisVerdict(axis other) = nil, want an error")
	}
	// The single-pass document is untouched: it knows none of the new fields,
	// so a single-pass seat cannot smuggle an axis in.
	for _, raw := range []string{
		`{"v":1,"reviewId":1,"axis":"spec","verdict":"ACCEPTED","summary":"s"}`,
		`{"v":1,"reviewId":1,"verdict":"ACCEPTED","summary":"s","criteria":[]}`,
		`{"v":1,"reviewId":1,"verdict":"CHANGES","summary":"s","findings":[{"file":"a","line":1,"why":"w","check":"c","rule":"r"}]}`,
	} {
		if _, err := ParseSeatVerdict([]byte(raw)); err == nil {
			t.Fatalf("ParseSeatVerdict(%s) = nil, want an unknown-field error", raw)
		}
	}
	if _, err := ParseAxisVerdict([]byte(`{"v":1,"reviewId":1,"axis":"spec","verdict":"ACCEPTED","summary":"s","criteria":[{"id":"AC1","status":"maybe","evidence":"e"}]}`)); err == nil {
		t.Fatal("ParseAxisVerdict(bad criterion status) = nil, want an error")
	}
}

func TestSeatVerdict_SchemaCapsCoverNewFields(t *testing.T) {
	t.Parallel()

	caps, arrays := AxisSchemaCaps(), AxisArrayCaps()
	for _, k := range []string{"findings[].rule", "criteria[].id", "criteria[].evidence", "summary", "findings[].why"} {
		if caps[k] <= 0 {
			t.Fatalf("AxisSchemaCaps()[%q] = %d, want a cap", k, caps[k])
		}
	}
	if arrays["criteria"] <= 0 || arrays["findings"] <= 0 {
		t.Fatalf("AxisArrayCaps() = %v, want criteria and findings caps", arrays)
	}
	// The single-pass seat's tool schema is built from SchemaCaps/ArrayCaps
	// and the SeatVerdict type: neither may learn the new fields.
	if _, ok := SchemaCaps()["findings[].rule"]; ok {
		t.Fatal("SchemaCaps() carries a two-axis field: the single-pass tool schema changed")
	}
	if _, ok := ArrayCaps()["criteria"]; ok {
		t.Fatal("ArrayCaps() carries a two-axis field: the single-pass tool schema changed")
	}
	if _, ok := reflect.TypeOf(SeatVerdict{}).FieldByName("Axis"); ok {
		t.Fatal("SeatVerdict gained Axis: the single-pass tool schema changed")
	}
	if _, ok := reflect.TypeOf(SeatFinding{}).FieldByName("Rule"); ok {
		t.Fatal("SeatFinding gained Rule: the single-pass tool schema changed")
	}
	long := strings.Repeat("x", axisFindingRuleMax+1)
	raw, _ := json.Marshal(AxisVerdict{V: 1, ReviewID: 1, Axis: AxisStandards, Verdict: VerdictChanges, Summary: "s",
		Findings: []AxisFinding{{SeatFinding: SeatFinding{File: "a", Line: 1, Why: "w", Check: "c"}, Rule: long}}})
	if _, err := ParseAxisVerdict(raw); err == nil {
		t.Fatal("ParseAxisVerdict(over-long rule) = nil, want a cap error")
	}
}

// twoAxisFixture is a running two-axis review with both passes open, each
// with its own run and token, and a digest carrying the given criteria.
type twoAxisFixture struct {
	svc    *Service
	row    store.StewardReview
	tokens map[Axis]string
	runs   map[Axis]string
}

func openTwoAxis(t *testing.T, d Digest) twoAxisFixture {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t)
	row, _ := openReview(t, svc, string(b))
	fx := twoAxisFixture{svc: svc, row: row, tokens: map[Axis]string{}, runs: map[Axis]string{}}
	for _, axis := range []Axis{AxisStandards, AxisSpec} {
		tok, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		fx.tokens[axis], fx.runs[axis] = tok, "run-"+string(axis)
		if _, err := svc.DB.InsertStewardReviewPass(store.StewardReviewPass{ReviewID: row.ID, Axis: string(axis), Status: PassRunning,
			RunID: fx.runs[axis], TokenHash: HashToken(tok)}, time.Now().UTC()); err != nil {
			t.Fatalf("InsertStewardReviewPass(%s) error = %v", axis, err)
		}
	}
	return fx
}

func (fx twoAxisFixture) submit(t *testing.T, axis Axis, v AxisVerdict) (View, error) {
	t.Helper()
	v.ReviewID = fx.row.ID
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return fx.svc.SubmitPassVerdict(fx.row.ID, axis, fx.tokens[axis], fx.runs[axis], raw)
}

func okFinding(rule string) AxisFinding {
	return AxisFinding{SeatFinding: SeatFinding{File: "a.go", Line: 7, Why: "reason", Check: "go test ./...", Severity: "P2"}, Rule: rule}
}

func TestTokenAxis_StandardsSeatCannotSubmitSpec(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	// The standards token with a spec verdict: refused, nothing consumed.
	v := specVerdict(VerdictAccepted, CriterionResult{ID: "AC1", Status: CriterionMet, Evidence: "evidence of the criterion"})
	v.ReviewID = fx.row.ID
	raw, _ := json.Marshal(v)
	if _, err := fx.svc.SubmitPassVerdict(fx.row.ID, AxisStandards, fx.tokens[AxisStandards], fx.runs[AxisStandards], raw); err == nil {
		t.Fatal("standards token + spec verdict = nil, want an axis error")
	}
	// The standards token on the spec pass: the hash does not match.
	if _, err := fx.svc.SubmitPassVerdict(fx.row.ID, AxisSpec, fx.tokens[AxisStandards], fx.runs[AxisSpec], raw); !errors.Is(err, ErrBadToken) {
		t.Fatalf("standards token on the spec pass error = %v, want ErrBadToken", err)
	}
	// The standards run with the spec token.
	if _, err := fx.svc.SubmitPassVerdict(fx.row.ID, AxisSpec, fx.tokens[AxisSpec], fx.runs[AxisStandards], raw); !errors.Is(err, ErrWrongRun) {
		t.Fatalf("standards run on the spec pass error = %v, want ErrWrongRun", err)
	}
	// The mirror image, without criteria to give the lie away: a spec-labelled
	// verdict that is otherwise a valid standards one, on the standards pass.
	lie := standardsVerdict(VerdictAccepted)
	lie.Axis, lie.ReviewID = AxisSpec, fx.row.ID
	lieRaw, _ := json.Marshal(lie)
	if _, err := fx.svc.SubmitPassVerdict(fx.row.ID, AxisStandards, fx.tokens[AxisStandards], fx.runs[AxisStandards], lieRaw); err == nil {
		t.Fatal("spec-labelled verdict on the standards pass = nil, want an axis error")
	}
	for _, axis := range []Axis{AxisStandards, AxisSpec} {
		p, _ := fx.svc.DB.GetStewardReviewPass(fx.row.ID, string(axis))
		if p.TokenUsed || p.Status != PassRunning {
			t.Fatalf("%s pass after refused submissions = %+v, want untouched", axis, p)
		}
	}
	if _, err := fx.submit(t, AxisSpec, v); err != nil {
		t.Fatalf("the spec pass with its own token error = %v", err)
	}
	if _, err := fx.submit(t, AxisSpec, v); !errors.Is(err, ErrVerdictExists) {
		t.Fatalf("a second spec verdict error = %v, want ErrVerdictExists", err)
	}
}

func TestPassVerdict_BothAcceptedStoresAccepted(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	got, err := fx.submit(t, AxisStandards, standardsVerdict(VerdictAccepted, okFinding("AGENTS.md: tests for prod code")))
	if err != nil || got.Verdict != "" || got.Status != "running" || len(got.Axes) != 2 {
		t.Fatalf("after the first pass = %+v, %v; want the review still open with two axes", got, err)
	}
	got, err = fx.submit(t, AxisSpec, specVerdict(VerdictAccepted, CriterionResult{ID: "AC1", Status: CriterionMet, Evidence: "combine.go implements it"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictAccepted || got.SeatVerdict != VerdictAccepted || got.Status != "done" {
		t.Fatalf("view = %+v, want a done ACCEPTED review", got)
	}
	if len(got.Findings) != 1 || got.Findings[0].Axis != AxisStandards || got.Findings[0].Rule == "" {
		t.Fatalf("findings = %+v, want the standards finding tagged with its axis and rule", got.Findings)
	}
	if got.Axes[1].Axis != AxisSpec || len(got.Axes[1].Criteria) != 1 || got.Axes[0].SeatVerdict != VerdictAccepted {
		t.Fatalf("axes = %+v, want standards then spec with the criteria", got.Axes)
	}
}

func TestPassVerdict_AxesDisagreeStoresLower(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	if _, err := fx.submit(t, AxisStandards, standardsVerdict(VerdictChanges, okFinding("AGENTS.md: naming"))); err != nil {
		t.Fatal(err)
	}
	got, err := fx.submit(t, AxisSpec, specVerdict(VerdictAccepted, CriterionResult{ID: "AC1", Status: CriterionMet, Evidence: "combine.go implements it"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictChanges || got.SeatVerdict != VerdictChanges || got.MergeReady {
		t.Fatalf("view = %+v, want CHANGES and not merge-ready", got)
	}
	if !strings.Contains(got.Summary, "axis standards: CHANGES") {
		t.Fatalf("summary = %q, want the lowering axis named", got.Summary)
	}
}

func TestPassVerdict_GateClampsCombinedVerdict(t *testing.T) {
	t.Parallel()

	d := axisDigest("AC1")
	d.Gates = []Gate{{ID: "ci", State: GateFail, Detail: "red"}}
	fx := openTwoAxis(t, d)
	if _, err := fx.submit(t, AxisStandards, standardsVerdict(VerdictAccepted)); err != nil {
		t.Fatal(err)
	}
	got, err := fx.submit(t, AxisSpec, specVerdict(VerdictAccepted, CriterionResult{ID: "AC1", Status: CriterionMet, Evidence: "combine.go implements it"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictChanges || got.SeatVerdict != VerdictAccepted || !strings.Contains(got.Summary, "clamped to CHANGES") {
		t.Fatalf("view = %+v, want CHANGES clamped from an ACCEPTED seat verdict", got)
	}
}

func TestPassVerdict_FailedAxisNeverAccepted(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	if _, err := fx.submit(t, AxisSpec, specVerdict(VerdictAccepted, CriterionResult{ID: "AC1", Status: CriterionMet, Evidence: "combine.go implements it"})); err != nil {
		t.Fatal(err)
	}
	if err := fx.svc.WritePassBlocked(fx.row.ID, AxisStandards, "timeout", "ran out of time", 0.2); err != nil {
		t.Fatal(err)
	}
	got, err := fx.svc.View(fx.row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictChanges || got.Status != "done" || got.MergeReady {
		t.Fatalf("view = %+v, want CHANGES, never ACCEPTED, with one axis failed", got)
	}
	if !strings.Contains(got.Summary, "axis standards did not complete (timeout)") || got.CostUSD != 0.2 {
		t.Fatalf("summary = %q cost = %v, want the failed axis named and the pass cost summed", got.Summary, got.CostUSD)
	}
	if got.Axes[0].Status != PassBlocked || got.Axes[0].BlockedReason != "timeout" {
		t.Fatalf("axes = %+v, want standards blocked with its reason", got.Axes)
	}
}

func TestPassVerdict_BothFailedBlocksReview(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	if err := fx.svc.WritePassBlocked(fx.row.ID, AxisStandards, "budget", "over budget", 0.4); err != nil {
		t.Fatal(err)
	}
	if err := fx.svc.WritePassBlocked(fx.row.ID, AxisSpec, "crashed", "seat crashed", 0.1); err != nil {
		t.Fatal(err)
	}
	got, _ := fx.svc.View(fx.row.ID)
	if got.Verdict != VerdictBlocked || got.Status != "blocked" || got.BlockedReason != "budget" || got.MergeReady {
		t.Fatalf("view = %+v, want a blocked review with the first reason", got)
	}
	if err := fx.svc.WritePassBlocked(fx.row.ID, AxisSpec, "not-a-reason", "x", 0); err == nil {
		t.Fatal("WritePassBlocked(non-daemon reason) = nil, want an error")
	}
}

func TestPassVerdict_SkippedSpecDoesNotBlockOrAccept(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest())
	if err := fx.svc.WritePassSkipped(fx.row.ID, AxisSpec); err != nil {
		t.Fatal(err)
	}
	got, err := fx.submit(t, AxisStandards, standardsVerdict(VerdictAccepted))
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictAccepted || got.Axes[1].Status != PassSkipped || got.Axes[1].Summary != "no spec available" || !strings.Contains(got.Summary, "axis spec skipped") {
		t.Fatalf("view = %+v, want ACCEPTED with the spec axis skipped as no spec available", got)
	}
}

func TestPassVerdict_IncompleteConsumesNothing(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	if _, err := fx.submit(t, AxisSpec, specVerdict(VerdictAccepted)); !errors.Is(err, ErrInvalidVerdict) {
		t.Fatalf("spec ACCEPTED without criteria error = %v, want a VerdictError", err)
	}
	p, _ := fx.svc.DB.GetStewardReviewPass(fx.row.ID, string(AxisSpec))
	if p.TokenUsed || p.Status != PassRunning {
		t.Fatalf("spec pass = %+v, want the token unconsumed", p)
	}
}

func TestPassVerdict_HoldWhenGateUnknownConsumesNothing(t *testing.T) {
	t.Parallel()

	d := axisDigest()
	d.Gates = []Gate{{ID: "ci", State: GateUnknown}}
	fx := openTwoAxis(t, d)
	v := standardsVerdict(VerdictAccepted)
	if _, err := fx.submit(t, AxisStandards, v); err == nil {
		t.Fatal("submitting with an unknown gate = nil, want an error")
	}
	if p, _ := fx.svc.DB.GetStewardReviewPass(fx.row.ID, string(AxisStandards)); p.TokenUsed {
		t.Fatal("the token was consumed while a gate is unknown")
	}
}

func TestView_AxesAbsentWhenSinglePass(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
	got, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if got.Axes != nil || strings.Contains(string(b), `"axes"`) {
		t.Fatalf("single-pass view JSON carries axes: %s", b)
	}
	if strings.Contains(string(b), `"rule"`) {
		t.Fatalf("single-pass view JSON carries a rule: %s", b)
	}
}

func TestSubmitVerdict_SinglePassPathIgnoresPassRows(t *testing.T) {
	t.Parallel()

	// A review without pass rows still takes the single-pass verdict path, and
	// its own token, exactly as before.
	svc := newTestService(t)
	row, token := openReview(t, svc, digestWithGates(Gate{ID: "ci", State: GatePass}))
	raw, _ := json.Marshal(map[string]any{"v": 1, "reviewId": row.ID, "verdict": "ACCEPTED", "summary": "s", "axis": "spec", "gates": []GateAck{{ID: "ci", Ack: true}}})
	if _, err := svc.SubmitVerdict(row.ID, token, row.RunID, raw); !errors.Is(err, ErrInvalidVerdict) {
		t.Fatalf("SubmitVerdict(with axis) error = %v, want an unknown-field VerdictError", err)
	}
	if _, err := svc.SubmitVerdict(row.ID, token, row.RunID, verdictRaw(row.ID, VerdictAccepted, "ci")); err != nil {
		t.Fatalf("SubmitVerdict() error = %v", err)
	}
}

func TestSubmitVerdict_RefusedOnTwoAxisReview(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest())
	// Even the review's own run and a token that matches the review row cannot
	// decide a two-axis review in one stroke.
	tok, _ := NewToken()
	if err := fx.svc.DB.SetStewardReviewToken(fx.row.ID, HashToken(tok)); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.svc.SubmitVerdict(fx.row.ID, tok, fx.row.RunID, verdictRaw(fx.row.ID, VerdictAccepted, "ci")); !errors.Is(err, ErrWrongRun) {
		t.Fatalf("SubmitVerdict on a two-axis review error = %v, want ErrWrongRun", err)
	}
	if row, _ := fx.svc.DB.GetStewardReview(fx.row.ID); row.Verdict != "" || row.TokenUsed {
		t.Fatalf("review = %+v, want it untouched", row)
	}
}

func TestPassSkipped_OnlySpecAndOnlyWithoutCriteria(t *testing.T) {
	t.Parallel()

	// Skipping the standards pass would let the spec pass alone ACCEPT.
	fx := openTwoAxis(t, axisDigest())
	if err := fx.svc.WritePassSkipped(fx.row.ID, AxisStandards); err == nil {
		t.Fatal("WritePassSkipped(standards) = nil, want a refusal")
	}
	if p, _ := fx.svc.DB.GetStewardReviewPass(fx.row.ID, string(AxisStandards)); p.Status != PassRunning {
		t.Fatalf("standards pass = %+v, want it untouched", p)
	}
	// A spec pass with criteria to judge cannot be skipped either.
	fx = openTwoAxis(t, axisDigest("AC1"))
	if err := fx.svc.WritePassSkipped(fx.row.ID, AxisSpec); err == nil {
		t.Fatal("WritePassSkipped(spec with criteria) = nil, want a refusal")
	}
}

func TestSeatVerdict_SpecAcceptedNeedsCriteriaToJudge(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest())
	if _, err := fx.submit(t, AxisSpec, specVerdict(VerdictAccepted)); !errors.Is(err, ErrInvalidVerdict) {
		t.Fatalf("spec ACCEPTED with no criteria in the digest error = %v, want a VerdictError", err)
	}
}

func TestCombine_ScopeViolationBlocksWholeReview(t *testing.T) {
	t.Parallel()

	for name, other := range map[string]PassResult{
		"other accepted": {Axis: AxisSpec, Status: PassDone, SeatVerdict: VerdictAccepted},
		"other open":     {Axis: AxisSpec, Status: PassRunning},
		"other skipped":  {Axis: AxisSpec, Status: PassSkipped},
	} {
		got, err := Combine([]PassResult{{Axis: AxisStandards, Status: PassBlocked, BlockedReason: "scope_violation"}, other})
		if err != nil || !got.Blocked || got.BlockedReason != "scope_violation" || !got.ScopeViolation || got.SeatVerdict != "" {
			t.Fatalf("%s: Combine() = %+v, %v; want the whole review BLOCKED with scope_violation", name, got, err)
		}
	}
	// An ordinary failed axis is not that.
	got, _ := Combine([]PassResult{{Axis: AxisStandards, Status: PassBlocked, BlockedReason: "timeout"}, {Axis: AxisSpec, Status: PassDone, SeatVerdict: VerdictAccepted}})
	if got.Blocked || got.ScopeViolation {
		t.Fatalf("Combine(timeout + ACCEPTED) = %+v, want CHANGES, not a scope violation", got)
	}
}

func TestPassVerdict_ScopeViolationBlocksWholeReview(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	if _, err := fx.submit(t, AxisSpec, specVerdict(VerdictAccepted, CriterionResult{ID: "AC1", Status: CriterionMet, Evidence: "combine.go implements it"})); err != nil {
		t.Fatal(err)
	}
	if err := fx.svc.WritePassBlocked(fx.row.ID, AxisStandards, "scope_violation", "the lead's worktree changed", 0.1); err != nil {
		t.Fatal(err)
	}
	got, err := fx.svc.View(fx.row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictBlocked || got.Status != "blocked" || got.BlockedReason != "scope_violation" || !got.ScopeViolation || got.MergeReady {
		t.Fatalf("view = %+v, want BLOCKED, scope_violation, flag set, not merge-ready", got)
	}
}

func TestPassVerdict_ScopeViolationDoesNotWaitForTheOtherPass(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, axisDigest("AC1"))
	if err := fx.svc.WritePassBlocked(fx.row.ID, AxisSpec, "scope_violation", "the lead's worktree changed", 0); err != nil {
		t.Fatal(err)
	}
	got, _ := fx.svc.View(fx.row.ID)
	if got.Verdict != VerdictBlocked || got.BlockedReason != "scope_violation" || !got.ScopeViolation {
		t.Fatalf("view = %+v, want the review blocked at once", got)
	}
	// The still-open pass cannot reopen it.
	if _, err := fx.submit(t, AxisStandards, standardsVerdict(VerdictAccepted)); !errors.Is(err, ErrVerdictExists) {
		t.Fatalf("late standards verdict error = %v, want ErrVerdictExists", err)
	}
}

// TestCombine_NotRunPassCapsAtChangesAndIsNeverTheBlockReason: a pass a one-axis
// re-run did not request is a failed axis (the review can never be ACCEPTED), but
// when nothing produced a verdict the review is blocked for a real reason.
func TestCombine_NotRunPassCapsAtChangesAndIsNeverTheBlockReason(t *testing.T) {
	t.Parallel()

	got, err := Combine([]PassResult{
		{Axis: AxisStandards, Status: PassDone, SeatVerdict: VerdictAccepted},
		{Axis: AxisSpec, Status: PassBlocked, BlockedReason: "not_run"},
	})
	if err != nil || got.Blocked || got.SeatVerdict != VerdictChanges {
		t.Fatalf("Combine(standards ACCEPTED, spec not run) = %+v, %v; want CHANGES", got, err)
	}
	got, _ = Combine([]PassResult{
		{Axis: AxisStandards, Status: PassBlocked, BlockedReason: "not_run"},
		{Axis: AxisSpec, Status: PassBlocked, BlockedReason: "timeout"},
	})
	if !got.Blocked || got.BlockedReason != "timeout" {
		t.Fatalf("Combine(not run + timeout) = %+v, want BLOCKED for timeout", got)
	}
	got, _ = Combine([]PassResult{
		{Axis: AxisStandards, Status: PassBlocked, BlockedReason: "not_run"},
		{Axis: AxisSpec, Status: PassSkipped},
	})
	if !got.Blocked || got.BlockedReason != "crashed" {
		t.Fatalf("Combine(not run + skipped) = %+v, want BLOCKED with the crashed fallback", got)
	}
}
