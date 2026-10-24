package main

import (
	"context"
	"regexp"
	"testing"
)

var (
	// mcpForbiddenToolName matches a tool name that could create/merge a PR or push.
	mcpForbiddenToolName = regexp.MustCompile(`(?i)pr_?(create|merge)|create_?pr|merge|push|pull_?request`)
	// mcpForbiddenDescription matches a description that says a tool creates or
	// merges a pull request, or pushes.
	mcpForbiddenDescription = regexp.MustCompile(`(?i)\b(creates?|opens?|merges?|merging|pushe?s?|pushing)\b[^.]{0,40}\b(pull[ -]?requests?|prs?)\b|\bmerges?\b|\bmerging\b|\bpush(es|ing)?\b|\bgit push\b`)
)

// TestMCPHasNoPRTools pins plan decision D5 (orchestration D9): no MCP tool can create or merge a
// pull request or push; those stay human-approved agent operations.
func TestMCPHasNoPRTools(t *testing.T) {
	env := newMCPSession(t)
	res, err := env.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("tools/list is empty")
	}
	for _, tool := range res.Tools {
		if mcpForbiddenToolName.MatchString(tool.Name) {
			t.Errorf("MCP tool name %q suggests PR create/merge or push", tool.Name)
		}
		if mcpForbiddenDescription.MatchString(tool.Description) {
			t.Errorf("MCP tool %s description mentions PR create/merge or push: %q", tool.Name, tool.Description)
		}
	}
}
