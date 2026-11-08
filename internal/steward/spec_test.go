package steward

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestSpec_SourcesResolveInOrder(t *testing.T) {
	in := Inputs{PR: &PRInfo{Body: "Spec: docs/plans/active/x.md and more words\n\nCloses #12. See ADR-0054 and ADR-0047.\n\nRefs #3\n"}}
	in.TwoAxis = &TwoAxisInputs{Spec: SpecInput{
		ContractCriteria: []SpecCriterionInput{{ID: "AC1", Text: "criterion one", NamedTests: []string{"TestFoo"}}},
		PlanPath:         "docs/plans/active/x.md",
		PlanText:         "# Plan\n\nbody\n",
		ADRFiles:         []string{"0047-steward-seat.md", "0054-steward-two-axis-review.md"},
	}}

	s, texts := resolveSpec(in)
	sum := sha256.Sum256([]byte("# Plan\n\nbody\n"))
	want := []SpecSource{
		{Kind: SpecSourceContract, Ref: "validation-contract", Status: SpecStatusFound},
		{Kind: SpecSourcePlan, Ref: "docs/plans/active/x.md", Status: SpecStatusFound, SHA256: hex.EncodeToString(sum[:])},
		{Kind: SpecSourcePRBody, Ref: "docs/plans/active/x.md", Status: SpecStatusFound},
		{Kind: SpecSourceIssue, Ref: "#12", Status: SpecStatusUnavailable},
		{Kind: SpecSourceIssue, Ref: "#3", Status: SpecStatusUnavailable},
		{Kind: SpecSourceADR, Ref: "ADR-0054", Status: SpecStatusFound},
		{Kind: SpecSourceADR, Ref: "ADR-0047", Status: SpecStatusFound},
	}
	if !reflect.DeepEqual(s.Sources, want) {
		t.Fatalf("sources = %+v, want %+v", s.Sources, want)
	}
	if len(s.Criteria) != 1 || s.Criteria[0].ID != "AC1" {
		t.Fatalf("criteria = %+v, want the contract's AC1", s.Criteria)
	}
	if texts["AC1"] != "criterion one" {
		t.Errorf("texts[AC1] = %q, want the contract text", texts["AC1"])
	}
	if !reflect.DeepEqual(s.TestsInTree, map[string]bool{"TestFoo": false}) {
		t.Errorf("testsInTree = %v, want TestFoo absent from the tree", s.TestsInTree)
	}
	if !s.HasSpec() {
		t.Error("HasSpec() = false, want true")
	}
}

func TestSpec_ContractBeatsPlanOnSameID(t *testing.T) {
	in := Inputs{TwoAxis: &TwoAxisInputs{Spec: SpecInput{
		ContractCriteria: []SpecCriterionInput{{ID: "AC1", Text: "contract text", NamedTests: []string{"TestContract"}}},
		PlanPath:         "docs/plans/active/x.md",
		PlanText:         "## Acceptance Criteria\n\n- AC1 plan text naming TestPlan\n- AC2 plan only\n",
	}}}

	s, texts := resolveSpec(in)
	if len(s.Criteria) != 2 || s.Criteria[0].ID != "AC1" || s.Criteria[1].ID != "AC2" {
		t.Fatalf("criteria = %+v, want AC1 then AC2", s.Criteria)
	}
	if got := s.Criteria[0].NamedTests; len(got) != 1 || got[0] != "TestContract" {
		t.Errorf("AC1 namedTests = %v, want the contract's [TestContract] (contract wins)", got)
	}
	if texts["AC1"] != "contract text" {
		t.Errorf("texts[AC1] = %q, want the contract text", texts["AC1"])
	}
	if texts["AC2"] != "plan only" {
		t.Errorf("texts[AC2] = %q, want the plan text", texts["AC2"])
	}
}

func TestSpec_PlanAcceptanceCriteriaParsed(t *testing.T) {
	plan := "# Plan\n\nintro\n\n## Acceptance Criteria\n\n- AC1 first criterion, verified by TestAlpha and TestBeta\n- AC10a a letter suffix\n- not-a-bullet AC3\n- AC4\n\n## Test Scenarios\n\n- AC99 must not be picked up here\n"
	in := Inputs{TwoAxis: &TwoAxisInputs{Spec: SpecInput{
		PlanPath:    "docs/plans/active/x.md",
		PlanText:    plan,
		TestsInTree: map[string]bool{"TestAlpha": true},
	}}}

	s, texts := resolveSpec(in)
	if len(s.Criteria) != 2 || s.Criteria[0].ID != "AC1" || s.Criteria[1].ID != "AC10a" {
		t.Fatalf("criteria = %+v, want only AC1 and AC10a from the section", s.Criteria)
	}
	if want := []string{"TestAlpha", "TestBeta"}; !reflect.DeepEqual(s.Criteria[0].NamedTests, want) {
		t.Errorf("AC1 namedTests = %v, want %v", s.Criteria[0].NamedTests, want)
	}
	if len(s.Criteria[1].NamedTests) != 0 {
		t.Errorf("AC10a namedTests = %v, want none", s.Criteria[1].NamedTests)
	}
	if texts["AC1"] != "first criterion, verified by TestAlpha and TestBeta" {
		t.Errorf("texts[AC1] = %q, want the bullet text", texts["AC1"])
	}
	if want := map[string]bool{"TestAlpha": true, "TestBeta": false}; !reflect.DeepEqual(s.TestsInTree, want) {
		t.Errorf("testsInTree = %v, want %v", s.TestsInTree, want)
	}
}

func TestSpec_IssueRefIsUnavailable(t *testing.T) {
	in := Inputs{PR: &PRInfo{Body: "Fixes #7, closes #8 and refs #9; an unrelated #10 stays out.\n"}}
	in.TwoAxis = &TwoAxisInputs{}

	s, _ := resolveSpec(in)
	if len(s.Sources) != 3 {
		t.Fatalf("sources = %+v, want the three issue refs and no more", s.Sources)
	}
	for i, ref := range []string{"#7", "#8", "#9"} {
		if s.Sources[i].Kind != SpecSourceIssue || s.Sources[i].Ref != ref || s.Sources[i].Status != SpecStatusUnavailable {
			t.Errorf("sources[%d] = %+v, want issue %s unavailable", i, s.Sources[i], ref)
		}
	}
	if s.HasSpec() {
		t.Error("HasSpec() = true, want false: no issue API, so nothing judgeable was resolved")
	}
}

func TestSpec_ADRMentionFoundOrMissing(t *testing.T) {
	in := Inputs{PR: &PRInfo{Body: "Implements ADR-0054 on top of adr-0047 and ADR-9999.\n"}}
	in.TwoAxis = &TwoAxisInputs{Spec: SpecInput{ADRFiles: []string{"0054-steward-two-axis-review.md", "0046-other.md"}}}

	s, _ := resolveSpec(in)
	if len(s.Sources) != 3 {
		t.Fatalf("sources = %+v, want three ADR mentions", s.Sources)
	}
	want := []struct{ ref, status string }{
		{"ADR-0054", SpecStatusFound},
		{"ADR-0047", SpecStatusMissing},
		{"ADR-9999", SpecStatusMissing},
	}
	for i, w := range want {
		if s.Sources[i].Kind != SpecSourceADR || s.Sources[i].Ref != w.ref || s.Sources[i].Status != w.status {
			t.Errorf("sources[%d] = %+v, want adr %s %s", i, s.Sources[i], w.ref, w.status)
		}
	}
}

func TestSpec_NoSpecHasNoSources(t *testing.T) {
	s, texts := resolveSpec(Inputs{TwoAxis: &TwoAxisInputs{}})
	if len(s.Sources) != 0 || len(s.Criteria) != 0 || len(texts) != 0 {
		t.Errorf("sources = %+v criteria = %+v texts = %v, want everything empty", s.Sources, s.Criteria, texts)
	}
	if s.HasSpec() {
		t.Error("HasSpec() = true, want false with nothing resolved")
	}
	if s.Sources == nil || s.Criteria == nil || s.TestsInTree == nil {
		t.Error("the section's lists must never be nil")
	}
}

func TestSpec_BadIDsAndTestNamesDropped(t *testing.T) {
	in := Inputs{TwoAxis: &TwoAxisInputs{Spec: SpecInput{
		ContractCriteria: []SpecCriterionInput{
			{ID: "../x", Text: "pathy"},
			{ID: "bad\nid", Text: "newline"},
			{ID: strings.Repeat("A", 65), Text: "too long"},
			{ID: "ok", Text: "fine", NamedTests: []string{"Test`Back`tick", "Test With Spaces", strings.Repeat("T", 101), "TestFine", "TestFine"}},
		},
	}}}

	s, texts := resolveSpec(in)
	if len(s.Criteria) != 1 || s.Criteria[0].ID != "ok" {
		t.Fatalf("criteria = %+v, want only the well-formed id", s.Criteria)
	}
	if got := s.Criteria[0].NamedTests; len(got) != 1 || got[0] != "TestFine" {
		t.Errorf("namedTests = %v, want [TestFine] (bad and duplicate names dropped)", got)
	}
	if len(texts) != 1 {
		t.Errorf("texts = %v, want only the surviving criterion's text", texts)
	}
	if len(s.TestsInTree) != 1 {
		t.Errorf("testsInTree = %v, want only TestFoo-class survivors", s.TestsInTree)
	}
}
