package projection

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
)

func TestProbeProjection_RequiresAttestation(t *testing.T) {
	skipPerms(t)
	now := time.Now()
	skills := []Skill{sk("learned-a", "a")}

	root := newRoot(t)
	if _, err := ProjectProbe(sandbox.Attestation{}, now, root, 7, skills); !errors.Is(err, sandbox.ErrNoAttestation) {
		t.Fatalf("zero attestation: err = %v, want ErrNoAttestation", err)
	}
	stale := sandbox.AttestationForTest(t, now.Add(-sandbox.AttestationTTL-time.Minute))
	if _, err := ProjectProbe(stale, now, root, 7, skills); !errors.Is(err, sandbox.ErrStaleAttestation) {
		t.Fatalf("stale attestation: err = %v, want ErrStaleAttestation", err)
	}
	future := sandbox.AttestationForTest(t, now.Add(time.Hour))
	if _, err := ProjectProbe(future, now, root, 7, skills); !errors.Is(err, sandbox.ErrStaleAttestation) {
		t.Fatalf("future-dated attestation: err = %v, want ErrStaleAttestation", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		ents, _ := os.ReadDir(root)
		t.Fatalf("a refused projection created %v (stat err %v)", ents, err)
	}

	good := sandbox.AttestationForTest(t, now)
	p, err := ProjectProbe(good, now, root, 7, skills)
	if err != nil || p.Hash == "" {
		t.Fatalf("valid attestation: %+v, %v", p, err)
	}
	if _, err := os.Stat(filepath.Join(p.SkillsDir, "learned-a", "SKILL.md")); err != nil {
		t.Errorf("skill not materialised: %v", err)
	}
	// Empty skills: nothing, even with a valid attestation (arm A with no skills).
	if p, err := ProjectProbe(good, now, newRoot(t), 7, nil); err != nil || p != (Projection{}) {
		t.Errorf("empty skills = %+v, %v", p, err)
	}
	// Even for empty skills the attestation is checked first.
	if _, err := ProjectProbe(sandbox.Attestation{}, now, newRoot(t), 7, nil); err == nil {
		t.Error("empty skills bypassed the attestation check")
	}
}

func TestProbeProjection_HashRecordedOnRun(t *testing.T) {
	skipPerms(t)
	now := time.Now()
	att := sandbox.AttestationForTest(t, now)
	active := []Skill{sk("learned-a", "a")}
	withCandidate := append(append([]Skill(nil), active...), sk("learned-cand", "candidate"))

	hash := func(root string, s []Skill) string {
		t.Helper()
		p, err := ProjectProbe(att, now, root, 7, s)
		if err != nil {
			t.Fatal(err)
		}
		return p.Hash
	}
	// Two separate roots: determinism does not depend on what is on disk.
	a1, a2 := hash(newRoot(t), active), hash(newRoot(t), active)
	rootB := newRoot(t)
	b := hash(rootB, withCandidate)
	if a1 != a2 {
		t.Errorf("same skills, different hashes: %s vs %s", a1, a2)
	}
	if a1 == b {
		t.Errorf("adding the candidate did not change the hash (%s)", a1)
	}
	if b != hash(rootB, withCandidate) {
		t.Error("rebuilding over an existing projection changed the hash")
	}
	// The hash is exactly the production Project's for the same skills.
	if p, err := Project(newRoot(t), 7, withCandidate); err != nil || p.Hash != b {
		t.Errorf("Project hash = %q (%v), want %s", p.Hash, err, b)
	}
}
