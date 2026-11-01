package permpolicy

import (
	"strconv"
	"strings"
	"testing"
)

// The approval card shows Normalized, so eliding a heredoc must never glue the
// operator's target onto whatever follows the body.
func TestSplitCommand_HeredocNormalizedKeepsSeparators(t *testing.T) {
	t.Parallel()
	body := "line one\nline two\n"
	n := " [heredoc " + strconv.Itoa(len(body)) + " bytes]"
	cases := []struct {
		name, src, want string
	}{
		{"and", "cat <<'EOF' > notes/r.md\n" + body + "EOF\n&& git status && npm test -- --run x", "cat <<EOF" + n + " > notes/r.md\n&& git status && npm test -- --run x"},
		{"or", "cat <<EOF > f\n" + body + "EOF\n|| true", "cat <<EOF" + n + " > f\n|| true"},
		{"semi", "cat <<EOF > f\n" + body + "EOF\n; ls", "cat <<EOF" + n + " > f\n; ls"},
		{"pipe", "cat <<EOF > f\n" + body + "EOF\n| wc -l", "cat <<EOF" + n + " > f\n| wc -l"},
		{"bg", "cat <<EOF > f\n" + body + "EOF\n& ls", "cat <<EOF" + n + " > f\n& ls"},
		{"next command", "cat <<EOF > f\n" + body + "EOF\nls -la", "cat <<EOF" + n + " > f\nls -la"},
		{"redirect after", "cat <<EOF\n" + body + "EOF\n> out.txt", "cat <<EOF" + n + "\n> out.txt"},
		{"same line pipe", "cat <<EOF | tr a-z A-Z\n" + body + "EOF\nls", "cat <<EOF" + n + " | tr a-z A-Z\nls"},
		{"double quoted tag", "cat <<\"EOF\" > f\n" + body + "EOF\n&& ls", "cat <<EOF" + n + " > f\n&& ls"},
		{"dash tag", "cat <<-EOF > f\n\t" + body + "\tEOF\n&& ls", "cat <<EOF [heredoc " + strconv.Itoa(len(body)+1) + " bytes] > f\n&& ls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SplitCommand(tc.src).Normalized
			if got != tc.want {
				t.Errorf("Normalized = %q, want %q", got, tc.want)
			}
			assertNoGlue(t, got)
		})
	}
}

func TestSplitCommand_HeredocNormalizedTwoOnOneLine(t *testing.T) {
	t.Parallel()
	got := SplitCommand("cat <<A <<B > f\na1\nA\nb1\nB\n&& ls").Normalized
	assertNoGlue(t, got)
	if !strings.HasSuffix(got, "\n&& ls") {
		t.Errorf("Normalized = %q, want a newline before &&", got)
	}
}

// assertNoGlue fails when an operator directly follows a non-space character
// that the elision could have produced (a file name or a size marker).
func assertNoGlue(t *testing.T, s string) {
	t.Helper()
	for _, op := range []string{"&&", "||", ";", "|", "&"} {
		for i := strings.Index(s, op); i > 0; {
			if c := s[i-1]; c != ' ' && c != '\n' && c != '|' && c != '&' && c != '(' {
				t.Errorf("operator %q glued to %q in %q", op, string(c), s)
			}
			j := strings.Index(s[i+1:], op)
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
}
