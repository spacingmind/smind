package permpolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dReq is a lead's Bash request in a fresh worktree with isolated homes.
func dReq(t *testing.T, cmd string) Request {
	t.Helper()
	wt := t.TempDir()
	return Request{
		Tool: "Bash", Command: cmd, Worktree: wt, Cwd: wt, TaskBranch: "task-branch",
		Seat: "lead", Workspace: "smind", Home: t.TempDir(), SmindHome: t.TempDir(),
	}
}

func mustLoad(t *testing.T, rules ...string) *Policy {
	t.Helper()
	pol, err := Load([]byte(policyDoc(rules...)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return pol
}

// bashRule renders a Bash rule on argv0 + subcommand scoped to the worktree.
func bashRule(id string, action Action, argv0, sub string) string {
	return "- id: " + id + "\n  action: " + string(action) + "\n  why: " + id + " why\n  match:\n    tool: Bash\n" +
		"    command:\n      argv0: " + argv0 + "\n      subcommand: " + sub +
		"\n    paths:\n      within: [\"{worktree}\"]"
}

const goTestAllow = "go-test"

func wantAction(t *testing.T, d Decision, want Action) {
	t.Helper()
	if d.Action != want {
		t.Fatalf("Action = %s (rule %q floor %q reasons %q), want %s", d.Action, d.RuleID, d.FloorID, d.Reasons, want)
	}
}

func TestDecide_PrecedenceDenyAskAllowIgnoresOrder(t *testing.T) {
	t.Parallel()
	rules := map[Action]string{
		ActionAllow: bashRule("r-allow", ActionAllow, "go", "test"),
		ActionAsk:   bashRule("r-ask", ActionAsk, "go", "test"),
		ActionDeny:  bashRule("r-deny", ActionDeny, "go", "test"),
	}
	orders := [][]Action{
		{ActionAllow, ActionAsk, ActionDeny}, {ActionAllow, ActionDeny, ActionAsk},
		{ActionAsk, ActionAllow, ActionDeny}, {ActionAsk, ActionDeny, ActionAllow},
		{ActionDeny, ActionAllow, ActionAsk}, {ActionDeny, ActionAsk, ActionAllow},
	}
	for _, o := range orders {
		var docs []string
		for _, a := range o {
			docs = append(docs, rules[a])
		}
		d := Decide(dReq(t, "go test ./..."), mustLoad(t, docs...))
		wantAction(t, d, ActionDeny)
		if d.RuleID != "r-deny" || d.Why != "r-deny why" {
			t.Errorf("order %v: RuleID=%q Why=%q", o, d.RuleID, d.Why)
		}
		// Without the deny, ask still beats allow in either order.
		d = Decide(dReq(t, "go test ./..."), mustLoad(t, rules[o[0]], rules[o[1]]))
		if o[0] != ActionDeny && o[1] != ActionDeny && d.Action != ActionAsk {
			t.Errorf("order %v: allow+ask = %s, want ask", o, d.Action)
		}
	}
	pol := mustLoad(t, rules[ActionAllow])
	d := Decide(dReq(t, "go test ./..."), pol)
	wantAction(t, d, ActionAllow)
	if d.RuleID != "r-allow" || d.PolicySha != pol.Sha || d.PolicySha == "" {
		t.Errorf("allow: RuleID=%q sha=%q", d.RuleID, d.PolicySha)
	}
}

func TestDecide_UnmatchedIsAsk(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, bashRule(goTestAllow, ActionAllow, "go", "test"))
	d := Decide(dReq(t, "ls -la"), pol)
	wantAction(t, d, ActionAsk)
	if len(d.Reasons) != 1 || d.Reasons[0] != "no rule matched" {
		t.Errorf("Reasons = %q", d.Reasons)
	}
	for _, cmd := range []string{"go build ./...", "go", "go -v test ./...", "nohup go test ./...", "go test ./... > out.txt", "go test $X"} {
		wantAction(t, Decide(dReq(t, cmd), pol), ActionAsk)
	}
	// Unknown tool, no deny/ask rule for it.
	req := dReq(t, "")
	req.Tool = "mcp:search.code"
	wantAction(t, Decide(req, pol), ActionAsk)
	req.Tool = ""
	wantAction(t, Decide(req, pol), ActionAsk)
	// A deny rule for an unknown tool still applies.
	denyTool := "- id: no-search\n  action: deny\n  why: w\n  match:\n    tool: \"mcp:search.code\""
	req.Tool = "mcp:search.code"
	wantAction(t, Decide(req, mustLoad(t, denyTool)), ActionDeny)
}

func TestDecide_AllowNeedsEverySegmentCovered(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t,
		bashRule("go-test", ActionAllow, "go", "test"),
		bashRule("go-vet", ActionAllow, "go", "vet"))
	d := Decide(dReq(t, "go test ./... && go vet ./..."), pol)
	wantAction(t, d, ActionAllow)
	if d.RuleID != "go-test,go-vet" || len(d.Segments) != 2 || d.Segments[1].RuleID != "go-vet" {
		t.Errorf("RuleID=%q segments=%+v", d.RuleID, d.Segments)
	}
	d = Decide(dReq(t, "go test ./... && go vet ./... && go test ./..."), pol)
	wantAction(t, d, ActionAllow)
	if d.RuleID != "go-test,go-vet" {
		t.Errorf("RuleID = %q, want distinct ids in segment order", d.RuleID)
	}
	d = Decide(dReq(t, "go test ./... && go build ./..."), pol)
	wantAction(t, d, ActionAsk)
	if len(d.Reasons) != 1 || !strings.Contains(d.Reasons[0], "segment 1") {
		t.Errorf("Reasons = %q, want the uncovered segment named", d.Reasons)
	}
}

func TestDecide_CompoundWithAllowedThenDisallowedIsAsk(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, bashRule(goTestAllow, ActionAllow, "go", "test"))
	d := Decide(dReq(t, "go test ./... && curl x"), pol)
	wantAction(t, d, ActionAsk)
	if d.Segments[0].Action != ActionAllow && d.Segments[0].Action != ActionAsk {
		t.Errorf("segments = %+v", d.Segments)
	}
	wantAction(t, Decide(dReq(t, "curl x; go test ./..."), pol), ActionAsk)
	wantAction(t, Decide(dReq(t, "go test ./... | tee out"), pol), ActionAsk)
}

func TestDecide_HeredocBodyNotMatchedAndElided(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, bashRule("git-commit", ActionAllow, "git", "commit"))
	body := "fix: handle `git push --force` and $(rm -rf /)"
	cmd := "git commit -F - <<'EOF'\n" + body + "\nEOF"
	d := Decide(dReq(t, cmd), pol)
	if d.Action == ActionDeny || d.FloorID != "" {
		t.Fatalf("heredoc body was matched: %+v", d)
	}
	if !strings.Contains(d.Normalized, "[heredoc") || strings.Contains(d.Normalized, "git push --force") || strings.Contains(d.Normalized, "rm -rf") {
		t.Errorf("Normalized = %q, want the body elided", d.Normalized)
	}
	// The same words outside a heredoc are a deny.
	wantAction(t, Decide(dReq(t, "echo x && git push --force origin x"), pol), ActionDeny)
}

func TestDecide_UnparseableIsAsk(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t,
		bashRule("push-own", ActionAllow, "git", "push"),
		bashRule("go-test", ActionAllow, "go", "test"))
	for _, cmd := range []string{
		`go test "./...`,
		"eval x",
		"command eval x",
		`bash -c "$x"`,
		"cat <(ls)",
		"echo $(git push origin task-branch)",
		"$X go test ./...",
	} {
		t.Run(cmd, func(t *testing.T) {
			t.Parallel()
			d := Decide(dReq(t, cmd), pol)
			wantAction(t, d, ActionAsk)
			if len(d.Reasons) == 0 {
				t.Error("an ask must carry a reason")
			}
		})
	}
}

func TestDecide_SymlinkAndDotDotEscapeNotWithin(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, bashRule(goTestAllow, ActionAllow, "go", "test"))
	req := dReq(t, "")
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(req.Worktree, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(req.Worktree, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	for cmd, want := range map[string]Action{
		"go test ./pkg":          ActionAllow,
		"go test ./link/x":       ActionAsk,
		"go test link/x":         ActionAsk,
		"go test ../x":           ActionAsk,
		"go test ./pkg/../../x":  ActionAsk,
		"go test " + outside:     ActionAsk,
		"cd link && go test ./x": ActionAsk,
		"cd pkg && go test ./x":  ActionAsk, // cd itself has no allow rule
	} {
		r := req
		r.Command = cmd
		if d := Decide(r, pol); d.Action != want {
			t.Errorf("%q = %s (%q), want %s", cmd, d.Action, d.Reasons, want)
		}
	}
}

func TestDecide_MissingPathResolvedByParent(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, bashRule(goTestAllow, ActionAllow, "go", "test"))
	req := dReq(t, "go test ./does/not/exist/yet")
	d := Decide(req, pol)
	wantAction(t, d, ActionAllow)
	want, err := ResolvePath(filepath.Join(req.Worktree, "does/not/exist/yet"), "")
	if err != nil || !containsWord(d.Paths, want) {
		t.Errorf("Paths = %q, want it to contain %q (%v)", d.Paths, want, err)
	}
}

func TestDecide_PathResolutionFailureIsAsk(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, bashRule(goTestAllow, ActionAllow, "go", "test"))
	req := dReq(t, "go test ./pkg")
	req.Cwd, req.Worktree = "", ""
	d := Decide(req, pol)
	wantAction(t, d, ActionAsk)
	if len(d.Reasons) == 0 || !strings.Contains(d.Reasons[0], "resolve") {
		t.Errorf("Reasons = %q", d.Reasons)
	}
	// A file tool with an unresolvable path asks too.
	req = dReq(t, "")
	req.Tool, req.Paths, req.Cwd, req.Worktree = "Read", []string{"rel/x"}, "", ""
	wantAction(t, Decide(req, pol), ActionAsk)
}

func scoped(id string, extra string) string {
	return "- id: " + id + "\n  action: allow\n  why: " + id + " why\n  match:\n" + extra +
		"    tool: Bash\n    command:\n      argv0: go\n      subcommand: test\n    paths:\n      within: [\"{worktree}\"]"
}

func TestDecide_SeatScopedRule(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, scoped("lead-only", "    seats: [lead]\n"))
	req := dReq(t, "go test ./...")
	wantAction(t, Decide(req, pol), ActionAllow)
	req.Seat = "peer"
	wantAction(t, Decide(req, pol), ActionAsk)
	req.Seat = ""
	wantAction(t, Decide(req, pol), ActionAsk)
}

func TestDecide_WorkspaceScopedRule(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, scoped("smind-only", "    workspaces: [smind]\n"))
	req := dReq(t, "go test ./...")
	wantAction(t, Decide(req, pol), ActionAllow)
	req.Workspace = "other"
	wantAction(t, Decide(req, pol), ActionAsk)
	req.Workspace = ""
	wantAction(t, Decide(req, pol), ActionAsk)
}

func TestFloor_BeatsAllowRule(t *testing.T) {
	t.Parallel()
	pol := &Policy{Version: 1, Sha: "abc", Rules: []Rule{{
		ID: "stash-ok", Action: ActionAllow, Why: "w",
		Match: Match{
			Tool:    "Bash",
			Command: &CommandMatch{Argv0: "git", Subcommand: "stash"},
			Paths:   &PathsMatch{Within: []string{"{worktree}"}},
		},
	}}}
	d := Decide(dReq(t, "git stash"), pol)
	wantAction(t, d, ActionDeny)
	if d.FloorID != "no-stash" || d.PolicySha != "abc" {
		t.Errorf("FloorID=%q sha=%q", d.FloorID, d.PolicySha)
	}
	if len(d.Segments) != 1 || d.Segments[0].FloorID != "no-stash" {
		t.Errorf("Segments = %+v", d.Segments)
	}
	// A file tool allow does not beat the path floor either.
	req := dReq(t, "")
	req.Tool, req.Paths = "Edit", []string{"/etc/x"}
	edit := &Policy{Version: 1, Rules: []Rule{{ID: "e", Action: ActionAllow, Why: "w",
		Match: Match{Tool: "Edit", Paths: &PathsMatch{Within: []string{"/etc"}}}}}}
	d = Decide(req, edit)
	wantAction(t, d, ActionDeny)
}

func TestFloor_AppliesEvenWhenPolicyInvalid(t *testing.T) {
	t.Parallel()
	d := Decide(dReq(t, "git stash"), nil)
	wantAction(t, d, ActionDeny)
	if d.FloorID != "no-stash" || d.PolicySha != "" {
		t.Errorf("FloorID=%q sha=%q", d.FloorID, d.PolicySha)
	}
	wantAction(t, Decide(dReq(t, "go test ./..."), nil), ActionAsk)
	req := dReq(t, "")
	req.Tool, req.Paths = "Read", []string{"~/.ssh/id_rsa"}
	wantAction(t, Decide(req, nil), ActionDeny)
	req.Truncated = true
	req.Tool, req.Command = "Bash", "git stash"
	wantAction(t, Decide(req, nil), ActionAsk) // truncated is judged before anything
}

const pushOwn = `- id: lead-push-own-branch
  action: allow
  why: Lead pushes its own task branch
  match:
    seats: [lead]
    tool: Bash
    command:
      argv0: git
      subcommand: push
      args:
        allow_only: ["origin", "{taskBranch}"]
        deny_flags: ["-f", "--force", "--force-with-lease", "--delete", "--mirror"]
    paths:
      within: ["{worktree}"]`

func TestPushOwnBranch_AllowedOnlyForTaskBranch(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, pushOwn)
	req := dReq(t, "git push origin task-branch")
	d := Decide(req, pol)
	wantAction(t, d, ActionAllow)
	if d.RuleID != "lead-push-own-branch" {
		t.Errorf("RuleID = %q", d.RuleID)
	}
	req.TaskBranch = "other"
	wantAction(t, Decide(req, pol), ActionAsk)
	req.TaskBranch = ""
	wantAction(t, Decide(req, pol), ActionAsk)
	req = dReq(t, "git push origin task-branch")
	req.Seat = "peer"
	wantAction(t, Decide(req, pol), ActionAsk)
	req = dReq(t, "git push origin task-branch")
	req.Worktree = ""
	wantAction(t, Decide(req, pol), ActionAsk)
}

func TestPushOwnBranch_OtherRefsAsk(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, pushOwn)
	for cmd, want := range map[string]Action{
		"git push":                              ActionAsk,
		"git push origin":                       ActionAsk,
		"git push origin other":                 ActionAsk,
		"git push -u origin task-branch":        ActionAsk,
		"git push origin HEAD:develop":          ActionDeny,
		"git push --force-with-lease":           ActionDeny,
		"git push origin +x":                    ActionDeny,
		"git push origin :x":                    ActionDeny,
		"git -C /tmp/x push origin task-branch": ActionAsk,
		"nohup git push origin task-branch":     ActionAsk,
		"git push origin task-branch && ls":     ActionAsk,
	} {
		if d := Decide(dReq(t, cmd), pol); d.Action != want {
			t.Errorf("%q = %s (rule %q floor %q %q), want %s", cmd, d.Action, d.RuleID, d.FloorID, d.Reasons, want)
		}
	}
}

func TestDecide_EnvAssignmentSegmentIsNotAllowed(t *testing.T) {
	t.Parallel()
	pol := mustLoad(t, bashRule(goTestAllow, ActionAllow, "go", "test"))
	wantAction(t, Decide(dReq(t, "go test ./..."), pol), ActionAllow)
	for _, cmd := range []string{"FOO=1 go test ./...", "GOFLAGS=-toolexec=evil go test ./...", "LD_PRELOAD=x go test ./..."} {
		d := Decide(dReq(t, cmd), pol)
		wantAction(t, d, ActionAsk)
		if len(d.Reasons) != 1 || !strings.Contains(d.Reasons[0], "environment assignment") {
			t.Errorf("%q: Reasons = %q", cmd, d.Reasons)
		}
	}
}
