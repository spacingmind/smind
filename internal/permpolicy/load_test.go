package permpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// policyDoc wraps rule bodies (each starting with "- id:") in the version
// header, so the negative cases read as the lines that differ.
func policyDoc(rules ...string) string {
	var b strings.Builder
	b.WriteString("version: 1\nrules:\n")
	for _, r := range rules {
		for _, line := range strings.Split(strings.TrimRight(r, "\n"), "\n") {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

// wantLoadErr fails unless Load rejected doc with a *LoadError whose message
// contains every want fragment.
func wantLoadErr(t *testing.T, doc string, want ...string) {
	t.Helper()
	_, err := Load([]byte(doc))
	if err == nil {
		t.Fatalf("Load accepted a policy it must reject (want %q)", want)
	}
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("error %v is %T, want *LoadError", err, err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("error %q does not contain %q", err.Error(), w)
		}
	}
}

const validV1 = `version: 1
rules:
  - id: lead-push-own-branch
    action: allow
    why: "Lead pushes its own task branch"
    match:
      seats: [lead]
      workspaces: [smind]
      tool: Bash
      command:
        argv0: git
        subcommand: push
        args:
          allow_only: ["origin", "{taskBranch}"]
          deny_flags: ["-f", "--force", "--force-with-lease", "--delete", "--mirror", "--all", "--tags"]
      paths:
        within: ["{worktree}"]
  - id: no-npm-publish
    action: deny
    why: "Publishing is the Human's"
    match:
      command:
        argv0: npm
        subcommand: publish
  - id: ask-curl
    action: ask
    why: "A network command must be named by a rule"
    match:
      tool: Bash
      command:
        argv0: curl
  - id: read-worktree
    action: allow
    why: "Reading inside the worktree is safe"
    match:
      tool: Read
      paths:
        within: ["{worktree}"]
  - id: ask-mcp-search
    action: ask
    why: "Every MCP search goes to the human"
    match:
      tool: "mcp:search.code"
`

func TestPolicyLoad_ValidV1(t *testing.T) {
	p, err := Load([]byte(validV1))
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != 1 || len(p.Rules) != 5 {
		t.Fatalf("policy = v%d with %d rules, want v1 with 5", p.Version, len(p.Rules))
	}
	sum := sha256.Sum256([]byte(validV1))
	if want := hex.EncodeToString(sum[:]); p.Sha != want {
		t.Fatalf("Sha = %q, want %q", p.Sha, want)
	}

	push := p.Rules[0]
	if push.ID != "lead-push-own-branch" || push.Action != ActionAllow || push.Why != "Lead pushes its own task branch" {
		t.Fatalf("push rule = %+v", push)
	}
	m := push.Match
	if len(m.Seats) != 1 || m.Seats[0] != "lead" || len(m.Workspaces) != 1 || m.Workspaces[0] != "smind" || m.Tool != "Bash" {
		t.Fatalf("push match = %+v", m)
	}
	if m.Command == nil || m.Command.Argv0 != "git" || m.Command.Subcommand != "push" {
		t.Fatalf("push command = %+v", m.Command)
	}
	if fmt.Sprint(m.Command.Args.AllowOnly) != fmt.Sprint([]string{"origin", "{taskBranch}"}) ||
		len(m.Command.Args.DenyFlags) != 7 || m.Command.Args.DenyFlags[2] != "--force-with-lease" {
		t.Fatalf("push args = %+v", m.Command.Args)
	}
	if m.Paths == nil || fmt.Sprint(m.Paths.Within) != fmt.Sprint([]string{"{worktree}"}) {
		t.Fatalf("push paths = %+v", m.Paths)
	}

	if npm := p.Rules[1]; npm.Action != ActionDeny || npm.Match.Tool != "" || npm.Match.Command.Argv0 != "npm" {
		t.Fatalf("npm rule = %+v", npm)
	}
	if curl := p.Rules[2]; curl.Action != ActionAsk || curl.Match.Command.Argv0 != "curl" {
		t.Fatalf("curl rule = %+v", curl)
	}
	if read := p.Rules[3]; read.Action != ActionAllow || read.Match.Tool != "Read" || len(read.Match.Paths.Within) != 1 {
		t.Fatalf("read rule = %+v", read)
	}
	if mcp := p.Rules[4]; mcp.Action != ActionAsk || mcp.Match.Tool != "mcp:search.code" {
		t.Fatalf("mcp rule = %+v", mcp)
	}

	// An empty rules list is a valid (fully-escalating) policy.
	empty, err := Load([]byte("version: 1\nrules: []\n"))
	if err != nil {
		t.Fatalf("empty rules: %v", err)
	}
	if len(empty.Rules) != 0 {
		t.Fatalf("empty rules = %d, want 0", len(empty.Rules))
	}
}

func TestPolicyLoad_RejectsDuplicateKeyAnchorTagUnknownField(t *testing.T) {
	t.Run("duplicate key", func(t *testing.T) {
		wantLoadErr(t, "version: 1\nversion: 2\nrules: []\n", "version", "duplicate key")
	})
	t.Run("anchor and alias", func(t *testing.T) {
		wantLoadErr(t, "version: &v 1\nrules: *v\n", "anchors and aliases")
	})
	t.Run("explicit tag", func(t *testing.T) {
		wantLoadErr(t, "version: !!int 1\nrules: []\n", "explicit YAML tags")
	})
	t.Run("unknown field", func(t *testing.T) {
		wantLoadErr(t, "version: 1\nrules: []\nbogus: true\n", "bogus", "not found")
	})
}

func TestPolicyLoad_RejectsWildcardSeatToolArgv(t *testing.T) {
	t.Run("seat", func(t *testing.T) {
		wantLoadErr(t, policyDoc("- id: r\n  action: deny\n  why: w\n  match:\n    seats: [\"*\"]"),
			"rules[0].match.seats[0]", "wildcard")
	})
	t.Run("tool", func(t *testing.T) {
		wantLoadErr(t, policyDoc("- id: r\n  action: deny\n  why: w\n  match:\n    tool: \"*\""),
			"rules[0].match.tool", "wildcard")
	})
	t.Run("argv0", func(t *testing.T) {
		wantLoadErr(t, policyDoc("- id: r\n  action: deny\n  why: w\n  match:\n    tool: Bash\n    command:\n      argv0: g*"),
			"rules[0].match.command.argv0", "wildcard")
	})
	t.Run("subcommand", func(t *testing.T) {
		wantLoadErr(t, policyDoc("- id: r\n  action: deny\n  why: w\n  match:\n    tool: Bash\n    command:\n      argv0: git\n      subcommand: p?sh"),
			"rules[0].match.command.subcommand", "wildcard")
	})
	t.Run("workspace", func(t *testing.T) {
		wantLoadErr(t, policyDoc("- id: r\n  action: deny\n  why: w\n  match:\n    workspaces: [\"*\"]"),
			"rules[0].match.workspaces[0]", "wildcard")
	})
}

func TestPolicyLoad_RejectsMutatingAllowWithoutPathScope(t *testing.T) {
	skipPOSIXPathsOnWindows(t)
	edit := "- id: edit-src\n  action: allow\n  why: w\n  match:\n    tool: Edit"
	bash := "- id: run-tests\n  action: allow\n  why: w\n  match:\n    tool: Bash\n    command:\n      argv0: go\n      subcommand: test"

	wantLoadErr(t, policyDoc(edit), "mutating", "paths.within")
	wantLoadErr(t, policyDoc(bash), "mutating", "paths.within")

	// The same rules pass once scoped, an absolute root included.
	dir := filepath.Join(t.TempDir(), "allowed")
	for _, r := range []string{
		edit + "\n    paths:\n      within: [\"{worktree}\"]",
		bash + "\n    paths:\n      within: [\"{worktree}\", \"" + filepath.ToSlash(dir) + "\"]",
	} {
		if _, err := Load([]byte(policyDoc(r))); err != nil {
			t.Fatalf("scoped allow rejected: %v", err)
		}
	}

	// A deny needs no path scope: a broad deny is safe.
	if _, err := Load([]byte(policyDoc("- id: no-rm\n  action: deny\n  why: w\n  match:\n    command:\n      argv0: rm"))); err != nil {
		t.Fatalf("deny without paths rejected: %v", err)
	}
}

func TestPolicyLoad_RejectsRegexAndOverCaps(t *testing.T) {
	t.Run("regex field", func(t *testing.T) {
		wantLoadErr(t, "version: 1\nrules: []\nregex: \".*\"\n", "regex", "not supported")
	})
	t.Run("too many rules", func(t *testing.T) {
		rules := make([]string, MaxRules+1)
		for i := range rules {
			rules[i] = fmt.Sprintf("- id: r-%d\n  action: deny\n  why: w\n  match:\n    tool: Read", i)
		}
		wantLoadErr(t, policyDoc(rules...), "rules", "cap is 200")
	})
	t.Run("why over cap", func(t *testing.T) {
		wantLoadErr(t, policyDoc("- id: r\n  action: deny\n  why: "+strings.Repeat("w", MaxWhy+1)+"\n  match:\n    tool: Read"),
			"rules[0].why", "cap is 160")
	})
	t.Run("id too long", func(t *testing.T) {
		wantLoadErr(t, policyDoc("- id: "+strings.Repeat("a", 65)+"\n  action: deny\n  why: w\n  match:\n    tool: Read"),
			"rules[0].id", "not a valid id")
	})
	t.Run("duplicate id", func(t *testing.T) {
		wantLoadErr(t, policyDoc(
			"- id: dup\n  action: deny\n  why: w\n  match:\n    tool: Read",
			"- id: dup\n  action: ask\n  why: w\n  match:\n    tool: Read"), "rules[1].id", "duplicate id")
	})
	t.Run("oversize input", func(t *testing.T) {
		wantLoadErr(t, "# "+strings.Repeat("x", MaxPolicyBytes+2), "oversize")
	})
	t.Run("list over cap", func(t *testing.T) {
		seats := make([]string, 65)
		for i := range seats {
			seats[i] = fmt.Sprintf("s%d", i)
		}
		wantLoadErr(t, policyDoc("- id: r\n  action: deny\n  why: w\n  match:\n    seats: ["+strings.Join(seats, ", ")+"]"),
			"rules[0].match.seats", "cap is 64")
	})
	t.Run("bad action", func(t *testing.T) {
		wantLoadErr(t, policyDoc("- id: r\n  action: maybe\n  why: w\n  match:\n    tool: Read"),
			"rules[0].action", "allow, deny, ask")
	})
	t.Run("version 2", func(t *testing.T) {
		wantLoadErr(t, "version: 2\nrules: []\n", "version", "must be 1")
	})
	t.Run("allow without tool", func(t *testing.T) {
		wantLoadErr(t, policyDoc("- id: r\n  action: allow\n  why: w\n  match:\n    seats: [lead]"),
			"rules[0].match.tool", "must name a tool")
	})
	t.Run("unknown placeholder", func(t *testing.T) {
		wantLoadErr(t, policyDoc(
			"- id: r\n  action: allow\n  why: w\n  match:\n    tool: Bash\n    command:\n      argv0: git\n      args:\n        allow_only: [\"{foo}\"]\n    paths:\n      within: [\"{worktree}\"]"),
			"allow_only[0]", "{foo}")
	})
}

func TestPolicyLoad_RejectsAllowThatFloorDenies(t *testing.T) {
	skipPOSIXPathsOnWindows(t)
	scope := "\n    paths:\n      within: [\"{worktree}\"]"
	bash := func(id, argv0, sub, allowOnly string) string {
		r := "- id: " + id + "\n  action: allow\n  why: w\n  match:\n    tool: Bash\n    command:\n      argv0: " + argv0 + "\n      subcommand: " + sub
		if allowOnly != "" {
			r += "\n      args:\n        allow_only: " + allowOnly
		}
		return r + scope
	}
	wantLoadErr(t, policyDoc(bash("stash", "git", "stash", "")), "rules[0]", "allow is denied by floor rule no-stash")
	wantLoadErr(t, policyDoc(bash("push", "git", "push", `["origin", "--force"]`)), "rules[0]", "no-force-push")
	wantLoadErr(t, policyDoc(bash("push-main", "git", "push", `["origin", "main"]`)), "rules[0]", "no-merge")
	wantLoadErr(t, policyDoc(bash("ok", "go", "test", ""), bash("add-all", "git", "add", `["-A"]`)), "rules[1]", "no-add-all")
	wantLoadErr(t, policyDoc("- id: edit-ssh\n  action: allow\n  why: w\n  match:\n    tool: Edit\n    paths:\n      within: [\"/h/.ssh\"]"),
		"rules[0]", "no-install-state")
	wantLoadErr(t, policyDoc("- id: edit-etc\n  action: allow\n  why: w\n  match:\n    tool: Write\n    paths:\n      within: [\"/etc/app\"]"),
		"rules[0]", "no-destructive-outside")

	// Near misses load: the branch push, a worktree edit, and a deny rule
	// naming a floor-denied command (a deny the floor already implies).
	for _, r := range []string{
		bash("push-own", "git", "push", `["origin", "{taskBranch}"]`),
		"- id: edit-wt\n  action: allow\n  why: w\n  match:\n    tool: Edit\n    paths:\n      within: [\"{worktree}\"]",
		"- id: no-stash\n  action: deny\n  why: w\n  match:\n    command:\n      argv0: git\n      subcommand: stash",
	} {
		if _, err := Load([]byte(policyDoc(r))); err != nil {
			t.Fatalf("rule rejected: %v\n%s", err, r)
		}
	}
}
