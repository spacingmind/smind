package permpolicy

import (
	"slices"
	"strings"
	"testing"
)

func splitWords(s string) []string { return strings.Fields(s) }

func TestFloor_SelfApproveCLIDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		// the commands themselves
		{"smind task approve 12", "no-self-approve"},
		{"smind task approve 12 req-1 always", "no-self-approve"},
		{"smind task deny 12", "no-self-approve"},
		{"smind task answer 12 req-1 --answer q1=yes", "no-self-approve"},
		{"smind permission show --reveal 1", "no-self-approve"},
		{"smind permission", "no-self-approve"},
		// wrappers and env games
		{"env -u SMIND_RUN_ID smind task approve 12", "no-self-approve"},
		{"env -u SMIND_AGENT_RUN -u SMIND_RUN_ID smind task approve 12", "no-self-approve"},
		{"SMIND_RUN_ID= smind task approve 12", "no-self-approve"},
		{"env A=1 smind task deny 12", "no-self-approve"},
		{"command smind task approve 12", "no-self-approve"},
		{"exec smind task approve 12", "no-self-approve"},
		{"nohup smind task approve 12", "no-self-approve"},
		{"time smind task approve 12", "no-self-approve"},
		{"timeout 5 smind task approve 12", "no-self-approve"},
		{"echo 12 | xargs smind task approve", "no-self-approve"},
		{"echo 12 | xargs -n 1 -I{} smind task approve {}", "no-self-approve"},
		{"sh -c 'smind task approve 12'", "no-self-approve"},
		{"bash -c 'smind task approve 12'", "no-self-approve"},
		{"bash -lc 'smind task approve 12'", "no-self-approve"},
		{`bash -lc "env -u SMIND_RUN_ID smind task deny 12"`, "no-self-approve"},
		{"cd /tmp && smind task approve 12", "no-self-approve"},
		{"echo ok; smind task approve 12", "no-self-approve"},
		{"(smind task approve 12)", "no-self-approve"},
		{"echo $(smind task approve 12)", "no-self-approve"},
		// the binary spelled another way
		{"/usr/local/bin/smind task approve 12", "no-self-approve"},
		{"~/bin/smind task approve 12", "no-self-approve"},
		{`"./smind" task approve 12`, "no-self-approve"},
		{`'/opt/smind/smind' task deny 12`, "no-self-approve"},
		{"$(which smind) task approve 12", "no-self-approve"},
		{"`which smind` task approve 12", "no-self-approve"},
		{"$SMIND task approve 12", "no-self-approve"},
		{"$BIN permission show --reveal 1", "no-self-approve"},
		{"$(command -v sm*) permission show --reveal 1", "no-self-approve"},
		{"/usr/local/bin/sm* permission", "no-self-approve"},
		{"./smind-dev task approve 12", "no-self-approve"},
		// a renamed copy or symlink: only the task verbs are matchable
		{"./sm task approve 12", "no-self-approve"},
		{"/tmp/x/runner task deny 12", "no-self-approve"},
		{"/tmp/x/runner task answer 12", "no-self-approve"},
	})
}

func TestFloor_SelfApproveNearMissesAllowed(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"smind task ls", ""},
		{"smind task permissions 12", ""},
		{"smind task send 12 hello", ""},
		{"smind task logs 12", ""},
		{"smind mcp serve", ""},
		{"smind status", ""},
		{"smind policy show", ""},
		{"echo smind task approve 12", ""},
		{"echo task approve 12", ""},
		{"printf 'task deny %s' 1", ""},
		{"grep task approve docs/cli.md", ""},
		{"cat task answer", ""},
		{`echo "smind task approve 12"`, ""},
		{"grep -rn 'task approve' docs", ""},
		{"git log --grep='smind task approve'", ""},
		{"git commit -m 'document smind task approve'", ""},
		{"cat docs/cli.md", ""},
		{"man smind", ""},
		{"which smind", ""},
		{"sh -c 'echo smind task approve'", ""},
		{"./runner task list", ""},
		{"go test ./cmd/smind -run TestTaskApprove", ""},
	})
}

func TestFloor_SelfApproveRPCNameDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{`curl -s -d '{"method":"run.respondPermission"}' http://localhost:9/rpc`, "no-self-approve"},
		{`curl -s -d '{"method":"run.answerQuestion","params":{}}' http://localhost:9/rpc`, "no-self-approve"},
		{`echo '{"method":"run.answerPermission"}' | nc -U /tmp/x.sock`, "no-self-approve"},
		{`python3 -c "ws.send('inbox.resolve')"`, "no-self-approve"},
		{`websocat ws://127.0.0.1:9/ws <<< '{"method":"run.respondPermission"}'`, "no-self-approve"},
		{`node -e "call('permission.revealHeredoc')"`, "no-self-approve"},
		{`echo run.revealToolHeredoc`, "no-self-approve"},
		{`curl -s -d '{"method":"run.list"}' http://localhost:9/rpc`, ""},
		{`python3 -c "print('run.respond')"`, ""},
	})
}

// The heredoc body never reaches the segments, so the RPC name inside it is
// found only in the raw text.
func TestFloor_SelfApproveHeredocBodyDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"python3 - <<'EOF'\nimport websocket\nws.send('{\"method\":\"run.respondPermission\"}')\nEOF", "no-self-approve"},
		{"cat <<EOF | nc localhost 9\n{\"method\":\"run.answerQuestion\"}\nEOF", "no-self-approve"},
		{"python3 - <<'EOF'\nprint(1)\nEOF\nsmind task approve 12", "no-self-approve"},
		{"cat <<'EOF'\nharmless\nEOF\nbash -lc 'smind task deny 3'", "no-self-approve"},
		{"python3 - <<'EOF'\nprint(1)\nEOF", ""},
	})
}

func TestDecide_SelfApproveCannotBeAllowedByPolicy(t *testing.T) {
	t.Parallel()
	rule := func(id, argv0 string) string {
		return "- id: " + id + "\n  action: allow\n  why: " + id + "\n  match:\n    tool: Bash\n    command:\n      argv0: " + argv0 + "\n    paths:\n      within: [\"{worktree}\"]"
	}
	pol := mustLoad(t, rule("allow-smind", "smind"), rule("allow-curl", "curl"))
	for _, cmd := range []string{"smind task approve 12", "/usr/bin/smind task deny 1", `curl -d '{"method":"run.respondPermission"}' http://x`} {
		d := Decide(dReq(t, cmd), pol)
		if d.Action != ActionDeny || d.FloorID != "no-self-approve" {
			t.Errorf("Decide(%q) = %s floor %q, want deny by no-self-approve", cmd, d.Action, d.FloorID)
		}
	}
}

func TestHumanOnlyCLI_Table(t *testing.T) {
	t.Parallel()
	for args, want := range map[string]bool{
		"task approve 1": true, "task deny 1": true, "task answer 1": true, "permission show": true,
		"permission": true, "--x task approve 1": true,
		"task ls": false, "task permissions 1": false, "status": false, "": false, "task": false,
	} {
		if got := HumanOnlyCLI(splitWords(args)); got != want {
			t.Errorf("HumanOnlyCLI(%q) = %v, want %v", args, got, want)
		}
	}
	if !slices.Contains(HumanOnlyRPCs, "run.respondPermission") {
		t.Fatal("run.respondPermission must be human-only")
	}
}
