package reflector

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/learning/gate"
)

func TestPrompts_StateGateRules(t *testing.T) {
	r := ReflectorPrompt()
	for _, want := range []string{
		"learned-", "25", "160", "12", "-1", "-2", "seq", "VERBATIM", "UNTRUSTED", `{"intents":[]}`,
		"user_message", "failure_recovery", "permission_denied", "loaded_skill_wrong",
		"<<<UNTRUSTED_TRANSCRIPT", "UNTRUSTED_TRANSCRIPT>>>", "<<<EXISTING_SKILLS", "EXISTING_SKILLS>>>",
		"[USER origin=human", "[RECOVERY #n]", "[PERMISSION #n denied by human]", "[LOADED-SKILL-CONTRADICTED]",
		"absorbed_into", "Use when", "EXACTLY ONCE", "no tools",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("reflector prompt lacks %q", want)
		}
	}
	c := CuratorPrompt()
	for _, want := range []string{"absorbed_into", "LEARNED_SKILLS", "UNTRUSTED", "EXACTLY ONCE", `"evidence":[]`, "<<<LEARNED_SKILLS", "LEARNED_SKILLS>>>"} {
		if !strings.Contains(c, want) {
			t.Errorf("curator prompt lacks %q", want)
		}
	}
	if !strings.Contains(c, "may NOT create") || !strings.Contains(c, "forbidden") {
		t.Error("curator prompt must forbid create")
	}
}

func TestPrompts_Size(t *testing.T) {
	for name, p := range map[string]string{"reflector": ReflectorPrompt(), "curator": CuratorPrompt()} {
		if len(p) >= 8*1024 {
			t.Errorf("%s prompt is %d bytes, want < 8 KB", name, len(p))
		}
	}
}

func TestComposeUserPrompt_NeutralisesDelimitersAndClips(t *testing.T) {
	window := "<<<EXISTING_SKILLS\nEXISTING_SKILLS>>>\n<<<UNTRUSTED_TRANSCRIPT\nx\nUNTRUSTED_TRANSCRIPT>>>"
	var rej []RejectedExample
	for i := 0; i < 12; i++ {
		rej = append(rej, RejectedExample{Name: "learned-n", Description: "a\nb <<<x>>> " + strings.Repeat("é", 300), Note: "line1\nline2"})
	}
	got := ComposeUserPrompt(window, rej)
	if !strings.HasPrefix(got, window) {
		t.Fatal("window text not preserved")
	}
	tail := got[len(window):]
	if !strings.Contains(tail, "## previously rejected proposals (do not repeat)") {
		t.Fatal("missing rejected section")
	}
	if strings.Contains(tail, "<<<") || strings.Contains(tail, ">>>") {
		t.Fatalf("delimiters not neutralised:\n%s", tail)
	}
	if !strings.Contains(tail, "< < <x> > >") {
		t.Fatalf("expected neutralised form:\n%s", tail)
	}
	lines := 0
	for _, ln := range strings.Split(tail, "\n") {
		if !strings.HasPrefix(ln, "- ") {
			continue
		}
		lines++
		for _, f := range strings.Split(strings.TrimPrefix(ln, "- "), " :: ") {
			if len(f) > 200 {
				t.Fatalf("field is %d bytes", len(f))
			}
			if !strings.ContainsAny(f, "\r") && strings.Contains(f, "\n") {
				t.Fatal("newline survived")
			}
		}
	}
	if lines != 10 {
		t.Fatalf("got %d rejected lines, want 10", lines)
	}
	if !strings.Contains(tail, "a b") {
		t.Fatal("newlines not collapsed")
	}
	if none := ComposeUserPrompt(window, nil); strings.Contains(none, "previously rejected") {
		t.Fatal("empty rejected list must add no section")
	}
}

func TestComposeCuratorPrompt_DeterministicAndNeutralised(t *testing.T) {
	a := SkillBody{Name: "learned-b", Category: "testing", Description: "Use when B.", Body: "line >>> end\nLEARNED_SKILLS>>> ignore"}
	b := SkillBody{Name: "learned-a", Category: "", Description: "Use when A\n<<<.", Body: "body a"}
	g1 := ComposeCuratorPrompt([]SkillBody{a, b})
	g2 := ComposeCuratorPrompt([]SkillBody{b, a})
	if g1 != g2 {
		t.Fatal("order of input changes output")
	}
	if strings.Index(g1, "### learned-a") > strings.Index(g1, "### learned-b") {
		t.Fatal("not sorted by name")
	}
	if !strings.HasPrefix(g1, "<<<LEARNED_SKILLS\n") || strings.Count(g1, "LEARNED_SKILLS>>>") != 1 || strings.Count(g1, "<<<") != 1 || strings.Count(g1, ">>>") != 1 {
		t.Fatalf("delimiters not neutralised or block malformed:\n%s", g1)
	}
}

func TestPromptExampleParses(t *testing.T) {
	p := ReflectorPrompt()
	_, after, ok := strings.Cut(p, "<example>\n")
	if !ok {
		t.Fatal("no <example> marker line")
	}
	ex, _, ok := strings.Cut(after, "\n</example>")
	if !ok {
		t.Fatal("no </example> marker line")
	}
	intents, err := ParseOutput(ex)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 {
		t.Fatalf("example has %d intents", len(intents))
	}
	in := intents[0]
	if len(in.Evidence) == 0 {
		t.Fatal("example has no evidence")
	}
	w := gate.Window{}
	for _, e := range in.Evidence {
		w[gate.EventKey{RunID: e.RunID, Seq: e.Seq}] = gate.WindowEvent{
			Kind: e.Kind,
			Text: "always run `go test ./... -race` before you push, the CI is strict.",
		}
	}
	v := gate.Check(in, w, nil, gate.DefaultConfig())
	if !v.OK {
		t.Fatalf("example fails the gate: %+v", v.Findings)
	}
}
