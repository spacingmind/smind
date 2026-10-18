package projection

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/gate"
)

func sk(name, body string) Skill {
	return Skill{Name: name, Category: "go", Description: "Use when testing " + name, Body: body}
}

// makeWritable undoes the read-only modes so cleanup and tampering work.
func makeWritable(path string) {
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			_ = os.Chmod(p, 0o755)
		} else {
			_ = os.Chmod(p, 0o644)
		}
		return nil
	})
}

func newRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "proj")
	t.Cleanup(func() { makeWritable(root) })
	return root
}

func skipPerms(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based assertions are not meaningful on windows")
	}
}

func TestProjection_IsReadOnlyAndContentAddressed(t *testing.T) {
	root := newRoot(t)
	skills := []Skill{sk("learned-a", "body a"), sk("learned-b", "body b")}
	p1, err := Project(root, 7, skills)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(p1.SkillsDir, "learned-a", "SKILL.md")
	st1, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	p2, err := Project(root, 7, skills)
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Fatalf("same input, different projection: %+v vs %+v", p1, p2)
	}
	st2, _ := os.Stat(file)
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("file rewritten on identical input")
	}
	if len(p1.Hash) != 16 {
		t.Fatalf("hash %q", p1.Hash)
	}
	if want := filepath.Join(root, "7", p1.Hash); p1.Dir != want {
		t.Fatalf("dir %q want %q", p1.Dir, want)
	}
	if want := filepath.Join(p1.Dir, ".claude", "skills"); p1.SkillsDir != want {
		t.Fatalf("skills dir %q want %q", p1.SkillsDir, want)
	}
	p3, err := Project(root, 7, []Skill{sk("learned-a", "changed"), sk("learned-b", "body b")})
	if err != nil {
		t.Fatal(err)
	}
	if p3.Hash == p1.Hash || p3.Dir == p1.Dir {
		t.Fatal("changed body must yield a new hash")
	}
	// Order of input must not matter.
	p4, err := Project(root, 7, []Skill{sk("learned-b", "body b"), sk("learned-a", "changed")})
	if err != nil || p4 != p3 {
		t.Fatalf("order changed projection: %+v %v", p4, err)
	}

	skipPerms(t)
	if st2.Mode().Perm() != 0o444 {
		t.Fatalf("file mode %v", st2.Mode().Perm())
	}
	dst, _ := os.Stat(filepath.Join(p1.SkillsDir, "learned-a"))
	if dst.Mode().Perm() != 0o555 {
		t.Fatalf("skill dir mode %v", dst.Mode().Perm())
	}
	top, _ := os.Stat(p1.Dir)
	if top.Mode().Perm() != 0o555 {
		t.Fatalf("dir mode %v", top.Mode().Perm())
	}
	if os.Geteuid() != 0 {
		if err := os.WriteFile(file, []byte("x"), 0o644); err == nil {
			t.Fatal("overwrite of projected file succeeded")
		}
		if err := os.WriteFile(filepath.Join(p1.SkillsDir, "learned-a", "new.md"), []byte("x"), 0o644); err == nil {
			t.Fatal("create in projected dir succeeded")
		}
	}
}

func TestProjection_OnlyGivenSkills(t *testing.T) {
	root := newRoot(t)
	skills := []Skill{sk("learned-one", "1"), sk("learned-two", "2"), sk("learned-three", "3")}
	p, err := Project(root, 1, skills)
	if err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(p.SkillsDir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ents {
		got = append(got, e.Name())
	}
	sort.Strings(got)
	want := []string{"learned-one", "learned-three", "learned-two"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("skills %v want %v", got, want)
	}
	for _, s := range skills {
		b, err := os.ReadFile(filepath.Join(p.SkillsDir, s.Name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != gate.Render(gate.Skill(s)) {
			t.Fatalf("%s content mismatch", s.Name)
		}
	}
}

func TestProjection_EmptyCreatesNothing(t *testing.T) {
	root := newRoot(t)
	for _, in := range [][]Skill{nil, {}} {
		p, err := Project(root, 3, in)
		if err != nil {
			t.Fatal(err)
		}
		if p != (Projection{}) {
			t.Fatalf("got %+v", p)
		}
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("root created for empty skills: %v", err)
	}
}

func TestProjection_EmptyDoesNotRemoveStale(t *testing.T) {
	root := newRoot(t)
	p, err := Project(root, 3, []Skill{sk("learned-a", "x")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Project(root, 3, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Dir); err != nil {
		t.Fatalf("empty Project removed the tree: %v", err)
	}
}

func TestProjection_RejectsBadNameAndTraversal(t *testing.T) {
	root := newRoot(t)
	for _, name := range []string{"../x", "learned-../x", "", "Learned-X", "learned-A", "learned-a/b", "x"} {
		if _, err := Project(root, 1, []Skill{sk(name, "b")}); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if _, err := Project(root, 1, []Skill{sk("learned-a", "1"), sk("learned-a", "2")}); err == nil {
		t.Error("duplicate name accepted")
	}
	if _, err := Project(root, 0, []Skill{sk("learned-a", "1")}); err == nil {
		t.Error("workspace 0 accepted")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("rejected input still touched disk: %v", err)
	}
}

func TestProjection_RefusesSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := Project(link, 1, []Skill{sk("learned-a", "b")}); err == nil {
		t.Fatal("symlink root accepted")
	}
	if ents, _ := os.ReadDir(real); len(ents) != 0 {
		t.Fatalf("wrote through symlink: %v", ents)
	}
	if err := Remove(link, 1); err == nil {
		t.Fatal("Remove accepted symlink root")
	}
}

func TestProjection_RefusesSymlinkWorkspaceDir(t *testing.T) {
	root := newRoot(t)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := t.TempDir()
	if err := os.Symlink(victim, filepath.Join(root, "1")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := Project(root, 1, []Skill{sk("learned-a", "b")}); err == nil {
		t.Fatal("symlinked workspace dir accepted")
	}
	if ents, _ := os.ReadDir(victim); len(ents) != 0 {
		t.Fatalf("wrote through symlink: %v", ents)
	}
	if err := Remove(root, 1); err == nil {
		t.Fatal("Remove followed workspace symlink")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal(err)
	}
}

func TestProjection_RepairsTamperedDir(t *testing.T) {
	skipPerms(t)
	root := newRoot(t)
	skills := []Skill{sk("learned-a", "good body")}
	p, err := Project(root, 1, skills)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(p.SkillsDir, "learned-a", "SKILL.md")
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("evil"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2, err := Project(root, 1, skills)
	if err != nil || p2 != p {
		t.Fatalf("%+v %v", p2, err)
	}
	b, _ := os.ReadFile(file)
	if string(b) != gate.Render(gate.Skill(skills[0])) {
		t.Fatalf("not restored: %q", b)
	}
	st, _ := os.Stat(file)
	if st.Mode().Perm() != 0o444 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}

	// An extra planted file is also repaired.
	if err := os.Chmod(filepath.Join(p.SkillsDir, "learned-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(p.SkillsDir, "learned-a", "extra.md")
	if err := os.WriteFile(extra, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Project(root, 1, skills); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(extra); !os.IsNotExist(err) {
		t.Fatal("extra file survived repair")
	}
}

func TestProjection_GCKeepsNewestTwo(t *testing.T) {
	root := newRoot(t)
	mk := func(body string) Projection {
		p, err := Project(root, 5, []Skill{sk("learned-a", body)})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	age := func(p Projection, d time.Duration) {
		at := time.Now().Add(-d)
		if err := os.Chtimes(p.Dir, at, at); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(p Projection) bool { _, err := os.Stat(p.Dir); return err == nil }
	a := mk("a")
	age(a, 3*time.Hour)
	b := mk("b")
	age(b, 2*time.Hour)
	if !exists(a) || !exists(b) {
		t.Fatal("two projections must both survive")
	}
	c := mk("c")
	age(c, time.Hour)
	if exists(a) || !exists(b) || !exists(c) {
		t.Fatalf("after c: a=%v b=%v c=%v", exists(a), exists(b), exists(c))
	}
	d := mk("d")
	if exists(b) || !exists(c) || !exists(d) {
		t.Fatalf("after d: b=%v c=%v d=%v", exists(b), exists(c), exists(d))
	}
	// Re-projecting an old hash makes it current; it must never GC itself.
	age(c, 5*time.Hour)
	age(d, 4*time.Hour)
	c2 := mk("c")
	if c2 != c || !exists(c) || !exists(d) {
		t.Fatalf("reuse: c=%v d=%v", exists(c), exists(d))
	}
	// Other workspaces are untouched.
	other, err := Project(root, 6, []Skill{sk("learned-a", "z")})
	if err != nil {
		t.Fatal(err)
	}
	mk("e")
	if !exists(other) {
		t.Fatal("GC crossed workspaces")
	}
}

func TestProjection_ConcurrentSameContent(t *testing.T) {
	root := newRoot(t)
	skills := []Skill{sk("learned-a", "a"), sk("learned-b", "b")}
	var wg sync.WaitGroup
	res := make([]Projection, 16)
	errs := make([]error, 16)
	for i := range res {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res[i], errs[i] = Project(root, 9, skills)
		}()
	}
	wg.Wait()
	for i := range res {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if res[i] != res[0] {
			t.Fatalf("divergent results %+v %+v", res[i], res[0])
		}
	}
	ents, _ := os.ReadDir(filepath.Join(root, "9"))
	if len(ents) != 1 {
		t.Fatalf("leftover entries: %v", ents)
	}
}

func TestProjection_RemoveDeletesWorkspaceTree(t *testing.T) {
	root := newRoot(t)
	_, err := Project(root, 2, []Skill{sk("learned-a", "a")})
	if err != nil {
		t.Fatal(err)
	}
	keep, err := Project(root, 3, []Skill{sk("learned-a", "a")})
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "2")); !os.IsNotExist(err) {
		t.Fatalf("tree survived: %v", err)
	}
	if _, err := os.Stat(keep.Dir); err != nil {
		t.Fatal("Remove touched another workspace")
	}

	if err := Remove(root, 2); err != nil {
		t.Fatalf("second Remove: %v", err)
	}
	if err := Remove(filepath.Join(root, "missing"), 2); err != nil {
		t.Fatalf("Remove with absent root: %v", err)
	}
}

func snapshot(t *testing.T, dirs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(p)
			if err != nil {
				t.Fatal(err)
			}
			v := info.ModTime().String() + "|" + info.Mode().String()
			switch {
			case info.Mode()&fs.ModeSymlink != 0:
				tgt, _ := os.Readlink(p)
				v += "|->" + tgt
			case info.Mode().IsRegular():
				b, _ := os.ReadFile(p)
				h := sha256.Sum256(b)
				v += "|" + hex.EncodeToString(h[:])
			}
			out[p] = v
			return nil
		})
	}
	return out
}

func TestNeverTouchesHumanSkills(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	home := filepath.Join(base, "home")
	for p, c := range map[string]string{
		filepath.Join(repo, ".agents", "skills", "human", "SKILL.md"): "human",
		filepath.Join(home, ".claude", "skills", "mine", "SKILL.md"):  "mine",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(".agents", filepath.Join(repo, ".claude")); err != nil {
		t.Skip("symlinks unavailable")
	}
	before := snapshot(t, repo, home)
	root := filepath.Join(base, "projections")
	t.Cleanup(func() { makeWritable(root) })
	_, err := Project(root, 1, []Skill{sk("learned-a", "a")})
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, 1); err != nil {
		t.Fatal(err)
	}

	after := snapshot(t, repo, home)
	if len(before) != len(after) {
		t.Fatalf("entry count changed %d -> %d", len(before), len(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("%s changed: %q -> %q", k, v, after[k])
		}
	}
}
