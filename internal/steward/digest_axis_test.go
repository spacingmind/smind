package steward

import (
	"encoding/json"
	"strings"
	"testing"
)

// fullAxisDigest carries every section and free-text field of a two-axis
// digest, each with a marker string the tests look for.
func fullAxisDigest() Digest {
	d := axisDigest("AC1")
	d.Spec.Sources = []SpecSource{{Kind: "plan", Ref: "docs/plans/active/MARK-PLAN.md", Status: "found"}}
	d.Spec.TestsInTree = map[string]bool{"TestMarkSpec": true}
	d.Standards = &StandardsSection{BaseRef: "MARK-BASE", Files: []StandardsFile{{Path: "MARK-STD.md"}}, ChangedInDiff: true}
	d.Task.Title = "MARK-SHARED-TITLE"
	d.Untrusted = Untrusted{
		PRTitle:           "MARK-SHARED-PRTITLE",
		PRBody:            map[string]string{"summary": "MARK-PRBODY"},
		CommitSubjects:    []string{"MARK-SHARED-COMMIT"},
		HandoffNotes:      []string{},
		StandardsHeadDiff: "MARK-HEADDIFF",
		StandardsRules:    []string{"MARK-RULE"},
		SpecCriteria:      map[string]string{"AC1": "MARK-CRITERION"},
	}
	return d
}

func TestAxisDigestJSON_EachSeatSeesOnlyItsAxis(t *testing.T) {
	t.Parallel()

	standardsText := []string{"MARK-BASE", "MARK-STD", "MARK-HEADDIFF", "MARK-RULE"}
	specText := []string{"MARK-PLAN", "TestMarkSpec", "MARK-CRITERION", "MARK-PRBODY"}
	shared := []string{"MARK-SHARED-PRTITLE", "MARK-SHARED-COMMIT"}

	tests := []struct {
		axis     Axis
		mustHave []string
		mustLack []string
		noKey    string
	}{
		{AxisStandards, append(append([]string{}, standardsText...), shared...), specText, `"spec"`},
		{AxisSpec, append(append([]string{}, specText...), shared...), standardsText, `"standards"`},
	}
	for _, tt := range tests {
		t.Run(string(tt.axis), func(t *testing.T) {
			t.Parallel()
			fx := openTwoAxis(t, fullAxisDigest())
			raw, err := fx.svc.AxisDigestJSON(fx.row.ID, tt.axis)
			if err != nil {
				t.Fatalf("AxisDigestJSON(%s) error = %v", tt.axis, err)
			}
			got := string(raw)
			for _, m := range tt.mustHave {
				if !strings.Contains(got, m) {
					t.Errorf("%s view lacks %q", tt.axis, m)
				}
			}
			for _, m := range tt.mustLack {
				if strings.Contains(got, m) {
					t.Errorf("%s view leaks %q", tt.axis, m)
				}
			}
			if strings.Contains(got, tt.noKey) {
				t.Errorf("%s view carries the other axis's section key %s", tt.axis, tt.noKey)
			}
			var d Digest
			if err := json.Unmarshal(raw, &d); err != nil || len(d.Gates) != 1 {
				t.Fatalf("%s view = %+v, %v; want the shared objective fields", tt.axis, d, err)
			}
		})
	}
}

func TestAxisDigestJSON_StoredDigestStaysWhole(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, fullAxisDigest())
	if _, err := fx.svc.AxisDigestJSON(fx.row.ID, AxisStandards); err != nil {
		t.Fatal(err)
	}
	full, err := fx.svc.DigestJSON(fx.row.ID)
	if err != nil || !strings.Contains(string(full), "MARK-CRITERION") || !strings.Contains(string(full), "MARK-HEADDIFF") {
		t.Fatalf("stored digest = %s, %v; want both axes' sections still there", full, err)
	}
}

func TestAxisDigestJSON_UnknownAxisIsRefused(t *testing.T) {
	t.Parallel()

	fx := openTwoAxis(t, fullAxisDigest())
	for _, axis := range []Axis{"", "other"} {
		if raw, err := fx.svc.AxisDigestJSON(fx.row.ID, axis); err == nil {
			t.Fatalf("AxisDigestJSON(%q) = %s, want an error, never the full digest", axis, raw)
		}
	}
}
