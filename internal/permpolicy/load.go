package permpolicy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Caps of a policy file (ADR-0047 §2 "Loader checks"). A file over a cap is
// invalid as a whole: the engine then runs floor + ask, never a partial or
// permissive reading of a broken rule set.
const (
	MaxPolicyBytes = 256 << 10
	MaxRules       = 200
	MaxWhy         = 160
)

// maxList caps every list a rule may carry, so a rule cannot be turned into
// a broad matcher by listing half the system.
const maxListLen = 64

// ruleIDRe bounds rule ids; maxRuleIDLen mirrors its length for messages.
const maxRuleIDLen = 64

var ruleIDRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// placeholderRe finds every {…} in an allow_only literal; only the two
// daemon-supplied placeholders exist in v1.
var placeholderRe = regexp.MustCompile(`\{([^{}]*)\}`)

// mutatingTools are the tools an `allow` may never cover without a path
// scope: every Bash allow counts as mutating in v1, because any Bash
// command can redirect into a file.
var mutatingTools = map[string]bool{
	"Bash":         true,
	"Edit":         true,
	"Write":        true,
	"MultiEdit":    true,
	"NotebookEdit": true,
}

// LoadError is a caller-fixable policy failure naming the YAML field it is
// about, e.g. `rules[1].match.tool`.
type LoadError struct{ Path, Msg string }

func (e *LoadError) Error() string { return e.Path + ": " + e.Msg }

func le(p, format string, a ...any) error {
	return &LoadError{Path: p, Msg: fmt.Sprintf(format, a...)}
}

// Policy is one loaded, validated policy file. Sha is the lowercase hex
// sha256 of the raw bytes, so every audit row can pin the exact rules that
// produced a decision even after the file changed.
type Policy struct {
	Version int
	Rules   []Rule
	Sha     string
}

// Rule is one human-authored standing decision. Match fields left empty
// mean "any" for that dimension; wildcards are rejected at load, so an
// empty field is the only breadth a rule can have.
type Rule struct {
	ID     string `yaml:"id"`
	Action Action `yaml:"action"`
	Why    string `yaml:"why"`
	Match  Match  `yaml:"match"`
}

// Match is what a rule fits. Tool is an exact tool name or `mcp:<server>.
// <tool>`; Command is the structured Bash match (never a pattern over the
// command string); Paths scopes where a request must stay.
type Match struct {
	Seats      []string      `yaml:"seats"`
	Workspaces []string      `yaml:"workspaces"`
	Tool       string        `yaml:"tool"`
	Command    *CommandMatch `yaml:"command"`
	Paths      *PathsMatch   `yaml:"paths"`
}

// CommandMatch matches a Bash request's argv0, its subcommand and its args.
type CommandMatch struct {
	Argv0      string    `yaml:"argv0"`
	Subcommand string    `yaml:"subcommand"`
	Args       ArgsMatch `yaml:"args"`
}

// ArgsMatch constrains arguments after the subcommand: AllowOnly lists the
// only exact literals allowed (placeholders expanded), DenyFlags lists
// flags that stop a match.
type ArgsMatch struct {
	AllowOnly []string `yaml:"allow_only"`
	DenyFlags []string `yaml:"deny_flags"`
}

// PathsMatch scopes a rule to roots the resolved request paths must stay
// within.
type PathsMatch struct {
	Within []string `yaml:"within"`
}

// Load parses and validates a policy file. Every failure it returns is a
// *LoadError naming the field; an invalid file is the caller's signal to
// run floor + ask, never to fall back to some rule subset.
func Load(data []byte) (*Policy, error) {
	if len(data) > MaxPolicyBytes {
		return nil, le("policy", "oversize: %d bytes, the cap is %d", len(data), MaxPolicyBytes)
	}
	if err := strictPolicyYAML(data); err != nil {
		return nil, err
	}
	var doc struct {
		Version int    `yaml:"version"`
		Rules   []Rule `yaml:"rules"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, le("policy", "%s", strings.TrimPrefix(err.Error(), "yaml: "))
	}
	if doc.Version != 1 {
		return nil, le("version", "must be 1, got %d", doc.Version)
	}
	if len(doc.Rules) > MaxRules {
		return nil, le("rules", "has %d rules, the cap is %d", len(doc.Rules), MaxRules)
	}
	if err := validateRules(doc.Rules); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return &Policy{Version: doc.Version, Rules: doc.Rules, Sha: hex.EncodeToString(sum[:])}, nil
}

// strictPolicyYAML rejects the YAML features a policy does not use: anchors,
// aliases, explicit tags, merge keys and duplicate keys at any depth. A
// policy is human-authored text read on the permission hot path; the subset
// keeps what loads unambiguous. Keys named like pattern fields are rejected
// with their own message so the mistake reads as intentional, not as a
// decode accident.
func strictPolicyYAML(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return le("policy", "%s", strings.TrimPrefix(err.Error(), "yaml: "))
	}
	return walkPolicyYAML(&doc, "")
}

func walkPolicyYAML(n *yaml.Node, p string) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return le(orRoot(p), "YAML anchors and aliases are not allowed")
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return le(orRoot(p), "explicit YAML tags are not allowed")
	}
	switch n.Kind {
	case yaml.MappingNode:
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Value == "<<" {
				return le(orRoot(p), "YAML merge keys are not allowed")
			}
			if seen[k.Value] {
				return le(joinPath(p, k.Value), "duplicate key")
			}
			seen[k.Value] = true
			switch k.Value {
			case "regex", "regexp", "pattern", "matches":
				return le(joinPath(p, k.Value), "regex matching is not supported in v1: patterns are the structured match fields")
			}
			if err := walkPolicyYAML(n.Content[i+1], joinPath(p, k.Value)); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			if err := walkPolicyYAML(c, fmt.Sprintf("%s[%d]", p, i)); err != nil {
				return err
			}
		}
	case yaml.DocumentNode:
		for _, c := range n.Content {
			if err := walkPolicyYAML(c, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func orRoot(p string) string {
	if p == "" {
		return "policy"
	}
	return p
}

func joinPath(p, k string) string {
	if p == "" {
		return k
	}
	return p + "." + k
}

func printable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validateRules(rules []Rule) error {
	ids := map[string]bool{}
	for i, r := range rules {
		p := fmt.Sprintf("rules[%d]", i)
		if !ruleIDRe.MatchString(r.ID) {
			return le(p+".id", "%q is not a valid id (lowercase letters, digits and -, at most %d)", r.ID, maxRuleIDLen)
		}
		if ids[r.ID] {
			return le(p+".id", "duplicate id %q", r.ID)
		}
		ids[r.ID] = true
		if r.Action != ActionAllow && r.Action != ActionDeny && r.Action != ActionAsk {
			return le(p+".action", "%q is not one of allow, deny, ask", string(r.Action))
		}
		if strings.TrimSpace(r.Why) == "" {
			return le(p+".why", "is required")
		}
		if len(r.Why) > MaxWhy {
			return le(p+".why", "is %d bytes, the cap is %d", len(r.Why), MaxWhy)
		}
		if !printable(r.Why) {
			return le(p+".why", "contains control characters")
		}
		m := r.Match
		if err := checkNoWildcard(p+".match.seats", m.Seats); err != nil {
			return err
		}
		if err := checkNoWildcard(p+".match.workspaces", m.Workspaces); err != nil {
			return err
		}
		if strings.ContainsAny(m.Tool, "*?[") {
			return le(p+".match.tool", "%q contains a wildcard: v1 matches exact names", m.Tool)
		}
		if m.Tool != "" && strings.HasPrefix(m.Tool, "mcp:") {
			rest := m.Tool[len("mcp:"):]
			dot := strings.Index(rest, ".")
			if dot <= 0 || dot == len(rest)-1 {
				return le(p+".match.tool", "%q is not mcp:<server>.<tool>", m.Tool)
			}
		}
		var argv0, subcommand string
		if m.Command != nil {
			if m.Tool != "" && m.Tool != "Bash" {
				return le(p+".match.command", "a command match is only valid with tool Bash")
			}
			argv0, subcommand = m.Command.Argv0, m.Command.Subcommand
			if strings.ContainsAny(argv0, "*?[") {
				return le(p+".match.command.argv0", "%q contains a wildcard: v1 matches exact names", argv0)
			}
			if strings.ContainsAny(subcommand, "*?[") {
				return le(p+".match.command.subcommand", "%q contains a wildcard: v1 matches exact names", subcommand)
			}
			for j, a := range m.Command.Args.AllowOnly {
				for _, ph := range placeholderRe.FindAllStringSubmatch(a, -1) {
					if ph[1] != "taskBranch" && ph[1] != "worktree" {
						return le(fmt.Sprintf("%s.match.command.args.allow_only[%d]", p, j), "%q uses placeholder {%s}; v1 has only {taskBranch} and {worktree}", a, ph[1])
					}
				}
			}
			for j, f := range m.Command.Args.DenyFlags {
				if !strings.HasPrefix(f, "-") {
					return le(fmt.Sprintf("%s.match.command.args.deny_flags[%d]", p, j), "%q is not a flag: deny_flags entries must start with '-'", f)
				}
			}
			if err := checkListCap(p+".match.command.args.allow_only", m.Command.Args.AllowOnly); err != nil {
				return err
			}
			if err := checkListCap(p+".match.command.args.deny_flags", m.Command.Args.DenyFlags); err != nil {
				return err
			}
		}
		if m.Paths != nil {
			if err := checkListCap(p+".match.paths.within", m.Paths.Within); err != nil {
				return err
			}
			for j, w := range m.Paths.Within {
				if w == "{worktree}" {
					continue
				}
				if !filepath.IsAbs(w) {
					return le(fmt.Sprintf("%s.match.paths.within[%d]", p, j), "%q must be {worktree} or an absolute path", w)
				}
				if strings.ContainsAny(w, "*?[") {
					return le(fmt.Sprintf("%s.match.paths.within[%d]", p, j), "%q contains a wildcard", w)
				}
				for _, seg := range strings.Split(filepath.ToSlash(w), "/") {
					if seg == ".." {
						return le(fmt.Sprintf("%s.match.paths.within[%d]", p, j), "%q must not contain ..", w)
					}
				}
			}
		}
		if r.Action != ActionAllow {
			continue
		}
		// Everything below is what an `allow` needs before it may answer a
		// request without the Human: a named tool, a structured Bash match
		// and a path scope for anything that can write.
		if m.Tool == "" {
			return le(p+".match.tool", "an allow rule must name a tool")
		}
		if m.Tool == "Bash" && (m.Command == nil || argv0 == "") {
			return le(p+".match.command.argv0", "an allow for Bash must match on argv0")
		}
		if mutatingTools[m.Tool] && (m.Paths == nil || len(m.Paths.Within) == 0) {
			return le(p+".match.paths.within", "an allow for %s is mutating and needs paths.within", m.Tool)
		}
	}
	return nil
}

// checkNoWildcard rejects the glob characters in a string list field; the
// path names the list so the message points at the entry.
func checkNoWildcard(p string, list []string) error {
	if err := checkListCap(p, list); err != nil {
		return err
	}
	for i, v := range list {
		if strings.ContainsAny(v, "*?[") {
			return le(fmt.Sprintf("%s[%d]", p, i), "%q contains a wildcard: v1 matches exact names", v)
		}
	}
	return nil
}

func checkListCap(p string, list []string) error {
	if len(list) > maxListLen {
		return le(p, "has %d entries, the cap is %d", len(list), maxListLen)
	}
	return nil
}
