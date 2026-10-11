package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func newHome(t *testing.T) string {
	t.Helper()
	h, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func targetRoot(home, target string) string {
	return filepath.Join(home, filepath.FromSlash(targetDirs[target]))
}

func mustBundle(t *testing.T) []Skill {
	t.Helper()
	b, err := Bundle()
	if err != nil || len(b) == 0 {
		t.Fatalf("Bundle() = %d skills, err %v", len(b), err)
	}
	return b
}

func TestSkillInstall_WritesBundleToAllTargets(t *testing.T) {
	home := newHome(t)
	if _, err := Install(Options{Home: home}); err != nil {
		t.Fatal(err)
	}
	for _, target := range TargetNames {
		for _, s := range mustBundle(t) {
			dir := filepath.Join(targetRoot(home, target), s.Name)
			for rel, want := range s.Files {
				got, err := os.ReadFile(filepath.Join(dir, rel))
				if err != nil || string(got) != string(want) {
					t.Fatalf("%s/%s/%s: err %v, content mismatch", target, s.Name, rel, err)
				}
			}
			mb, err := os.ReadFile(filepath.Join(dir, ManifestName))
			if err != nil {
				t.Fatalf("manifest missing in %s: %v", dir, err)
			}
			if !strings.Contains(string(mb), `"SKILL.md": "`+hashBytes(s.Files["SKILL.md"])+`"`) {
				t.Fatalf("manifest lacks SKILL.md hash: %s", mb)
			}
			fi, _ := os.Stat(filepath.Join(dir, "SKILL.md"))
			if fi.Mode().Perm() != 0o644 {
				t.Fatalf("SKILL.md mode = %v, want 0644", fi.Mode().Perm())
			}
		}
	}
}

func TestSkillInstall_IdempotentNoRewrite(t *testing.T) {
	home := newHome(t)
	if _, err := Install(Options{Home: home}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(targetRoot(home, "claude"), "smind", "SKILL.md")
	mp := filepath.Join(filepath.Dir(p), ManifestName)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, f := range []string{p, mp} {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Install(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if len(r.Written) != 0 || r.Status != StatusInstalled {
			t.Fatalf("second install: %+v, want nothing written", r)
		}
	}
	for _, f := range []string{p, mp} {
		fi, _ := os.Stat(f)
		if !fi.ModTime().Equal(old) {
			t.Fatalf("%s rewritten on second install", f)
		}
	}
}

func TestSkillInstall_DoesNotClobberUserEditedFile(t *testing.T) {
	home := newHome(t)
	opts := Options{Home: home, Targets: []string{"claude"}}
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(targetRoot(home, "claude"), "smind", "SKILL.md")
	if err := os.WriteFile(p, []byte("my edits\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "my edits\n" {
		t.Fatalf("edited file overwritten: %q", got)
	}
	var found bool
	for _, r := range res {
		if r.Skill == "smind" {
			found = true
			if r.Status != StatusModified || len(r.Skipped) != 1 || r.Skipped[0] != "SKILL.md" {
				t.Fatalf("smind result = %+v, want modified/skipped SKILL.md", r)
			}
		}
	}
	if !found {
		t.Fatal("no result for smind")
	}
}

func TestSkillInstall_ForceOverwritesEdited(t *testing.T) {
	home := newHome(t)
	opts := Options{Home: home, Targets: []string{"claude"}}
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(targetRoot(home, "claude"), "smind", "SKILL.md")
	if err := os.WriteFile(p, []byte("my edits\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts.Force = true
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	var want []byte
	for _, s := range mustBundle(t) {
		if s.Name == "smind" {
			want = s.Files["SKILL.md"]
		}
	}
	if got, _ := os.ReadFile(p); string(got) != string(want) {
		t.Fatal("--force did not restore bundled content")
	}
}

func TestSkillInstall_LeavesOtherSkillsAndUnmanagedFiles(t *testing.T) {
	home := newHome(t)
	root := targetRoot(home, "claude")
	other := filepath.Join(root, "my-skill", "SKILL.md")
	extra := filepath.Join(root, "smind", "notes.md")
	for _, f := range []string{other, extra} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Home: home, Targets: []string{"claude"}}
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(opts); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{other, extra} {
		if b, err := os.ReadFile(f); err != nil || string(b) != "mine\n" {
			t.Fatalf("%s touched: %q %v", f, b, err)
		}
	}
}

func TestSkillUninstall_RemovesOnlyManagedFiles(t *testing.T) {
	home := newHome(t)
	opts := Options{Home: home, Targets: []string{"claude"}}
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	root := targetRoot(home, "claude")
	extra := filepath.Join(root, "smind-advisor", "notes.md")
	edited := filepath.Join(root, "smind-handoff", "SKILL.md")
	os.WriteFile(extra, []byte("mine\n"), 0o644)
	os.WriteFile(edited, []byte("edited\n"), 0o644)

	if _, err := Uninstall(opts); err != nil {
		t.Fatal(err)
	}
	// Fully managed skill dir is gone.
	if _, err := os.Stat(filepath.Join(root, "smind")); !os.IsNotExist(err) {
		t.Fatalf("smind dir should be removed: %v", err)
	}
	// Unmanaged file survives, managed SKILL.md and manifest are gone, dir stays.
	if _, err := os.Stat(extra); err != nil {
		t.Fatalf("unmanaged file removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "smind-advisor", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("managed SKILL.md not removed")
	}
	if _, err := os.Stat(filepath.Join(root, "smind-advisor", ManifestName)); !os.IsNotExist(err) {
		t.Fatal("manifest not removed")
	}
	// User-edited managed file survives.
	if b, _ := os.ReadFile(edited); string(b) != "edited\n" {
		t.Fatalf("edited file removed/changed: %q", b)
	}
}

func TestSkillInstall_RefusesSymlinkEscape(t *testing.T) {
	home := newHome(t)
	outside := newHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}
	_, err := Install(Options{Home: home, Targets: []string{"claude"}})
	if err == nil || !strings.Contains(err.Error(), "outside the home") {
		t.Fatalf("err = %v, want refusal", err)
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("wrote into escaped dir: %v", ents)
	}
	if _, err := Uninstall(Options{Home: home, Targets: []string{"claude"}}); err == nil {
		t.Fatal("uninstall should refuse too")
	}

	// A single skill dir symlinked outside is refused as well.
	home2 := newHome(t)
	root := targetRoot(home2, "agents")
	os.MkdirAll(root, 0o755)
	if err := os.Symlink(outside, filepath.Join(root, "smind")); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(Options{Home: home2, Targets: []string{"agents"}}); err == nil {
		t.Fatal("want refusal for escaping skill dir symlink")
	}
}

func TestSkillStatus_ReportsInstalledAndModified(t *testing.T) {
	home := newHome(t)
	opts := Options{Home: home, Targets: []string{"claude"}}
	statusOf := func(name string) string {
		res, err := Status(opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res {
			if r.Skill == name {
				return r.Status
			}
		}
		t.Fatalf("no status for %s", name)
		return ""
	}
	if got := statusOf("smind"); got != StatusMissing {
		t.Fatalf("before install = %s", got)
	}
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if got := statusOf("smind"); got != StatusInstalled {
		t.Fatalf("after install = %s", got)
	}
	os.WriteFile(filepath.Join(targetRoot(home, "claude"), "smind", "SKILL.md"), []byte("x\n"), 0o644)
	if got := statusOf("smind"); got != StatusModified {
		t.Fatalf("after edit = %s", got)
	}
	if got := statusOf("smind-leader"); got != StatusInstalled {
		t.Fatalf("untouched skill = %s", got)
	}
}

func TestSkillBundle_FrontmatterValid(t *testing.T) {
	nameRe := regexp.MustCompile(`(?m)^name:\s*(\S+)\s*$`)
	descRe := regexp.MustCompile(`(?m)^description:\s*(\S.*)$`)
	for _, s := range mustBundle(t) {
		body, ok := s.Files["SKILL.md"]
		if !ok {
			t.Fatalf("%s: no SKILL.md", s.Name)
		}
		text := string(body)
		if !strings.HasPrefix(text, "---\n") {
			t.Fatalf("%s: SKILL.md does not start with ---", s.Name)
		}
		end := strings.Index(text[4:], "\n---")
		if end < 0 {
			t.Fatalf("%s: frontmatter not closed", s.Name)
		}
		fm := text[4 : 4+end]
		if m := nameRe.FindStringSubmatch(fm); m == nil || m[1] != s.Name {
			t.Fatalf("%s: frontmatter name = %v, want %q", s.Name, m, s.Name)
		}
		if m := descRe.FindStringSubmatch(fm); m == nil || strings.TrimSpace(m[1]) == "" {
			t.Fatalf("%s: empty description", s.Name)
		}
	}
}

// TestSkillUninstall_RefusesSymlinkedSubdirEscape: a manifest entry under a
// subdirectory that is a symlink leaving the home directory must not let
// uninstall delete a file outside it, even when the hash matches.
func TestSkillUninstall_RefusesSymlinkedSubdirEscape(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".claude", "skills", "smind")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}
	m := manifest{Files: map[string]string{"sub/victim.txt": hashBytes([]byte("keep me"))}}
	b, err := marshalManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(Options{Home: home, Targets: []string{"claude"}, Names: []string{"smind"}}); err == nil {
		t.Fatal("Uninstall() error = nil, want a refusal")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("file outside the home was removed: %v", err)
	}
}
