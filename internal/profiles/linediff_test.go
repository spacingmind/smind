package profiles

import (
	"slices"
	"testing"
)

func TestLineDiff(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want []string
	}{
		{
			name: "identical text: every line kept",
			a:    "one\ntwo",
			b:    "one\ntwo",
			want: []string{" one", " two"},
		},
		{
			name: "one changed middle line, surrounding lines kept",
			a:    "one\nold\nthree",
			b:    "one\nnew\nthree",
			want: []string{" one", "-old", "+new", " three"},
		},
		{
			name: "added trailing line",
			a:    "one",
			b:    "one\ntwo",
			want: []string{" one", "+two"},
		},
		{
			name: "removed line",
			a:    "one\ntwo",
			b:    "one",
			want: []string{" one", "-two"},
		},
		{
			// strings.Split("", "\n") is one empty element, so the empty a's
			// line is removed first and every b line is added after it.
			name: "empty a",
			a:    "",
			b:    "x\ny",
			want: []string{"-", "+x", "+y"},
		},
	}
	for _, tt := range tests {
		if got := LineDiff(tt.a, tt.b); !slices.Equal(got, tt.want) {
			t.Errorf("%s: LineDiff(%q, %q) = %q, want %q", tt.name, tt.a, tt.b, got, tt.want)
		}
	}
	if got, want := UnifiedDiff("one\nold\nthree", "one\nnew\nthree"), " one\n-old\n+new\n three"; got != want {
		t.Errorf("UnifiedDiff = %q, want %q", got, want)
	}
}
