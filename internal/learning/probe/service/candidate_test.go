package probesvc

import (
	"errors"
	"testing"

	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
	"github.com/spacingmind/smind/internal/learning/projection"
	"github.com/spacingmind/smind/internal/store"
)

func active(names ...string) []store.LearnedSkill {
	var out []store.LearnedSkill
	for i, n := range names {
		out = append(out, store.LearnedSkill{ID: int64(i + 1), Name: n, Category: "c", Description: "Use when " + n + ".", Body: "body " + n + "\n"})
	}
	return out
}

func names(s []projection.Skill) []string {
	var out []string
	for _, k := range s {
		out = append(out, k.Name)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCandidateSkills_Apply(t *testing.T) {
	t.Parallel()
	skill := func(n, body string) gate.Skill {
		return gate.Skill{Name: n, Category: "c", Description: "Use when " + n + ".", Body: body}
	}
	cases := []struct {
		name  string
		kind  string
		items []approval.Item
		want  []string
		check func(t *testing.T, got []projection.Skill)
	}{
		{"create appends", "create", []approval.Item{{Intent: gate.Intent{Action: gate.ActionCreate}, Result: skill("new", "n\n")}}, []string{"one", "two", "new"}, nil},
		{"patch replaces target", "patch", []approval.Item{{Intent: gate.Intent{Action: gate.ActionPatch}, TargetSkillID: 2, Result: skill("two", "patched\n")}},
			[]string{"one", "two"}, func(t *testing.T, got []projection.Skill) {
				if got[1].Body != "patched\n" || got[0].Body != "body one\n" {
					t.Fatalf("got %+v", got)
				}
			}},
		{"bundle patches umbrella and drops absorbed", "bundle", []approval.Item{
			{Intent: gate.Intent{Action: gate.ActionPatch}, TargetSkillID: 1, Result: skill("one", "merged\n")},
			{Intent: gate.Intent{Action: gate.ActionDelete, AbsorbedInto: "one"}, TargetSkillID: 2}},
			[]string{"one"}, func(t *testing.T, got []projection.Skill) {
				if got[0].Body != "merged\n" {
					t.Fatalf("got %+v", got)
				}
			}},
		{"delete removes", "patch", []approval.Item{{Intent: gate.Intent{Action: gate.ActionDelete}, TargetSkillID: 1}}, []string{"two"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := active("one", "two")
			got, err := candidateSkills(base, tc.kind, tc.items)
			if err != nil {
				t.Fatal(err)
			}
			if !eq(names(got), tc.want) {
				t.Fatalf("names = %v, want %v", names(got), tc.want)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
			if base[0].Body != "body one\n" || len(base) != 2 {
				t.Fatalf("the active set was mutated: %+v", base)
			}
		})
	}
	if _, err := candidateSkills(active("one"), "patch", []approval.Item{{Intent: gate.Intent{Action: gate.ActionPatch}, TargetSkillID: 9, Result: skill("x", "y")}}); err == nil {
		t.Fatal("missing patch target accepted")
	}
	same := []approval.Item{{Intent: gate.Intent{Action: gate.ActionPatch}, TargetSkillID: 1, Result: gate.Skill{Name: "one", Description: "Use when one.", Body: "body one\n"}}}
	if _, err := candidateSkills(active("one"), "patch", same); !errors.Is(err, errNoChange) {
		t.Fatalf("no-op patch err = %v, want errNoChange", err)
	}
}

func TestProbe_ProjectionHashMatchesRole(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	if _, err := f.st.LandSkill(store.LandSkillInput{
		Skill: store.LearnedSkill{WorkspaceID: f.ws.ID, Name: "learned-existing", Category: "c", Description: "Use when existing.", Body: "b\n", Source: "reflector"},
		Actor: "human", Reason: "seed", Action: "approve"}); err != nil {
		t.Fatal(err)
	}
	s := f.svc(nil)
	cand, err := s.projections(f.prop, sandbox.AttestationForTest(t, s.d.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if cand.a.Hash == "" || cand.b.Hash == "" || cand.a.Hash == cand.b.Hash {
		t.Fatalf("A=%q B=%q: A must exist and differ from B", cand.a.Hash, cand.b.Hash)
	}
	if cand.hashFor("A") != cand.hashFor("A2") || cand.hashFor("A") != cand.a.Hash || cand.hashFor("B") != cand.b.Hash {
		t.Fatalf("role hashes: A=%s A2=%s B=%s", cand.hashFor("A"), cand.hashFor("A2"), cand.hashFor("B"))
	}
	// B alone carries the candidate.
	if cand.name != "learned-skill-a" {
		t.Fatalf("candidate name = %q", cand.name)
	}
	// No attestation, no projection.
	if _, err := s.projections(f.prop, sandbox.Attestation{}); err == nil {
		t.Fatal("projection built without an attestation")
	}
}

func TestCandidateSkills_PatchAppliesOnlyDescriptionAndBody(t *testing.T) {
	t.Parallel()
	items := []approval.Item{{Intent: gate.Intent{Action: gate.ActionPatch}, TargetSkillID: 1,
		Result: gate.Skill{Name: "renamed", Category: "other", Description: "Use when changed.", Body: "new\n"}}}
	got, err := candidateSkills(active("one"), "patch", items)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "one" || got[0].Category != "c" || got[0].Description != "Use when changed." || got[0].Body != "new\n" {
		t.Fatalf("got %+v: only description and body may change", got[0])
	}
	// A stale base version is refused.
	stale := active("one")
	stale[0].Version = 3
	items[0].BaseVersion = 2
	if _, err := candidateSkills(stale, "patch", items); !errors.Is(err, errStaleBase) {
		t.Fatalf("err = %v, want errStaleBase", err)
	}
}
