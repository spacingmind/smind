package steward

import (
	"slices"
	"strings"
	"testing"
)

func TestStewardSeat_NoBashNoWriteTool(t *testing.T) {
	t.Parallel()

	forbidden := []string{"Bash", "BashOutput", "KillShell", "Edit", "MultiEdit", "Write", "NotebookEdit", "WebFetch", "WebSearch", "Task"}
	for _, annotate := range []bool{false, true} {
		for _, f := range forbidden {
			if slices.Contains(SeatAllowedTools(annotate), f) {
				t.Errorf("SeatAllowedTools(%v) contains forbidden tool %q", annotate, f)
			}
		}
	}
	for _, f := range forbidden {
		if !slices.Contains(SeatDisallowedTools, f) {
			t.Errorf("SeatDisallowedTools is missing forbidden tool %q", f)
		}
	}
	if !slices.Equal(SeatBuiltinTools, []string{"Read", "Grep", "Glob"}) {
		t.Fatalf("SeatBuiltinTools = %v, want exactly [Read Grep Glob]", SeatBuiltinTools)
	}
	if !slices.Equal(SeatDisallowedTools, []string{
		"Bash", "BashOutput", "KillShell",
		"Edit", "MultiEdit", "Write", "NotebookEdit",
		"WebFetch", "WebSearch",
		"Task", "SlashCommand", "TodoWrite",
	}) {
		t.Fatalf("SeatDisallowedTools = %v", SeatDisallowedTools)
	}
}

func TestStewardMCPEntry_OnlyTwoToolsByDefault(t *testing.T) {
	t.Parallel()

	two := SeatMCPTools(false)
	if !slices.Equal(two, []string{ToolDigestGet, ToolVerdictSubmit}) {
		t.Fatalf("SeatMCPTools(false) = %v, want [steward_digest_get steward_verdict_submit]", two)
	}
	three := SeatMCPTools(true)
	if !slices.Equal(three, []string{ToolDigestGet, ToolVerdictSubmit, ToolAnnotate}) {
		t.Fatalf("SeatMCPTools(true) = %v, want the two tools plus exactly steward_annotate", three)
	}
	for _, name := range slices.Concat(two, three) {
		for _, banned := range []string{"task_", "approve", "merge", "permission", "validation"} {
			if strings.Contains(name, banned) {
				t.Errorf("seat MCP tool %q contains banned substring %q", name, banned)
			}
		}
	}
}

func TestStewardMCPEntry_TokenOnlyInSeatEnv(t *testing.T) {
	t.Parallel()

	env := SeatMCPEnv(7, "tok123")
	if len(env) != 2 || env[EnvReviewID] != "7" || env[EnvToken] != "tok123" {
		t.Fatalf("SeatMCPEnv(7, tok123) = %v, want exactly %s=7 and %s=tok123", env, EnvReviewID, EnvToken)
	}

	d := Build(base())
	_, sha, err := d.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if prompt := BuildPrompt(d, 7, sha); strings.Contains(prompt, "tok123") {
		t.Fatal("BuildPrompt output contains the seat token")
	}
}
