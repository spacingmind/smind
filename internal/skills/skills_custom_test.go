package skills

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/testutil"
)

func customSkill() Skill {
	return Skill{Name: "learned-x", Files: map[string][]byte{"SKILL.md": []byte("---\nname: learned-x\n---\n\nbody\n")}}
}

// treeState records every path (lstat) with file content or link target.
func treeState(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			out[rel] = "link:" + l
		case d.IsDir():
			out[rel] = "dir"
		default:
			b, _ := os.ReadFile(p)
			out[rel] = "file:" + string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameState(t *testing.T, a, b map[string]string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("tree changed: %v -> %v", a, b)
	}
	for k, v := range a {
		if b[k] != v {
			t.Fatalf("tree changed at %s", k)
		}
	}
}

func TestInstall_CustomSkills(t *testing.T) {
	home := newHome(t)
	opts := Options{Home: home, Targets: []string{"claude"}, Skills: []Skill{customSkill()}}
	rs, err := Install(opts)
	if err != nil || len(rs) != 1 || rs[0].Status != StatusInstalled {
		t.Fatalf("install: %+v %v", rs, err)
	}
	dir := filepath.Join(targetRoot(home, "claude"), "learned-x")
	if _, err := os.Stat(filepath.Join(dir, ManifestName)); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	before := treeState(t, home)
	rs, err = Install(opts)
	if err != nil || len(rs[0].Written) != 0 {
		t.Fatalf("reinstall wrote: %+v %v", rs, err)
	}
	sameState(t, before, treeState(t, home))

	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, _ = Install(opts)
	if rs[0].Status != StatusModified || len(rs[0].Skipped) != 1 {
		t.Fatalf("want modified, got %+v", rs[0])
	}
	if b, _ := os.ReadFile(p); string(b) != "edited" {
		t.Fatal("edited file clobbered")
	}
	opts.Force = true
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), "edited") {
		t.Fatal("force did not overwrite")
	}
}

func TestInstall_CustomSkillsValidation(t *testing.T) {
	home := newHome(t)
	for _, s := range []Skill{
		{Name: "../evil", Files: map[string][]byte{"SKILL.md": nil}},
		{Name: "a/b", Files: map[string][]byte{"SKILL.md": nil}},
		{Name: "Bad", Files: map[string][]byte{"SKILL.md": nil}},
		{Name: "ok", Files: map[string][]byte{"../x": nil}},
		{Name: "ok", Files: map[string][]byte{"/abs": nil}},
	} {
		if _, err := Install(Options{Home: home, Skills: []Skill{s}}); err == nil {
			t.Errorf("accepted %q %v", s.Name, s.Files)
		}
	}
	if st := treeState(t, home); len(st) != 1 {
		t.Fatalf("wrote something: %v", st)
	}
}

func TestInstall_RepoRootContainment(t *testing.T) {
	root := newHome(t)
	rs, err := Install(Options{Root: root, Skills: []Skill{customSkill()}})
	if err != nil || len(rs) != 1 || rs[0].Target != RepoTarget {
		t.Fatalf("%+v %v", rs, err)
	}
	want := filepath.Join(root, ".agents", "skills", "learned-x")
	if rs[0].Dir != want {
		t.Fatalf("dir %s want %s", rs[0].Dir, want)
	}
	if _, err := os.Stat(filepath.Join(want, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	rs, err = Install(Options{Root: root, RepoSubdir: ".claude/skills", Skills: []Skill{customSkill()}})
	if err != nil || !strings.HasPrefix(rs[0].Dir, filepath.Join(root, ".claude", "skills")) {
		t.Fatalf("%+v %v", rs, err)
	}
	for _, o := range []Options{
		{Root: root, Skills: []Skill{{Name: "..", Files: map[string][]byte{"SKILL.md": nil}}}},
		{Root: root, RepoSubdir: "../x"},
		{Root: "relative/dir"},
		{Root: filepath.Join(root, "missing")},
	} {
		if _, err := Install(o); err == nil {
			t.Errorf("accepted %+v", o)
		}
	}
}

func TestInstall_RepoRefusesSymlinkedSkillsDir(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"claude->agents": func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o755); err != nil {
				t.Fatal(err)
			}
			testutil.Link(t, filepath.Join(root, ".agents"), filepath.Join(root, ".claude"))
		},
		"symlinked skills": func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
				t.Fatal(err)
			}
			testutil.Link(t, filepath.Join(root, "real"), filepath.Join(root, ".claude", "skills"))
		},
		"symlinked skill dir": func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, ".claude", "skills"), 0o755); err != nil {
				t.Fatal(err)
			}
			testutil.Link(t, filepath.Join(root, "real"), filepath.Join(root, ".claude", "skills", "learned-x"))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			root := newHome(t)
			setup(t, root)
			before := treeState(t, root)
			for _, force := range []bool{false, true} {
				_, err := Install(Options{Root: root, RepoSubdir: ".claude/skills", Force: force, Skills: []Skill{customSkill()}})
				if !errors.Is(err, ErrSymlinkedTarget) {
					t.Fatalf("force=%v: err = %v, want ErrSymlinkedTarget", force, err)
				}
			}
			sameState(t, before, treeState(t, root))
		})
	}
}

func TestInstall_BundleBehaviourUnchanged(t *testing.T) {
	home := newHome(t)
	rs, err := Status(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if want := len(mustBundle(t)) * len(TargetNames); len(rs) != want {
		t.Fatalf("default options list %d results, want %d", len(rs), want)
	}
	if got := strings.Join(TargetNames, ","); got != "claude,agents,codex" {
		t.Fatalf("TargetNames = %s", got)
	}
}
