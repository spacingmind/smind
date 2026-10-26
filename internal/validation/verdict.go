package validation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Verdict statuses and item kinds (ADR-0045 §3).
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusBlocked = "blocked"

	KindTest     = "test"
	KindMutation = "mutation"
	KindFrame    = "frame"
	KindCommand  = "command"
	KindAC       = "ac"
)

// Blocked reasons. A validator may submit validatorBlockedReasons; the daemon
// alone writes timeout, budget, crashed and scope_violation.
const (
	ReasonNoBrowser          = "no_browser"
	ReasonNoToolchain        = "no_toolchain"
	ReasonFlakyUnresolved    = "flaky_unresolved"
	ReasonContractUnrunnable = "contract_unrunnable"
	ReasonTimeout            = "timeout"
	ReasonBudget             = "budget"
	ReasonCrashed            = "crashed"
	ReasonScopeViolation     = "scope_violation"
)

var validatorBlockedReasons = map[string]bool{
	ReasonNoBrowser: true, ReasonNoToolchain: true, ReasonFlakyUnresolved: true, ReasonContractUnrunnable: true,
}

var daemonBlockedReasons = map[string]bool{
	ReasonTimeout: true, ReasonBudget: true, ReasonCrashed: true, ReasonScopeViolation: true,
}

const (
	MaxVerdictBytes   = 256 << 10
	MaxExcerptBytes   = 2 << 10
	MaxEvidencePerItm = 8
	MaxFindings       = 50
	MaxFileEvidence   = 5 << 20
)

var sha40Re = regexp.MustCompile(`^[0-9a-f]{40}$`)

// IsSHA reports whether s is a full 40-hex commit id.
func IsSHA(s string) bool { return sha40Re.MatchString(s) }

// Verdict is the structured result a validator run submits.
type Verdict struct {
	V              int       `json:"v"`
	ContractID     int64     `json:"contractId"`
	ContractSha256 string    `json:"contractSha256"`
	CommitSHA      string    `json:"commitSha"`
	Status         string    `json:"status"`
	BlockedReason  string    `json:"blockedReason,omitempty"`
	Summary        string    `json:"summary"`
	Items          []Item    `json:"items"`
	Findings       []Finding `json:"findings,omitempty"`
	Usage          *Usage    `json:"usage,omitempty"`
}

// Item is one check's result with its evidence.
type Item struct {
	Kind     string     `json:"kind"`
	ID       string     `json:"id"`
	Status   string     `json:"status"` // pass | fail | blocked | skipped
	Attempts int        `json:"attempts,omitempty"`
	Flaky    bool       `json:"flaky,omitempty"`
	Restored *bool      `json:"restored,omitempty"`
	Note     string     `json:"note,omitempty"`
	Via      []string   `json:"via,omitempty"`
	Evidence []Evidence `json:"evidence,omitempty"`
}

// Evidence is an excerpt of real tool output or a hashed evidence file.
type Evidence struct {
	Type   string `json:"type"` // excerpt | file
	Text   string `json:"text,omitempty"`
	Path   string `json:"path,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// Finding is an adversarial-review finding.
type Finding struct {
	Severity string `json:"severity"` // P0..P3
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Text     string `json:"text"`
}

// Usage is the validator's own cost and time.
type Usage struct {
	CostUSD float64 `json:"costUsd"`
	Minutes float64 `json:"minutes"`
}

// ParseVerdict decodes a verdict strictly (unknown fields are an error, so a
// validator cannot set a daemon-only field).
func ParseVerdict(raw []byte) (Verdict, error) {
	if len(raw) > MaxVerdictBytes {
		return Verdict{}, fe("verdict", "is %d bytes, the cap is %d", len(raw), MaxVerdictBytes)
	}
	var v Verdict
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return Verdict{}, fe("verdict", "%s", err.Error())
	}
	return v, nil
}

type itemKey struct{ kind, id string }

// CheckVerdict applies the completeness rules to a validator-submitted
// verdict against the contract c it judges. The daemon, not the model,
// decides what a pass is.
func CheckVerdict(c Contract, v Verdict) error {
	if v.V != 1 {
		return fe("v", "must be 1")
	}
	if !IsSHA(v.CommitSHA) {
		return fe("commitSha", "must be a full 40-hex commit id")
	}
	if len(v.Summary) > 800 || !printableNL(v.Summary) {
		return fe("summary", "must be at most 800 characters of text")
	}
	if len(v.Findings) > MaxFindings {
		return fe("findings", "has %d items, the cap is %d", len(v.Findings), MaxFindings)
	}
	for i, f := range v.Findings {
		p := fmt.Sprintf("findings[%d]", i)
		switch f.Severity {
		case "P0", "P1", "P2", "P3":
		default:
			return fe(p+".severity", "must be P0, P1, P2 or P3")
		}
		if strings.TrimSpace(f.Text) == "" || len(f.Text) > 400 {
			return fe(p+".text", "is required, at most 400 characters")
		}
		if f.File != "" && !SafeRelPath(f.File) {
			return fe(p+".file", "must be a relative path")
		}
	}

	known := map[itemKey]bool{}
	for _, t := range c.Tests {
		known[itemKey{KindTest, t.Name}] = true
	}
	for _, m := range c.Mutations {
		known[itemKey{KindMutation, m.ID}] = true
	}
	for _, f := range c.Frames {
		known[itemKey{KindFrame, f.ID}] = true
	}
	for _, cm := range c.Commands {
		known[itemKey{KindCommand, cm.ID}] = true
	}
	for _, a := range c.ACs {
		known[itemKey{KindAC, a.ID}] = true
	}
	items := map[itemKey]Item{}
	anyFail := false
	for i, it := range v.Items {
		p := fmt.Sprintf("items[%d]", i)
		k := itemKey{it.Kind, it.ID}
		if !known[k] {
			return fe(p, "%s %q is not in the contract", it.Kind, it.ID)
		}
		if _, dup := items[k]; dup {
			return fe(p, "duplicate %s item %q", it.Kind, it.ID)
		}
		switch it.Status {
		case StatusPass, StatusFail, StatusBlocked, "skipped":
		default:
			return fe(p+".status", "must be pass, fail, blocked or skipped")
		}
		if len(it.Note) > 400 || !printableNL(it.Note) {
			return fe(p+".note", "must be at most 400 characters of text")
		}
		if len(it.Evidence) > MaxEvidencePerItm {
			return fe(p+".evidence", "has %d entries, the cap is %d", len(it.Evidence), MaxEvidencePerItm)
		}
		for j, ev := range it.Evidence {
			if err := checkEvidence(fmt.Sprintf("%s.evidence[%d]", p, j), ev); err != nil {
				return err
			}
		}
		if it.Status == StatusFail {
			anyFail = true
		}
		items[k] = it
	}

	switch v.Status {
	case StatusBlocked:
		if v.BlockedReason == "" {
			return fe("blockedReason", "is required when status is blocked")
		}
		if !validatorBlockedReasons[v.BlockedReason] {
			if daemonBlockedReasons[v.BlockedReason] {
				return fe("blockedReason", "%q is written by the daemon, not by a validator", v.BlockedReason)
			}
			return fe("blockedReason", "%q is not a known reason", v.BlockedReason)
		}
		if strings.TrimSpace(v.Summary) == "" {
			return fe("summary", "must say what the validator lacks when status is blocked")
		}
		return nil
	case StatusFail:
		if v.BlockedReason != "" {
			return fe("blockedReason", "is only for blocked verdicts")
		}
		if !anyFail {
			return fe("items", "a fail verdict needs at least one failing item")
		}
		if len(v.Findings) == 0 && !failHasEvidence(v.Items) {
			return fe("findings", "a fail verdict needs a finding or an excerpt saying why")
		}
		return nil
	case StatusPass:
	default:
		return fe("status", "must be pass, fail or blocked")
	}
	if v.BlockedReason != "" {
		return fe("blockedReason", "is only for blocked verdicts")
	}
	return checkPass(c, v, items)
}

func checkPass(c Contract, v Verdict, items map[itemKey]Item) error {
	quarantined := map[string]bool{}
	for _, t := range c.Tests {
		quarantined[t.Name] = t.Quarantine
	}
	need := func(kind, id string) (Item, error) {
		it, ok := items[itemKey{kind, id}]
		if !ok {
			return Item{}, fe("items", "a pass verdict needs a %s item for %q", kind, id)
		}
		if it.Status == StatusPass {
			return it, nil
		}
		if kind == KindTest && quarantined[id] && it.Status == StatusFail {
			if len(v.Findings) == 0 {
				return Item{}, fe("findings", "quarantined test %q failed: add a finding saying so", id)
			}
			return it, nil
		}
		return Item{}, fe("items", "%s %q is %s, so the verdict cannot be pass", kind, id, it.Status)
	}
	evidenceNeeded := func(kind, id string, it Item) error {
		if len(it.Evidence) == 0 {
			return fe("items", "%s %q has no evidence", kind, id)
		}
		return nil
	}
	for _, t := range c.Tests {
		it, err := need(KindTest, t.Name)
		if err != nil {
			return err
		}
		if err := evidenceNeeded(KindTest, t.Name, it); err != nil {
			return err
		}
	}
	for _, m := range c.Mutations {
		it, err := need(KindMutation, m.ID)
		if err != nil {
			return err
		}
		if err := evidenceNeeded(KindMutation, m.ID, it); err != nil {
			return err
		}
		if it.Restored == nil || !*it.Restored {
			return fe("items", "mutation %q must say restored: true", m.ID)
		}
	}
	for _, cm := range c.Commands {
		it, err := need(KindCommand, cm.ID)
		if err != nil {
			return err
		}
		if err := evidenceNeeded(KindCommand, cm.ID, it); err != nil {
			return err
		}
	}
	for _, f := range c.Frames {
		it, err := need(KindFrame, f.ID)
		if err != nil {
			return err
		}
		if err := evidenceNeeded(KindFrame, f.ID, it); err != nil {
			return err
		}
		if strings.TrimSpace(it.Note) == "" {
			return fe("items", "frame %q needs a note saying what you observed", f.ID)
		}
	}
	for _, a := range c.ACs {
		it, err := need(KindAC, a.ID)
		if err != nil {
			return err
		}
		if len(it.Via) == 0 {
			return fe("items", "AC %q must name the checks it rests on in via", a.ID)
		}
		nonFrame := false
		for _, ref := range it.Via {
			rit, kind, ok := lookupVia(items, ref)
			if !ok {
				return fe("items", "AC %q names %q in via, which is not an item of this verdict", a.ID, ref)
			}
			if rit.Status != StatusPass && !(kind == KindTest && quarantined[ref]) {
				return fe("items", "AC %q rests on %q, which is %s", a.ID, ref, rit.Status)
			}
			if kind != KindFrame {
				nonFrame = true
			}
		}
		if !nonFrame && !a.Visual {
			return fe("items", "AC %q rests only on frames and is not marked visual", a.ID)
		}
	}
	return nil
}

// lookupVia finds the non-AC item a via entry names.
func lookupVia(items map[itemKey]Item, ref string) (Item, string, bool) {
	for _, kind := range []string{KindTest, KindMutation, KindCommand, KindFrame} {
		if it, ok := items[itemKey{kind, ref}]; ok {
			return it, kind, true
		}
	}
	return Item{}, "", false
}

func failHasEvidence(items []Item) bool {
	for _, it := range items {
		if it.Status == StatusFail && len(it.Evidence) > 0 {
			return true
		}
	}
	return false
}

func checkEvidence(p string, ev Evidence) error {
	switch ev.Type {
	case "excerpt":
		if strings.TrimSpace(ev.Text) == "" {
			return fe(p+".text", "is required for an excerpt")
		}
		if len(ev.Text) > MaxExcerptBytes {
			return fe(p+".text", "is %d bytes, the cap is %d", len(ev.Text), MaxExcerptBytes)
		}
		if ev.Path != "" {
			return fe(p+".path", "is only for file evidence")
		}
	case "file":
		if !SafeRelPath(ev.Path) {
			return fe(p+".path", "must be a relative path inside the scratch worktree")
		}
		if ev.Text != "" {
			return fe(p+".text", "is only for excerpts")
		}
	default:
		return fe(p+".type", "must be excerpt or file")
	}
	return nil
}

func printableNL(s string) bool {
	for _, r := range s {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return false
		}
	}
	return true
}

// MergeReady reports whether verdict status/sha is a current pass: the latest
// verdict of the task, at the branch head now, against the task's highest
// contract version, with no scope violation. It is a label for a human's
// decision, never an action. The reason is non-empty when it is false.
func MergeReady(status, commitSHA string, scopeViolation bool, head string, contractIsLatest bool) (bool, string) {
	switch {
	case status != StatusPass:
		return false, "the latest verdict is not a pass"
	case scopeViolation:
		return false, "the validator changed the lead's worktree (scope violation)"
	case head == "" || commitSHA != head:
		return false, "stale: the branch head moved after this verdict"
	case !contractIsLatest:
		return false, "stale: the contract was replaced by a newer version"
	}
	return true, ""
}
