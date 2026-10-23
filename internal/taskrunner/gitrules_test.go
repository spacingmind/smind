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
		// read-only
		{"git status", GitClassReadOnly},
		{"git diff --stat", GitClassReadOnly},
		{"git log --oneline", GitClassReadOnly},
		{"git log --grep push", GitClassReadOnly}, // look-alike: read-only log, not a push
		{"git show HEAD", GitClassReadOnly},
		{"git fetch origin", GitClassReadOnly},
		{"git rev-parse HEAD", GitClassReadOnly},
		{"git ls-files", GitClassReadOnly},
		{"git branch", GitClassReadOnly},
		{"git branch -l", GitClassReadOnly},
		{"git branch --list", GitClassReadOnly},
		{"git remote -v", GitClassReadOnly},
		{"git remote", GitClassReadOnly},
		// commit
		{"git commit -m x", GitClassCommit},
		// always-ask
		{"git push -f", GitClassAlwaysAsk},
		{"git push --force-with-lease", GitClassAlwaysAsk},
		{"git push origin +main", GitClassAlwaysAsk},
		{"git -C dir push", GitClassAlwaysAsk},
		{"git --no-pager -c core.x=1 -C a push", GitClassAlwaysAsk},
		{"git --git-dir=.git --work-tree=. push", GitClassAlwaysAsk},
		{"gh pr create", GitClassAlwaysAsk},
		{"gh -R o/r pr create --fill", GitClassAlwaysAsk},
		{"gh pr merge 5", GitClassAlwaysAsk},
		{"git reset --hard", GitClassAlwaysAsk},
		{"git branch -D x", GitClassAlwaysAsk},
		{"git branch -d -f x", GitClassAlwaysAsk},
		{"git branch --delete --force x", GitClassAlwaysAsk},
		{"git clean -fd", GitClassAlwaysAsk},
		{"git clean -fdx", GitClassAlwaysAsk},
		{"git clean -df", GitClassAlwaysAsk},
		{"git checkout -- .", GitClassAlwaysAsk},
		{"git restore .", GitClassAlwaysAsk},
		{"git restore :/", GitClassAlwaysAsk},
		{"git restore", GitClassAlwaysAsk},
		{"git stash drop", GitClassAlwaysAsk},
		{"git stash clear", GitClassAlwaysAsk},
		{"git commit --amend", GitClassAlwaysAsk},
		// no rule
		{"git pushd", ""},
		{`grep -r "git push" .`, ""},
		{"gh pr view", ""},
		{"git reset --soft HEAD~1", ""},
		{"git branch newname", ""},
		{"git clean -n", ""},
		{"git restore file.go", ""},
		{"git checkout main", ""},
		{"git stash", ""},
		{"git frobnicate", ""},
		{"git", ""},
		{"gh", ""},
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

func TestClassifyGitPushTarget(t *testing.T) {
	cases := []struct {
		cmd, command, remote, branch string
	}{
		{"git push", "git push", "", ""},
		{"git push origin smind/task-x", "git push origin smind/task-x", "origin", "smind/task-x"},
		{"git push -u origin feat", "git push -u origin feat", "origin", "feat"},
		{"git push --force origin +main", "git push --force origin +main", "origin", "main"},
		{`git push origin "a b"`, "git push origin a b", "origin", "a b"},
		{"git -C dir push origin x; ls", "git -C dir push origin x", "origin", "x"},
	}
	for _, c := range cases {
		r := ClassifyGitCommand(c.cmd)
		if r == nil {
			t.Errorf("%q: no rule", c.cmd)
			continue
		}
		if r.Command != c.command || r.Remote != c.remote || r.Branch != c.branch {
			t.Errorf("%q: got command=%q remote=%q branch=%q, want %q %q %q",
				c.cmd, r.Command, r.Remote, r.Branch, c.command, c.remote, c.branch)
		}
		if r.Label != "git push always needs your approval" || r.Class != GitClassAlwaysAsk {
			t.Errorf("%q: label/class = %q/%s", c.cmd, r.Label, r.Class)
		}
	}
	if r := ClassifyGitCommand("gh pr create --fill"); r == nil || r.Label != "gh pr create always needs your approval" {
		t.Errorf("gh pr create label: %+v", r)
	}
	if r := ClassifyGitCommand("git commit -m x"); r == nil || r.Label != "git commit" {
		t.Errorf("git commit label: %+v", r)
	}
}

func TestClassifyGitCommandSeverity(t *testing.T) {
	for cmd, want := range map[string]GitRuleClass{
		"git status && git push":      GitClassAlwaysAsk,
		"git push && git status":      GitClassAlwaysAsk,
		"git status; git commit -m x": GitClassCommit,
		"git commit -m x; git status": GitClassCommit,
		"git commit -m x && git push": GitClassAlwaysAsk,
		"git status; git diff":        GitClassReadOnly,
	} {
		if r := ClassifyGitCommand(cmd); r == nil || r.Class != want {
			t.Errorf("%q: got %+v, want %s", cmd, r, want)
		}
	}
}
