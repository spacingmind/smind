//go:build windows

package permpolicy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// realWinEnv is a task on the real filesystem of a Windows runner, where
// TEMP holds root, wt and home. The temp zone is allowed for scratch writes
// by design, so outside is a directory NAMED under the system drive, outside
// both TEMP and home, and never created. tmpOutside is a real directory
// (inside TEMP) for tests that need an existing target.
type realWinEnv struct {
	req        Request
	root       string
	wt         string
	outside    string
	tmpOutside string
	home       string
}

func realWinReq(t *testing.T, cmd string) realWinEnv {
	t.Helper()
	root := t.TempDir()
	wt := filepath.Join(root, "wt")
	tmpOutside := filepath.Join(root, "outside")
	home := filepath.Join(root, "home")
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = "C:"
	}
	outside := drive + `\permpolicy-outside-` + strings.ReplaceAll(t.Name(), "/", "_")
	if inTempZone(outside) {
		t.Skipf("%s is inside the temp zone on this runner", outside)
	}
	for _, d := range []string{wt, tmpOutside, home, filepath.Join(home, ".smind"), filepath.Join(home, ".ssh")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return realWinEnv{
		req: Request{
			Tool: "Bash", Command: cmd, Worktree: wt, Cwd: wt,
			Home: home, SmindHome: filepath.Join(home, ".smind"),
		},
		root: root, wt: wt, outside: outside, tmpOutside: tmpOutside, home: home,
	}
}

func inTempZone(p string) bool {
	pl := hostPlatform()
	return pl.within(p, tempRoots(pl))
}

func q(p string) string { return `"` + p + `"` }

func TestDecideRealWindows_OutsideDirIsAsk(t *testing.T) {
	e := realWinReq(t, "")
	if err := os.MkdirAll(filepath.Join(e.wt, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	pol := mustLoad(t, bashRule(goTestAllow, ActionAllow, "go", "test"))
	cases := []struct {
		cmd  string
		want Action
	}{
		{`go test .\pkg`, ActionAllow},
		{`go test ` + q(strings.ToUpper(e.wt)+`\pkg`), ActionAllow},
		{`go test ` + q(e.outside), ActionAsk},
		{`go test ` + q(filepath.ToSlash(e.outside)), ActionAsk},
		{`go test ` + q(strings.ToUpper(e.outside)), ActionAsk},
		{`go test ..\outside`, ActionAsk},
	}
	for _, c := range cases {
		req := e.req
		req.Command = c.cmd
		d := Decide(req, pol)
		if d.Action != c.want {
			t.Errorf("%s: Action = %s (rule %q floor %q reasons %q), want %s", c.cmd, d.Action, d.RuleID, d.FloorID, d.Reasons, c.want)
		}
	}
}

func TestDecideRealWindows_JunctionEscapeIsAsk(t *testing.T) {
	e := realWinReq(t, "")
	if err := os.MkdirAll(filepath.Join(e.wt, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(e.wt, "jn")
	if err := exec.Command("cmd", "/c", "mklink", "/J", link, e.tmpOutside).Run(); err != nil {
		t.Skipf("mklink /J: %v", err)
	}
	pol := mustLoad(t, bashRule(goTestAllow, ActionAllow, "go", "test"))
	for cmd, want := range map[string]Action{
		`go test .\jn\x`: ActionAsk,
		`go test .\pkg`:  ActionAllow,
	} {
		req := e.req
		req.Command = cmd
		d := Decide(req, pol)
		if d.Action != want {
			t.Errorf("%s: Action = %s (rule %q floor %q reasons %q), want %s", cmd, d.Action, d.RuleID, d.FloorID, d.Reasons, want)
		}
	}
}

func TestFloorRealWindows_Deny(t *testing.T) {
	e := realWinReq(t, "")
	cases := []struct {
		cmd  string
		want []string
	}{
		{`git stash`, []string{"no-stash"}},
		{`Remove-Item -Recurse ` + q(e.outside), []string{"no-destructive-outside"}},
		{`del ` + q(e.outside+`\x`), []string{"no-destructive-outside"}},
		{`type ` + q(e.home+`\.ssh\id_rsa`), []string{"no-install-state", "no-secrets"}},
		{`rd /s /q ` + q(e.home+`\.smind`), []string{"no-install-state"}},
		{`copy a.go C:\Windows\System32\a.go`, []string{"no-install-state", "no-destructive-outside"}},
		{`HOME=` + q(e.home) + ` go test ./...`, []string{"no-real-home-tests"}},
	}
	for _, c := range cases {
		req := e.req
		req.Command = c.cmd
		d := Decide(req, nil)
		ok := false
		for _, w := range c.want {
			if d.FloorID == w {
				ok = true
			}
		}
		if d.Action != ActionDeny || !ok {
			t.Errorf("%s: Action = %s FloorID = %q, want deny %v", c.cmd, d.Action, d.FloorID, c.want)
		}
	}
}

func TestFloorRealWindows_FileTools(t *testing.T) {
	e := realWinReq(t, "")
	cases := []struct {
		tool string
		path string
		want string
	}{
		{"Edit", e.outside + `\x.go`, "no-destructive-outside"},
		{"Write", `/etc/x`, "no-destructive-outside"},
		{"Read", e.home + `\.ssh\config`, "no-install-state"},
	}
	for _, c := range cases {
		req := e.req
		req.Tool, req.Command, req.Paths = c.tool, "", []string{c.path}
		d := Decide(req, nil)
		if d.Action != ActionDeny || d.FloorID != c.want {
			t.Errorf("%s %s: Action = %s FloorID = %q, want deny %s", c.tool, c.path, d.Action, d.FloorID, c.want)
		}
	}
	req := e.req
	req.Tool, req.Command, req.Paths = "Edit", "", []string{e.wt + `\a.go`}
	if h, hit := CheckFloorPaths(req); hit {
		t.Errorf("worktree Edit hit floor %q", h.ID)
	}
}
