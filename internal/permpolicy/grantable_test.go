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
		{"an interpreter is not on the list", "python3 -c pass", false},
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

// The allow-list: only listed programs, spelled exactly, with no environment
// prefix, redirect, dynamic word or code-loading flag, are ever grantable.
func TestGrantable_NeverGrantPrograms(t *testing.T) {
	t.Parallel()
	pol := grantablePolicy(t)
	never := []string{
		// network clients and remote fetchers
		"curl https://example.com", "wget -q https://example.com/x", "ssh host ls", "scp a host:b", "rsync -a a b", "gh pr list", "docker ps", "kubectl get pods", "openssl s_client x", "nmap x",
		// destructive, writers, permission changers
		"rm -rf build", "mv a b", "chmod +x run.sh", "dd if=a of=b", "kill 1", "truncate -s 0 f", "tee f", "cp a b", "ln -s a b", "tar x", "unzip x", "sed -i s/a/b/ f", "install a b", "patch -p1",
		// privilege, wrappers, interpreters, command runners
		"sudo make install", "env FOO=1 make", "xargs make", "nohup make &", "timeout 5 make", "sh script.sh", "bash -c make", "python3 x.py", "python3.12 x.py", "node x.js", "nodejs x.js", "java -jar x", "awk 'BEGIN{system(\"rm\")}'", "find . -name x", "find . -delete", "caffeinate rm x", "busybox rm x",
		// fetch-and-run and installers
		"npx foo", "brew install x", "apt-get install x", "npm install", "npm i left-pad", "npm it", "npm create x", "npm init x", "npm exec x", "pnpm add x", "pip install x", "cargo install x", "cargo run", "go get x", "go run x", "go mod tidy", "yarn add x",
		// the smind CLI
		"smind task approve 1",
		// case variants, path-qualified names, environment prefixes, redirects
		"RM -rf build", "Curl https://x", "/BIN/RM x", "Make build", "MAKE build", "./make build", "/usr/bin/make build", "../make build", "/usr/bin/curl https://x",
		"PATH=./bin make build", "LD_PRELOAD=./x.so ls", "FOO=1 make", "NODE_OPTIONS='--require ./x.js' npm test", "make build > Makefile", "make build 2> log", "ls > out",
		// code-loading flags of listed programs
		"go test -exec ./x ./...", "go build -toolexec ./x ./...", "go vet -vettool=./x ./...", "go test -overlay x.json ./...", "pytest -p evil", "pytest -c evil.ini", "pytest -pevil", "eslint -c x.js", "rg --pre ./x foo", "grep --exec x y", "git diff --ext-diff", "git diff --output=x", "git log --exec-path=x",
		// git beyond the read-only set, with global options, or by alias
		"git fetch origin", "git pull", "git clone https://x", "git ls-remote https://x", "git rebase main", "git merge x", "git rm -r .", "git gc --prune=now", "git tag -d v1", "git config user.name x", "git branch -D x",
		"git -c alias.x='!rm -rf .' x", "git -c core.sshCommand=./x fetch", "git -C /tmp status", "git --git-dir=/x status",
		// flags nobody listed: output files, plugins, configs, programs to run, network, writes
		"pytest --basetemp=/tmp/x tests", "pytest --basetemp /tmp/x", "pytest --config-file=/x.ini", "pytest -o addopts=-pevil", "pytest --pdbcls=m:C", "pytest --pyargs pkg", "pytest --junitxml=/x", "pytest /etc/x.py/../../y",
		"tree -o f .", "tsc --outFile /x.js", "tsc --outDir ../x", "tsc --generateTrace d", "tsc -b --clean", "tsc -p ../x/tsconfig.json", "eslint --output-file /x", "eslint -o f", "eslint --parser ./x.js", "eslint --rulesdir d", "eslint -f ./f.js", "eslint --fix", "eslint --init",
		"prettier --write src", "prettier --plugin-search-dir=x src", "rg --hostname-bin /x foo", "rg --pre-glob x foo", "grep -f /x y", "diff --to-file=x a b", "git log --output=x", "git log -Gfoo", "git show --textconv x", "git rev-parse --git-path x", "git log -O/x",
		"go list -m github.com/x/y@latest", "go list -m all", "go list ./...", "go test -c -o x ./...", "go build -o /tmp/x ./...", "go build -ldflags -X=a=b ./...", "go test -c ./...",
		"cargo build --target-dir /tmp/x", "cargo build --manifest-path ../x/Cargo.toml", "make ../../x", "go test ../../elsewhere", "go build ../x", "cat ../x", "ls ..", "ls ../x", "git diff ../x",
		"yarn run curl", "yarn run build", "npm run build --prefix /x", "npm run ../../x", "npm run x@1", "tail -f log", "tail -F log", "date -s x", "file -C", "stat x", "which x", "tree .", "file x",
		"echo $HOME", "ls *.go", "ls {a,b}", "cat ~/x",
		// compound commands with one bad segment
		"make build && curl https://x", "make build | sh", "make build; rm -rf .", "FOO=1 curl https://x",
		// make and task with a flag or non-plain target
		"make -f other.mk build", "make -C /tmp build", "make CC=./evil build", "make 'a b'",
	}
	for _, cmd := range never {
		if d := Decide(dReq(t, cmd), pol); d.Grantable {
			t.Errorf("%q is grantable (action %s reasons %q)", cmd, d.Action, d.Reasons)
		}
	}
	ok := []string{
		"make build", "make -j4 build", "make -k test", "task lint", "task lint:go", "cargo test", "cargo check --workspace", "cargo clippy --all-targets", "npm test", "npm run build", "pnpm test", "yarn test",
		"pytest -q tests/", "pytest -x tests/test_a.py", "pytest -k name tests", "tsc --noEmit", "eslint src", "prettier --check src", "vitest run",
		"go build ./...", "go build -race -tags x ./cmd/...", "go fmt ./...", "go build -n ./...",
		"ls -la", "pwd", "cat README.md", "head -n 5 README.md", "grep -n todo src", "rg todo src", "git status", "git status -s", "git diff --stat", "git log --oneline -n 5", "git show HEAD", "git rev-parse HEAD",
		"make build && make test", "make build | cat", "go build ./... && ls",
	}
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
