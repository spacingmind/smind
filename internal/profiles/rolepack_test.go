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

func TestSeedRoles_CreatesThreeSeatsInOrder(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	res := seed(t, r)
	want := []string{"peer-sonnet", "lead", "validator"}
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
		!strings.Contains(lead.Instructions, "end your turn") || len(lead.Skills) != 1 || lead.Skills[0] != "smind-leader" {
		t.Errorf("lead = %+v", lead)
	}
	v := byName(t, r, "validator")
	if v.PermissionMode != "auto" || v.OrchestrationTools == nil || *v.OrchestrationTools || v.ChildPolicy != "none" || v.Model != "claude-sonnet-5-5" ||
		!strings.Contains(v.Instructions, "validation_verdict_submit") {
		t.Errorf("validator = %+v", v)
	}
	all, _ := r.List()
	for _, p := range all {
		if p.Name == "reviewer" {
			t.Errorf("the pack must no longer seed a reviewer: %+v", p)
		}
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
	if len(all) != 3 {
		t.Errorf("profiles = %d, want 3", len(all))
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

func TestSeedRoles_ExistingSeatKeptUntouched(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	// A Human-made "lead" with a different provider (and no seat contract) is
	// kept untouched.
	pre := mustCreate(t, r, store.AgentProfile{Name: "lead", Provider: "glm", AutoAccept: true})
	res := seed(t, r)
	if res[1].Name != "lead" || res[1].Action != SeedKept || res[1].ID != pre.ID {
		t.Fatalf("lead result = %+v, want kept %d", res[1], pre.ID)
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

// TestSeedRoles_SeatsExactly: the single-agent-loop audit folded the
// Supervisor seats and peer-glm out of the pack (ADR-0041 amendment
// 2026-10-25): the Human or a Steward briefs a Lead directly.
func TestSeedRoles_SeatsExactly(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	all, _ := r.List()
	got := map[string]bool{}
	for _, p := range all {
		got[p.Name] = true
		if strings.HasPrefix(p.Name, "supervisor") || p.Name == "peer-glm" || p.ChildPolicy == "profiles" || p.Provider == "glm" {
			t.Errorf("pack seeds %q (provider %q, childPolicy %q)", p.Name, p.Provider, p.ChildPolicy)
		}
	}
	for _, n := range []string{"lead", "peer-sonnet", "validator"} {
		if !got[n] {
			t.Errorf("pack lacks %s", n)
		}
	}
	if len(all) != 3 {
		t.Errorf("profiles = %d, want exactly lead, peer-sonnet, validator", len(all))
	}
}

// TestSeedRoles_ClaudeSeatsWaitOnRateLimit: every seat re-enqueues the
// cut-off prompt at the reset (the F2 "wait" policy), so a tree heals by
// itself after an account usage limit.
func TestSeedRoles_ClaudeSeatsWaitOnRateLimit(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	for _, n := range []string{"peer-sonnet", "lead", "validator"} {
		if got := byName(t, r, n).OnRateLimit; got != "wait" {
			t.Errorf("%s OnRateLimit = %q, want wait", n, got)
		}
	}
}

// previousLeadInstructionsLen is the byte length of the Lead text before the
// single-agent-loop fold (about 2,240 bytes); the audit counted prompt weight, so the
// new text must stay shorter.
const previousLeadInstructionsLen = 2237

// TestSeedRoles_LeadIsTheSingleLoop: the Lead does S/M work itself, launches
// only peer-sonnet and only for parallel parts or independent verification,
// reads the Peer's json handoff first, and carries no Peer prices or timings.
func TestSeedRoles_LeadIsTheSingleLoop(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	lead := byName(t, r, "lead").Instructions
	for _, want := range []string{"peer-sonnet", "Do small and medium items yourself", "independent verifier", "json block", "missing or invalid block", "Never merge"} {
		if !strings.Contains(lead, want) {
			t.Errorf("lead instructions lack %q", want)
		}
	}
	for _, bad := range []string{"peer-glm", "GLM", "DEFAULT TO", "$0.", "minutes", "Supervisor"} {
		if strings.Contains(lead, bad) {
			t.Errorf("lead instructions still carry %q", bad)
		}
	}
	if len(lead) >= previousLeadInstructionsLen {
		t.Errorf("lead instructions = %d bytes, want fewer than before (%d)", len(lead), previousLeadInstructionsLen)
	}
}

// TestSeedRoles_PeerEndsWithJSONHandoff: the Peer is a tool with a structured
// result, put first so the parent notifier's cut keeps it.
func TestSeedRoles_PeerEndsWithJSONHandoff(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	peer := byName(t, r, "peer-sonnet").Instructions
	for _, want := range []string{"STARTS with exactly one fenced json block", `"status":"done|blocked|failed"`, `"branch"`, `"commits"`, `"tests_run"`, `"not_verified"`, `"notes"`} {
		if !strings.Contains(peer, want) {
			t.Errorf("peer-sonnet instructions lack %q", want)
		}
	}
}

// A seat's own background task finishing does not start a run, so a Lead that
// ends its turn "waiting" on one stalls the tree (dogfood log entry 76): the
// Lead carries the rule, a Peer (which works to completion) is not burdened
// with it.
func TestSeedRoles_LeadNeverWaitsOnBackgroundWork(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	const rule = "Never end a turn waiting on a background command or monitor you started"
	if got := byName(t, r, "lead").Instructions; !strings.Contains(got, rule) || !strings.Contains(got, "hand it to a child task") {
		t.Errorf("lead instructions lack the background-wait rule: %q", got)
	}
	if got := byName(t, r, "peer-sonnet").Instructions; strings.Contains(got, rule) {
		t.Errorf("peer-sonnet instructions carry the Lead rule: %q", got)
	}
}

// TestSeedRoles_ExistingReviewerKept: a Human's own `reviewer` profile (from the
// old pack) is neither touched nor removed when the pack now seeds `validator`.
func TestSeedRoles_ExistingReviewerKept(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	mine, err := r.Create(store.AgentProfile{Name: "reviewer", Provider: "claude-native", Model: "claude-sonnet-5-5", PermissionMode: "plan", Instructions: "my own text"})
	if err != nil {
		t.Fatal(err)
	}
	seed(t, r)
	got := byName(t, r, "reviewer")
	if got.ID != mine.ID || got.Instructions != "my own text" {
		t.Fatalf("reviewer changed: %+v", got)
	}
	if byName(t, r, "validator").ID == 0 {
		t.Fatalf("validator not seeded")
	}
}

// TestSeedRoles_ValidatorNeverOverwritten: re-seeding keeps a Human-edited validator.
func TestSeedRoles_ValidatorNeverOverwritten(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	seed(t, r)
	v := byName(t, r, "validator")
	v.Instructions = "edited by a human"
	if _, err := r.Update(v); err != nil {
		t.Fatal(err)
	}
	res := seed(t, r)
	for _, x := range res {
		if x.Name == "validator" && x.Action != SeedKept {
			t.Fatalf("validator action = %v, want kept", x.Action)
		}
	}
	if byName(t, r, "validator").Instructions != "edited by a human" {
		t.Fatalf("a human edit was overwritten")
	}
}
