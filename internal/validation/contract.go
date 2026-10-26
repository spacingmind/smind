// Package validation implements ADR-0045: the per-milestone validation
// contract a Lead submits before implementation, the commit-bound verdict a
// validator run issues against it, and the merge-ready label computed from
// the two. smind never merges: nothing here acts on a verdict.
package validation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Size and count caps (ADR-0045 §1).
const (
	MaxFrontMatterBytes = 64 << 10
	MaxBodyBytes        = 24 << 10
	MaxACs              = 40
	MaxTests            = 80
	MaxMutations        = 20
	MaxFrames           = 24
	MaxCommands         = 20

	// DefaultMaxMinutes / CeilingMaxMinutes bound limits.maxMinutes.
	DefaultMaxMinutes = 30
	CeilingMaxMinutes = 120
)

// Limits is the daemon-side configuration a contract is validated against.
type Limits struct {
	MaxBudgetUSD     float64 // ceiling for limits.budgetUsd (validation.maxBudgetUsd)
	DefaultBudgetUSD float64 // used when the contract leaves limits.budgetUsd unset
}

// DefaultLimits are the ADR-0045 Q12 numbers.
var DefaultLimits = Limits{MaxBudgetUSD: 5, DefaultBudgetUSD: 1.5}

// Contract is the validated front matter of a validation contract.
type Contract struct {
	V          int        `yaml:"v" json:"v"`
	Milestone  string     `yaml:"milestone" json:"milestone"`
	Base       string     `yaml:"base" json:"base"`
	PlanRef    string     `yaml:"planRef,omitempty" json:"planRef,omitempty"`
	ACs        []AC       `yaml:"acs" json:"acs"`
	Tests      []Test     `yaml:"tests,omitempty" json:"tests,omitempty"`
	Mutations  []Mutation `yaml:"mutations,omitempty" json:"mutations,omitempty"`
	Frames     []Frame    `yaml:"frames,omitempty" json:"frames,omitempty"`
	Commands   []Command  `yaml:"commands,omitempty" json:"commands,omitempty"`
	WriteScope WriteScope `yaml:"writeScope" json:"writeScope"`
	// EvidenceDir is always "auto": the daemon picks the stable path.
	EvidenceDir string  `yaml:"evidenceDir,omitempty" json:"evidenceDir,omitempty"`
	Limits      CLimits `yaml:"limits" json:"limits"`
}

// AC is one acceptance criterion and the checks it rests on.
type AC struct {
	ID           string `yaml:"id" json:"id"`
	Text         string `yaml:"text" json:"text"`
	Covers       Covers `yaml:"covers" json:"covers"`
	Visual       bool   `yaml:"visual,omitempty" json:"visual,omitempty"`
	VisualReason string `yaml:"visualReason,omitempty" json:"visualReason,omitempty"`
}

// Covers lists, by id or test name, the checks an AC rests on.
type Covers struct {
	Tests     []string `yaml:"tests,omitempty" json:"tests,omitempty"`
	Mutations []string `yaml:"mutations,omitempty" json:"mutations,omitempty"`
	Frames    []string `yaml:"frames,omitempty" json:"frames,omitempty"`
	Commands  []string `yaml:"commands,omitempty" json:"commands,omitempty"`
}

// Test is a named test the validator runs and must see pass.
type Test struct {
	Name       string `yaml:"name" json:"name"`
	Run        string `yaml:"run" json:"run"`
	TimeoutSec int    `yaml:"timeoutSec,omitempty" json:"timeoutSec,omitempty"`
	Quarantine bool   `yaml:"quarantine,omitempty" json:"quarantine,omitempty"`
}

// Mutation is one change after which a named test must fail.
type Mutation struct {
	ID         string   `yaml:"id" json:"id"`
	File       string   `yaml:"file" json:"file"`
	Change     string   `yaml:"change" json:"change"`
	ExpectFail []string `yaml:"expectFail" json:"expectFail"`
}

// Frame is a screen state the validator looks at.
type Frame struct {
	ID       string   `yaml:"id" json:"id"`
	Surface  string   `yaml:"surface" json:"surface"`
	State    string   `yaml:"state" json:"state"`
	Viewport []int    `yaml:"viewport,omitempty" json:"viewport,omitempty"`
	Theme    []string `yaml:"theme,omitempty" json:"theme,omitempty"`
	Harness  string   `yaml:"harness,omitempty" json:"harness,omitempty"`
}

// Command is run once and its exit status recorded.
type Command struct {
	ID         string `yaml:"id" json:"id"`
	Run        string `yaml:"run" json:"run"`
	TimeoutSec int    `yaml:"timeoutSec,omitempty" json:"timeoutSec,omitempty"`
}

// WriteScope is what the validator may touch inside its scratch worktree;
// empty means nothing beyond the scratch worktree itself.
type WriteScope struct {
	Paths []string `yaml:"paths" json:"paths"`
}

// CLimits are the contract's own cost limits (clamped by the daemon ceiling).
type CLimits struct {
	BudgetUSD  float64 `yaml:"budgetUsd" json:"budgetUsd"`
	MaxMinutes int     `yaml:"maxMinutes" json:"maxMinutes"`
}

// FieldError is a model-readable validation failure naming the field.
type FieldError struct {
	Path string
	Msg  string
}

func (e *FieldError) Error() string { return e.Path + ": " + e.Msg }

// Is makes every FieldError an ErrInvalid.
func (e *FieldError) Is(target error) bool { return target == ErrInvalid }

func fe(p, format string, a ...any) error {
	return &FieldError{Path: p, Msg: fmt.Sprintf(format, a...)}
}

// Parsed is a contract document after ParseDocument: the validated contract
// (defaults applied), its canonical JSON, the sha256 of that JSON, and the
// markdown body.
type Parsed struct {
	Contract Contract
	JSON     []byte
	Sha256   string
	Body     string
}

// ParseDocument parses "---\n<yaml>\n---\n<markdown>" and validates it.
func ParseDocument(src []byte, lim Limits) (Parsed, error) {
	front, body, err := splitFrontMatter(src)
	if err != nil {
		return Parsed{}, err
	}
	if len(front) > MaxFrontMatterBytes {
		return Parsed{}, fe("front matter", "is %d bytes, the cap is %d", len(front), MaxFrontMatterBytes)
	}
	if len(body) > MaxBodyBytes {
		return Parsed{}, fe("body", "is %d bytes, the cap is %d", len(body), MaxBodyBytes)
	}
	if err := strictYAML(front); err != nil {
		return Parsed{}, err
	}
	var c Contract
	dec := yaml.NewDecoder(bytes.NewReader(front))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return Parsed{}, fe("front matter", "%s", strings.TrimPrefix(err.Error(), "yaml: "))
	}
	if err := normalize(&c, lim); err != nil {
		return Parsed{}, err
	}
	canon, err := json.Marshal(c)
	if err != nil {
		return Parsed{}, fmt.Errorf("marshal contract: %w", err)
	}
	sum := sha256.Sum256(canon)
	return Parsed{Contract: c, JSON: canon, Sha256: hex.EncodeToString(sum[:]), Body: body}, nil
}

func splitFrontMatter(src []byte) (front []byte, body string, err error) {
	s := strings.ReplaceAll(string(src), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, "", fe("front matter", "the document must start with a --- line")
	}
	rest := s[4:]
	idx := strings.Index(rest, "\n---\n")
	switch {
	case idx >= 0:
		return []byte(rest[:idx+1]), rest[idx+5:], nil
	case strings.HasSuffix(rest, "\n---"):
		return []byte(rest[:len(rest)-3]), "", nil
	}
	return nil, "", fe("front matter", "no closing --- line")
}

// strictYAML rejects the YAML features a contract does not use: anchors,
// aliases, explicit tags, merge keys and duplicate keys.
func strictYAML(front []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(front, &doc); err != nil {
		return fe("front matter", "%s", strings.TrimPrefix(err.Error(), "yaml: "))
	}
	return walkYAML(&doc, "")
}

func walkYAML(n *yaml.Node, p string) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return fe(orRoot(p), "YAML anchors and aliases are not allowed")
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return fe(orRoot(p), "explicit YAML tags are not allowed")
	}
	switch n.Kind {
	case yaml.MappingNode:
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Value == "<<" {
				return fe(orRoot(p), "YAML merge keys are not allowed")
			}
			if seen[k.Value] {
				return fe(joinPath(p, k.Value), "duplicate key")
			}
			seen[k.Value] = true
			if err := walkYAML(n.Content[i+1], joinPath(p, k.Value)); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			if err := walkYAML(c, fmt.Sprintf("%s[%d]", p, i)); err != nil {
				return err
			}
		}
	case yaml.DocumentNode:
		for _, c := range n.Content {
			if err := walkYAML(c, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func orRoot(p string) string {
	if p == "" {
		return "front matter"
	}
	return p
}

func joinPath(p, k string) string {
	if p == "" {
		return k
	}
	return p + "." + k
}

var (
	acIDRe    = regexp.MustCompile(`^[A-Z][A-Z0-9-]{0,15}$`)
	itemIDRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)
	validRefs = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@{}~^-]{0,199}$`)
)

func printable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func textField(p, s string, max int) error {
	if strings.TrimSpace(s) == "" {
		return fe(p, "is required")
	}
	if len(s) > max {
		return fe(p, "is %d bytes, the cap is %d", len(s), max)
	}
	if !printable(s) {
		return fe(p, "contains control characters")
	}
	return nil
}

// SafeRelPath reports whether p is a relative slash-or-backslash path with no
// ".." segment, no drive letter and no empty or "." result.
func SafeRelPath(p string) bool {
	if p == "" || strings.ContainsAny(p, "\x00") {
		return false
	}
	q := strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(q, "/") || (len(q) >= 2 && q[1] == ':') {
		return false
	}
	for _, seg := range strings.Split(q, "/") {
		if seg == ".." {
			return false
		}
	}
	c := path.Clean(q)
	return c != "." && c != ""
}

func normalize(c *Contract, lim Limits) error {
	if lim.MaxBudgetUSD <= 0 {
		lim = DefaultLimits
	}
	if c.V != 1 {
		return fe("v", "must be 1, got %d", c.V)
	}
	if err := textField("milestone", c.Milestone, 120); err != nil {
		return err
	}
	if !validRefs.MatchString(c.Base) || strings.Contains(c.Base, "..") {
		return fe("base", "must be a git ref such as origin/develop")
	}
	if c.PlanRef != "" {
		if err := textField("planRef", c.PlanRef, 200); err != nil {
			return err
		}
	}
	if c.EvidenceDir != "" && c.EvidenceDir != "auto" {
		return fe("evidenceDir", `must be "auto" (the daemon picks the stable path)`)
	}
	c.EvidenceDir = "auto"
	switch {
	case len(c.ACs) == 0:
		return fe("acs", "needs at least one acceptance criterion")
	case len(c.ACs) > MaxACs:
		return fe("acs", "has %d items, the cap is %d", len(c.ACs), MaxACs)
	case len(c.Tests) > MaxTests:
		return fe("tests", "has %d items, the cap is %d", len(c.Tests), MaxTests)
	case len(c.Mutations) > MaxMutations:
		return fe("mutations", "has %d items, the cap is %d", len(c.Mutations), MaxMutations)
	case len(c.Frames) > MaxFrames:
		return fe("frames", "has %d items, the cap is %d", len(c.Frames), MaxFrames)
	case len(c.Commands) > MaxCommands:
		return fe("commands", "has %d items, the cap is %d", len(c.Commands), MaxCommands)
	}

	tests := map[string]bool{}
	for i, t := range c.Tests {
		p := fmt.Sprintf("tests[%d]", i)
		if err := textField(p+".name", t.Name, 160); err != nil {
			return err
		}
		if err := textField(p+".run", t.Run, 1000); err != nil {
			return err
		}
		if tests[t.Name] {
			return fe(p+".name", "duplicate test %q", t.Name)
		}
		tests[t.Name] = true
		if t.TimeoutSec < 0 || t.TimeoutSec > 3600 {
			return fe(p+".timeoutSec", "must be between 0 and 3600")
		}
	}
	ids := map[string]string{} // id -> kind, for mutations, frames, commands
	claim := func(p, id, kind string) error {
		if !itemIDRe.MatchString(id) {
			return fe(p+".id", "%q is not a valid id (letters, digits, - and _, starting with a letter)", id)
		}
		if k, dup := ids[id]; dup {
			return fe(p+".id", "%q is already used by a %s", id, k)
		}
		ids[id] = kind
		return nil
	}
	for i, m := range c.Mutations {
		p := fmt.Sprintf("mutations[%d]", i)
		if err := claim(p, m.ID, "mutation"); err != nil {
			return err
		}
		if !SafeRelPath(m.File) {
			return fe(p+".file", "must be a relative path without ..")
		}
		if err := textField(p+".change", m.Change, 500); err != nil {
			return err
		}
		if len(m.ExpectFail) == 0 {
			return fe(p+".expectFail", "needs at least one test that must fail")
		}
		for _, n := range m.ExpectFail {
			if !tests[n] {
				return fe(p+".expectFail", "names %q, which is not in tests", n)
			}
		}
	}
	for i, f := range c.Frames {
		p := fmt.Sprintf("frames[%d]", i)
		if err := claim(p, f.ID, "frame"); err != nil {
			return err
		}
		if err := textField(p+".surface", f.Surface, 200); err != nil {
			return err
		}
		if err := textField(p+".state", f.State, 300); err != nil {
			return err
		}
		for _, w := range f.Viewport {
			if w < 200 || w > 8000 {
				return fe(p+".viewport", "width %d is out of range", w)
			}
		}
		for _, th := range f.Theme {
			if th != "light" && th != "dark" {
				return fe(p+".theme", "%q must be light or dark", th)
			}
		}
	}
	for i, cm := range c.Commands {
		p := fmt.Sprintf("commands[%d]", i)
		if err := claim(p, cm.ID, "command"); err != nil {
			return err
		}
		if err := textField(p+".run", cm.Run, 1000); err != nil {
			return err
		}
		if cm.TimeoutSec < 0 || cm.TimeoutSec > 3600 {
			return fe(p+".timeoutSec", "must be between 0 and 3600")
		}
	}

	acs := map[string]bool{}
	for i, a := range c.ACs {
		p := fmt.Sprintf("acs[%d]", i)
		if !acIDRe.MatchString(a.ID) {
			return fe(p+".id", "%q is not a valid AC id (A-Z, digits, -, at most 16)", a.ID)
		}
		if acs[a.ID] {
			return fe(p+".id", "duplicate AC %q", a.ID)
		}
		acs[a.ID] = true
		if err := textField(p+".text", a.Text, 400); err != nil {
			return err
		}
		for _, n := range a.Covers.Tests {
			if !tests[n] {
				return fe(p+".covers.tests", "names %q, which is not in tests", n)
			}
		}
		for _, grp := range []struct {
			field string
			refs  []string
			kind  string
		}{{"mutations", a.Covers.Mutations, "mutation"}, {"frames", a.Covers.Frames, "frame"}, {"commands", a.Covers.Commands, "command"}} {
			for _, r := range grp.refs {
				if ids[r] != grp.kind {
					return fe(p+".covers."+grp.field, "names %q, which is not a %s in this contract", r, grp.kind)
				}
			}
		}
		nonFrame := len(a.Covers.Tests) + len(a.Covers.Mutations) + len(a.Covers.Commands)
		switch {
		case nonFrame+len(a.Covers.Frames) == 0:
			return fe(p, "%s has no check: cover it with a test, mutation, command or frame", a.ID)
		case nonFrame == 0 && !a.Visual:
			return fe(p, "%s rests on frames only: add a test or command, or mark it visual: true with a visualReason", a.ID)
		case a.Visual && strings.TrimSpace(a.VisualReason) == "":
			return fe(p+".visualReason", "is required when visual is true")
		}
		if a.VisualReason != "" {
			if err := textField(p+".visualReason", a.VisualReason, 300); err != nil {
				return err
			}
		}
	}

	for i, wp := range c.WriteScope.Paths {
		p := fmt.Sprintf("writeScope.paths[%d]", i)
		if !SafeRelPath(wp) {
			return fe(p, "%q must be a relative path without ..", wp)
		}
		if strings.ContainsAny(wp, "*?[]{}") {
			return fe(p, "%q must be a file or a directory prefix, not a glob", wp)
		}
	}
	if c.WriteScope.Paths == nil {
		c.WriteScope.Paths = []string{}
	}

	if c.Limits.BudgetUSD < 0 {
		return fe("limits.budgetUsd", "must not be negative")
	}
	if c.Limits.BudgetUSD == 0 {
		c.Limits.BudgetUSD = lim.DefaultBudgetUSD
	}
	if c.Limits.BudgetUSD > lim.MaxBudgetUSD {
		return fe("limits.budgetUsd", "%.2f is above the daemon ceiling %.2f (validation.maxBudgetUsd)", c.Limits.BudgetUSD, lim.MaxBudgetUSD)
	}
	if c.Limits.MaxMinutes < 0 || c.Limits.MaxMinutes > CeilingMaxMinutes {
		return fe("limits.maxMinutes", "must be between 1 and %d", CeilingMaxMinutes)
	}
	if c.Limits.MaxMinutes == 0 {
		c.Limits.MaxMinutes = DefaultMaxMinutes
	}
	return nil
}

// ErrInvalid wraps every caller-fixable failure (bad contract, bad verdict).
var ErrInvalid = errors.New("invalid")
