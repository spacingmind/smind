package main

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

var humanOnlyInvocations = [][]string{
	{"task", "approve", "run-1"},
	{"task", "approve", "run-1", "req-1", "always"},
	{"task", "deny", "run-1"},
	{"task", "answer", "run-1", "req-1"},
	{"permission", "show", "--reveal", "1"},
	{"permission"},
	{"--x", "task", "approve", "run-1"},
	{"host", "approve", "laptop"},
	{"host", "reject", "laptop"},
	{"relay", "devices", "approve", "abc"},
	{"relay", "devices", "revoke", "abc"},
	{"task", "chat", "fork", "3", "--at", "run-1"},
	{"task", "chat", "fork", "3", "--at", "run-1", "--seeded"},
	{"profile", "add", "--name", "x"},
	{"profile", "edit", "3", "--model", "x"},
	{"profile", "seed", "roles", "--apply"},
	{"profile", "seed", "roles", "--take", "lead", "--apply"},
}

func TestHumanOnlyCommandsRefuseInAgentRun(t *testing.T) {
	for _, env := range []string{taskrunner.AgentRunEnv, taskrunner.RunIDEnv} {
		for _, args := range humanOnlyInvocations {
			t.Run(env+"/"+strings.Join(args, "_"), func(t *testing.T) {
				t.Setenv(taskrunner.AgentRunEnv, "")
				t.Setenv(taskrunner.RunIDEnv, "")
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
	t.Setenv(taskrunner.AgentRunEnv, "")
	t.Setenv(taskrunner.RunIDEnv, "")
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
	t.Setenv(taskrunner.AgentRunEnv, "1")
	for _, args := range [][]string{{"task", "ls"}, {"task", "permissions"}, {"task", "send"}, {"task", "logs"}, {"status"}} {
		stderr := captureStderr(t, func() { _ = run(args) })
		if strings.Contains(stderr, "is for humans") {
			t.Fatalf("run(%v) refused in an agent run: %q", args, stderr)
		}
	}
}

func TestHumanOnlyRefusalNamesTheCommandNotTheFlag(t *testing.T) {
	t.Setenv(taskrunner.AgentRunEnv, "1")
	stderr := captureStderr(t, func() { _ = run([]string{"--x", "task", "approve", "1"}) })
	if !strings.Contains(stderr, "smind task approve is for humans") {
		t.Fatalf("refusal = %q, want it to name `task approve`", stderr)
	}
}
