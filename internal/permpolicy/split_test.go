package permpolicy

import (
	"reflect"
	"strings"
	"testing"
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
			if len(p.Segments) != 1 {
				t.Fatalf("segments = %+v, want one", p.Segments)
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
		{"backtick command", "`cmd` x", "expansion", 1},
		{"subshell", "(cd x && rm y)", "subshell", 2},
		{"process substitution", "echo <(ls)", "process substitution", 1},
		{"dangling redirect", "echo hi >", "target", 1},
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
