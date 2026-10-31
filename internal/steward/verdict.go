package steward

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// MaxVerdictBytes is the hard ceiling on a raw seat verdict document.
const MaxVerdictBytes = 128 * 1024

// ErrInvalidVerdict is the sentinel behind every VerdictError, so callers can
// errors.Is without matching on message text.
var ErrInvalidVerdict = errors.New("invalid steward verdict")

// VerdictError is the only error ParseSeatVerdict and CheckSeatVerdict
// return: Field is a JSON path into the verdict document, Msg names the
// missing or invalid item.
type VerdictError struct {
	Field string
	Msg   string
}

func (e VerdictError) Error() string { return e.Field + ": " + e.Msg }

// Is makes errors.Is(err, ErrInvalidVerdict) true for every VerdictError.
func (e VerdictError) Is(target error) bool { return target == ErrInvalidVerdict }

// SeatFinding is one review finding. Check names the check that shows the
// finding fixed; Severity is "" or P0..P3.
type SeatFinding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Why      string `json:"why"`
	Check    string `json:"check"`
	Severity string `json:"severity,omitempty"` // "", P0, P1, P2, P3
}

// GateAck is the seat's acknowledgement of one gate of the digest.
type GateAck struct {
	ID   string `json:"id"`
	Ack  bool   `json:"ack"`
	Note string `json:"note,omitempty"`
}

// FrameObservation is what the seat saw in one evidence frame.
type FrameObservation struct {
	Path        string `json:"path"`
	Observation string `json:"observation"`
}

// SeatVerdict is the structured verdict the seat submits (ADR-0047 §3.5);
// Clamp decides what is stored.
type SeatVerdict struct {
	V             int                `json:"v"`
	ReviewID      int64              `json:"reviewId"`
	Verdict       string             `json:"verdict"`
	Summary       string             `json:"summary"`
	Findings      []SeatFinding      `json:"findings"`
	Gates         []GateAck          `json:"gates"`
	Frames        []FrameObservation `json:"frames"`
	BlockedReason string             `json:"blockedReason,omitempty"`
}

// Caps on the verdict document's fields.
const (
	verdictSummaryMax       = 800
	verdictBlockedReasonMax = 200
	verdictFindingsMax      = 50
	verdictGateAcksMax      = 40
	verdictObservationsMax  = 200
	findingFileMax          = 300
	findingWhyMax           = 600
	findingCheckMax         = 400
	gateAckNoteMax          = 300
	observationPathMax      = 300
	observationTextMax      = 1000
	observationMinChars     = 10
)

func ve(field, format string, args ...any) error {
	return VerdictError{Field: field, Msg: fmt.Sprintf(format, args...)}
}

// ParseSeatVerdict decodes a seat verdict strictly: at most MaxVerdictBytes,
// no unknown fields, exactly one JSON value, and every field cap enforced.
// Nil slices become empty ones. ReviewID is not checked here; the service
// that stores the verdict checks it.
func ParseSeatVerdict(raw []byte) (SeatVerdict, error) {
	if len(raw) > MaxVerdictBytes {
		return SeatVerdict{}, ve("verdict", "too large: %d bytes, the cap is %d", len(raw), MaxVerdictBytes)
	}
	var v SeatVerdict
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return SeatVerdict{}, ve("verdict", "%s", decodeMsg(err))
	}
	if dec.More() {
		return SeatVerdict{}, ve("verdict", "trailing data after the JSON object")
	}
	if v.V != 1 {
		return SeatVerdict{}, ve("v", "must be 1")
	}
	switch v.Verdict {
	case VerdictAccepted, VerdictChanges, VerdictBlocked:
	default:
		return SeatVerdict{}, ve("verdict", "must be ACCEPTED, CHANGES or BLOCKED")
	}
	if strings.TrimSpace(v.Summary) == "" {
		return SeatVerdict{}, ve("summary", "is required")
	}
	if len(v.Summary) > verdictSummaryMax {
		return SeatVerdict{}, ve("summary", "is %d bytes, the cap is %d", len(v.Summary), verdictSummaryMax)
	}
	if len(v.BlockedReason) > verdictBlockedReasonMax {
		return SeatVerdict{}, ve("blockedReason", "is %d bytes, the cap is %d", len(v.BlockedReason), verdictBlockedReasonMax)
	}
	if len(v.Findings) > verdictFindingsMax {
		return SeatVerdict{}, ve("findings", "has %d items, the cap is %d", len(v.Findings), verdictFindingsMax)
	}
	if len(v.Gates) > verdictGateAcksMax {
		return SeatVerdict{}, ve("gates", "has %d items, the cap is %d", len(v.Gates), verdictGateAcksMax)
	}
	if len(v.Frames) > verdictObservationsMax {
		return SeatVerdict{}, ve("frames", "has %d items, the cap is %d", len(v.Frames), verdictObservationsMax)
	}
	for i, f := range v.Findings {
		p := fmt.Sprintf("findings[%d]", i)
		if len(f.File) > findingFileMax {
			return SeatVerdict{}, ve(p+".file", "is %d bytes, the cap is %d", len(f.File), findingFileMax)
		}
		if len(f.Why) > findingWhyMax {
			return SeatVerdict{}, ve(p+".why", "is %d bytes, the cap is %d", len(f.Why), findingWhyMax)
		}
		if len(f.Check) > findingCheckMax {
			return SeatVerdict{}, ve(p+".check", "is %d bytes, the cap is %d", len(f.Check), findingCheckMax)
		}
		switch f.Severity {
		case "", "P0", "P1", "P2", "P3":
		default:
			return SeatVerdict{}, ve(p+".severity", "must be P0, P1, P2 or P3")
		}
	}
	for i, g := range v.Gates {
		if len(g.Note) > gateAckNoteMax {
			return SeatVerdict{}, ve(fmt.Sprintf("gates[%d].note", i), "is %d bytes, the cap is %d", len(g.Note), gateAckNoteMax)
		}
	}
	for i, f := range v.Frames {
		p := fmt.Sprintf("frames[%d]", i)
		if len(f.Path) > observationPathMax {
			return SeatVerdict{}, ve(p+".path", "is %d bytes, the cap is %d", len(f.Path), observationPathMax)
		}
		if len(f.Observation) > observationTextMax {
			return SeatVerdict{}, ve(p+".observation", "is %d bytes, the cap is %d", len(f.Observation), observationTextMax)
		}
	}
	if v.Findings == nil {
		v.Findings = []SeatFinding{}
	}
	if v.Gates == nil {
		v.Gates = []GateAck{}
	}
	if v.Frames == nil {
		v.Frames = []FrameObservation{}
	}
	return v, nil
}

// decodeMsg renders a decoder error as a verdict message; the unknown-field
// error is rewritten so it names the field plainly.
func decodeMsg(err error) string {
	if err == io.EOF {
		return "no JSON object"
	}
	msg := err.Error()
	const prefix = `json: unknown field "`
	if s, ok := strings.CutPrefix(msg, prefix); ok {
		if name, ok := strings.CutSuffix(s, `"`); ok {
			return fmt.Sprintf("unknown field %q", name)
		}
	}
	return msg
}

// CheckSeatVerdict applies the AC17 completeness rules to a parsed seat
// verdict against the digest it judged. Every error is a VerdictError whose
// message names the missing item. ReviewID is not checked here; the service
// does it.
func CheckSeatVerdict(v SeatVerdict, d Digest) error {
	known := make(map[string]bool, len(d.Gates))
	for _, g := range d.Gates {
		known[g.ID] = true
	}
	for i, a := range v.Gates {
		if !known[a.ID] {
			return ve(fmt.Sprintf("gates[%d].id", i), "unknown gate %q", a.ID)
		}
	}
	switch v.Verdict {
	case VerdictAccepted:
		return checkAccepted(v, d)
	case VerdictChanges:
		return checkChanges(v)
	case VerdictBlocked:
		if strings.TrimSpace(v.BlockedReason) == "" {
			return ve("blockedReason", "required for BLOCKED")
		}
		return nil
	default:
		return ve("verdict", "must be ACCEPTED, CHANGES or BLOCKED")
	}
}

// checkAccepted enforces what an ACCEPTED verdict must carry: every gate of
// the digest acknowledged, a real observation per frame, no P0/P1 or unrated
// finding, and no blocked reason.
func checkAccepted(v SeatVerdict, d Digest) error {
	acked := make(map[string]bool, len(v.Gates))
	for _, a := range v.Gates {
		if a.Ack {
			acked[a.ID] = true
		}
	}
	for _, g := range d.Gates {
		if !acked[g.ID] {
			return ve("gates", "gate %s not acknowledged", g.ID)
		}
	}
	observed := make(map[string]string, len(v.Frames))
	for _, f := range v.Frames {
		observed[f.Path] = f.Observation
	}
	for _, item := range d.Frames.Items {
		if nonSpaceChars(observed[item.Path]) < observationMinChars {
			return ve("frames", "no observation for %s", item.Path)
		}
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

// checkChanges enforces what a CHANGES verdict must carry: at least one
// finding, each with file, line, why and the check that shows it fixed.
func checkChanges(v SeatVerdict) error {
	if len(v.Findings) == 0 {
		return ve("findings", "CHANGES needs at least one finding")
	}
	for i, f := range v.Findings {
		p := fmt.Sprintf("findings[%d]", i)
		if strings.TrimSpace(f.File) == "" {
			return ve(p+".file", "is required")
		}
		if f.Line <= 0 {
			return ve(p+".line", "must be greater than 0")
		}
		if strings.TrimSpace(f.Why) == "" {
			return ve(p+".why", "is required")
		}
		if strings.TrimSpace(f.Check) == "" {
			return ve(p+".check", "the check that shows it fixed is required")
		}
	}
	return nil
}

// nonSpaceChars counts the characters of s that are not whitespace.
func nonSpaceChars(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}
