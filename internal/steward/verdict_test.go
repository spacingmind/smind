package steward

import (
	"errors"
	"strings"
	"testing"
)

// uiDigest is a fully passing digest whose diff also touches web/, so
// Frames.Items carries the four matrix frame paths.
func uiDigest(t *testing.T) Digest {
	t.Helper()
	in := base()
	in.Numstat = "1\t1\tinternal/steward/a.go\n1\t1\tweb/packages/ui/src/x.css\n"
	in.WriteScope = []string{"internal/steward/", "web/"}
	in.Frames = framesAt(in.Task.HeadSHA)
	d := Build(in)
	for _, g := range d.Gates {
		if g.State != GatePass && g.State != GateNA {
			t.Fatalf("uiDigest gate %s = %q (%s), want pass or n/a", g.ID, g.State, g.Detail)
		}
	}
	return d
}

// ackAll acknowledges every gate of the digest, n/a included.
func ackAll(d Digest) []GateAck {
	acks := make([]GateAck, 0, len(d.Gates))
	for _, g := range d.Gates {
		acks = append(acks, GateAck{ID: g.ID, Ack: true})
	}
	return acks
}

// observeAll leaves one real observation per distinct frame path.
func observeAll(d Digest) []FrameObservation {
	seen := map[string]bool{}
	obs := []FrameObservation{}
	for _, f := range d.Frames.Items {
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		obs = append(obs, FrameObservation{Path: f.Path, Observation: "layout holds and the button is visible"})
	}
	return obs
}

// acceptedVerdict is the complete ACCEPTED verdict for d.
func acceptedVerdict(d Digest) SeatVerdict {
	return SeatVerdict{
		V:        1,
		ReviewID: 5,
		Verdict:  VerdictAccepted,
		Summary:  "every gate acknowledged and every frame observed",
		Gates:    ackAll(d),
		Frames:   observeAll(d),
	}
}

func TestVerdictSubmit_AcceptedNeedsAllGatesAndFrameObservations(t *testing.T) {
	d := uiDigest(t)

	if err := CheckSeatVerdict(acceptedVerdict(d), d); err != nil {
		t.Fatalf("complete ACCEPTED verdict: error = %v", err)
	}

	// Missing one ack: error names the gate.
	drop := acceptedVerdict(d)
	dropped := drop.Gates[len(drop.Gates)-1].ID
	drop.Gates = drop.Gates[:len(drop.Gates)-1]
	err := CheckSeatVerdict(drop, d)
	if err == nil || !strings.Contains(err.Error(), "gate "+dropped+" not acknowledged") {
		t.Errorf("missing ack for %s: error = %v", dropped, err)
	}
	assertField(t, err, "gates")

	// A false ack is no ack.
	deny := acceptedVerdict(d)
	deny.Gates[0].Ack = false
	err = CheckSeatVerdict(deny, d)
	if err == nil || !strings.Contains(err.Error(), "gate "+deny.Gates[0].ID+" not acknowledged") {
		t.Errorf("ack=false for %s: error = %v", deny.Gates[0].ID, err)
	}

	// Missing frame observation: error names the path.
	noObs := acceptedVerdict(d)
	noObs.Frames = nil
	err = CheckSeatVerdict(noObs, d)
	if err == nil || !strings.Contains(err.Error(), "no observation for "+d.Frames.Items[0].Path) {
		t.Errorf("no frame observation: error = %v", err)
	}
	assertField(t, err, "frames")

	// A too-short observation does not count.
	short := acceptedVerdict(d)
	for i := range short.Frames {
		short.Frames[i].Observation = "ok"
	}
	err = CheckSeatVerdict(short, d)
	if err == nil || !strings.Contains(err.Error(), "no observation for "+d.Frames.Items[0].Path) {
		t.Errorf("short observation: error = %v", err)
	}

	// An ack naming a gate the digest does not have.
	unknown := acceptedVerdict(d)
	unknown.Gates = append(unknown.Gates, GateAck{ID: "definitely_not_a_gate", Ack: true})
	err = CheckSeatVerdict(unknown, d)
	if err == nil || !strings.Contains(err.Error(), "unknown gate") {
		t.Errorf("unknown gate ack: error = %v", err)
	}
	assertField(t, err, "gates[11].id")

	// A P1 finding cannot ride on an ACCEPTED verdict.
	p1 := acceptedVerdict(d)
	p1.Findings = []SeatFinding{{File: "internal/steward/a.go", Line: 3, Why: "wrong", Check: "go test", Severity: "P1"}}
	err = CheckSeatVerdict(p1, d)
	if err == nil || !strings.Contains(err.Error(), "cannot carry a P0/P1") {
		t.Errorf("P1 finding on ACCEPTED: error = %v", err)
	}
	assertField(t, err, "findings[0].severity")

	// An unrated finding is refused just the same.
	unrated := acceptedVerdict(d)
	unrated.Findings = []SeatFinding{{File: "internal/steward/a.go", Line: 3, Why: "wrong", Check: "go test"}}
	err = CheckSeatVerdict(unrated, d)
	if err == nil || !strings.Contains(err.Error(), "unrated finding") {
		t.Errorf("unrated finding on ACCEPTED: error = %v", err)
	}

	// A P3 finding may ride along.
	p3 := acceptedVerdict(d)
	p3.Findings = []SeatFinding{{File: "internal/steward/a.go", Line: 3, Why: "nit", Check: "go test", Severity: "P3"}}
	if err := CheckSeatVerdict(p3, d); err != nil {
		t.Errorf("P3 finding on ACCEPTED: error = %v", err)
	}

	// An ACCEPTED verdict cannot also carry a blocked reason.
	blocked := acceptedVerdict(d)
	blocked.BlockedReason = "cannot review"
	err = CheckSeatVerdict(blocked, d)
	if err == nil {
		t.Error("ACCEPTED with blockedReason passed, want an error")
	}
}

func TestVerdictSubmit_ChangesNeedsFindingsWithCheck(t *testing.T) {
	d := uiDigest(t)
	good := SeatFinding{File: "internal/steward/a.go", Line: 12, Why: "the loop skips the last row", Check: "go test ./internal/steward/ -run TestDigest", Severity: "P2"}

	none := SeatVerdict{V: 1, ReviewID: 5, Verdict: VerdictChanges, Summary: "needs fixes"}
	err := CheckSeatVerdict(none, d)
	if err == nil || !strings.Contains(err.Error(), "CHANGES needs at least one finding") {
		t.Errorf("CHANGES without findings: error = %v", err)
	}
	assertField(t, err, "findings")

	cases := []struct {
		name      string
		finding   SeatFinding
		wantField string
	}{
		{"empty check", SeatFinding{File: good.File, Line: good.Line, Why: good.Why, Check: "  "}, "findings[0].check"},
		{"missing check", SeatFinding{File: good.File, Line: good.Line, Why: good.Why}, "findings[0].check"},
		{"line 0", SeatFinding{File: good.File, Line: 0, Why: good.Why, Check: good.Check}, "findings[0].line"},
		{"empty file", SeatFinding{File: " ", Line: good.Line, Why: good.Why, Check: good.Check}, "findings[0].file"},
		{"empty why", SeatFinding{File: good.File, Line: good.Line, Why: "", Check: good.Check}, "findings[0].why"},
	}
	for _, tc := range cases {
		v := SeatVerdict{V: 1, ReviewID: 5, Verdict: VerdictChanges, Summary: "needs fixes", Findings: []SeatFinding{tc.finding}}
		err := CheckSeatVerdict(v, d)
		if err == nil {
			t.Errorf("%s: no error, want one", tc.name)
			continue
		}
		assertField(t, err, tc.wantField)
	}

	ok := SeatVerdict{V: 1, ReviewID: 5, Verdict: VerdictChanges, Summary: "needs fixes", Findings: []SeatFinding{good}}
	if err := CheckSeatVerdict(ok, d); err != nil {
		t.Errorf("complete CHANGES verdict: error = %v", err)
	}
}

func TestVerdictSubmit_BlockedNeedsReason(t *testing.T) {
	d := uiDigest(t)
	v := SeatVerdict{V: 1, ReviewID: 5, Verdict: VerdictBlocked, Summary: "cannot judge this"}
	err := CheckSeatVerdict(v, d)
	if err == nil || !strings.Contains(err.Error(), "required for BLOCKED") {
		t.Errorf("BLOCKED without reason: error = %v", err)
	}
	assertField(t, err, "blockedReason")

	v.BlockedReason = "the digest is missing the contract"
	if err := CheckSeatVerdict(v, d); err != nil {
		t.Errorf("BLOCKED with reason: error = %v", err)
	}

	// An unknown gate ack is refused whatever the verdict kind.
	v.Gates = []GateAck{{ID: "nope", Ack: true}}
	err = CheckSeatVerdict(v, d)
	if err == nil || !strings.Contains(err.Error(), "unknown gate") {
		t.Errorf("unknown gate ack on BLOCKED: error = %v", err)
	}
}

func TestParseSeatVerdict_Strict(t *testing.T) {
	const valid = `{"v":1,"reviewId":5,"verdict":"CHANGES","summary":"fix it","findings":[{"file":"a.go","line":2,"why":"w","check":"c","severity":"P2"}]}`

	cases := []struct {
		name      string
		raw       string
		wantField string
		wantMsg   string
	}{
		{"unknown field", `{"v":1,"extra":2,"verdict":"CHANGES","summary":"s"}`, "verdict", `unknown field "extra"`},
		{"trailing data", valid + ` {"v":1}`, "verdict", "trailing data"},
		{"two objects", valid + valid, "verdict", "trailing data"},
		{"not json", `nonsense`, "verdict", ""},
		{"v is 2", `{"v":2,"verdict":"CHANGES","summary":"s"}`, "v", "must be 1"},
		{"bad verdict value", `{"v":1,"verdict":"MAYBE","summary":"s"}`, "verdict", "must be ACCEPTED, CHANGES or BLOCKED"},
		{"blank summary", `{"v":1,"verdict":"CHANGES","summary":"   "}`, "summary", "is required"},
		{"summary over cap", `{"v":1,"verdict":"CHANGES","summary":"` + strings.Repeat("s", 801) + `"}`, "summary", "the cap is 800"},
		{"51 findings", `{"v":1,"verdict":"CHANGES","summary":"s","findings":[` + findingsJSON(51) + `]}`, "findings", "the cap is 50"},
		{"41 gate acks", `{"v":1,"verdict":"CHANGES","summary":"s","gates":[` + gateAcksJSON(41) + `]}`, "gates", "the cap is 40"},
		{"201 observations", `{"v":1,"verdict":"CHANGES","summary":"s","frames":[` + observationsJSON(201) + `]}`, "frames", "the cap is 200"},
		{"finding file over cap", `{"v":1,"verdict":"CHANGES","summary":"s","findings":[{"file":"` + strings.Repeat("f", 301) + `","line":1,"why":"w","check":"c"}]}`, "findings[0].file", "the cap is 300"},
		{"finding why over cap", `{"v":1,"verdict":"CHANGES","summary":"s","findings":[{"file":"a.go","line":1,"why":"` + strings.Repeat("w", 601) + `","check":"c"}]}`, "findings[0].why", "the cap is 600"},
		{"finding check over cap", `{"v":1,"verdict":"CHANGES","summary":"s","findings":[{"file":"a.go","line":1,"why":"w","check":"` + strings.Repeat("c", 401) + `"}]}`, "findings[0].check", "the cap is 400"},
		{"bad severity", `{"v":1,"verdict":"CHANGES","summary":"s","findings":[{"file":"a.go","line":1,"why":"w","check":"c","severity":"P9"}]}`, "findings[0].severity", "must be P0, P1, P2 or P3"},
		{"gate ack note over cap", `{"v":1,"verdict":"CHANGES","summary":"s","gates":[{"id":"ci","ack":true,"note":"` + strings.Repeat("n", 301) + `"}]}`, "gates[0].note", "the cap is 300"},
		{"observation path over cap", `{"v":1,"verdict":"CHANGES","summary":"s","frames":[{"path":"` + strings.Repeat("p", 301) + `","observation":"o"}]}`, "frames[0].path", "the cap is 300"},
		{"observation over cap", `{"v":1,"verdict":"CHANGES","summary":"s","frames":[{"path":"f.png","observation":"` + strings.Repeat("o", 1001) + `"}]}`, "frames[0].observation", "the cap is 1000"},
		{"blockedReason over cap", `{"v":1,"verdict":"BLOCKED","summary":"s","blockedReason":"` + strings.Repeat("b", 201) + `"}`, "blockedReason", "the cap is 200"},
	}
	for _, tc := range cases {
		_, err := ParseSeatVerdict([]byte(tc.raw))
		if err == nil {
			t.Errorf("%s: no error, want one", tc.name)
			continue
		}
		assertField(t, err, tc.wantField)
		if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
			t.Errorf("%s: error = %q, want it to contain %q", tc.name, err, tc.wantMsg)
		}
	}

	// Oversize document.
	oversize := `{"v":1,"verdict":"CHANGES","summary":"` + strings.Repeat("s", MaxVerdictBytes) + `"}`
	_, err := ParseSeatVerdict([]byte(oversize))
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("oversize verdict: error = %v, want too large", err)
	}
	assertField(t, err, "verdict")

	// Nil slices become empty ones.
	v, err := ParseSeatVerdict([]byte(valid))
	if err != nil {
		t.Fatalf("valid verdict: error = %v", err)
	}
	if v.Gates == nil || len(v.Gates) != 0 {
		t.Errorf("Gates = %#v, want non-nil empty", v.Gates)
	}
	if v.Frames == nil || len(v.Frames) != 0 {
		t.Errorf("Frames = %#v, want non-nil empty", v.Frames)
	}
	blocked, err := ParseSeatVerdict([]byte(`{"v":1,"reviewId":6,"verdict":"BLOCKED","summary":"s","blockedReason":"no contract"}`))
	if err != nil {
		t.Fatalf("valid BLOCKED verdict: error = %v", err)
	}
	if blocked.Findings == nil || len(blocked.Findings) != 0 {
		t.Errorf("Findings = %#v, want non-nil empty", blocked.Findings)
	}
}

func TestVerdictError_IsInvalid(t *testing.T) {
	_, err := ParseSeatVerdict([]byte(`{"v":2}`))
	if err == nil {
		t.Fatal("no error")
	}
	if !errors.Is(err, ErrInvalidVerdict) {
		t.Errorf("errors.Is(%v, ErrInvalidVerdict) = false, want true", err)
	}
	err = CheckSeatVerdict(SeatVerdict{Verdict: VerdictChanges, Summary: "s"}, uiDigest(t))
	if !errors.Is(err, ErrInvalidVerdict) {
		t.Errorf("errors.Is(CheckSeatVerdict err, ErrInvalidVerdict) = false, want true")
	}
	if errors.Is(errors.New("other"), ErrInvalidVerdict) {
		t.Error("errors.Is(other, ErrInvalidVerdict) = true, want false")
	}
	e := VerdictError{Field: "findings[2].file", Msg: "is required"}
	if got := e.Error(); got != "findings[2].file: is required" {
		t.Errorf("Error() = %q, want %q", got, "findings[2].file: is required")
	}
	if !errors.Is(e, ErrInvalidVerdict) {
		t.Error("errors.Is(VerdictError, ErrInvalidVerdict) = false, want true")
	}
}

// assertField checks err is a VerdictError with the given JSON-path field.
func assertField(t *testing.T, err error, field string) {
	t.Helper()
	var ve VerdictError
	if !errors.As(err, &ve) {
		t.Fatalf("error %v (%T) is not a VerdictError", err, err)
	}
	if ve.Field != field {
		t.Errorf("error field = %q, want %q (msg %q)", ve.Field, field, ve.Msg)
	}
}

// findingsJSON builds n comma-separated finding objects.
func findingsJSON(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = `{"file":"a.go","line":1,"why":"w","check":"c","severity":"P2"}`
	}
	return strings.Join(parts, ",")
}

// gateAcksJSON builds n comma-separated gate ack objects.
func gateAcksJSON(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = `{"id":"ci","ack":true}`
	}
	return strings.Join(parts, ",")
}

// observationsJSON builds n comma-separated frame observations.
func observationsJSON(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = `{"path":"f.png","observation":"looks right"}`
	}
	return strings.Join(parts, ",")
}
