package permpolicy

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveWindows_DriveCaseUNCMixedSeparators(t *testing.T) {
	pl := newWin(map[string]fakeEntry{
		`C:\Work\Wt`:     {kind: "dir"},
		`C:\Work\Wt\pkg`: {kind: "dir"},
		`\\srv\share\a`:  {kind: "dir"},
	}, nil)
	cases := []struct{ p, cwd, want string }{
		{`c:/work/WT\pkg/../x`, "", `C:\Work\Wt\x`},
		{`pkg\y`, `C:\Work\Wt`, `C:\Work\Wt\pkg\y`},
		{`\\SRV\share\a\b`, "", `\\srv\share\a\b`},
		{`..\..\x`, `C:\Work\Wt`, `C:\x`},
	}
	for _, c := range cases {
		got, err := pl.resolve(c.p, c.cwd)
		if err != nil || got != c.want {
			t.Errorf("resolve(%q,%q) = %q, %v; want %q", c.p, c.cwd, got, err, c.want)
		}
	}
	if _, err := pl.resolve(`rel\x`, ""); err == nil {
		t.Error("relative with empty cwd: want error")
	}
	for _, p := range []string{`C:foo`, `\rooted`, `C:\x\NUL`} {
		if _, err := pl.resolve(p, ""); !errors.Is(err, errUnrepresentable) {
			t.Errorf("resolve(%q) err = %v; want errUnrepresentable", p, err)
		}
	}
}

func TestResolveWindows_LinkJunctionDanglingEscape(t *testing.T) {
	pl := newWin(map[string]fakeEntry{
		`C:\Wt`:       {kind: "dir"},
		`C:\Wt\link`:  {kind: "link", target: `D:\outside`},
		`D:\outside`:  {kind: "dir"},
		`C:\Wt\jn`:    {kind: "junction", target: `D:\outside`},
		`C:\Wt\dl`:    {kind: "link", target: `C:\Nope\new`},
		`C:\Wt\loop1`: {kind: "link", target: `C:\Wt\loop2`},
		`C:\Wt\loop2`: {kind: "link", target: `C:\Wt\loop1`},
	}, nil)
	roots := []string{`C:\Wt`}
	for _, p := range []string{`C:\Wt\link\x`, `C:\Wt\jn\x`} {
		got, err := pl.resolve(p, "")
		if err != nil || got != `D:\outside\x` {
			t.Fatalf("resolve(%q) = %q, %v", p, got, err)
		}
		if pl.within(got, roots) {
			t.Errorf("%q must not be within %v", got, roots)
		}
	}
	got, err := pl.resolve(`C:\Wt\dl\f`, "")
	if err != nil || got != `C:\Nope\new\f` {
		t.Fatalf("dangling: %q, %v", got, err)
	}
	if pl.within(got, roots) {
		t.Errorf("dangling link target judged inside %v", roots)
	}
	if got, err := pl.resolve(`C:\Wt\a\..\..\x`, ""); err != nil || got != `C:\x` {
		t.Errorf("dotdot: %q, %v", got, err)
	}
	if got, err := pl.resolve(`C:\Wt\loop1\x`, ""); err == nil {
		t.Errorf("loop: want error, got %q", got)
	}
	if !pl.within(`C:\wt\pkg`, []string{`C:\WT`}) {
		t.Error(`within(C:\wt\pkg, C:\WT) = false`)
	}
	if pl.within(`C:\Wtx`, roots) {
		t.Error(`within(C:\Wtx, C:\Wt) = true`)
	}
}

func TestResolveWindows_HomeExpansionForms(t *testing.T) {
	pl := newWin(map[string]fakeEntry{`C:\Users`: {kind: "dir"}}, map[string]string{"USERPROFILE": `C:\Users\bob`})
	for _, p := range []string{
		`~\.ssh`, `~/.ssh`, `$HOME\.ssh`, `${HOME}/.ssh`, `$USERPROFILE\.ssh`,
		`${USERPROFILE}\.ssh`, `%USERPROFILE%\.ssh`, `%userprofile%/.ssh`, `$env:USERPROFILE\.ssh`,
	} {
		got, err := pl.resolve(p, "")
		if err != nil || got != `C:\Users\bob\.ssh` {
			t.Errorf("resolve(%q) = %q, %v", p, got, err)
		}
	}
	if got, err := pl.resolve("~", ""); err != nil || got != `C:\Users\bob` {
		t.Errorf("bare ~: %q, %v", got, err)
	}
	if got, err := pl.resolve("~x", ""); err == nil {
		t.Errorf("~x must not expand; got %q", got)
	}
}

func TestResolvePosix_FlavourUnchanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX flavour check; the Windows flavour has its own tests")
	}
	d := t.TempDir()
	in := filepath.Join(d, "a", "..", "b", "c")
	want, werr := ResolvePath(in, "")
	got, err := posixPlatform(osFS{}).resolve(in, "")
	if got != want || (err == nil) != (werr == nil) {
		t.Errorf("posix flavour = %q, %v; ResolvePath = %q, %v", got, err, want, werr)
	}
}
