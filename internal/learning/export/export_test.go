package export

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/skills"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/testutil"
)

func dir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func skill() store.LearnedSkill {
	return store.LearnedSkill{Name: "learned-x", Category: "go", Description: "do x \"well\"", Body: "# X\n\nsteps\n", Status: "active"}
}

func rendered(s store.LearnedSkill) string {
	return gate.Render(gate.Skill{Name: s.Name, Category: s.Category, Description: s.Description, Body: s.Body})
}

// state hashes every path except those under skip (relative prefixes).
func state(t *testing.T, root string, skip ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		for _, s := range skip {
			if rel == s || strings.HasPrefix(rel, s+string(filepath.Separator)) {
				return nil
			}
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			out[rel] = "link:" + l
		case d.IsDir():
			out[rel] = "dir"
		default:
			b, _ := os.ReadFile(p)
			h := sha256.Sum256(b)
			out[rel] = string(h[:])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func equal(t *testing.T, a, b map[string]string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("tree changed:\n%v\n%v", a, b)
	}
	for k, v := range a {
		if b[k] != v {
			t.Fatalf("tree changed at %s", k)
		}
	}
}

func TestExport_WritesRenderedSkill(t *testing.T) {
	s := skill()
	want := rendered(s)
	cases := map[Target]string{
		TargetClaude: ".claude/skills",
		TargetAgents: ".agents/skills",
		TargetCodex:  ".codex/skills",
		TargetRepo:   ".agents/skills",
	}
	for tg, sub := range cases {
		t.Run(string(tg), func(t *testing.T) {
			base := dir(t)
			req := Request{Skill: s, Target: tg, Home: base, RepoPath: base}
			res, err := Export(req)
			if err != nil || res.Status != skills.StatusInstalled {
				t.Fatalf("%+v %v", res, err)
			}
			d := filepath.Join(base, filepath.FromSlash(sub), s.Name)
			if res.Dir != d {
				t.Fatalf("dir %s want %s", res.Dir, d)
			}
			got, err := os.ReadFile(filepath.Join(d, "SKILL.md"))
			if err != nil || string(got) != want {
				t.Fatalf("content mismatch: %v\n%s", err, got)
			}
			ents, _ := os.ReadDir(d)
			for _, e := range ents {
				if e.Name() != "SKILL.md" && e.Name() != skills.ManifestName {
					t.Fatalf("unexpected file %s", e.Name())
				}
			}
		})
	}
}

func TestExport_Idempotent(t *testing.T) {
	base := dir(t)
	req := Request{Skill: skill(), Target: TargetRepo, RepoPath: base}
	if _, err := Export(req); err != nil {
		t.Fatal(err)
	}
	before := state(t, base)
	res, err := Export(req)
	if err != nil || res.Status != skills.StatusInstalled || len(res.Files) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	equal(t, before, state(t, base))
}

func TestExport_NeverClobbersHumanSkillOfSameName(t *testing.T) {
	for _, tg := range []Target{TargetRepo, TargetClaude} {
		base := dir(t)
		sub := ".agents/skills"
		if tg == TargetClaude {
			sub = ".claude/skills"
		}
		p := filepath.Join(base, sub, "learned-x", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		human := []byte("---\nname: learned-x\n---\nmine\n")
		if err := os.WriteFile(p, human, 0o644); err != nil {
			t.Fatal(err)
		}
		req := Request{Skill: skill(), Target: tg, Home: base, RepoPath: base}
		res, err := Export(req)
		if err != nil || res.Status != skills.StatusModified || len(res.Skipped) != 1 {
			t.Fatalf("%s: %+v %v", tg, res, err)
		}
		if b, _ := os.ReadFile(p); string(b) != string(human) {
			t.Fatalf("%s: human skill changed", tg)
		}
		req.Force = true
		if _, err := Export(req); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(p); string(b) != rendered(skill()) {
			t.Fatalf("%s: force did not overwrite", tg)
		}
	}
}

func TestExport_RefusesSymlinkedRepoSkillsDir(t *testing.T) {
	repo := dir(t)
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Link(t, filepath.Join(repo, ".agents"), filepath.Join(repo, ".claude"))
	before := state(t, repo)
	for _, force := range []bool{false, true} {
		_, err := Export(Request{Skill: skill(), Target: TargetRepo, RepoPath: repo, Force: force})
		if !errors.Is(err, skills.ErrSymlinkedTarget) || !strings.Contains(err.Error(), "real directory") {
			t.Fatalf("err = %v", err)
		}
	}
	equal(t, before, state(t, repo))

	// Real .agents/skills alongside a tracked .claude/skills symlink: works,
	// and the symlink target tree is untouched.
	repo = dir(t)
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills", "human"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "skills", "human", "SKILL.md"), []byte("h"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Link(t, filepath.Join(repo, ".agents", "skills"), filepath.Join(repo, ".claude", "skills"))
	before = state(t, repo, ".agents/skills/learned-x")
	res, err := Export(Request{Skill: skill(), Target: TargetRepo, RepoPath: repo})
	if err != nil || res.Dir != filepath.Join(repo, ".agents", "skills", "learned-x") {
		t.Fatalf("%+v %v", res, err)
	}
	// the symlink resolves to the same dir, so only the new skill dir may differ
	equal(t, before, state(t, repo, ".agents/skills/learned-x"))
}

func TestExport_RepoUsesRealClaudeSkillsWhenNoAgents(t *testing.T) {
	repo := dir(t)
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Export(Request{Skill: skill(), Target: TargetRepo, RepoPath: repo})
	if err != nil || res.Dir != filepath.Join(repo, ".claude", "skills", "learned-x") {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents")); err == nil {
		t.Fatal(".agents created")
	}
}

func TestExport_RepoNeverTouchesOtherFiles(t *testing.T) {
	repo := dir(t)
	for p, c := range map[string]string{"README.md": "r", "src/a.go": "package a", ".git/HEAD": "ref", ".agents/skills/human/SKILL.md": "h", ".gitignore": "x"} {
		fp := filepath.Join(repo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fp, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	skip := ".agents/skills/learned-x"
	before := state(t, repo, skip)
	if _, err := Export(Request{Skill: skill(), Target: TargetRepo, RepoPath: repo}); err != nil {
		t.Fatal(err)
	}
	equal(t, before, state(t, repo, skip))
}

func TestExport_RejectsBadNameAndEmptyBody(t *testing.T) {
	base := dir(t)
	bad := []store.LearnedSkill{skill(), skill(), skill(), skill(), skill()}
	bad[0].Name = "../evil"
	bad[1].Name = "x"
	bad[2].Body = " \n"
	bad[3].Status = "proposed"
	bad[4].Name = "learned-" + strings.Repeat("a", 70)
	for i, s := range bad {
		if _, err := Export(Request{Skill: s, Target: TargetAgents, Home: base}); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	for _, r := range []Request{
		{Skill: skill(), Target: "nope", Home: base},
		{Skill: skill(), Target: TargetRepo, RepoPath: "rel"},
		{Skill: skill(), Target: TargetRepo, RepoPath: filepath.Join(base, "missing")},
	} {
		if _, err := Export(r); err == nil {
			t.Errorf("accepted %+v", r)
		}
	}
	if n := len(state(t, base)); n != 1 {
		t.Fatalf("wrote something: %v", state(t, base))
	}
}

func TestExport_NeverTouchesHumanSkills(t *testing.T) {
	for _, tg := range []Target{TargetClaude, TargetAgents, TargetCodex} {
		base := dir(t)
		sub := map[Target]string{TargetClaude: ".claude", TargetAgents: ".agents", TargetCodex: ".codex"}[tg]
		p := filepath.Join(base, sub, "skills", "human", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		if _, err := Export(Request{Skill: skill(), Target: tg, Home: base}); err != nil {
			t.Fatal(err)
		}
		fi, _ := os.Stat(p)
		b, _ := os.ReadFile(p)
		if string(b) != "mine" || !fi.ModTime().Equal(old) {
			t.Fatalf("%s: human skill touched", tg)
		}
	}
}
