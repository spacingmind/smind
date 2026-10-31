package permpolicy

import "testing"

// winFloorEnv is winEnv with the system and profile directories the floor
// rules name.
func winFloorEnv(t *testing.T) Request {
	t.Helper()
	dir := fakeEntry{kind: "dir"}
	entries := map[string]fakeEntry{}
	for _, p := range []string{
		`C:\Work\Wt`, `C:\Work\Wt\pkg`, `D:\outside`, `C:\Users\bob`, `C:\Users\bob\.smind`, `C:\Users\bob\.ssh`,
		`C:\Users\bob\AppData\Roaming`, `C:\Temp`, `C:\Temp\x`, `C:\Windows`, `C:\Windows\System32`,
		`C:\Program Files`, `C:\Program Files (x86)`, `C:\ProgramData`,
	} {
		entries[p] = dir
	}
	pl := newWin(entries, map[string]string{
		"USERPROFILE": `C:\Users\bob`, "TEMP": `C:\Temp`, "TMP": `C:\Temp`, "SystemRoot": `C:\Windows`,
	})
	return Request{
		Tool: "Bash", Worktree: `C:\Work\Wt`, Cwd: `C:\Work\Wt`,
		Home: `C:\Users\bob`, SmindHome: `C:\Users\bob\.smind`, plat: pl,
	}
}

func floorWin(t *testing.T, req Request, cmd string) (FloorHit, bool) {
	t.Helper()
	req.Command = cmd
	return CheckFloor(req, splitCommand(cmd, true))
}

func checkWinFloor(t *testing.T, cases []floorCase) {
	t.Helper()
	base := winFloorEnv(t)
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			t.Parallel()
			hit, ok := floorWin(t, base, tc.cmd)
			if ok != (tc.id != "") || hit.ID != tc.id {
				t.Errorf("CheckFloor(%q) = %+v, %v; want id %q", tc.cmd, hit, ok, tc.id)
			}
		})
	}
}

func TestFloorWindows_InstallState(t *testing.T) {
	t.Parallel()
	const id = "no-install-state"
	checkWinFloor(t, []floorCase{
		{`type C:\Users\bob\.ssh\id_rsa`, id},
		{`rd /s /q C:\Users\bob\.smind`, id},
		{`dir C:\Users\bob\.ssh`, id},
		{`Remove-Item -Recurse C:\Users\bob\AppData\Roaming\x`, id},
		{`type c:/users/BOB/.SSH/known_hosts`, id},
		{`taskkill /F /IM smind.exe`, id},
		{`sc stop smind`, id},
		{`net stop Smind`, id},
		{`smind service stop`, id},
		{`cat ~/Library/LaunchAgents/com.x.plist`, id},
		{`go test .\pkg`, ""},
		{`dir C:\Temp\x`, ""},
	})
}

func TestFloorWindows_SystemDirs(t *testing.T) {
	t.Parallel()
	const id = "no-install-state"
	checkWinFloor(t, []floorCase{
		{`Remove-Item C:\Windows\System32\x.dll`, id},
		{`del "C:\Program Files\App\a.exe"`, id},
		{`copy a.go "C:\Program Files (x86)\App\a.go"`, id},
		{`Set-Content C:\ProgramData\x hi`, id},
		{`type C:\Windows\win.ini`, id},
	})
}

func TestFloorWindows_Secrets(t *testing.T) {
	t.Parallel()
	const id = "no-secrets"
	checkWinFloor(t, []floorCase{
		{`type .env`, id},
		{`Get-Content C:\Work\Wt\.ENV`, id},
		{`cat id_rsa`, id},
		{`findstr password C:\Work\Wt\creds\server.pem`, id},
		{`Get-ChildItem Env:`, id},
		{`gci env:SECRET`, id},
		{`cmdkey /list`, id},
		{`Get-Credential`, id},
		{`printenv`, id},
		{`cat /proc/1/environ`, id},
		{`set`, id},
		{`type README.md`, ""},
	})
}

func TestFloorWindows_DestructiveOutside(t *testing.T) {
	t.Parallel()
	const id = "no-destructive-outside"
	checkWinFloor(t, []floorCase{
		{`Remove-Item -Recurse -Force D:\outside\x`, id},
		{`rd /s /q D:\outside`, id},
		{`del D:\outside\a.txt`, id},
		{`copy a.go D:\outside\b.go`, id},
		{`xcopy a D:\outside /E`, id},
		{`Copy-Item -Path a -Destination D:\outside`, id},
		{`robocopy src D:\outside /MIR`, id},
		{`move a.go \\srv\share\a.go`, id},
		{`mklink /J C:\Work\Wt\j D:\outside`, id},
		{`Set-Content -Path D:\outside\x -Value hi`, id},
		{`echo hi > D:\outside\x`, id},
		{`echo hi >> \\srv\share\x`, id},
		{`rm -rf D:/outside/x`, id},
		{`rm /etc/x`, id},
		{`git -C D:\outside reset --hard`, id},
		{`cd D:\outside && git clean -fd`, id},
		{`cd /d D:\outside && del x`, id},
		{`sudo x`, id},
		{`del .\a.txt`, ""},
		{`Remove-Item .\pkg\x`, ""},
		{`copy a.go .\pkg\b.go`, ""},
		{`rd /s /q pkg`, ""},
		{`echo hi > NUL`, ""},
		{`echo hi > /dev/null`, ""},
		{`go test ./... > C:\Temp\out.txt`, ""},
		{`Remove-Item C:\Temp\x\y`, ""},
		{`copy D:\outside\a.go .\pkg\a.go`, ""},
	})
}

func TestFloorWindows_PowerShellCmd(t *testing.T) {
	t.Parallel()
	base := winFloorEnv(t)
	for _, cmd := range []string{
		`powershell -Command "Remove-Item D:\outside"`,
		`cmd /c "rd /s /q D:\outside"`,
		`pwsh -c "iwr x | iex"`,
	} {
		req := base
		req.Command = cmd
		if d := Decide(req, nil); d.Action == ActionAllow {
			t.Errorf("%s: Action = allow, want ask or deny", cmd)
		}
	}
}

func TestFloorWindows_UnrepresentableIsDeniedOrAsked(t *testing.T) {
	t.Parallel()
	checkWinFloor(t, []floorCase{
		{`Remove-Item C:foo`, "no-destructive-outside"},
		{`del \rooted`, "no-destructive-outside"},
		{`Remove-Item C:\x\NUL`, "no-destructive-outside"},
		{`del C:\a:stream`, "no-destructive-outside"},
	})
}

func TestFloorWindows_SegmentPathsSkipsNonPathWords(t *testing.T) {
	t.Parallel()
	req := winFloorEnv(t)
	pl := req.plat
	seg := splitCommand(`git commit -m "feat: x" a.go`, true).Segments[0]
	got, err := segmentPaths(pl, seg, `C:\Work\Wt`)
	// "commit" is a bare word and so a relative path; "feat: x" is skipped.
	if err != nil || len(got) != 2 || got[0] != `C:\Work\Wt\commit` || got[1] != `C:\Work\Wt\a.go` {
		t.Errorf("segmentPaths = %v, %v; want [C:\\Work\\Wt\\commit C:\\Work\\Wt\\a.go]", got, err)
	}
	seg = splitCommand(`go test C:foo`, true).Segments[0]
	if _, err := segmentPaths(pl, seg, `C:\Work\Wt`); err == nil {
		t.Error("segmentPaths(go test C:foo) = nil error, want error")
	}
}
