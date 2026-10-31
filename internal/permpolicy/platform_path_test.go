package permpolicy

import (
	"errors"
	"testing"
)

func winc(t *testing.T, in string) string {
	t.Helper()
	pl := windowsPlatform(nil, nil)
	n, err := pl.normalize(in)
	if err != nil {
		t.Fatalf("normalize(%q): %v", in, err)
	}
	return pl.clean(n)
}

func TestPlatform_WindowsClean(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`C:\a\..\b`, `C:\b`},
		{`c:/Users//bob/./x/`, `C:\Users\bob\x`},
		{`C:\..\x`, `C:\x`},
		{`\\srv\share\a\..\..\b`, `\\srv\share\b`},
		{`C:\`, `C:\`},
		{`C:\a/b\c`, `C:\a\b\c`},
		{`\\?\C:\x\y`, `C:\x\y`},
		{`\\?\UNC\srv\share\x`, `\\srv\share\x`},
		{`/c/Users/bob`, `C:\Users\bob`},
		{`/cygdrive/d/x`, `D:\x`},
		{`a\b\..\c`, `a\c`},
	} {
		if got := winc(t, c.in); got != c.want {
			t.Errorf("clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPlatform_WindowsIsAbs(t *testing.T) {
	pl := windowsPlatform(nil, nil)
	for _, p := range []string{`C:\x`, `c:/x`, `\\srv\share`, `\\srv\share\x`, `/c/x`} {
		n, err := pl.normalize(p)
		if err != nil || !pl.isAbs(n) {
			t.Errorf("isAbs(%q) = false (norm %q, err %v), want true", p, n, err)
		}
	}
	for _, p := range []string{`x\y`, `.\x`, `..\x`} {
		n, err := pl.normalize(p)
		if err != nil || pl.isAbs(n) {
			t.Errorf("isAbs(%q) = true (norm %q, err %v), want false", p, n, err)
		}
	}
}

func TestPlatform_WindowsUnrepresentable(t *testing.T) {
	pl := windowsPlatform(nil, nil)
	for _, p := range []string{
		`C:foo`, `C:`, `\rooted`, `\\.\COM1`, `\\srv`, `\\srv\`, `C:\a:stream`, `C:\a*b`, `C:\a?`, `C:\a|b`,
		`C:\x.`, `C:\x `, `C:\dir\NUL`, `C:\dir\nul.txt`, `C:\CON`, `/etc/x`,
	} {
		if _, err := pl.normalize(p); !errors.Is(err, errUnrepresentable) {
			t.Errorf("normalize(%q) err = %v, want errUnrepresentable", p, err)
		}
	}
	for _, p := range []string{`C:\x`, `x\y`, `..\x`, `C:\a..b`, `C:\.hidden`} {
		if _, err := pl.normalize(p); err != nil {
			t.Errorf("normalize(%q) err = %v, want nil", p, err)
		}
	}
}

func TestPlatform_WindowsUnder(t *testing.T) {
	pl := windowsPlatform(nil, nil)
	for _, c := range []struct {
		p, root string
		want    bool
	}{
		{`C:\Work\Wt\pkg`, `C:\work\wt`, true},
		{`C:\Work\Wtx`, `C:\work\wt`, false},
		{`D:\Work\Wt`, `C:\work\wt`, false},
		{`C:\Work\Wt`, `C:\Work\Wt`, true},
		{`\\srv\share\a`, `\\SRV\Share`, true},
		{`C:\x`, `C:\`, true},
		{`C:\work\wt\..\other`, `C:\work\wt`, false},
	} {
		p, err := pl.normalize(c.p)
		if err != nil {
			t.Fatal(err)
		}
		root, err := pl.normalize(c.root)
		if err != nil {
			t.Fatal(err)
		}
		if got := pl.under(p, root); got != c.want {
			t.Errorf("under(%q, %q) = %v, want %v", c.p, c.root, got, c.want)
		}
	}
}

func TestPlatform_WindowsToNative(t *testing.T) {
	pl := windowsPlatform(nil, nil)
	for _, c := range []struct{ in, want string }{
		{`/c/Users/bob`, `C:\Users\bob`},
		{`/cygdrive/d/x`, `D:\x`},
		{`\\?\C:\x\y`, `C:\x\y`},
		{`\\?\UNC\srv\share\x`, `\\srv\share\x`},
		{`c:\Windows`, `C:\Windows`},
	} {
		got, err := pl.normalize(c.in)
		if err != nil || got != c.want {
			t.Errorf("normalize(%q) = %q, %v, want %q", c.in, got, err, c.want)
		}
	}
}

func TestPlatform_PosixUnchanged(t *testing.T) {
	pl := posixPlatform(nil)
	if got := pl.clean("/a/../b/"); got != "/b" {
		t.Errorf("clean = %q", got)
	}
	if got := pl.join("/wt", "pkg", "../x"); got != "/wt/x" {
		t.Errorf("join = %q", got)
	}
	if pl.under("/a/bc", "/a/b") {
		t.Error("under(/a/bc, /a/b) = true")
	}
	if !pl.under("/a/b/c", "/a/b") {
		t.Error("under(/a/b/c, /a/b) = false")
	}
	if got := pl.cmdName("/usr/bin/git"); got != "git" {
		t.Errorf("cmdName = %q", got)
	}
	if got := pl.cmdName("git.exe"); got != "git.exe" {
		t.Errorf("cmdName = %q", got)
	}
	if got, err := pl.normalize(`C:\x`); got != `C:\x` || err != nil {
		t.Errorf("normalize = %q, %v", got, err)
	}
	if !pl.isNullDevice("/dev/null") || pl.isNullDevice("nul") {
		t.Error("isNullDevice wrong")
	}
}

func TestPlatform_WindowsCmdName(t *testing.T) {
	pl := windowsPlatform(nil, nil)
	for in, want := range map[string]string{
		`C:\Program Files\Git\cmd\git.exe`: "git",
		`GIT.EXE`:                          "git",
		`Remove-Item`:                      "remove-item",
		`/usr/bin/git`:                     "git",
		`x.cmd`:                            "x",
	} {
		if got := pl.cmdName(in); got != want {
			t.Errorf("cmdName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlatform_WindowsNullDevice(t *testing.T) {
	pl := windowsPlatform(nil, nil)
	for _, w := range []string{"NUL", "nul", "/dev/null"} {
		if !pl.isNullDevice(w) {
			t.Errorf("isNullDevice(%q) = false", w)
		}
	}
	if pl.isNullDevice("nulx") {
		t.Error("isNullDevice(nulx) = true")
	}
}
