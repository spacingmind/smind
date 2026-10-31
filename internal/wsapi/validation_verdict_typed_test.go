package wsapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/validation"
)

// typedVerdict marshals a real validation.Verdict -- exactly what the MCP
// layer's typed validation_verdict_submit input sends over the wire, now
// that the schema is an object rather than an integer array.
func typedVerdict(t *testing.T, e *seatEnv, r requestResult, items []validation.Item) json.RawMessage {
	t.Helper()
	ct, err := e.svc.GetContract(r.ContractID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(validation.Verdict{
		V:              1,
		ContractID:     r.ContractID,
		ContractSha256: ct.Sha256,
		CommitSHA:      r.CommitSHA,
		Status:         validation.StatusPass,
		Summary:        "all good",
		Items:          items,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func typedVerdictItems() []validation.Item {
	return []validation.Item{
		{Kind: validation.KindTest, ID: "TestA", Status: validation.StatusPass, Evidence: []validation.Evidence{{Type: "excerpt", Text: "ok"}}},
		{Kind: validation.KindAC, ID: "AC1", Status: validation.StatusPass, Via: []string{"TestA"}},
	}
}

// TestVerdictSubmit_TypedStructPayloadIsAccepted proves the payload the
// MCP tool now produces (json.Marshal(validation.Verdict)) is accepted by
// the daemon as a complete pass.
func TestVerdictSubmit_TypedStructPayloadIsAccepted(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	r, err := e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatal(err)
	}
	res := e.call(t, e.validatorRC(r), "validation.verdict.submit", map[string]any{
		"validationId": r.ValidationID,
		"token":        e.tokenOf(r),
		"verdict":      typedVerdict(t, e, r, typedVerdictItems()),
	})
	if res["status"] != "pass" || res["mergeReady"] != true {
		t.Fatalf("verdict result = %v, want a merge-ready pass", res)
	}
	e.stopValidator(t, r)
	e.waitDone(t, r.ValidationID)
}

// TestVerdictSubmit_IncompletePassNotAccepted: the same typed payload
// minus the AC item is refused with the missing item named -- the daemon,
// not the model, decides what a pass is.
func TestVerdictSubmit_IncompletePassNotAccepted(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	r, err := e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatal(err)
	}
	items := typedVerdictItems()[:1] // the test item only, no AC item
	raw, err := budgetCall(t, e.api, e.validatorRC(r), "validation.verdict.submit", map[string]any{
		"validationId": r.ValidationID,
		"token":        e.tokenOf(r),
		"verdict":      typedVerdict(t, e, r, items),
	})
	if err == nil {
		t.Fatalf("an incomplete pass was accepted: %s", raw)
	}
	msg := err.Error()
	if !strings.Contains(msg, `ac item for "AC1"`) {
		t.Fatalf("error = %q, want it to name the missing AC1 item", msg)
	}
	e.stopValidator(t, r)
	e.waitDone(t, r.ValidationID)
}
