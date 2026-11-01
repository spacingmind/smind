package permpolicy

import (
	"strings"
	"testing"
)

const secretTok = "SECRET_TOKEN_123"

// nestedHeredocCases are commands whose heredoc body sits inside a nested
// scan (subshell, substitution, group, compound command, quoted script).
// Normalized feeds the approval card and the audit log, so no body may reach
// it, and neither may a segment's Raw.
func nestedHeredocCases() map[string]string {
	b := "token=" + secretTok + "\n"
	return map[string]string{
		"subshell":          "(cat <<EOF > f\n" + b + "EOF\n) && ls",
		"subshell one line": "(cat <<EOF\n" + b + "EOF\n)",
		"command subst":     "echo $(cat <<EOF\n" + b + "EOF\n) done",
		"backticks":         "echo `cat <<EOF\n" + b + "EOF\n` done",
		"brace group":       "{ cat <<EOF > f\n" + b + "EOF\n; }",
		"two levels":        "( echo $(cat <<EOF\n" + b + "EOF\n) )",
		"three levels":      "( ( { cat <<EOF\n" + b + "EOF\n; } ) )",
		"if body":           "if true; then cat <<EOF > f\n" + b + "EOF\nfi",
		"while body":        "while read x; do cat <<EOF > f\n" + b + "EOF\ndone",
		"bash -c":           "bash -c 'cat <<EOF\n" + b + "EOF\n'",
		"bash -c double":    "bash -c \"cat <<EOF\n" + b + "EOF\n\"",
		"bash -c in sub":    "(bash -c 'cat <<EOF\n" + b + "EOF\n')",
		"sh -c quoted tag":  "sh -c 'cat <<\"EOF\" > f\n" + b + "EOF\n'",
		"plain":             "cat <<EOF > f\n" + b + "EOF\n",
	}
}

func TestSplitCommand_NestedHeredocBodyNeverInNormalized(t *testing.T) {
	t.Parallel()
	for name, src := range nestedHeredocCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(src)
			if strings.Contains(p.Normalized, secretTok) {
				t.Errorf("Normalized leaks the body: %q", p.Normalized)
			}
			for _, sg := range p.Segments {
				if strings.Contains(sg.Raw, secretTok) {
					t.Errorf("segment Raw leaks the body: %q", sg.Raw)
				}
			}
		})
	}
}

// A here-string is not a heredoc: its word is an ordinary argument and stays
// visible, because the user is approving exactly that text.
func TestSplitCommand_HereStringStaysVisible(t *testing.T) {
	t.Parallel()
	p := SplitCommand(`cat <<< "` + secretTok + `"`)
	if !strings.Contains(p.Normalized, secretTok) {
		t.Errorf("Normalized = %q, want the here-string word visible", p.Normalized)
	}
}
