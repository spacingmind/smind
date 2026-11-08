package steward

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/store"
)

// The two review axes (ADR-0054). A two-axis review runs one seat pass per
// axis; SeatVerdict and SeatFinding stay the single-pass documents, so the
// seat's tool schema and every stored single-pass review are unchanged.
type Axis string

// The axes, aliasing the store's strings so there is one source of truth.
const (
	AxisStandards Axis = store.StewardAxisStandards
	AxisSpec      Axis = store.StewardAxisSpec
)

// Criterion statuses a Spec pass reports for each acceptance criterion.
const (
	CriterionMet          = "met"
	CriterionUnmet        = "unmet"
	CriterionUnverifiable = "unverifiable"
)

// AxisFinding is a finding of a two-axis verdict: the single-pass finding
// plus the rule it relies on (Standards) and its axis. The daemon sets Axis
// from the pass's token on storage; a seat that sets it must set its own.
type AxisFinding struct {
	SeatFinding
	Axis Axis   `json:"axis,omitempty"`
	Rule string `json:"rule,omitempty"`
}

// CriterionResult is the Spec pass's judgement of one acceptance criterion.
type CriterionResult struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

// AxisVerdict is the structured verdict one pass of a two-axis review
// submits (ADR-0054 §4). Axis must equal the axis bound to the pass token.
type AxisVerdict struct {
	V             int                `json:"v"`
	ReviewID      int64              `json:"reviewId"`
	Axis          Axis               `json:"axis"`
	Verdict       string             `json:"verdict"`
	Summary       string             `json:"summary"`
	Findings      []AxisFinding      `json:"findings"`
	Gates         []GateAck          `json:"gates"`
	Frames        []FrameObservation `json:"frames"`
	Criteria      []CriterionResult  `json:"criteria"`
	BlockedReason string             `json:"blockedReason,omitempty"`
}

// Caps on the two-axis fields.
const (
	axisFindingRuleMax   = 300
	axisCriteriaMax      = 60
	criterionIDMax       = 64
	criterionEvidenceMax = 600
	criterionMinEvidence = 10
)

// AxisSchemaCaps are the two-axis verdict field caps in bytes, on top of
// SchemaCaps, for the two-axis seat's tool schema.
func AxisSchemaCaps() map[string]int {
	caps := SchemaCaps()
	caps["findings[].rule"] = axisFindingRuleMax
	caps["criteria[].id"] = criterionIDMax
	caps["criteria[].evidence"] = criterionEvidenceMax
	return caps
}

// AxisArrayCaps are the item-count caps of a two-axis verdict's arrays.
func AxisArrayCaps() map[string]int {
	caps := ArrayCaps()
	caps["criteria"] = axisCriteriaMax
	return caps
}

// ParseAxisVerdict decodes a two-axis verdict as strictly as ParseSeatVerdict
// does: size, unknown fields, one JSON value, enums and every cap. Axis, the
// review id and the completeness rules are checked by CheckAxisVerdict and
// the service.
func ParseAxisVerdict(raw []byte) (AxisVerdict, error) {
	if len(raw) > MaxVerdictBytes {
		return AxisVerdict{}, ve("verdict", "too large: %d bytes, the cap is %d", len(raw), MaxVerdictBytes)
	}
	var v AxisVerdict
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return AxisVerdict{}, ve("verdict", "%s", decodeMsg(err))
	}
	if dec.More() {
		return AxisVerdict{}, ve("verdict", "trailing data after the JSON object")
	}
	if v.V != 1 {
		return AxisVerdict{}, ve("v", "must be 1")
	}
	switch v.Axis {
	case AxisStandards, AxisSpec:
	default:
		return AxisVerdict{}, ve("axis", "must be standards or spec")
	}
	switch v.Verdict {
	case VerdictAccepted, VerdictChanges, VerdictBlocked:
	default:
		return AxisVerdict{}, ve("verdict", "must be ACCEPTED, CHANGES or BLOCKED")
	}
	if strings.TrimSpace(v.Summary) == "" {
		return AxisVerdict{}, ve("summary", "is required")
	}
	if len(v.Summary) > verdictSummaryMax {
		return AxisVerdict{}, ve("summary", "is %d bytes, the cap is %d", len(v.Summary), verdictSummaryMax)
	}
	if len(v.BlockedReason) > verdictBlockedReasonMax {
		return AxisVerdict{}, ve("blockedReason", "is %d bytes, the cap is %d", len(v.BlockedReason), verdictBlockedReasonMax)
	}
	if len(v.Findings) > verdictFindingsMax {
		return AxisVerdict{}, ve("findings", "has %d items, the cap is %d", len(v.Findings), verdictFindingsMax)
	}
	if len(v.Gates) > verdictGateAcksMax {
		return AxisVerdict{}, ve("gates", "has %d items, the cap is %d", len(v.Gates), verdictGateAcksMax)
	}
	if len(v.Frames) > verdictObservationsMax {
		return AxisVerdict{}, ve("frames", "has %d items, the cap is %d", len(v.Frames), verdictObservationsMax)
	}
	if len(v.Criteria) > axisCriteriaMax {
		return AxisVerdict{}, ve("criteria", "has %d items, the cap is %d", len(v.Criteria), axisCriteriaMax)
	}
	if err := checkListCaps(v.seatFindings(), v.Gates, v.Frames); err != nil {
		return AxisVerdict{}, err
	}
	for i, f := range v.Findings {
		p := fmt.Sprintf("findings[%d]", i)
		if len(f.Rule) > axisFindingRuleMax {
			return AxisVerdict{}, ve(p+".rule", "is %d bytes, the cap is %d", len(f.Rule), axisFindingRuleMax)
		}
		switch f.Axis {
		case "", AxisStandards, AxisSpec:
		default:
			return AxisVerdict{}, ve(p+".axis", "must be standards or spec")
		}
	}
	for i, c := range v.Criteria {
		p := fmt.Sprintf("criteria[%d]", i)
		if strings.TrimSpace(c.ID) == "" {
			return AxisVerdict{}, ve(p+".id", "is required")
		}
		if len(c.ID) > criterionIDMax {
			return AxisVerdict{}, ve(p+".id", "is %d bytes, the cap is %d", len(c.ID), criterionIDMax)
		}
		switch c.Status {
		case CriterionMet, CriterionUnmet, CriterionUnverifiable:
		default:
			return AxisVerdict{}, ve(p+".status", "must be met, unmet or unverifiable")
		}
		if len(c.Evidence) > criterionEvidenceMax {
			return AxisVerdict{}, ve(p+".evidence", "is %d bytes, the cap is %d", len(c.Evidence), criterionEvidenceMax)
		}
	}
	if v.Findings == nil {
		v.Findings = []AxisFinding{}
	}
	if v.Gates == nil {
		v.Gates = []GateAck{}
	}
	if v.Frames == nil {
		v.Frames = []FrameObservation{}
	}
	if v.Criteria == nil {
		v.Criteria = []CriterionResult{}
	}
	return v, nil
}

// seatFindings is the findings as single-pass findings, for the shared caps
// and completeness checks.
func (v AxisVerdict) seatFindings() []SeatFinding {
	out := make([]SeatFinding, len(v.Findings))
	for i, f := range v.Findings {
		out[i] = f.SeatFinding
	}
	return out
}

// CheckAxisVerdict applies ADR-0054 §4's completeness rules to a parsed
// verdict of the pass bound to axis, against the digest it judged.
// criteriaIDs are the ids of the criteria the daemon extracted for the Spec
// pass (empty for Standards). Every error is a VerdictError naming the
// missing item. ReviewID is not checked here; the service does it.
//
// The axis must equal the axis bound to the pass token: a Standards seat can
// never satisfy the Spec pass.
func CheckAxisVerdict(v AxisVerdict, d Digest, axis Axis, criteriaIDs []string) error {
	if v.Axis != axis {
		return ve("axis", "this pass is %q, the verdict says %q", axis, v.Axis)
	}
	known := make(map[string]bool, len(d.Gates))
	for _, g := range d.Gates {
		known[g.ID] = true
	}
	for i, a := range v.Gates {
		if !known[a.ID] {
			return ve(fmt.Sprintf("gates[%d].id", i), "unknown gate %q", a.ID)
		}
	}
	for i, f := range v.Findings {
		if f.Axis != "" && f.Axis != axis {
			return ve(fmt.Sprintf("findings[%d].axis", i), "must be %q", axis)
		}
		if axis == AxisStandards && strings.TrimSpace(f.Rule) == "" {
			return ve(fmt.Sprintf("findings[%d].rule", i), "a standards finding must cite the rule it relies on")
		}
	}
	if axis == AxisStandards && len(v.Criteria) > 0 {
		return ve("criteria", "only the spec pass reports criteria")
	}
	if axis == AxisSpec {
		if err := checkCriteria(v.Criteria, criteriaIDs); err != nil {
			return err
		}
	}
	// ParseAxisVerdict only lets the three verdicts through.
	switch v.Verdict {
	case VerdictAccepted:
		return checkAxisAccepted(v, d, axis)
	case VerdictChanges:
		return checkAxisChanges(v, axis)
	}
	if strings.TrimSpace(v.BlockedReason) == "" {
		return ve("blockedReason", "required for BLOCKED")
	}
	return nil
}

// checkCriteria requires exactly one result per extracted criterion, no
// unknown id and, for met and unverifiable, real evidence.
func checkCriteria(results []CriterionResult, ids []string) error {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	seen := make(map[string]bool, len(results))
	for i, c := range results {
		p := fmt.Sprintf("criteria[%d]", i)
		if !want[c.ID] {
			return ve(p+".id", "unknown criterion %q", c.ID)
		}
		if seen[c.ID] {
			return ve(p+".id", "criterion %q is reported twice", c.ID)
		}
		seen[c.ID] = true
		if c.Status != CriterionUnmet && nonSpaceChars(c.Evidence) < criterionMinEvidence {
			return ve(p+".evidence", "criterion %q needs evidence", c.ID)
		}
	}
	for _, id := range ids {
		if !seen[id] {
			return ve("criteria", "criterion %q has no result", id)
		}
	}
	return nil
}

// checkAxisAccepted enforces what an ACCEPTED verdict of one axis carries.
// The Spec pass keeps every ACCEPTED rule of a single-pass verdict (gate
// acknowledgements, a real observation per frame) and may have no unmet
// criterion; the Standards pass is shown neither gates nor images.
func checkAxisAccepted(v AxisVerdict, d Digest, axis Axis) error {
	if axis == AxisSpec {
		if hasUnmet(v.Criteria) {
			return ve("criteria", "an ACCEPTED verdict cannot carry an unmet criterion")
		}
		if len(v.Criteria) == 0 {
			return ve("criteria", "an ACCEPTED spec verdict needs a result for every criterion; there are none to judge")
		}
		return checkAccepted(SeatVerdict{
			Verdict: v.Verdict, Findings: v.seatFindings(), Gates: v.Gates, Frames: v.Frames, BlockedReason: v.BlockedReason,
		}, d)
	}
	for i, f := range v.Findings {
		switch f.Severity {
		case "P2", "P3":
		default:
			return ve(fmt.Sprintf("findings[%d].severity", i), "an ACCEPTED verdict cannot carry a P0/P1 or unrated finding")
		}
	}
	if v.BlockedReason != "" {
		return ve("blockedReason", "is only for BLOCKED verdicts")
	}
	return nil
}

// checkAxisChanges enforces what a CHANGES verdict of one axis carries: a
// complete finding, or for the Spec pass an unmet criterion instead.
func checkAxisChanges(v AxisVerdict, axis Axis) error {
	if len(v.Findings) == 0 && !(axis == AxisSpec && hasUnmet(v.Criteria)) {
		return ve("findings", "CHANGES needs at least one finding")
	}
	for i, f := range v.Findings {
		if err := checkFindingComplete(i, f.SeatFinding); err != nil {
			return err
		}
	}
	return nil
}

// hasUnmet reports whether any criterion result is unmet.
func hasUnmet(results []CriterionResult) bool {
	for _, c := range results {
		if c.Status == CriterionUnmet {
			return true
		}
	}
	return false
}
