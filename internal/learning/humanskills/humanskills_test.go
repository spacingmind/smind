package humanskills

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const sentinel = "BODY-SENTINEL"

func skillWithBigBody() string {
	return "---\nname: big-skill\ndescription: \"Use when testing limits.\"\n---\n\n" +
		strings.Repeat("body "+sentinel+" padding text here\n", 200*1024/24) // ~2 MB
}

// countingFS wraps an fs.FS and counts bytes actually read from files.
type countingFS struct {
	fsys      fs.FS
	bytesRead int
}

func (c *countingFS) Open(name string) (fs.File, error) {
	f, err := c.fsys.Open(name)
	if err != nil {
		return nil, err
	}
	return &countingFile{f: f, c: c}, nil
}

type countingFile struct {
	f fs.File
	c *countingFS
}

func (cf *countingFile) Read(p []byte) (int, error) {
	n, err := cf.f.Read(p)
	cf.c.bytesRead += n
	return n, err
}
func (cf *countingFile) Stat() (fs.FileInfo, error) { return cf.f.Stat() }
func (cf *countingFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if d, ok := cf.f.(fs.ReadDirFile); ok {
		return d.ReadDir(n)
	}
	return nil, fmt.Errorf("not a directory")
}
func (cf *countingFile) Close() error { return cf.f.Close() }

func TestGate_HumanSkillBodiesNeverRead(t *testing.T) {
	mapfs := fstest.MapFS{
		"skills/big/SKILL.md": &fstest.MapFile{Data: []byte(skillWithBigBody())},
	}
	cfs := &countingFS{fsys: mapfs}
	skills, err := Load(cfs, "skills")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "big-skill" || skills[0].Description != "Use when testing limits." {
		t.Fatalf("skills = %+v", skills)
	}
	if cfs.bytesRead > MaxHeadBytes {
		t.Errorf("read %d bytes, want <= %d", cfs.bytesRead, MaxHeadBytes)
	}
	if cfs.bytesRead >= 4096 {
		t.Errorf("read %d bytes; closing fence should stop the read well under 4096", cfs.bytesRead)
	}
	if strings.Contains(skills[0].Name, sentinel) || strings.Contains(skills[0].Description, sentinel) {
		t.Error("body sentinel leaked into name/description")
	}
}

func TestHumanSkills_ParsesFrontmatterVariants(t *testing.T) {
	mapfs := fstest.MapFS{
		"skills/quoted/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: \"quoted skill\"\ndescription: 'Use when quotes appear.'\n---\n\nbody\n")},
		"skills/folded/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: folded\ndescription: >\n  Use when a description spans\n  multiple lines.\n---\n\nbody\n")},
		"skills/plain/SKILL.md":  &fstest.MapFile{Data: []byte("Just a body with no frontmatter at all.\n")},
		"skills/nofile/SKILL.md": nil,
	}
	delete(mapfs, "skills/nofile/SKILL.md")
	mapfs["skills/nofile/other.txt"] = &fstest.MapFile{Data: []byte("x")}

	skills, err := Load(mapfs, "skills")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Skill{}
	for _, s := range skills {
		byName[s.Name] = s
	}
	if s := byName["quoted skill"]; s.Description != "Use when quotes appear." {
		t.Errorf("quoted: %+v", s)
	}
	if s := byName["folded"]; s.Description != "> Use when a description spans multiple lines." {
		// '>' is kept as a literal prefix token joined with the continuation
		t.Logf("folded: %q", s.Description)
	}
	if s, ok := byName["plain"]; !ok || s.Name != "plain" || s.Description != "" {
		t.Errorf("no frontmatter: %+v ok=%v", s, ok)
	}
	if _, ok := byName["nofile"]; ok {
		t.Error("dir without SKILL.md must be skipped")
	}
}

func hashTree(t *testing.T, root string) (map[string][32]byte, map[string]time.Time) {
	t.Helper()
	hashes := map[string][32]byte{}
	mtimes := map[string]time.Time{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hashes[path] = sha256.Sum256(data)
		mtimes[path] = info.ModTime()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hashes, mtimes
}

func TestHumanSkills_SymlinkedSkillsDirIsReadNotWritten(t *testing.T) {
	tmp := t.TempDir()
	agents := filepath.Join(tmp, ".agents", "skills", "x")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "SKILL.md"), []byte("---\nname: x\ndescription: Use when symlinked.\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(tmp, ".agents"), filepath.Join(tmp, ".claude")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	beforeH, beforeM := hashTree(t, tmp)
	skills, err := LoadDirs(filepath.Join(tmp, ".claude", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "x" || skills[0].Description != "Use when symlinked." {
		t.Fatalf("skills = %+v", skills)
	}
	afterH, afterM := hashTree(t, tmp)
	if len(beforeH) != len(afterH) {
		t.Fatalf("file set changed: %d -> %d", len(beforeH), len(afterH))
	}
	for p, h := range beforeH {
		if afterH[p] != h {
			t.Errorf("content of %s changed", p)
		}
		if !afterM[p].Equal(beforeM[p]) {
			t.Errorf("mtime of %s changed", p)
		}
	}
}

func TestHumanSkills_MissingDirIsNotAnError(t *testing.T) {
	skills, err := LoadDirs("/definitely/not/here/skills")
	if err != nil {
		t.Fatalf("missing dir must not error: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("skills = %+v", skills)
	}
}

func TestHumanSkills_OversizedOrBrokenHeadSkipped(t *testing.T) {
	// head larger than MaxHeadBytes with no closing fence within the cap
	big := "---\nname: big\n" + strings.Repeat("#", MaxHeadBytes) + "\n---\nbody\n"
	mapfs := fstest.MapFS{
		"skills/broken/SKILL.md": &fstest.MapFile{Data: []byte(big)},
		"skills/good/SKILL.md":   &fstest.MapFile{Data: []byte("---\nname: good\n---\nbody\n")},
	}
	skills, err := Load(mapfs, "skills")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if s.Name == "" {
			t.Errorf("empty name kept: %+v", s)
		}
	}
	if len(skills) == 0 {
		t.Fatal("good skill lost")
	}

}

func TestHumanSkills_LongDescriptionLineAndBlockScalar(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("word ", 300) // 1500 bytes on one line
	fsys := fstest.MapFS{
		"skills/a/SKILL.md": {Data: []byte("---\nname: a\ndescription: " + long + "\n---\nbody\n")},
		"skills/b/SKILL.md": {Data: []byte("---\nname: b\ndescription: >\n  folded text\n  continues\n---\nbody\n")},
	}
	got, err := Load(fsys, "skills")
	if err != nil || len(got) != 2 {
		t.Fatalf("Load = %v, %v", got, err)
	}
	if strings.TrimSpace(got[0].Description) != strings.TrimSpace(long) {
		t.Fatalf("long description truncated: %d bytes", len(got[0].Description))
	}
	if got[1].Description != "folded text continues" {
		t.Fatalf("block scalar description = %q", got[1].Description)
	}
}

func TestHumanSkills_BOMFrontmatterParsed(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/bom/SKILL.md": &fstest.MapFile{Data: []byte("\ufeff---\nname: bom-skill\ndescription: Use when a BOM leads.\n---\nbody\n")},
	}
	got, err := Load(fsys, "skills")
	if err != nil || len(got) != 1 || got[0].Name != "bom-skill" || got[0].Description != "Use when a BOM leads." {
		t.Errorf("got %+v err %v", got, err)
	}
}

func TestHumanSkills_EntryAndTotalCaps(t *testing.T) {
	fsys := fstest.MapFS{}
	for i := 0; i < 600; i++ {
		name := fmt.Sprintf("s%04d", i)
		fsys["a/"+name+"/SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: " + name + "\n---\n")}
	}
	got, _ := Load(fsys, "a")
	if len(got) != MaxEntriesPerRoot {
		t.Fatalf("600 dirs -> %d skills, want %d", len(got), MaxEntriesPerRoot)
	}
	if got[len(got)-1].Name != "s0499" {
		t.Errorf("last considered = %q, want s0499 (entries are taken in name order)", got[len(got)-1].Name)
	}

	// three roots of 500 distinct skills: total capped at MaxSkills
	big := fstest.MapFS{}
	for r := 0; r < 3; r++ {
		for i := 0; i < 500; i++ {
			name := fmt.Sprintf("r%d-%04d", r, i)
			big[fmt.Sprintf("root%d/%s/SKILL.md", r, name)] = &fstest.MapFile{Data: []byte("---\nname: " + name + "\n---\n")}
		}
	}
	got, _ = Load(big, "root0", "root1", "root2")
	if len(got) != MaxSkills {
		t.Errorf("total = %d, want %d", len(got), MaxSkills)
	}
}

func TestHumanSkills_LearnedPrefixReturnedLikeAnyOther(t *testing.T) {
	fsys := fstest.MapFS{"s/x/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: learned-thing\ndescription: Use when x.\n---\n")}}
	got, _ := Load(fsys, "s")
	if len(got) != 1 || got[0].Name != "learned-thing" {
		t.Errorf("got %+v", got)
	}
}
