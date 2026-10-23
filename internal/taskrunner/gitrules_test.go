package taskrunner

import (
	"reflect"
	"testing"
)

func TestScanShell(t *testing.T) {
	cases := []struct {
		in   string
		want [][]string
		subs []string
	}{
		{"git push", [][]string{{"git", "push"}}, nil},
		{"a && b || c; d | e\nf & g", [][]string{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}, {"f"}, {"g"}}, nil},
		{`echo "a b" 'c d' e\ f`, [][]string{{"echo", "a b", "c d", "e f"}}, nil},
		{"(git push)", [][]string{{"git", "push"}}, nil},
		{"echo $(git push)", [][]string{{"echo", "$(git push)"}}, []string{"git push"}},
		{"echo `git push`", [][]string{{"echo", "`git push`"}}, []string{"git push"}},
		{`echo "$(a "b c")"`, [][]string{{"echo", `$(a "b c")`}}, []string{`a "b c"`}},
		{"ls # git push", [][]string{{"ls"}}, nil},
	}
	for _, c := range cases {
		cmds, subs, err := scanShell(c.in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		var got [][]string
		for _, w := range cmds {
			got = append(got, wordTexts(w))
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: cmds = %q, want %q", c.in, got, c.want)
		}
		if !reflect.DeepEqual(subs, c.subs) {
			t.Errorf("%q: subs = %q, want %q", c.in, subs, c.subs)
		}
	}
	for _, bad := range []string{"echo 'x", `echo "x`, "(a", "a)", "echo $(a", "echo `a"} {
		if _, _, err := scanShell(bad); err == nil {
			t.Errorf("%q: expected parse error", bad)
		}
	}
}

func TestClassifyGitCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want GitRuleClass // "" = no rule
	}{
		{"git push", GitClassAlwaysAsk},
		{"git push origin smind/task-x", GitClassAlwaysAsk},
		{"cd x && git push", GitClassAlwaysAsk},
		{"git push;ls", GitClassAlwaysAsk},
		{"(git push)", GitClassAlwaysAsk},
		{"$(git push)", GitClassAlwaysAsk},
		{"`git push`", GitClassAlwaysAsk},
		{"true | git push", GitClassAlwaysAsk},
		{"echo git push", ""},
		{"ls", ""},
	}
	for _, c := range cases {
		got := ClassifyGitCommand(c.cmd)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%q: got %+v, want no rule", c.cmd, got)
		case c.want != "" && (got == nil || got.Class != c.want):
			t.Errorf("%q: got %+v, want class %s", c.cmd, got, c.want)
		}
	}
}
