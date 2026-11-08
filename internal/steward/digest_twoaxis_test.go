package steward

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// twoAxisBase is base() with two-axis inputs attached but nothing resolved
// yet: no head diff, no rules, an empty spec.
func twoAxisBase() Inputs {
	in := base()
	in.TwoAxis = &TwoAxisInputs{
		Standards: StandardsInput{
			BaseRef: strings.Repeat("c", 40),
			Files: []StandardsFileInput{
				{Path: "AGENTS.md", Bytes: 4180, SHA256: strings.Repeat("1", 64)},
				{Path: "CLAUDE.md", Bytes: 96, SHA256: strings.Repeat("2", 64), Truncated: true},
			},
		},
	}
	return in
}

func TestDigest_TwoAxisOffIsUnchanged(t *testing.T) {
	in := base()
	in.TwoAxis = nil

	data, _, err := Build(in).Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	before, _, _ := Build(base()).Marshal()
	if string(data) != string(before) {
		t.Error("the digest of the same Inputs differs between builds")
	}
	if strings.Contains(string(data), `"standards"`) {
		t.Errorf("single-pass digest carries a standards section: %s", data)
	}
	if strings.Contains(string(data), `"spec"`) {
		t.Errorf("single-pass digest carries a spec section: %s", data)
	}
}

func TestDigest_StandardsFilesBaseHashesAndChangedFlag(t *testing.T) {
	in := twoAxisBase()
	in.TwoAxis.Standards.HeadDiff = "diff --git a/AGENTS.md b/AGENTS.md\n--- a/AGENTS.md\n+++ b/AGENTS.md\n@@ -1 +1 @@\n-old\n+new\n"
	in.TwoAxis.Standards.Files = append(in.TwoAxis.Standards.Files,
		StandardsFileInput{Path: "CONTRIBUTING.md", Bytes: 5, SHA256: strings.Repeat("3", 64)})

	d := Build(in)
	if d.Standards == nil {
		t.Fatal("standards section is nil")
	}
	if d.Standards.BaseRef != in.TwoAxis.Standards.BaseRef {
		t.Errorf("baseRef = %q, want the merge-base sha", d.Standards.BaseRef)
	}
	want := []StandardsFile{
		{Path: "AGENTS.md", Bytes: 4180, SHA256: strings.Repeat("1", 64), ChangedInDiff: true},
		{Path: "CLAUDE.md", Bytes: 96, SHA256: strings.Repeat("2", 64), Truncated: true},
		{Path: "CONTRIBUTING.md", Bytes: 5, SHA256: strings.Repeat("3", 64)},
	}
	if !reflect.DeepEqual(d.Standards.Files, want) {
		t.Errorf("files = %+v, want %+v (base sizes and hashes, AGENTS.md flagged)", d.Standards.Files, want)
	}
	if !d.Standards.ChangedInDiff {
		t.Error("changedInDiff = false, want true when a standards file is in the head diff")
	}

	// A head diff that touches no standards file flags nothing.
	in.TwoAxis.Standards.HeadDiff = "diff --git a/other.md b/other.md\n"
	d = Build(in)
	if d.Standards.ChangedInDiff {
		t.Error("changedInDiff = true, want false")
	}
	for _, f := range d.Standards.Files {
		if f.ChangedInDiff {
			t.Errorf("files[%s].changedInDiff = true, want false", f.Path)
		}
	}

	// No standards files at all: still a list, never null.
	in.TwoAxis.Standards.Files = nil
	data, _, err := Build(in).Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"files":[]`) {
		t.Errorf("digest JSON lacks \"files\":[]: %s", data)
	}
}

func TestDigest_StandardsHeadDiffCappedAndUntrusted(t *testing.T) {
	in := twoAxisBase()
	in.TwoAxis.Standards.HeadDiff = "diff --git a/AGENTS.md b/AGENTS.md\n+" + strings.Repeat("h", 10*1024) + "\n"

	data, _, err := Build(in).Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	std, ok := top["standards"].(map[string]any)
	if !ok {
		t.Fatalf("standards section missing: %v", top)
	}
	if _, ok := std["headDiff"]; ok {
		t.Error("the standards section carries headDiff: it belongs only under untrusted")
	}
	if n := strings.Count(string(data), "diff --git a/AGENTS.md"); n != 1 {
		t.Errorf("the head diff appears %d times, want exactly once (under untrusted)", n)
	}
	u, _ := top["untrusted"].(map[string]any)
	hd, _ := u["standardsHeadDiff"].(string)
	if len(hd) > standardsHeadDiffMax {
		t.Errorf("standardsHeadDiff is %d bytes, want <= %d", len(hd), standardsHeadDiffMax)
	}
	if !strings.Contains(hd, "diff --git a/AGENTS.md b/AGENTS.md") {
		t.Errorf("standardsHeadDiff lost its file header: %q", hd)
	}
}

func TestDigest_StandardsRulesCapped(t *testing.T) {
	in := twoAxisBase()
	for i := 0; i < 30; i++ {
		in.TwoAxis.Standards.Rules = append(in.TwoAxis.Standards.Rules, "rule-"+strings.Repeat("r", 1000))
	}

	d := Build(in)
	if len(d.Untrusted.StandardsRules) != standardsRulesMax {
		t.Fatalf("standardsRules has %d entries, want %d", len(d.Untrusted.StandardsRules), standardsRulesMax)
	}
	for i, r := range d.Untrusted.StandardsRules {
		if len(r) > standardsRuleMax {
			t.Errorf("rule %d is %d bytes, want <= %d", i, len(r), standardsRuleMax)
		}
	}
}

// shrinkTestName is a valid test name of exactly 100 bytes.
func shrinkTestName(i, j int) string {
	prefix := fmt.Sprintf("TestShrink%03dCase%02d", i, j)
	return prefix + strings.Repeat("x", 100-len(prefix))
}

func TestDigest_TwoAxisSectionsShrinkBeforeExistingOnes(t *testing.T) {
	in := base()
	in.PR.Body = "## Validated\nran go test ./internal/steward and go vet\n"
	ta := &TwoAxisInputs{}
	ta.Standards.BaseRef = strings.Repeat("c", 40)
	ta.Standards.HeadDiff = "diff --git a/AGENTS.md b/AGENTS.md\n+" + strings.Repeat("d", 40*1024) + "\n"
	for i := 0; i < standardsRulesMax; i++ {
		ta.Standards.Rules = append(ta.Standards.Rules, strings.Repeat("r", standardsRuleMax))
	}
	for i := 0; i < 52; i++ {
		c := SpecCriterionInput{ID: fmt.Sprintf("AC%02d", i), Text: strings.Repeat("t", specTextMax)}
		for j := 0; j < specNamedMax; j++ {
			c.NamedTests = append(c.NamedTests, shrinkTestName(i, j))
		}
		ta.Spec.ContractCriteria = append(ta.Spec.ContractCriteria, c)
	}
	ta.Spec.TestsInTree = map[string]bool{}
	for _, c := range ta.Spec.ContractCriteria {
		for _, n := range c.NamedTests {
			ta.Spec.TestsInTree[n] = true
		}
	}
	in.TwoAxis = ta

	data, _, err := Build(in).Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if len(data) > DigestMaxBytes {
		t.Fatalf("digest is %d bytes, want <= %d", len(data), DigestMaxBytes)
	}

	var d Digest
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"untrusted.standardsHeadDiff", "untrusted.specCriteria", "spec.testsInTree", "untrusted.standardsRules"}
	if !reflect.DeepEqual(d.Truncated, want) {
		t.Fatalf("Truncated = %v, want exactly the two-axis shrinks, in order, and no existing one: %v", d.Truncated, want)
	}
	if len(d.Gates) != len(GateIDs) {
		t.Errorf("gates = %d, want all %d intact", len(d.Gates), len(GateIDs))
	}
	if v := d.Untrusted.PRBody["validated"]; v != "ran go test ./internal/steward and go vet" {
		t.Errorf("prBody[validated] = %q, want the PR text intact", v)
	}
	if len(d.Untrusted.SpecCriteria) != specCriteriaShrinkMax {
		t.Errorf("specCriteria has %d entries, want %d", len(d.Untrusted.SpecCriteria), specCriteriaShrinkMax)
	}
	for id, text := range d.Untrusted.SpecCriteria {
		if len(text) > specCriteriaTextShrinkMax {
			t.Errorf("specCriteria[%s] is %d bytes, want <= %d", id, len(text), specCriteriaTextShrinkMax)
		}
	}
	if len(d.Spec.TestsInTree) != 0 {
		t.Errorf("testsInTree = %v, want cleared", d.Spec.TestsInTree)
	}
	if len(d.Untrusted.StandardsRules) != standardsRulesShrinkMax {
		t.Errorf("standardsRules has %d entries, want %d", len(d.Untrusted.StandardsRules), standardsRulesShrinkMax)
	}
	if len(d.Untrusted.StandardsHeadDiff) > standardsHeadDiffShrinkMax {
		t.Errorf("standardsHeadDiff is %d bytes, want <= %d", len(d.Untrusted.StandardsHeadDiff), standardsHeadDiffShrinkMax)
	}
}
