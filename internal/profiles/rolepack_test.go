package profiles

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func seed(t *testing.T, r *Registry) []SeedResult {
	t.Helper()
	existing, err := r.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	res, err := SeedRoles(r, existing)
	if err != nil {
		t.Fatalf("SeedRoles() error = %v", err)
	}
	return res
}

func byName(t *testing.T, r *Registry, name string) store.AgentProfile {
	t.Helper()
	all, err := r.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, p := range all {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("profile %q not found", name)
	return store.AgentProfile{}
}

func TestSeedRoles_CreatesFiveSeatsInOrder(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	res := seed(t, r)
	want := []string{"peer-sonnet", "peer-glm", "lead", "reviewer", "supervisor"}
	if len(res) != len(want) {
		t.Fatalf("results = %+v, want %d", res, len(want))
	}
	for i, n := range want {
		if res[i].Name != n || res[i].Action != SeedCreated || res[i].ID == 0 {
			t.Errorf("result[%d] = %+v, want created %s", i, res[i], n)
		}
		if i > 0 && res[i].ID <= res[i-1].ID {
			t.Errorf("ids not increasing: %+v", res)
		}
	}
	lead := byName(t, r, "lead")
	if lead.ChildPolicy != "leaf" || lead.OrchestrationTools == nil || !*lead.OrchestrationTools ||
		!strings.Contains(lead.Instructions, "END YOUR TURN") || len(lead.Skills) != 1 || lead.Skills[0] != "smind-leader" {
		t.Errorf("lead = %+v", lead)
	}
	if rv := byName(t, r, "reviewer"); rv.PermissionMode != "plan" {
		t.Errorf("reviewer mode = %q", rv.PermissionMode)
	}
}

func TestSeedRoles_IdempotentAndKeepsExisting(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	first := seed(t, r)
	second := seed(t, r)
	if len(second) != len(first) {
		t.Fatalf("second = %+v", second)
	}
	for i, s := range second {
		if s.Action != SeedKept || s.ID != first[i].ID || s.Name != first[i].Name {
			t.Errorf("second[%d] = %+v, want kept %+v", i, s, first[i])
		}
	}
	all, _ := r.List()
	if len(all) != 5 {
		t.Errorf("profiles = %d, want 5", len(all))
	}
}

func TestSeedRoles_EditSurvivesRerun(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	lead := byName(t, r, "lead")
	lead.Notes = "my notes"
	lead.Model = "claude-haiku-4-5"
	if _, err := r.Update(lead); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	seed(t, r)
	got := byName(t, r, "lead")
	if got.Notes != "my notes" || got.Model != "claude-haiku-4-5" {
		t.Errorf("lead after reseed = notes %q model %q", got.Notes, got.Model)
	}
}

func TestSeedRoles_PeerGlmFallsBackToPeerSonnet(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	glm, sonnet := byName(t, r, "peer-glm"), byName(t, r, "peer-sonnet")
	if glm.OnRateLimit != "fallback" || glm.FallbackProfileID == nil || *glm.FallbackProfileID != sonnet.ID {
		t.Errorf("peer-glm = %+v, want fallback to %d", glm, sonnet.ID)
	}
	if !glm.AutoAccept || glm.Provider != "glm" {
		t.Errorf("peer-glm = %+v", glm)
	}
}

func TestSeedRoles_SupervisorChildListIsLead(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	sup, lead := byName(t, r, "supervisor"), byName(t, r, "lead")
	if sup.ChildPolicy != "profiles" || len(sup.ChildProfileIDs) != 1 || sup.ChildProfileIDs[0] != lead.ID {
		t.Errorf("supervisor = %+v, want children [%d]", sup, lead.ID)
	}
	if sup.Model != "" || sup.PermissionMode != "" {
		t.Errorf("supervisor model %q mode %q, want provider defaults", sup.Model, sup.PermissionMode)
	}
}

func TestSeedRoles_NoOpusModel(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	all, _ := r.List()
	for _, p := range all {
		if strings.Contains(strings.ToLower(p.Model), "opus") {
			t.Errorf("%s uses model %q", p.Name, p.Model)
		}
	}
}

func TestSeedRoles_PartialFailureCompletesOnRerun(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	// A Human-made "lead" with a different provider (and no seat contract) is
	// kept untouched, and the supervisor is wired to THAT lead.
	pre := mustCreate(t, r, store.AgentProfile{Name: "lead", Provider: "glm", AutoAccept: true})
	res := seed(t, r)
	if res[2].Name != "lead" || res[2].Action != SeedKept || res[2].ID != pre.ID {
		t.Fatalf("lead result = %+v, want kept %d", res[2], pre.ID)
	}
	sup := byName(t, r, "supervisor")
	if len(sup.ChildProfileIDs) != 1 || sup.ChildProfileIDs[0] != pre.ID {
		t.Errorf("supervisor children = %v, want [%d]", sup.ChildProfileIDs, pre.ID)
	}
	if got := byName(t, r, "lead"); got.Provider != "glm" || got.Instructions != "" {
		t.Errorf("pre-existing lead was modified: %+v", got)
	}
	// Re-running completes nothing new and keeps everything.
	for _, s := range seed(t, r) {
		if s.Action != SeedKept {
			t.Errorf("rerun %+v, want kept", s)
		}
	}
}

// TestSeedRoles_ClaudeSeatsWaitOnRateLimit: the claude-native seats re-enqueue
// the cut-off prompt at the reset (the F2 "wait" policy), so a tree heals by
// itself after an account usage limit (the second in-smind dogfood tree
// stalled for two hours without it). peer-glm keeps its fallback to peer-sonnet.
func TestSeedRoles_ClaudeSeatsWaitOnRateLimit(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	for _, n := range []string{"peer-sonnet", "lead", "reviewer", "supervisor"} {
		if got := byName(t, r, n).OnRateLimit; got != "wait" {
			t.Errorf("%s OnRateLimit = %q, want wait", n, got)
		}
	}
	if got := byName(t, r, "peer-glm").OnRateLimit; got != "fallback" {
		t.Errorf("peer-glm OnRateLimit = %q, want fallback", got)
	}
}

// TestSeedRoles_LeadInstructionsDefaultToSonnetPeers: the dogfood measured
// 28 min / 3.0M input tokens for a GLM step Sonnet does in ~1.5 min, so the
// Lead is told to default to peer-sonnet and treat peer-glm as opt-in.
func TestSeedRoles_LeadInstructionsDefaultToSonnetPeers(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	lead := byName(t, r, "lead").Instructions
	for _, want := range []string{"DEFAULT TO peer-sonnet", "peer-glm only when the Human's brief asks for it"} {
		if !strings.Contains(lead, want) {
			t.Errorf("lead instructions lack %q", want)
		}
	}
	if !strings.Contains(byName(t, r, "peer-glm").Notes, "Opt-in coder") {
		t.Errorf("peer-glm notes = %q, want the opt-in wording", byName(t, r, "peer-glm").Notes)
	}
}

// A seat's own background task finishing does not start a run, so a Lead or
// Supervisor that ends its turn "waiting" on one stalls the tree (dogfood log
// entry 76): both seats carry the rule, a Peer (which works to completion) is
// not burdened with it.
func TestSeedRoles_LeadAndSupervisorNeverWaitOnBackgroundWork(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	const rule = "Never end a turn waiting on a background command or monitor you started"
	for _, name := range []string{"lead", "supervisor"} {
		if got := byName(t, r, name).Instructions; !strings.Contains(got, rule) || !strings.Contains(got, "hand it to a child task") {
			t.Errorf("%s instructions lack the background-wait rule: %q", name, got)
		}
	}
	if got := byName(t, r, "peer-sonnet").Instructions; strings.Contains(got, rule) {
		t.Errorf("peer-sonnet instructions carry the Lead/Supervisor rule: %q", got)
	}
}
