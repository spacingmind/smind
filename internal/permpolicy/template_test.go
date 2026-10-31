package permpolicy

import (
	"strings"
	"testing"
)

func TestTemplate_LoadsWithZeroAllowRules(t *testing.T) {
	p, err := Load([]byte(Template))
	if err != nil {
		t.Fatalf("Load(Template): %v", err)
	}
	if len(p.Rules) != 0 {
		t.Fatalf("Rules = %d, want 0", len(p.Rules))
	}
	for _, line := range strings.Split(Template, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "action: allow") {
			t.Errorf("active allow line: %q", line)
		}
	}
}

func TestTemplate_ExamplesAreValidWhenUncommented(t *testing.T) {
	var b strings.Builder
	b.WriteString("version: 1\nrules:\n")
	for _, line := range strings.Split(Template, "\n") {
		if rest, ok := strings.CutPrefix(line, "#> "); ok {
			b.WriteString(rest + "\n")
		}
	}
	p, err := Load([]byte(b.String()))
	if err != nil {
		t.Fatalf("Load(uncommented examples): %v", err)
	}
	allows := 0
	for _, r := range p.Rules {
		if r.Action == ActionAllow {
			allows++
		}
	}
	if len(p.Rules) < 7 || allows < 5 {
		t.Fatalf("rules = %d (allow %d), want >= 7 and >= 5", len(p.Rules), allows)
	}
}
