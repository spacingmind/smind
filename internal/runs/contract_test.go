package runs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/skills"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestBuildInstructions_InstructionsThenSkillBodies(t *testing.T) {
	t.Parallel()
	bundle := []skills.Skill{
		{Name: "alpha", Files: map[string][]byte{"SKILL.md": []byte("---\nname: alpha\ndescription: d\n---\n\n# Alpha body\nline\n")}},
		{Name: "beta", Files: map[string][]byte{"SKILL.md": []byte("---\r\nname: beta\r\n---\r\nBeta body\r\n")}},
	}
	got := buildInstructionsFrom(store.AgentProfile{Instructions: "ROLE", Skills: []string{"beta", "alpha"}}, bundle)
	want := "ROLE\n\n## Skill: beta\n\nBeta body\n\n## Skill: alpha\n\n# Alpha body\nline"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "description: d") || strings.Contains(got, "---") {
		t.Fatalf("frontmatter leaked: %q", got)
	}
}

func TestBuildInstructions_RealBundle(t *testing.T) {
	t.Parallel()
	got, err := buildInstructions(store.AgentProfile{Instructions: "ROLE", Skills: []string{"smind-leader"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "ROLE\n\n## Skill: smind-leader\n\n# Leader / coder pattern") || strings.Contains(got, "user-invocable") {
		t.Fatalf("unexpected text start: %.120q", got)
	}
}

func TestBuildInstructions_UnknownSkillSkipped(t *testing.T) {
	t.Parallel()
	got, err := buildInstructions(store.AgentProfile{Instructions: "ROLE", Skills: []string{"no-such-skill"}})
	if err != nil || got != "ROLE" {
		t.Fatalf("got %q, %v; want ROLE", got, err)
	}
}

func TestBuildInstructions_Empty(t *testing.T) {
	t.Parallel()
	for _, p := range []store.AgentProfile{{}, {Skills: []string{"no-such-skill"}}, {Instructions: "  \n"}} {
		if got, err := buildInstructions(p); err != nil || got != "" {
			t.Fatalf("buildInstructions(%+v) = %q, %v; want empty", p, got, err)
		}
	}
}

// startContractRun runs a GLM prompt on a fresh task with the fake agent
// recording prompt blocks and returns the stored run row and what the agent saw.
func startContractRun(t *testing.T, opts func(st *store.Store) []StartOption) (store.Run, []string, string) {
	t.Helper()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	recordPrompts(t, task)
	reg := newTestRegistry(t, st)
	runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "the user text", taskrunner.PermissionSettings{}, "", opts(st)...)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
	row, err := st.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	return row, promptBlockLines(t, *task.WorktreePath), runID
}

func TestDrive_ProfileInstructionsReachTheRunner(t *testing.T) {
	t.Parallel()
	row, lines, _ := startContractRun(t, func(st *store.Store) []StartOption {
		p, err := st.CreateAgentProfile(store.AgentProfile{Name: "seat", Provider: "glm", Instructions: "ROLE-TEXT-X", Skills: []string{"smind-leader"}})
		if err != nil {
			t.Fatal(err)
		}
		return []StartOption{WithProfile(p.ID)}
	})
	if len(lines) != 1 || !strings.Contains(lines[0], "Role instructions for this session") ||
		!strings.Contains(lines[0], "ROLE-TEXT-X") || !strings.Contains(lines[0], "## Skill: smind-leader") ||
		!strings.Contains(lines[0], "the user text") {
		t.Fatalf("agent saw %.300q", lines)
	}
	if row.Prompt != "the user text" {
		t.Fatalf("runs.prompt = %q", row.Prompt)
	}
}

func TestDrive_LegacyProfileNoContract(t *testing.T) {
	t.Parallel()
	_, lines, _ := startContractRun(t, func(st *store.Store) []StartOption {
		p, err := st.CreateAgentProfile(store.AgentProfile{Name: "legacy", Provider: "glm"})
		if err != nil {
			t.Fatal(err)
		}
		return []StartOption{WithProfile(p.ID)}
	})
	if len(lines) != 1 || strings.Contains(lines[0], "Role instructions") {
		t.Fatalf("agent saw %q", lines)
	}
}

func TestDrive_DeletedProfileNoContract(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	_ = wm
	reg := newTestRegistry(t, st)
	p, err := st.CreateAgentProfile(store.AgentProfile{Name: "gone", Provider: "glm", Instructions: "ROLE"})
	if err != nil {
		t.Fatal(err)
	}
	// Resolve the missing profile exactly as drive does: the profile row is
	// gone by the time the run drives.
	r := &run{id: "r1", profileID: p.ID}
	if err := st.DeleteAgentProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	ctx := reg.withRunContract(context.Background(), r)
	if got := taskrunner.RunContractFrom(ctx).Instructions; got != "" {
		t.Fatalf("contract for a deleted profile = %q", got)
	}
}

func TestDrive_SystemRunNoContract(t *testing.T) {
	t.Parallel()
	_, lines, _ := startContractRun(t, func(st *store.Store) []StartOption {
		p, err := st.CreateAgentProfile(store.AgentProfile{Name: "seat2", Provider: "glm", Instructions: "ROLE"})
		if err != nil {
			t.Fatal(err)
		}
		return []StartOption{WithProfile(p.ID), WithPurpose("reflect")}
	})
	// A system run goes through RunPrompt without isolation here; it must
	// still carry no contract.
	for _, l := range lines {
		if strings.Contains(l, "Role instructions") {
			t.Fatalf("system run saw the role text: %q", l)
		}
	}
}

// policyFor builds the contract the way drive does for a run of profile p.
func policyFor(t *testing.T, p store.AgentProfile) (taskrunner.RunContract, string) {
	t.Helper()
	_, st := newTestWorkspaceManager(t)
	reg := newTestRegistry(t, st)
	reg.SetSmindHome("/daemon/home")
	p.Name, p.Provider = "seat-"+t.Name(), "glm"
	p, err := st.CreateAgentProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	r := &run{id: "run-42", taskID: 7, chatID: 9, profileID: p.ID}
	return taskrunner.RunContractFrom(reg.withRunContract(context.Background(), r)), r.id
}

func boolPtr(b bool) *bool { return &b }

func TestDrive_OrchestrationProfileBuildsMcpPolicyWithRunIdentity(t *testing.T) {
	t.Parallel()
	c, runID := policyFor(t, store.AgentProfile{OrchestrationTools: boolPtr(true)})
	if c.Mcp == nil || !c.Mcp.Orchestration {
		t.Fatalf("Mcp = %+v, want orchestration policy", c.Mcp)
	}
	want := map[string]string{"SMIND_HOME": "/daemon/home", "SMIND_RUN_ID": runID, "SMIND_TASK_ID": "7", "SMIND_CHAT_ID": "9"}
	if len(c.Mcp.Env) != len(want) {
		t.Fatalf("env keys = %d, want %d", len(c.Mcp.Env), len(want))
	}
	for k, v := range want {
		if c.Mcp.Env[k] != v {
			t.Fatalf("env[%s] = %q, want %q", k, c.Mcp.Env[k], v)
		}
	}
}

func TestDrive_PeerProfileHasPolicyWithoutOrchestration(t *testing.T) {
	t.Parallel()
	c, _ := policyFor(t, store.AgentProfile{OrchestrationTools: boolPtr(false)})
	if c.Mcp == nil || c.Mcp.Orchestration || c.Mcp.Env != nil {
		t.Fatalf("Mcp = %+v, want a policy with no orchestration and nil env", c.Mcp)
	}
}

func TestDrive_UnmanagedProfileHasNoMcpPolicy(t *testing.T) {
	t.Parallel()
	c, _ := policyFor(t, store.AgentProfile{Instructions: "ROLE"})
	if c.Mcp != nil || c.Instructions != "ROLE" {
		t.Fatalf("contract = %+v, want instructions only", c)
	}
}
