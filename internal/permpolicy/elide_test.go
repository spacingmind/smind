package permpolicy

import (
	"strings"
	"testing"
)

func TestElideHeredocs(t *testing.T) {
	t.Parallel()
	a, b := "token=SECRET_TOKEN_123\n", "second body\n"
	cases := []struct {
		name, cmd string
		bodies    []string
	}{
		{"none", "ls -la", nil},
		{"plain", "cat <<EOF > f\n" + a + "EOF\n&& ls", []string{a}},
		{"subshell", "(cat <<EOF\n" + a + "EOF\n)", []string{a}},
		{"nested and top level", "cat <<A\n" + b + "A\n; ( cat <<B\n" + a + "B\n)", []string{b, a}},
		{"two on one line", "cat <<A <<B\n" + a + "A\n" + b + "B\n", []string{a, b}},
		{"bash -c", "bash -c 'cat <<EOF\n" + a + "EOF\n'", []string{a}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			shown, bodies := ElideHeredocs(tc.cmd)
			if strings.Contains(shown, "SECRET_TOKEN_123") || strings.Contains(shown, "second body") {
				t.Errorf("shown leaks a body: %q", shown)
			}
			if len(bodies) != len(tc.bodies) {
				t.Fatalf("bodies = %d, want %d (shown %q)", len(bodies), len(tc.bodies), shown)
			}
			for i, want := range tc.bodies {
				if bodies[i].Text != want || bodies[i].Bytes != len(want) {
					t.Errorf("body %d = %+v, want %q", i, bodies[i], want)
				}
			}
		})
	}
}

func TestElideHeredocs_OversizeAndRedactedHaveNoBodies(t *testing.T) {
	t.Parallel()
	shown, bodies := ElideHeredocs(strings.Repeat("a", maxCommandBytes+1) + " <<EOF\nSECRET_TOKEN_123\nEOF\n")
	if len(bodies) != 0 || strings.Contains(shown, "SECRET_TOKEN_123") {
		t.Errorf("oversize: shown %q bodies %d", shown[:40], len(bodies))
	}
	shown, bodies = ElideHeredocs("cat <<\xff\nSECRET_TOKEN_123\n\xff\n")
	if len(bodies) != 0 || strings.Contains(shown, "SECRET_TOKEN_123") {
		t.Errorf("bad tag: shown %q bodies %d", shown, len(bodies))
	}
}
