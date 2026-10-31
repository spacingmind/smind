package permpolicy

import (
	"reflect"
	"testing"
)

func winArgv(t *testing.T, cmd string, seg int) []string {
	t.Helper()
	p := splitCommand(cmd, true)
	if seg >= len(p.Segments) {
		t.Fatalf("%q: want segment %d, got %d segments", cmd, seg, len(p.Segments))
	}
	return p.Segments[seg].Argv
}

func TestSplitWindows_BackslashLiteral(t *testing.T) {
	if got, want := winArgv(t, `go test D:\gotmp\x\y`, 0), []string{"go", "test", `D:\gotmp\x\y`}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
	cases := []struct{ cmd, want string }{
		{`rm C:\Users\bob\.ssh\id_rsa`, `C:\Users\bob\.ssh\id_rsa`},
		{`echo "C:\\x"`, `C:\x`},
		{`cat 'C:\a\b'`, `C:\a\b`},
	}
	for _, c := range cases {
		argv := winArgv(t, c.cmd, 0)
		if len(argv) < 2 || argv[1] != c.want {
			t.Errorf("%q: got %q want %q", c.cmd, argv, c.want)
		}
	}
}

func TestSplitWindows_MetacharKeepsEscape(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{`echo a\;b`, "a;b"},
		{`echo \"x`, `"x`},
		{`echo a\ b`, "a b"},
	}
	for _, c := range cases {
		argv := winArgv(t, c.cmd, 0)
		if len(argv) != 2 || argv[1] != c.want {
			t.Errorf("%q: got %q want [echo %q]", c.cmd, argv, c.want)
		}
	}
}

func TestSplitWindows_NoCommandHidingInQuote(t *testing.T) {
	cases := []struct{ cmd, second string }{
		{`echo \"; rm -rf /`, "rm"},
		{`echo x\y; git stash`, "git"},
	}
	for _, c := range cases {
		p := splitCommand(c.cmd, true)
		if len(p.Segments) != 2 || p.Segments[1].Argv[0] != c.second {
			t.Errorf("%q: got segments %+v, want 2 with second %q", c.cmd, p.Segments, c.second)
		}
	}
}

func TestSplitWindows_PosixModeUnchanged(t *testing.T) {
	if got := SplitCommand(`go test D:\gotmp\x`).Segments[0].Argv[2]; got != `D:gotmpx` {
		t.Errorf("got %q want D:gotmpx", got)
	}
}
