package permpolicy

import (
	"context"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// checkSegments compares the parsed segments field by field; a want Raw of ""
// skips the Raw comparison so cases can ignore source text.
func checkSegments(t *testing.T, p Parsed, want []Segment) {
	t.Helper()
	if len(p.Segments) != len(want) {
		t.Fatalf("got %d segments %+v, want %d", len(p.Segments), p.Segments, len(want))
	}
	for i, w := range want {
		got := p.Segments[i]
		if !reflect.DeepEqual(got.Argv, w.Argv) {
			t.Errorf("segment %d Argv = %q, want %q", i, got.Argv, w.Argv)
		}
		if !reflect.DeepEqual(got.Env, w.Env) {
			t.Errorf("segment %d Env = %q, want %q", i, got.Env, w.Env)
		}
		if !reflect.DeepEqual(got.Redirects, w.Redirects) {
			t.Errorf("segment %d Redirects = %+v, want %+v", i, got.Redirects, w.Redirects)
		}
		if got.Dynamic != w.Dynamic {
			t.Errorf("segment %d Dynamic = %v, want %v", i, got.Dynamic, w.Dynamic)
		}
		if got.PipeNext != w.PipeNext {
			t.Errorf("segment %d PipeNext = %v, want %v", i, got.PipeNext, w.PipeNext)
		}
		if w.Raw != "" && got.Raw != w.Raw {
			t.Errorf("segment %d Raw = %q, want %q", i, got.Raw, w.Raw)
		}
	}
}

func TestSplitCommand_Basics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cmd  string
		want []Segment
	}{
		{"simple", "ls -la /tmp", []Segment{{Argv: []string{"ls", "-la", "/tmp"}, Raw: "ls -la /tmp"}}},
		{"runs of spaces", "echo  hi   world", []Segment{{Argv: []string{"echo", "hi", "world"}}}},
		{"env assignments lead", "FOO=bar BAR=baz cmd arg", []Segment{
			{Argv: []string{"cmd", "arg"}, Env: []string{"FOO=bar", "BAR=baz"}},
		}},
		{"assignment after argv0 is an argument", "echo FOO=bar", []Segment{{Argv: []string{"echo", "FOO=bar"}}}},
		{"assignment only", "A=1", []Segment{{Env: []string{"A=1"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(tc.cmd)
			checkSegments(t, p, tc.want)
			if len(p.Unparsed) != 0 {
				t.Errorf("Unparsed = %q, want none", p.Unparsed)
			}
		})
	}
	for _, in := range []string{"", "   \t "} {
		p := SplitCommand(in)
		if len(p.Segments) != 0 || len(p.Unparsed) != 0 {
			t.Errorf("blank input %q: %+v", in, p)
		}
	}
	if p := SplitCommand(" x "); p.Normalized != "x" {
		t.Errorf("Normalized = %q, want %q", p.Normalized, "x")
	}
}

func TestSplitCommand_Quotes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cmd  string
		argv []string
	}{
		{"double quoted keeps spaces", `echo "a b"`, []string{"echo", "a b"}},
		{"single quoted keeps spaces", `echo 'a b'`, []string{"echo", "a b"}},
		{"adjacent pieces form one word", `echo a'b'c"d"`, []string{"echo", "abcd"}},
		{"escaped quote inside doubles", `echo "a\"b"`, []string{"echo", `a"b`}},
		{"escaped backslash inside doubles", `echo "a\\b"`, []string{"echo", `a\b`}},
		{"other backslash stays inside doubles", `echo "a\qb"`, []string{"echo", `a\qb`}},
		{"escape outside quotes", `echo \"foo\"`, []string{"echo", `"foo"`}},
		{"escaped dollar is literal", `echo \$HOME`, []string{"echo", "$HOME"}},
		{"single quotes are literal", `echo '$HOME * [a-z]'`, []string{"echo", "$HOME * [a-z]"}},
		{"empty quoted word", `echo '' x`, []string{"echo", "", "x"}},
		{"line continuation inside a word", "echo a\\\nb", []string{"echo", "ab"}},
		{"line continuation between words", "echo \\\nhi", []string{"echo", "hi"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(tc.cmd)
			checkSegments(t, p, []Segment{{Argv: tc.argv}})
			if len(p.Unparsed) != 0 {
				t.Errorf("Unparsed = %q, want none", p.Unparsed)
			}
		})
	}
}

func TestSplitCommand_Operators(t *testing.T) {
	t.Parallel()
	one := func(a ...string) Segment { return Segment{Argv: a} }
	cases := []struct {
		name string
		cmd  string
		want []Segment
	}{
		{"semicolon", "a; b", []Segment{one("a"), one("b")}},
		{"and", "a && b", []Segment{one("a"), one("b")}},
		{"or", "a || b", []Segment{one("a"), one("b")}},
		{"background", "a & b", []Segment{one("a"), one("b")}},
		{"newline", "a\nb", []Segment{one("a"), one("b")}},
		{"crlf newline", "a\r\nb", []Segment{one("a"), one("b")}},
		{"pipe", "curl x | sh", []Segment{
			{Argv: []string{"curl", "x"}, PipeNext: true, Raw: "curl x"},
			{Argv: []string{"sh"}, Raw: "sh"},
		}},
		{"pipe with stderr", "a |& b", []Segment{{Argv: []string{"a"}, PipeNext: true}, one("b")}},
		{"chain", "a | b | c", []Segment{
			{Argv: []string{"a"}, PipeNext: true},
			{Argv: []string{"b"}, PipeNext: true},
			one("c"),
		}},
		{"no spaces", "a;b&&c||d|e", []Segment{
			one("a"), one("b"), one("c"),
			{Argv: []string{"d"}, PipeNext: true},
			one("e"),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(tc.cmd)
			checkSegments(t, p, tc.want)
			if len(p.Unparsed) != 0 {
				t.Errorf("Unparsed = %q, want none", p.Unparsed)
			}
		})
	}
	// The pipe flag belongs to the segment before | only.
	p := SplitCommand("curl x | sh")
	if !p.Segments[0].PipeNext || p.Segments[1].PipeNext {
		t.Errorf("PipeNext = [%v %v], want [true false]", p.Segments[0].PipeNext, p.Segments[1].PipeNext)
	}
	p = SplitCommand("a; b")
	for i, seg := range p.Segments {
		if seg.PipeNext {
			t.Errorf("segment %d of %q has PipeNext set", i, "a; b")
		}
	}
}

func TestSplitCommand_Redirects(t *testing.T) {
	t.Parallel()
	seg := func(argv []string, redirs ...Redirect) Segment {
		return Segment{Argv: argv, Redirects: redirs}
	}
	cases := []struct {
		name string
		cmd  string
		want []Segment
	}{
		{"output", "echo hi > f.txt", []Segment{seg([]string{"echo", "hi"}, Redirect{Op: ">", Target: "f.txt"})}},
		{"append", "echo hi >> f", []Segment{seg([]string{"echo", "hi"}, Redirect{Op: ">>", Target: "f"})}},
		{"input", "sort < in", []Segment{seg([]string{"sort"}, Redirect{Op: "<", Target: "in"})}},
		{"stderr", "cmd 2> err", []Segment{seg([]string{"cmd"}, Redirect{Op: "2>", Target: "err"})}},
		{"stderr append", "cmd 2>> err", []Segment{seg([]string{"cmd"}, Redirect{Op: "2>>", Target: "err"})}},
		{"stderr to stdout inline", "cmd 2>&1", []Segment{seg([]string{"cmd"}, Redirect{Op: "2>&1"})}},
		{"both streams", "cmd &> all", []Segment{seg([]string{"cmd"}, Redirect{Op: "&>", Target: "all"})}},
		{"here string", `grep x <<< "hi there"`, []Segment{seg([]string{"grep", "x"}, Redirect{Op: "<<<", Target: "hi there"})}},
		{"two redirects", "cmd > out 2> err", []Segment{seg([]string{"cmd"},
			Redirect{Op: ">", Target: "out"}, Redirect{Op: "2>", Target: "err"})}},
		{"fd attached to the operator", "cmd 2>err", []Segment{seg([]string{"cmd"}, Redirect{Op: "2>", Target: "err"})}},
		{"quoted target", "echo x > 'my file'", []Segment{seg([]string{"echo", "x"}, Redirect{Op: ">", Target: "my file"})}},
		{"digits stay an argument when spaced", "cmd 2 > err", []Segment{seg([]string{"cmd", "2"}, Redirect{Op: ">", Target: "err"})}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(tc.cmd)
			checkSegments(t, p, tc.want)
			if len(p.Unparsed) != 0 {
				t.Errorf("Unparsed = %q, want none", p.Unparsed)
			}
		})
	}
	// A redirect target that expands marks the segment dynamic.
	p := SplitCommand("cmd > $out")
	if len(p.Segments) != 1 || !p.Segments[0].Dynamic || p.Segments[0].Redirects[0].Target != "$out" {
		t.Errorf("dynamic redirect target: %+v", p)
	}
}

func TestSplitCommand_Dynamic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		cmd     string
		dynamic bool
	}{
		{"dollar", "echo $HOME", true},
		{"braced", "echo ${X}", true},
		{"dollar in doubles", `echo "$X"`, true},
		{"backtick", "echo `id`", true},
		{"glob star", "echo *.go", true},
		{"glob question", "echo file?", true},
		{"glob bracket", "echo [a-z]*.md", true},
		{"dollar in singles is literal", "echo '$X'", false},
		{"glob in doubles is literal", `echo "a*b"`, false},
		{"escaped dollar is literal", `echo \$X`, false},
		{"plain", "echo hello", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(tc.cmd)
			if len(p.Segments) == 0 {
				t.Fatalf("segments = %+v, want at least one", p.Segments)
			}
			if p.Segments[0].Dynamic != tc.dynamic {
				t.Errorf("Dynamic = %v, want %v", p.Segments[0].Dynamic, tc.dynamic)
			}
			if len(p.Unparsed) != 0 {
				t.Errorf("Unparsed = %q, want none", p.Unparsed)
			}
		})
	}
	// An expanding assignment value is still a word of the segment.
	p := SplitCommand("HOME=$X cmd")
	checkSegments(t, p, []Segment{{Argv: []string{"cmd"}, Env: []string{"HOME=$X"}, Dynamic: true}})
}

func TestSplitCommand_UnparsedTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		cmd       string
		reasonSub string
		wantSegs  int
	}{
		{"unbalanced double quote", `echo "unclosed`, "quote", 1},
		{"unbalanced single quote", "echo 'unclosed", "quote", 1},
		{"eval", "eval x", "eval", 1},
		{"dollar command", "$CMD arg", "expansion", 1},
		{"backtick command", "`cmd` x", "expansion", 2},
		{"process substitution", "echo <(ls)", "process substitution", 1},
		{"dangling redirect", "echo hi >", "target", 1},
		{"shell -c non-literal", `sh -c "$x"`, "shell -c", 1},
		{"shell -c without a script", "bash -c", "shell -c", 1},
		{"unclosed command substitution", "echo $(id", "unclosed", 1},
		{"unclosed subshell", "(a; b", "unclosed", 2},
		{"nesting too deep", strings.Repeat("$(", 10) + "x" + strings.Repeat(")", 10), "nesting", 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(tc.cmd)
			if len(p.Unparsed) == 0 || !strings.Contains(strings.Join(p.Unparsed, "; "), tc.reasonSub) {
				t.Errorf("Unparsed = %q, want a reason containing %q", p.Unparsed, tc.reasonSub)
			}
			if len(p.Segments) != tc.wantSegs {
				t.Errorf("got %d segments %+v, want %d (best effort)", len(p.Segments), p.Segments, tc.wantSegs)
			}
		})
	}
	// Oversize input is refused outright: no segments, one reason.
	p := SplitCommand(strings.Repeat("echo a ", 12*1024))
	if len(p.Segments) != 0 || len(p.Unparsed) != 1 || !strings.Contains(p.Unparsed[0], "64 KiB") {
		t.Errorf("oversize: %d segments, Unparsed %q", len(p.Segments), p.Unparsed)
	}
}

func TestSplitCommand_Heredoc(t *testing.T) {
	t.Parallel()
	// The whole point: a body full of command-looking text stays a body.
	body := "fix: thing\n$(rm -rf /)\n`git push --force`\n"
	p := SplitCommand("git commit -F - <<'EOF'\n" + body + "EOF")
	want := "git commit -F - <<EOF [heredoc " + strconv.Itoa(len(body)) + " bytes]"
	checkSegments(t, p, []Segment{{
		Argv:      []string{"git", "commit", "-F", "-"},
		Redirects: []Redirect{{Op: "<<", Target: "EOF"}},
		Raw:       want,
	}})
	if p.HeredocBytes != len(body) {
		t.Errorf("HeredocBytes = %d, want %d", p.HeredocBytes, len(body))
	}
	if p.Normalized != want {
		t.Errorf("Normalized = %q, want %q", p.Normalized, want)
	}
	for _, leak := range []string{"rm -rf", "git push --force", "fix: thing"} {
		if strings.Contains(p.Normalized, leak) {
			t.Errorf("Normalized leaks body text %q", leak)
		}
	}
	if len(p.Unparsed) != 0 {
		t.Errorf("Unparsed = %q, want none", p.Unparsed)
	}

	cases := []struct {
		name       string
		cmd        string
		want       []Segment
		bytes      int
		normSubstr string
	}{
		{
			"tabs stripped for <<-", "cat <<-EOF\n\tindented\n\tEOF\necho done",
			[]Segment{
				{Argv: []string{"cat"}, Redirects: []Redirect{{Op: "<<", Target: "EOF"}}},
				{Argv: []string{"echo", "done"}},
			},
			len("\tindented\n"), "cat <<EOF [heredoc 10 bytes]",
		},
		{
			"two heredocs on one line", "cat <<A <<B\nbodyA\nA\nbodyB\nB",
			[]Segment{{Argv: []string{"cat"}, Redirects: []Redirect{
				{Op: "<<", Target: "A"}, {Op: "<<", Target: "B"},
			}}},
			len("bodyA\n") + len("bodyB\n"), "[heredoc 6 bytes]",
		},
		{
			"heredoc then pipe on the same line", "cat <<EOF | sh\nbody\nEOF",
			[]Segment{
				{Argv: []string{"cat"}, Redirects: []Redirect{{Op: "<<", Target: "EOF"}}, PipeNext: true, Raw: "cat <<EOF [heredoc 5 bytes]"},
				{Argv: []string{"sh"}, Raw: "sh"},
			},
			len("body\n"), "cat <<EOF [heredoc 5 bytes] | sh",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(tc.cmd)
			checkSegments(t, p, tc.want)
			if p.HeredocBytes != tc.bytes {
				t.Errorf("HeredocBytes = %d, want %d", p.HeredocBytes, tc.bytes)
			}
			if !strings.Contains(p.Normalized, tc.normSubstr) {
				t.Errorf("Normalized = %q, want it to contain %q", p.Normalized, tc.normSubstr)
			}
			if strings.Contains(p.Normalized, "body") && !strings.Contains(tc.normSubstr, "body") {
				t.Errorf("Normalized leaks body text: %q", p.Normalized)
			}
			if len(p.Unparsed) != 0 {
				t.Errorf("Unparsed = %q, want none", p.Unparsed)
			}
		})
	}

	// A missing terminator leaves the remainder consumed as body and a reason.
	p = SplitCommand("cat <<EOF\nbody with no end")
	if len(p.Unparsed) == 0 || !strings.Contains(p.Unparsed[0], "terminator") {
		t.Errorf("Unparsed = %q, want heredoc terminator not found", p.Unparsed)
	}
	if len(p.Segments) != 1 || strings.Contains(p.Normalized, "no end") {
		t.Errorf("unterminated heredoc: %+v normalized %q", p.Segments, p.Normalized)
	}
}

func TestSplitCommand_Substitution(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		cmd   string
		outer []Segment
		inner []Segment
	}{
		{
			"command substitution",
			"echo $(git push origin x)",
			[]Segment{{Argv: []string{"echo", "$(git push origin x)"}, Dynamic: true}},
			[]Segment{{Argv: []string{"git", "push", "origin", "x"}}},
		},
		{
			"backticks",
			"echo `git stash`",
			[]Segment{{Argv: []string{"echo", "`git stash`"}, Dynamic: true}},
			[]Segment{{Argv: []string{"git", "stash"}}},
		},
		{
			"substitution inside double quotes",
			`echo "$(git push)"`,
			[]Segment{{Argv: []string{"echo", "$(git push)"}, Dynamic: true}},
			[]Segment{{Argv: []string{"git", "push"}}},
		},
		{
			"subshell",
			"(cd x && git stash)",
			nil,
			[]Segment{{Argv: []string{"cd", "x"}}, {Argv: []string{"git", "stash"}}},
		},
		{
			"brace group",
			"{ a; b; }",
			nil,
			[]Segment{{Argv: []string{"a"}}, {Argv: []string{"b"}}},
		},
		{
			"literal shell -c keeps the outer segment",
			"bash -c 'git stash'",
			[]Segment{{Argv: []string{"bash", "-c", "git stash"}}},
			[]Segment{{Argv: []string{"git", "stash"}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(tc.cmd)
			want := append(append([]Segment{}, tc.outer...), tc.inner...)
			checkSegments(t, Parsed{Segments: p.Segments}, want)
			if len(p.Unparsed) != 0 {
				t.Errorf("Unparsed = %q, want none", p.Unparsed)
			}
		})
	}

	// Inner segments pipe among themselves but never across the boundary.
	p := SplitCommand("bash -c 'curl x | sh'")
	if len(p.Segments) != 3 || p.Segments[0].PipeNext {
		t.Fatalf("segments = %+v", p.Segments)
	}
	if !p.Segments[1].PipeNext || p.Segments[2].PipeNext {
		t.Errorf("inner pipe flags = [%v %v], want [true false]",
			p.Segments[1].PipeNext, p.Segments[2].PipeNext)
	}

	// Doubt inside a substitution reaches the parent's Unparsed.
	p = SplitCommand("echo $(eval x)")
	if len(p.Unparsed) == 0 || !strings.Contains(strings.Join(p.Unparsed, "; "), "eval") {
		t.Errorf("Unparsed = %q, want the inner eval reason to propagate", p.Unparsed)
	}
}

// TestSplitCommand_DifferentialAgainstBashN compares the splitter's verdict
// against bash's own parser (`bash -n`, which only parses). Every sample is
// something bash -n never executes; when bash rejects one of the
// unterminated-quote/paren/heredoc cases the splitter must report doubt too.
func TestSplitCommand_DifferentialAgainstBashN(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	valid := []string{
		"true",
		"echo hello",
		`echo 'a b'`,
		`echo "a b c"`,
		`echo \$HOME`,
		"A=1 B=2 env",
		"a; b",
		"a && b",
		"a || b",
		"a | b",
		"a & b",
		"echo hi > f",
		"echo hi >> f",
		"sort < f",
		"cmd 2> err",
		"cmd 2>&1",
		"cmd &> all",
		`grep x <<< "hi"`,
		"a\nb",
		"cat <<EOF\nbody\nEOF",
		"echo $(id)",
		"(a; b)",
		"{ a; b; }",
		"bash -c 'true'",
	}
	for _, cmd := range valid {
		if !bashAccepts(t, bash, cmd) {
			t.Errorf("sample %q should be valid for bash -n", cmd)
			continue
		}
		SplitCommand(cmd) // must not panic
	}
	invalid := []struct {
		cmd        string
		assertable bool // unterminated quote/paren/heredoc: splitter must doubt
	}{
		{`echo "unclosed`, true},
		{"echo 'unclosed", true},
		{"echo $(id", true},
		{"(a; b", true},
		{"echo `id", true},
		{"((a", true},
		{"echo (a b)", false},
		{"if a; then", false},
	}
	for _, tc := range invalid {
		if bashAccepts(t, bash, tc.cmd) {
			t.Errorf("sample %q should be invalid for bash -n", tc.cmd)
			continue
		}
		p := SplitCommand(tc.cmd)
		if tc.assertable && len(p.Unparsed) == 0 {
			t.Errorf("bash rejects %q but SplitCommand reports no doubt: %+v", tc.cmd, p)
		}
	}
}

// bashAccepts parses cmd with `bash -n -c` (no stdin, short timeout).
func bashAccepts(t *testing.T, bash, cmd string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, bash, "-n", "-c", cmd)
	return c.Run() == nil
}

func TestSplitCommand_BraceExpansionIsDynamic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		cmd     string
		dynamic bool
	}{
		{"builds a flag", "git push --forc{e,} origin x", true},
		{"quoted braces are literal", `echo "{a,b}"`, false},
		{"single-quoted braces are literal", `echo '{a,b}'`, false},
		{"env assignment unaffected", "a=1 b", false},
		{"find placeholder is inert", "find . -exec rm {} ;", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(tc.cmd)
			if len(p.Segments) == 0 {
				t.Fatalf("segments = %+v, want at least one", p.Segments)
			}
			if p.Segments[0].Dynamic != tc.dynamic {
				t.Errorf("Dynamic = %v, want %v", p.Segments[0].Dynamic, tc.dynamic)
			}
		})
	}
}

func TestSplitCommand_WrappedEvalIsUnparsed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		cmd      string
		unparsed bool
	}{
		{"command eval", "command eval x", true},
		{"nohup source", "nohup source f", true},
		{"env dot", "env A=1 . ./f", true},
		{"bare source", "source f", true},
		{"bare dot", ". ./f", true},
		{"echo eval", "echo eval", false},
		{"wrapper only", "command ls eval", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := SplitCommand(tc.cmd)
			if got := len(p.Unparsed) > 0; got != tc.unparsed {
				t.Errorf("Unparsed = %q, want unparsed=%v", p.Unparsed, tc.unparsed)
			}
		})
	}
}

func TestSplitCommand_ShellFlagClusterC(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"-c", "-lc", "-xc", "-ec", "-ic"} {
		p := SplitCommand("bash " + flag + " 'git stash'")
		if len(p.Segments) != 2 || !reflect.DeepEqual(p.Segments[0].Argv, []string{"bash", flag, "git stash"}) ||
			!reflect.DeepEqual(p.Segments[1].Argv, []string{"git", "stash"}) {
			t.Errorf("%s: segments = %+v, want bash then the inner git stash", flag, p.Segments)
		}
	}
	p := SplitCommand(`sh -xc "$x"`)
	if len(p.Unparsed) == 0 {
		t.Errorf("sh -xc \"$x\": Unparsed empty, want non-literal script reported")
	}
}
