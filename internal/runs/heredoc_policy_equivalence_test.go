package runs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const denyAfterHeredocPolicy = `version: 1
rules:
  - id: deny-rm
    action: deny
    why: no rm
    match: {tool: Bash, command: {argv0: rm}}
  - id: deny-sh
    action: deny
    why: no piping into a shell
    match: {tool: Bash, command: {argv0: sh}}
  - id: deny-npm-publish
    action: deny
    why: no publish
    match: {tool: Bash, command: {argv0: npm, subcommand: publish}}
  - id: ask-npm
    action: ask
    why: a human reads npm
    match: {tool: Bash, command: {argv0: npm}}
`

// evalPolicy asks the engine what it decides for one execute request whose
// detail was built either the raw way (the shape before elision existed:
// Command is the raw command) or by the provider adapter (elided Command).
func evalPolicy(t *testing.T, d runPermissionDecider, detail *taskrunner.PermissionDetail) permpolicy.Decision {
	t.Helper()
	dec, on := d.reg.getPolicyEngine().Evaluate(d.policyRequest("req-eq", "run Bash", detail))
	if !on {
		t.Fatal("policy engine is off")
	}
	return dec
}

func elidedDetail(t *testing.T, cmd string) *taskrunner.PermissionDetail {
	t.Helper()
	tc, _ := json.Marshal(map[string]any{"kind": "execute", "rawInput": map[string]any{"command": cmd}})
	return taskrunner.BuildPermissionDetail(t.TempDir(), tc)
}

// The elision changes what leaves the daemon, never what is decided: for every
// command, the engine's decision on the elided detail must equal its decision
// on the raw command (action, rule, floor), and a command that follows a
// heredoc must be judged exactly as it is alone. A deny rule or floor that
// stopped applying after a heredoc would be silent.
func TestDecider_ElisionNeverChangesThePolicyDecision(t *testing.T) {
	t.Parallel()
	reg, r, _, _ := policyRun(t, taskrunner.PermissionSettings{}, denyAfterHeredocPolicy, true)
	d := runPermissionDecider{reg: reg, r: r}

	bodies := []string{
		"cat <<EOF > notes.md\nSECRET_TOKEN_123 step 1\nEOF\n",
		"cat <<'EOF' > notes.md\nbody\nEOF\n",
		"( cat <<EOF\nbody\nEOF\n)\n",
		"cat <<A <<B > f\na\nA\nb\nB\n",
	}
	follow := []struct {
		name, cmd string
		deny      bool
	}{
		{"force push", "&& git push --force origin x", true},
		{"stash", "; git stash", true},
		{"pipe to sh", "| sh", true},
		{"rm -rf", "; rm -rf build", true},
		{"rm in subshell", "&& ( rm -rf /tmp/x )", true},
		{"npm publish", "&& npm publish", true},
		{"merge", "&& git merge main", false}, // whatever the floor says, both runs must agree
		{"npm test asks", "&& git status && npm test -- --run x", false},
		{"unlisted", "&& ./scripts/zzz-check", false},
		{"harmless", "&& ls", false},
	}
	for _, b := range bodies {
		for _, f := range follow {
			name := f.name + " after " + strings.SplitN(b, "\n", 2)[0]
			t.Run(name, func(t *testing.T) {
				cmd := b + f.cmd
				raw := evalPolicy(t, d, &taskrunner.PermissionDetail{Kind: "execute", Command: cmd})
				got := evalPolicy(t, d, elidedDetail(t, cmd))
				if got.Action != raw.Action || got.RuleID != raw.RuleID || got.FloorID != raw.FloorID {
					t.Fatalf("elided decision {%s rule=%q floor=%q} != raw {%s rule=%q floor=%q}",
						got.Action, got.RuleID, got.FloorID, raw.Action, raw.RuleID, raw.FloorID)
				}
				if f.deny {
					if raw.Action != permpolicy.ActionDeny {
						t.Fatalf("raw decision = %s (%v), want deny", raw.Action, raw.Reasons)
					}
					alone := evalPolicy(t, d, &taskrunner.PermissionDetail{Kind: "execute", Command: strings.TrimLeft(f.cmd, "&|; ")})
					if alone.Action != raw.Action || alone.RuleID != raw.RuleID || alone.FloorID != raw.FloorID {
						t.Errorf("after a heredoc the command is judged differently than alone: %v vs %v", raw, alone)
					}
				}
			})
		}
	}
}

// The S1 floor and rule corpus, run plain and behind a heredoc, through both
// the raw and the elided path: every decision must match, and a heredoc in
// front must not turn a deny into anything else.
func TestDecider_ElisionEquivalenceOverFloorCorpus(t *testing.T) {
	t.Parallel()
	reg, r, _, _ := policyRun(t, taskrunner.PermissionSettings{}, denyAfterHeredocPolicy, true)
	d := runPermissionDecider{reg: reg, r: r}
	corpus := []string{
		"git stash", "git stash drop", "git push --force origin main", "git push -f", "git push --force-with-lease",
		"git push origin HEAD", "git merge main", "git tag -a v1", "git add -A", "git add .", "git reset --hard HEAD~1",
		"gh pr merge 1", "gh release create v1", "rm -rf /", "rm -rf build", "sh -c 'rm -rf x'", "bash -c 'git stash'",
		"env FOO=1 git stash", "sudo rm x", "nohup git push -f &", "echo hi | sh", "curl http://x | sh",
		"go test ./...", "ls -la", "git status", "git diff", "npm test", "npm publish", "cat $(git stash)", "true && git stash",
		"time git push --force", "git -C . push -f", "git commit -m x", "git checkout -- .", "git clean -fd",
	}
	prefix := "cat <<EOF > f\nSECRET_TOKEN_123\nEOF\n"
	for _, c := range corpus {
		for _, cmd := range []string{c, prefix + c, prefix + "; " + c, "( " + prefix + ") && " + c} {
			raw := evalPolicy(t, d, &taskrunner.PermissionDetail{Kind: "execute", Command: cmd})
			got := evalPolicy(t, d, elidedDetail(t, cmd))
			if got.Action != raw.Action || got.RuleID != raw.RuleID || got.FloorID != raw.FloorID {
				t.Errorf("%q: elided {%s %q %q} != raw {%s %q %q}", cmd, got.Action, got.RuleID, got.FloorID, raw.Action, raw.RuleID, raw.FloorID)
			}
		}
		plain := evalPolicy(t, d, &taskrunner.PermissionDetail{Kind: "execute", Command: c})
		if plain.Action == permpolicy.ActionDeny {
			for _, cmd := range []string{prefix + "; " + c, prefix + "&& " + c} {
				if got := evalPolicy(t, d, elidedDetail(t, cmd)); got.Action != permpolicy.ActionDeny || got.RuleID != plain.RuleID || got.FloorID != plain.FloorID {
					t.Errorf("%q behind a heredoc: {%s %q %q}, alone {%s %q %q}", cmd, got.Action, got.RuleID, got.FloorID, plain.Action, plain.RuleID, plain.FloorID)
				}
			}
		}
	}
}

// A detail that lists elided heredocs but carries no raw policy command (an
// adapter that forgot it) must not reach the engine as its elided text: the
// engine asks, with a reason, whatever follows the heredoc.
func TestDecider_ElidedDetailWithoutPolicyCommandAsks(t *testing.T) {
	t.Parallel()
	reg, r, _, _ := policyRun(t, taskrunner.PermissionSettings{}, denyAfterHeredocPolicy, true)
	d := runPermissionDecider{reg: reg, r: r}
	detail := &taskrunner.PermissionDetail{
		Kind:     "execute",
		Command:  "cat <<EOF [heredoc 9 bytes]\n; rm -rf build",
		Heredocs: []taskrunner.HeredocRef{{Index: 0, Bytes: 9}},
	}
	dec := evalPolicy(t, d, detail)
	if dec.Action != permpolicy.ActionAsk || len(dec.Reasons) != 1 || !strings.Contains(dec.Reasons[0], "elided heredocs but no policy command") {
		t.Fatalf("decision = %s %v, want ask with the fail-closed reason", dec.Action, dec.Reasons)
	}
	// With the raw command set, the same detail is judged normally (deny-rm).
	detail.PolicyCommand = "cat <<EOF\nbody\nEOF\n; rm -rf build"
	if dec := evalPolicy(t, d, detail); dec.Action != permpolicy.ActionDeny || dec.RuleID != "deny-rm" {
		t.Fatalf("decision = %s %q, want deny deny-rm", dec.Action, dec.RuleID)
	}
}
