package main

import (
	"strings"
	"testing"
)

var humanOnlyInvocations = [][]string{
	{"task", "approve", "run-1"},
	{"task", "approve", "run-1", "req-1", "always"},
	{"task", "deny", "run-1"},
	{"task", "answer", "run-1", "req-1"},
	{"permission", "show", "--reveal", "1"},
	{"permission"},
}

func TestHumanOnlyCommandsRefuseInAgentRun(t *testing.T) {
	for _, env := range []string{"SMIND_AGENT_RUN", "SMIND_RUN_ID"} {
		for _, args := range humanOnlyInvocations {
			t.Run(env+"/"+strings.Join(args, "_"), func(t *testing.T) {
				t.Setenv("SMIND_AGENT_RUN", "")
				t.Setenv("SMIND_RUN_ID", "")
				t.Setenv(env, "1")
				var code int
				stderr := captureStderr(t, func() { code = run(args) })
				if code != 1 || !strings.Contains(stderr, "is for humans, not agents") {
					t.Fatalf("run(%v) with %s set = %d, stderr %q; want exit 1 and the refusal", args, env, code, stderr)
				}
			})
		}
	}
}

// Without a marker the commands run: with no daemon and no arguments they get
// as far as their usage line (exit 2), which proves the guard did not fire.
func TestHumanOnlyCommandsRunForHumans(t *testing.T) {
	t.Setenv("SMIND_AGENT_RUN", "")
	t.Setenv("SMIND_RUN_ID", "")
	for _, args := range [][]string{{"task", "approve"}, {"task", "deny"}, {"permission"}} {
		var code int
		stderr := captureStderr(t, func() { code = run(args) })
		if code != 2 || strings.Contains(stderr, "is for humans") {
			t.Fatalf("run(%v) as a human = %d, stderr %q; want its usage (exit 2)", args, code, stderr)
		}
	}
}

// The read and orchestration commands an agent uses are untouched.
func TestHumanOnlyGuardLeavesAgentCommandsAlone(t *testing.T) {
	t.Setenv("SMIND_AGENT_RUN", "1")
	for _, args := range [][]string{{"task", "ls"}, {"task", "permissions"}, {"task", "send"}, {"task", "logs"}, {"status"}} {
		stderr := captureStderr(t, func() { _ = run(args) })
		if strings.Contains(stderr, "is for humans") {
			t.Fatalf("run(%v) refused in an agent run: %q", args, stderr)
		}
	}
}
