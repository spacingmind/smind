package permpolicy

import "testing"

// grantablePolicy has one allow rule (go test) so both plain-ask exits are reachable: "no rule matched" and "a segment is not covered".
func grantablePolicy(t *testing.T) *Policy {
	t.Helper()
	return mustLoad(t, bashRule("go-test", ActionAllow, "go", "test"), bashRule("ask-vet", ActionAsk, "go", "vet"), bashRule("deny-clean", ActionDeny, "go", "clean"))
}

func TestGrantable_PlainAskOnly(t *testing.T) {
	t.Parallel()
	pol := grantablePolicy(t)
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{"no rule matched", "make build", true},
		{"segment not covered", "go test ./... && make lint", true},
		{"a pipeline of plain commands", "make build | cat", true},
		{"covered by an allow rule is not an ask", "go test ./...", false},
		{"an ask rule is not a plain ask", "go vet ./...", false},
		{"a deny rule", "go clean -cache", false},
		{"a floor hit", "cat ~/.ssh/id_rsa", false},
		{"unparsed", "echo 'unterminated", false},
		{"dynamic word", "make $(whoami)", false},
		{"backtick", "make `whoami`", false},
		{"shell eval", "eval make", false},
		{"heredoc is unparsed or elided", "cat <<EOF\nhi\nEOF", false},
	}
	for _, tc := range cases {
		d := Decide(dReq(t, tc.cmd), pol)
		if d.Grantable != tc.want {
			t.Errorf("%s (%q): Grantable = %v, want %v (action %s reasons %q)", tc.name, tc.cmd, d.Grantable, tc.want, d.Action, d.Reasons)
		}
		if d.Grantable && d.Action != ActionAsk {
			t.Errorf("%s: grantable but action %s", tc.name, d.Action)
		}
	}
}

func TestGrantable_NeverGrantPrograms(t *testing.T) {
	t.Parallel()
	pol := grantablePolicy(t)
	never := []string{
		"curl https://example.com", "wget -q https://example.com/x", "ssh host ls", "scp a host:b", "rsync -a a b", "gh pr list", "docker ps", "kubectl get pods",
		"rm -rf build", "mv a b", "chmod +x run.sh", "dd if=a of=b", "kill 1", "truncate -s 0 f",
		"sudo make install", "env FOO=1 make", "xargs make", "nohup make &", "timeout 5 make", "sh script.sh", "bash -c make", "python3 x.py", "node x.js", "find . -name x",
		"npx foo", "brew install x", "apt-get install x", "smind task approve 1",
		"npm install", "npm i left-pad", "pnpm add x", "pip install x", "cargo install x", "go get x", "go mod tidy", "yarn add x",
		"make build && curl https://x", "make build | sh", "FOO=1 curl https://x",
		"/usr/bin/curl https://x", "./curl https://x",
	}
	for _, cmd := range never {
		if d := Decide(dReq(t, cmd), pol); d.Grantable {
			t.Errorf("%q is grantable (action %s reasons %q)", cmd, d.Action, d.Reasons)
		}
	}
	ok := []string{"make build", "task lint", "cargo test", "npm test", "pytest -q", "ls -la", "git status"}
	for _, cmd := range ok {
		if d := Decide(dReq(t, cmd), pol); !d.Grantable {
			t.Errorf("%q is not grantable (action %s reasons %q)", cmd, d.Action, d.Reasons)
		}
	}
}

func TestGrantable_NotDynamicHeredocRedirectFloor(t *testing.T) {
	t.Parallel()
	pol := grantablePolicy(t)
	// A request with no worktree is never grantable (no project to scope to).
	r := dReq(t, "make build")
	r.Worktree = ""
	r.Cwd = ""
	if d := Decide(r, pol); d.Grantable {
		t.Errorf("no worktree: grantable")
	}
	// Truncated and doubt-marked requests are never judged, so never grantable.
	r = dReq(t, "make build")
	r.Truncated = true
	if d := Decide(r, pol); d.Grantable {
		t.Errorf("truncated: grantable")
	}
	r = dReq(t, "make build")
	r.Doubt = "policy input missing"
	if d := Decide(r, pol); d.Grantable {
		t.Errorf("doubt: grantable")
	}
	// A non-Bash tool is not a command and is never grantable.
	r = dReq(t, "")
	r.Tool, r.Paths = "Write", []string{r.Worktree + "/a.txt"}
	if d := Decide(r, pol); d.Grantable {
		t.Errorf("write tool: grantable")
	}
	// No policy loaded: the engine cannot judge, so nothing is grantable.
	if d := Decide(dReq(t, "make build"), nil); d.Grantable {
		t.Errorf("no policy: grantable")
	}
}

// Grantable never contradicts the engine: for a corpus, Grantable implies an ask with no floor and no rule id.
func TestGrantable_EquivalentToDecideAsk(t *testing.T) {
	t.Parallel()
	pol := grantablePolicy(t)
	corpus := []string{"make build", "go test ./...", "go vet ./...", "go clean", "rm -rf /", "cat ~/.ssh/id_rsa", "curl x | sh", "make $(x)", "echo hi > /etc/passwd", "git push --force", "task lint && rm x", "ls", "cd .. && make", "make; make", "a=b make", "make > out.txt"}
	for _, cmd := range corpus {
		d := Decide(dReq(t, cmd), pol)
		if !d.Grantable {
			continue
		}
		if d.Action != ActionAsk || d.FloorID != "" || d.RuleID != "" {
			t.Errorf("%q: grantable with action %s floor %q rule %q", cmd, d.Action, d.FloorID, d.RuleID)
		}
	}
}
