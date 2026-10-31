package permpolicy

import (
	"reflect"
	"strings"
	"testing"
)

// wrapArgvs splits cmd in Windows mode and returns each segment's argv joined
// with single spaces, outer wrapper segment first.
func wrapArgvs(cmd string) ([]string, Parsed) {
	p := splitCommand(cmd, true)
	out := make([]string, len(p.Segments))
	for i, s := range p.Segments {
		out[i] = strings.Join(s.Argv, " ")
	}
	return out, p
}

func TestSplitWrapper_PowerShell(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, cmd string
		inner     []string // segments after the wrapper itself
	}{
		{"semicolon", `powershell -Command "Get-Date; Remove-Item D:\outside"`, []string{"Get-Date", `Remove-Item D:\outside`}},
		{"pwsh short flag", `pwsh -c "Get-Date; ls"`, []string{"Get-Date", "ls"}},
		{"flag abbreviation", `pwsh -com "a; b"`, []string{"a", "b"}},
		{"leading options", `powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "x; y"`, []string{"x", "y"}},
		{"exe suffix", `powershell.exe -c "x; y"`, []string{"x", "y"}},
		{"full path", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -c "x; y"`, []string{"x", "y"}},
		{"upper case", `POWERSHELL -COMMAND "x; y"`, []string{"x", "y"}},
		{"unquoted rest joined", `powershell -c Remove-Item D:\outside`, []string{`Remove-Item D:\outside`}},
		{"positional script", `pwsh "x; y"`, []string{"x", "y"}},
		{"call operator", `powershell -c "& 'C:\a b\t.exe' arg; z"`, []string{`C:\a b\t.exe arg`, "z"}},
		{"single quote doubling", `pwsh -c "echo 'it''s'"`, []string{"echo it's"}},
		{"and-and PS7", `pwsh -c "a && b || c"`, []string{"a", "b", "c"}},
		{"redirect kept off argv", `powershell -c "echo hi > C:\Temp\o.txt"`, []string{"echo hi"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, p := wrapArgvs(tc.cmd)
			if len(p.Unparsed) != 0 {
				t.Fatalf("Unparsed = %v", p.Unparsed)
			}
			if len(got) < 1 || !reflect.DeepEqual(got[1:], tc.inner) {
				t.Errorf("segments = %q, want wrapper + %q", got, tc.inner)
			}
		})
	}
}

func TestSplitWrapper_PowerShellPipeAndRedirect(t *testing.T) {
	t.Parallel()
	p := splitCommand(`pwsh -c "Get-Content a | Select-String b 2>&1"`, true)
	if len(p.Segments) != 3 || !p.Segments[1].PipeNext || p.Segments[2].PipeNext {
		t.Fatalf("segments = %+v", p.Segments)
	}
	if rd := p.Segments[2].Redirects; len(rd) != 1 || rd[0].Op != "2>&1" {
		t.Errorf("redirects = %+v", rd)
	}
	p = splitCommand(`pwsh -c "echo hi >> C:\Temp\o.txt"`, true)
	if rd := p.Segments[1].Redirects; len(rd) != 1 || rd[0].Op != ">>" || rd[0].Target != `C:\Temp\o.txt` {
		t.Errorf("redirects = %+v", p.Segments[1].Redirects)
	}
}

func TestSplitWrapper_PowerShellUnparsed(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{
		`powershell -EncodedCommand SQBFAFgA`,
		`powershell -enc SQBFAFgA`,
		`powershell -Command`,
		`powershell -c "ls | ForEach-Object { Remove-Item $_ }"`,
		"powershell -c 'Remove-Item `x'",
		`powershell -c 'iex $x'`,
		`powershell -c 'Invoke-Expression "ls"'`,
		`powershell -c 'icm { ls }'`,
		`powershell -c 'Start-Process cmd'`,
		`powershell -c '& $x'`,
		`powershell -c 'ls & ls'`,
		`powershell -c 'ls # hidden'`,
		`powershell -c 'ls @args'`,
		`powershell -c "echo 'unterminated"`,
		`powershell -Mystery -c ls`,
		"powershell -c 'Remove-Item D:\\o`utsi`de'",
		`powershell -c "Get-Item x | ForEach-Object { ls }"`,
		`powershell -c "Get-Date $HOME"`,
		`powershell -c "Write-Output (Remove-Item D:\outside)"`,
		`powershell -c 'cat < x'`,
		`powershell -c ';;'`,
	} {
		if p := splitCommand(cmd, true); len(p.Unparsed) == 0 {
			t.Errorf("%s: want Unparsed, got segments %+v", cmd, p.Segments)
		}
	}
}

func TestSplitWrapper_PowerShellFileIsPlainSegment(t *testing.T) {
	t.Parallel()
	got, p := wrapArgvs(`powershell -NoProfile -File .\x.ps1`)
	if len(p.Unparsed) != 0 || len(got) != 1 {
		t.Errorf("segments = %q unparsed = %v; want just the wrapper", got, p.Unparsed)
	}
}

func TestSplitWrapper_PowerShellDynamic(t *testing.T) {
	t.Parallel()
	p := splitCommand(`pwsh -c 'Remove-Item $env:TEMP\x'`, true)
	if len(p.Unparsed) != 0 || !p.Segments[1].Dynamic {
		t.Errorf("env word must be dynamic: %+v unparsed %v", p.Segments, p.Unparsed)
	}
	p = splitCommand(`pwsh -c 'Remove-Item *.go'`, true)
	if !p.Segments[1].Dynamic {
		t.Errorf("glob must be dynamic: %+v", p.Segments)
	}
}

func TestSplitWrapper_Cmd(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, cmd string
		inner     []string
	}{
		{"ampersand", `cmd /c "dir & del x"`, []string{"dir", "del x"}},
		{"and-and", `cmd /c "a && b"`, []string{"a", "b"}},
		{"or-or", `cmd /c "a || b"`, []string{"a", "b"}},
		{"pipe", `cmd /c "type a | find b"`, []string{"type a", "find b"}},
		{"switches before c", `cmd /d /s /c "x & y"`, []string{"x", "y"}},
		{"upper case", `CMD.EXE /C "x & y"`, []string{"x", "y"}},
		{"keep running /k", `cmd /k "x & y"`, []string{"x", "y"}},
		{"full path", `C:\Windows\System32\cmd.exe /c "x & y"`, []string{"x", "y"}},
		{"multi word", `cmd /c dir C:\Temp`, []string{`dir C:\Temp`}},
		{"quoting restored", `cmd /c del "C:\a & b"`, []string{`del C:\a & b`}},
		{"single quote is not a quote", `cmd /c "echo 'a & del x'"`, []string{"echo 'a", "del x'"}},
		{"redirect", `cmd /c "dir > C:\Temp\o.txt"`, []string{"dir"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, p := wrapArgvs(tc.cmd)
			if len(p.Unparsed) != 0 {
				t.Fatalf("Unparsed = %v", p.Unparsed)
			}
			if len(got) < 1 || !reflect.DeepEqual(got[1:], tc.inner) {
				t.Errorf("segments = %q, want wrapper + %q", got, tc.inner)
			}
		})
	}
}

func TestSplitWrapper_CmdUnparsed(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{
		`cmd /c`,
		`cmd /c "echo a ^& del x"`,
		`cmd /c "if exist x del x"`,
		`cmd /c "for %i in (a) do del %i"`,
		`cmd /v:on /c "echo !x!"`,
		"cmd /c \"dir\ndel x\"",
		`cmd /c "start calc"`,
		`cmd /c "call x.bat"`,
		`cmd /c "echo \"open"`,
		`cmd /c "%COMSPEC% /c dir"`,
		`cmd /c "&&"`,
	} {
		if p := splitCommand(cmd, true); len(p.Unparsed) == 0 {
			t.Errorf("%s: want Unparsed, got segments %+v", cmd, p.Segments)
		}
	}
}

func TestSplitWrapper_CmdPercentIsDynamic(t *testing.T) {
	t.Parallel()
	p := splitCommand(`cmd /c "del %TEMP%\x"`, true)
	if len(p.Unparsed) != 0 || !p.Segments[1].Dynamic {
		t.Errorf("%%VAR%% word must be dynamic: %+v unparsed %v", p.Segments, p.Unparsed)
	}
}

func TestSplitWrapper_CmdNoFlagIsPlainSegment(t *testing.T) {
	t.Parallel()
	got, p := wrapArgvs(`cmd`)
	if len(p.Unparsed) != 0 || len(got) != 1 {
		t.Errorf("segments = %q unparsed = %v", got, p.Unparsed)
	}
}

func TestSplitWrapper_Nested(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, cmd string
		want      []string // every segment after the outermost wrapper
	}{
		{"cmd then powershell", `cmd /c powershell -c "Remove-Item D:\outside; Get-Date"`,
			[]string{`powershell -c Remove-Item D:\outside; Get-Date`, `Remove-Item D:\outside`, "Get-Date"}},
		{"cmd quoted then powershell", `cmd /c "powershell -c \"Remove-Item D:\outside\""`,
			[]string{`powershell -c Remove-Item D:\outside`, `Remove-Item D:\outside`}},
		{"powershell then cmd", `pwsh -c "cmd /c 'del x & del y'"`,
			[]string{`cmd /c del x & del y`, "del x", "del y"}},
		{"powershell then bash", `pwsh -c "bash -c 'git stash'"`,
			[]string{"bash -c git stash", "git stash"}},
		{"bash then powershell", `bash -c "powershell -c 'x; y'"`,
			[]string{"powershell -c x; y", "x", "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, p := wrapArgvs(tc.cmd)
			if len(p.Unparsed) != 0 {
				t.Fatalf("Unparsed = %v", p.Unparsed)
			}
			if len(got) == 0 || !reflect.DeepEqual(got[1:], tc.want) {
				t.Errorf("segments = %q, want wrapper + %q", got, tc.want)
			}
		})
	}
}

func TestSplitWrapper_NestingTooDeep(t *testing.T) {
	t.Parallel()
	cmd := "x"
	for i := 0; i < maxNestDepth+2; i++ {
		cmd = `cmd /c "` + strings.ReplaceAll(cmd, `"`, `\"`) + `"`
	}
	if p := splitCommand(cmd, false); len(p.Unparsed) == 0 {
		t.Error("deeply nested wrappers must be Unparsed")
	}
}

// A leading ( ... ) group and a $( ... ) subexpression are parsed in the
// script's own dialect, so the commands inside them reach the floor and the
// rules instead of hiding as words of another command.
func TestSplitWrapper_GroupsAndSubexpressionsAreParsed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, cmd string
		wantSeg   string // an inner segment that must be visible
	}{
		{"powershell leading group", `powershell -c "(Remove-Item D:\outside)"`, `Remove-Item D:\outside`},
		{"powershell subexpression", `powershell -c 'ls $(Remove-Item D:\outside)'`, `Remove-Item D:\outside`},
		{"powershell subexpression in quotes", `pwsh -c 'echo "$(del x)"'`, "del x"},
		{"cmd leading group", `cmd /c "(dir & rd /s /q D:\outside)"`, `rd /s /q D:\outside`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, p := wrapArgvs(tc.cmd)
			if len(p.Unparsed) != 0 {
				t.Fatalf("Unparsed = %v", p.Unparsed)
			}
			for _, g := range got[1:] {
				if g == tc.wantSeg {
					return
				}
			}
			t.Errorf("segments = %q, want one equal to %q", got, tc.wantSeg)
		})
	}
}

// Each escalation names its own reason, so a regression in one rule cannot hide
// behind another rule that happens to catch the same input.
func TestSplitWrapper_UnparsedReasons(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ cmd, reason string }{
		{`powershell -EncodedCommand SQBFAFgA`, "cannot be decoded"},
		{`powershell -c "Get-Date $HOME"`, "outer-shell expansion"},
		{`powershell -c "ls | ForEach-Object { ls }"`, "script block"},
		{`powershell -c "Write-Output (ls)"`, "parenthesised"},
		{`cmd /c "echo a ^& dir"`, "caret"},
		{"cmd /c \"dir\nrd x\"", "newline"},
	} {
		p := splitCommand(tc.cmd, true)
		if !strings.Contains(strings.Join(p.Unparsed, "; "), tc.reason) {
			t.Errorf("%s: Unparsed = %v, want a reason mentioning %q", tc.cmd, p.Unparsed, tc.reason)
		}
	}
}
