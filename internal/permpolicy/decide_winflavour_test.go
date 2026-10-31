package permpolicy

import "testing"

func winEnv(t *testing.T) (*platform, Request) {
	t.Helper()
	dir := fakeEntry{kind: "dir"}
	pl := newWin(map[string]fakeEntry{
		`C:\Work\Wt`:          dir,
		`C:\Work\Wt\pkg`:      dir,
		`D:\outside`:          dir,
		`C:\Users\bob`:        dir,
		`C:\Users\bob\.smind`: dir,
		`C:\Users\bob\.ssh`:   dir,
		`C:\Temp`:             dir,
	}, map[string]string{"USERPROFILE": `C:\Users\bob`, "TEMP": `C:\Temp`, "TMP": `C:\Temp`, "SystemRoot": `C:\Windows`})
	return pl, Request{
		Tool: "Bash", Worktree: `C:\Work\Wt`, Cwd: `C:\Work\Wt`,
		Home: `C:\Users\bob`, SmindHome: `C:\Users\bob\.smind`, plat: pl,
	}
}

func TestDecide_WindowsOutsideDirIsAsk(t *testing.T) {
	t.Parallel()
	_, base := winEnv(t)
	pol := mustLoad(t, bashRule(goTestAllow, ActionAllow, "go", "test"))
	cases := []struct {
		cmd  string
		want Action
	}{
		{`go test .\pkg`, ActionAllow},
		{`go test C:\work\wt\pkg`, ActionAllow},
		{`go test D:\outside`, ActionAsk},
		{`go test d:/outside/x`, ActionAsk},
		{`go test \\srv\share\x`, ActionAsk},
		{`go test C:\Work\Wt\..\x`, ActionAsk},
		{`go test ..\x`, ActionAsk},
		{`go test C:foo`, ActionAsk},
		{`go test D:\outside\a\..\..\x`, ActionAsk},
	}
	for _, c := range cases {
		req := base
		req.Command = c.cmd
		d := Decide(req, pol)
		if d.Action != c.want {
			t.Errorf("%s: Action = %s (rule %q floor %q reasons %q), want %s", c.cmd, d.Action, d.RuleID, d.FloorID, d.Reasons, c.want)
		}
	}
}

func TestDecide_WindowsFloorBeatsAllowRule(t *testing.T) {
	t.Parallel()
	_, base := winEnv(t)

	edit := &Policy{Version: 1, Rules: []Rule{{ID: "e", Action: ActionAllow, Why: "w",
		Match: Match{Tool: "Edit", Paths: &PathsMatch{Within: []string{`D:\outside`}}}}}}
	req := base
	req.Tool, req.Paths = "Edit", []string{`D:\outside\x.go`}
	d := Decide(req, edit)
	wantAction(t, d, ActionDeny)
	if d.FloorID != "no-destructive-outside" {
		t.Errorf("Edit FloorID = %q", d.FloorID)
	}

	req.Tool, req.Paths = "Write", []string{`/etc/x`}
	d = Decide(req, edit)
	wantAction(t, d, ActionDeny)
	if d.FloorID != "no-destructive-outside" {
		t.Errorf("Write /etc/x FloorID = %q", d.FloorID)
	}

	stash := &Policy{Version: 1, Sha: "abc", Rules: []Rule{{
		ID: "stash-ok", Action: ActionAllow, Why: "w",
		Match: Match{
			Tool:    "Bash",
			Command: &CommandMatch{Argv0: "git", Subcommand: "stash"},
			Paths:   &PathsMatch{Within: []string{"{worktree}"}},
		},
	}}}
	req = base
	req.Command = "git stash"
	d = Decide(req, stash)
	wantAction(t, d, ActionDeny)
	if d.FloorID != "no-stash" {
		t.Errorf("git stash FloorID = %q", d.FloorID)
	}
}

func TestDecide_WindowsUnparseableIsAsk(t *testing.T) {
	t.Parallel()
	_, base := winEnv(t)
	pol := mustLoad(t, bashRule(goTestAllow, ActionAllow, "go", "test"))
	for _, cmd := range []string{`go test %TEMP%\x`, `go test C:\x\NUL`} {
		req := base
		req.Command = cmd
		d := Decide(req, pol)
		if d.Action != ActionAsk {
			t.Errorf("%s: Action = %s (rule %q floor %q reasons %q), want ask", cmd, d.Action, d.RuleID, d.FloorID, d.Reasons)
		}
	}
}
