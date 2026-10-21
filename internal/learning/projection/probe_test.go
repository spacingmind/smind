package projection

import (
	"errors"
	"io/fs"
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
	// The probe tree carries the plugin layout as well, so its hash differs
	// from the production projection's of the same skills.
	if p, err := Project(newRoot(t), 7, withCandidate); err != nil || p.Hash == b {
		t.Errorf("Project hash = %q (%v) must differ from the probe hash %s", p.Hash, err, b)
	}
}

func TestProbeProjection_PluginLayout(t *testing.T) {
	skipPerms(t)
	now := time.Now()
	att := sandbox.AttestationForTest(t, now)
	active := []Skill{sk("learned-a", "a")}
	withCandidate := append(append([]Skill(nil), active...), sk("learned-cand", "candidate"))

	root := newRoot(t)
	pb, err := ProjectProbe(att, now, root, 7, withCandidate)
	if err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(pb.Dir, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		return string(b)
	}
	if got, want := read(filepath.Join(".claude-plugin", "plugin.json")), `{"name":"smindprobe","version":"0.0.1","description":"smind probe skills"}`+"\n"; got != want {
		t.Errorf("plugin.json = %q, want %q", got, want)
	}
	for _, name := range []string{"learned-a", "learned-cand"} {
		claude := read(filepath.Join(".claude", "skills", name, "SKILL.md"))
		plugin := read(filepath.Join("skills", name, "SKILL.md"))
		if claude != plugin || claude == "" {
			t.Errorf("%s: the two copies differ or are empty", name)
		}
	}
	// Nothing else is in the tree, and everything is read-only.
	var files []string
	_ = filepath.WalkDir(pb.Dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(pb.Dir, p)
			files = append(files, rel)
			if fi, _ := d.Info(); fi.Mode().Perm() != 0o444 {
				t.Errorf("%s is %v, want read-only", rel, fi.Mode().Perm())
			}
		}
		return nil
	})
	if len(files) != 5 {
		t.Errorf("tree has %d files %v, want plugin.json + 2 skills x 2 layouts", len(files), files)
	}
	// Deterministic, and A (no candidate) differs from B.
	pa, err := ProjectProbe(att, now, newRoot(t), 7, active)
	if err != nil {
		t.Fatal(err)
	}
	pb2, err := ProjectProbe(att, now, newRoot(t), 7, withCandidate)
	if err != nil {
		t.Fatal(err)
	}
	if pb.Hash != pb2.Hash || pa.Hash == pb.Hash {
		t.Errorf("hashes: B=%s B again=%s A=%s", pb.Hash, pb2.Hash, pa.Hash)
	}
	// Production Project writes no plugin files.
	pp, err := Project(newRoot(t), 7, withCandidate)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".claude-plugin", "skills"} {
		if _, err := os.Lstat(filepath.Join(pp.Dir, rel)); err == nil {
			t.Errorf("production Project wrote %s", rel)
		}
	}
}
