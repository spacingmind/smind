package steward

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTwoAxisOff_ReviewIsByteIdentical pins ADR-0054 AC1: a single-pass review
// (steward.twoAxis false, the only mode before P2) stores and serves exactly
// what it did before the two-axis change. The golden was generated on the
// code that predates the change; run with UPDATE_GOLDEN=1 only to regenerate
// it on purpose.
func TestTwoAxisOff_ReviewIsByteIdentical(t *testing.T) {
	t.Parallel()

	type outcome struct {
		Name string          `json:"name"`
		Row  map[string]any  `json:"row"`
		View json.RawMessage `json:"view"`
	}
	run := func(name string, gates []Gate, verdict SeatVerdict) outcome {
		svc := newTestService(t)
		row, token := openReview(t, svc, digestWithGates(gates...))
		verdict.V, verdict.ReviewID = 1, row.ID
		raw, err := json.Marshal(verdict)
		if err != nil {
			t.Fatal(err)
		}
		v, err := svc.SubmitVerdict(row.ID, token, row.RunID, raw)
		if err != nil {
			t.Fatalf("%s: SubmitVerdict() error = %v", name, err)
		}
		v.CreatedAt, v.FinishedAt = time.Time{}, nil
		vb, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		got, err := svc.DB.GetStewardReview(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		return outcome{Name: name, View: vb, Row: map[string]any{
			"seatVerdict": got.SeatVerdict, "verdict": got.Verdict, "findings": got.FindingsJSON, "gates": got.GatesJSON,
			"summary": got.Summary, "status": got.Status, "tokenUsed": got.TokenUsed, "blockedReason": got.BlockedReason,
			"runBound": got.RunID == "seat-run-1",
		}}
	}
	pass := []Gate{{ID: "ci", State: GatePass}, {ID: "scope", State: GatePass}}
	acks := []GateAck{{ID: "ci", Ack: true}, {ID: "scope", Ack: true}}
	got := []outcome{
		run("accepted", pass, SeatVerdict{Verdict: VerdictAccepted, Summary: "all good", Gates: acks,
			Findings: []SeatFinding{{File: "a.go", Line: 3, Why: "nit", Check: "gofmt", Severity: "P3"}}}),
		run("changes", pass, SeatVerdict{Verdict: VerdictChanges, Summary: "fix it",
			Findings: []SeatFinding{{File: "b.go", Line: 9, Why: "race", Check: "go test -race", Severity: "P1"}}}),
		run("clamped", []Gate{{ID: "ci", State: GateFail, Detail: "red"}, {ID: "scope", State: GatePass}},
			SeatVerdict{Verdict: VerdictAccepted, Summary: "looks fine", Gates: acks}),
		run("blocked", pass, SeatVerdict{Verdict: VerdictBlocked, Summary: "stacked", BlockedReason: "stacked on another PR"}),
	}
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	path := filepath.Join("testdata", "review_single_pass.golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(want) != string(b) {
		t.Fatalf("single-pass review changed.\n got: %s\nwant: %s", b, want)
	}
}
