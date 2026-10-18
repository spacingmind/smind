package gate

import (
	"strings"
	"testing"
)

func TestRender_Frontmatter(t *testing.T) {
	s := Skill{
		Name:        "learned-go-race",
		Category:    "go",
		Description: `Use when tests fail: check "shared state" first.`,
		Body:        "\nRun `go test -race`.\n\n",
	}
	got := Render(s)
	want := "---\n" +
		"name: learned-go-race\n" +
		"description: \"Use when tests fail: check \\\"shared state\\\" first.\"\n" +
		"category: go\n" +
		"---\n\n" +
		"Run `go test -race`.\n"
	if got != want {
		t.Errorf("Render mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
	// No category -> no category line.
	got = Render(Skill{Name: "learned-x", Description: "Use when x."})
	if strings.Contains(got, "category:") {
		t.Errorf("unexpected category line: %s", got)
	}
}

func TestApplyPatch(t *testing.T) {
	body := "alpha\nbeta\ngamma\n"
	// exactly one match
	got, r, ok := ApplyPatch(body, "beta", "BETA")
	if !ok || r != "" || got != "alpha\nBETA\ngamma\n" {
		t.Errorf("one match: got %q, %v, %v", got, r, ok)
	}
	// zero matches
	_, r, ok = ApplyPatch(body, "delta", "x")
	if ok || r != ReasonPatchTargetMissing {
		t.Errorf("zero matches: %v, %v", r, ok)
	}
	// two matches
	dup := "alpha\nalpha\n"
	_, r, ok = ApplyPatch(dup, "alpha", "x")
	if ok || r != ReasonAmbiguousPatch {
		t.Errorf("two matches: %v, %v", r, ok)
	}
	// empty old string
	_, r, ok = ApplyPatch(body, "", "x")
	if ok || r != ReasonAmbiguousPatch {
		t.Errorf("empty old: %v, %v", r, ok)
	}
}

func TestCheckHiddenText(t *testing.T) {
	bad := []struct {
		name string
		text string
	}{
		{"zero-width space", "a\u200Bb"},
		{"bidi override", "ev\u202Eil"},
		{"fullword", "\uFF49\uFF47\uFF4E\uFF4F\uFF52\uFF45"},
		{"halfwidth katakana", "\uFF71"},
		{"control char", "a\x00b"},
		{"unassigned private use", "\U000F0000"},
	}
	for _, tc := range bad {
		fs := CheckHiddenText(tc.text)
		if !hasReason(fs, ReasonHiddenText) {
			t.Errorf("%s: CheckHiddenText = %v, want hidden_text", tc.name, fs)
		}
	}
	if fs := CheckHiddenText("line1\nline2\ttabbed\r\nend"); len(fs) != 0 {
		t.Errorf("newline/tab/carriage-return rejected: %v", fs)
	}
	if fs := CheckHiddenText("plain ASCII and 中文 text"); len(fs) != 0 {
		t.Errorf("ordinary CJK rejected: %v", fs)
	}
	detail := CheckHiddenText("\u200B")[0].Detail
	if !strings.Contains(detail, "U+200B") {
		t.Errorf("detail %q lacks codepoint", detail)
	}
}
