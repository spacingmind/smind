package main

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/skills"
)

// skillNonToolIdentifiers are snake_case tokens in the smind skill that
// share a tool prefix but are not MCP tools (none today).
var skillNonToolIdentifiers = map[string]bool{}

// TestSkillBundle_ToolNamesMatchCatalog keeps the `smind` skill and the MCP
// catalog in lock-step: every tool-looking name in the skill must be a
// registered tool, and every registered tool must be mentioned.
func TestSkillBundle_ToolNamesMatchCatalog(t *testing.T) {
	env := newMCPSession(t)
	catalog := map[string]bool{}
	prefixes := map[string]bool{}
	for _, p := range []string{"task", "chat", "run", "workspace", "provider", "profile", "queue", "usage"} {
		prefixes[p] = true
	}
	for _, name := range mcpToolNames(t, env.cs) {
		catalog[name] = true
		if p, _, ok := strings.Cut(name, "_"); ok {
			prefixes[p] = true
		}
	}

	var body string
	bundle, err := skills.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range bundle {
		if s.Name == "smind" {
			body = string(s.Files["SKILL.md"])
		}
	}
	if body == "" {
		t.Fatal("smind skill not in bundle")
	}

	var alts []string
	for p := range prefixes {
		alts = append(alts, regexp.QuoteMeta(p))
	}
	sort.Strings(alts)
	re := regexp.MustCompile(`\b(?:` + strings.Join(alts, "|") + `)_[a-z][a-z0-9_]*[a-z0-9]\b`)

	mentioned := map[string]bool{}
	for _, tok := range re.FindAllString(body, -1) {
		mentioned[tok] = true
		if !catalog[tok] && !skillNonToolIdentifiers[tok] {
			t.Errorf("smind skill mentions %q, which is not a tool in tools/list", tok)
		}
	}
	for name := range catalog {
		if !mentioned[name] {
			t.Errorf("tool %q is in tools/list but not mentioned in the smind skill", name)
		}
	}
}
