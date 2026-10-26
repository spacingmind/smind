package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

type tripFixture struct {
	repo, skills1, skills2 string
	tw                     *Tripwire
}

func newTripFixture(t *testing.T) *tripFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &tripFixture{repo: filepath.Join(root, "repo"), skills1: filepath.Join(root, "skills1"), skills2: filepath.Join(root, "missing-skills")}
	for _, d := range []string{f.repo, f.skills1, filepath.Join(f.skills1, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	must := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(f.repo, "a.txt"), "a")
	must(filepath.Join(f.skills1, "SKILL.md"), "skill")
	must(filepath.Join(f.skills1, "sub", "x.md"), "x")
	if err := os.Symlink("SKILL.md", filepath.Join(f.skills1, "link")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, f.repo, "init", "-q")
	gitIn(t, f.repo, "add", ".")
	gitIn(t, f.repo, "commit", "-q", "-m", "init")
	f.tw, err = Arm(TripwireConfig{RepoDir: f.repo, HumanSkillDirs: []string{f.skills1, f.skills2}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func kinds(fs []Finding) string {
	var k []string
	for _, f := range fs {
		k = append(k, f.Kind)
	}
	return strings.Join(k, ",")
}

func TestTripwire_CleanIsEmpty(t *testing.T) {
	f := newTripFixture(t)
	if got := f.tw.Check(); len(got) != 0 {
		t.Errorf("clean state produced findings: %v", got)
	}
	if got := f.tw.Check(); len(got) != 0 {
		t.Errorf("a second Check disturbed the checkout: %v", got)
	}
	if _, err := os.Stat(filepath.Join(f.repo, ".git", "index.lock")); err == nil {
		t.Error("index.lock left behind")
	}
}

func TestTripwire_RealCheckoutChangedMarksUnsafe(t *testing.T) {
	cases := map[string]func(t *testing.T, f *tripFixture){
		"modify tracked": func(t *testing.T, f *tripFixture) {
			_ = os.WriteFile(filepath.Join(f.repo, "a.txt"), []byte("changed"), 0o644)
		},
		"add untracked": func(t *testing.T, f *tripFixture) {
			_ = os.WriteFile(filepath.Join(f.repo, "new.txt"), []byte("n"), 0o644)
		},
		"new commit":     func(t *testing.T, f *tripFixture) { gitIn(t, f.repo, "commit", "-q", "--allow-empty", "-m", "x") },
		"new branch ref": func(t *testing.T, f *tripFixture) { gitIn(t, f.repo, "branch", "other") },
		"new tag":        func(t *testing.T, f *tripFixture) { gitIn(t, f.repo, "tag", "v1") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newTripFixture(t)
			mutate(t, f)
			got := f.tw.Check()
			if len(got) == 0 || got[0].Kind != KindRepoChanged {
				t.Errorf("findings = %v, want repo_changed", got)
			}
		})
	}
}

func TestTripwire_HumanSkillDirHashChangedMarksUnsafe(t *testing.T) {
	cases := map[string]func(f *tripFixture){
		"edit":   func(f *tripFixture) { _ = os.WriteFile(filepath.Join(f.skills1, "SKILL.md"), []byte("edited"), 0o644) },
		"add":    func(f *tripFixture) { _ = os.WriteFile(filepath.Join(f.skills1, "sub", "new.md"), nil, 0o644) },
		"delete": func(f *tripFixture) { _ = os.Remove(filepath.Join(f.skills1, "sub", "x.md")) },
		"symlink retarget": func(f *tripFixture) {
			_ = os.Remove(filepath.Join(f.skills1, "link"))
			_ = os.Symlink("sub/x.md", filepath.Join(f.skills1, "link"))
		},
		"chmod":               func(f *tripFixture) { _ = os.Chmod(filepath.Join(f.skills1, "SKILL.md"), 0o600) },
		"missing dir created": func(f *tripFixture) { _ = os.Mkdir(f.skills2, 0o755) },
		"dir removed":         func(f *tripFixture) { _ = os.RemoveAll(f.skills1) },
	}
	if runtime.GOOS == "windows" {
		delete(cases, "chmod") // Windows has no Unix mode bits to change
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newTripFixture(t)
			mutate(f)
			got := f.tw.Check()
			if len(got) != 1 || got[0].Kind != KindSkillsChanged {
				t.Errorf("findings = %v, want one skills_changed", got)
			}
		})
	}
}

func TestTripwire_ViolationLogMarksUnsafe(t *testing.T) {
	if got := CheckInit(Observation{InitTools: AllowedTools()}); len(got) != 0 {
		t.Errorf("clean init flagged: %v", got)
	}
	for name, o := range map[string]Observation{
		"extra tool":     {InitTools: append(AllowedTools(), "WebFetch")},
		"missing tool":   {InitTools: AllowedTools()[1:]},
		"skill missing":  {InitTools: dropTool(AllowedTools(), "Skill")},
		"mcp server":     {InitTools: AllowedTools(), MCPServers: 1},
		"plugin":         {InitTools: AllowedTools(), Plugins: 2},
		"no init at all": {},
	} {
		if got := CheckInit(o); len(got) == 0 || got[0].Kind != KindInitTools {
			t.Errorf("%s: findings = %v", name, got)
		}
	}
}

func TestTripwire_UnreadableRepoFailsClosed(t *testing.T) {
	f := newTripFixture(t)
	if _, err := Arm(TripwireConfig{RepoDir: t.TempDir()}); err == nil {
		t.Error("Arm accepted a directory that is not a repo")
	}
	if _, err := Arm(TripwireConfig{RepoDir: "relative"}); err == nil {
		t.Error("Arm accepted a relative repo dir")
	}
	moved := f.repo + "-moved"
	if err := os.Rename(f.repo, moved); err != nil {
		t.Fatal(err)
	}
	got := f.tw.Check()
	if len(got) == 0 || got[0].Kind != KindRepoChanged || !strings.Contains(got[0].Detail, "cannot re-read") {
		t.Errorf("findings = %v, want a fail-closed repo_changed", got)
	}
}

func dropTool(in []string, drop string) []string {
	var out []string
	for _, s := range in {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}

func TestSandboxTools_SkillIsPartOfTheSurface(t *testing.T) {
	has := false
	for _, n := range AllowedTools() {
		has = has || n == "Skill"
	}
	if !has || len(AllowedTools()) != 8 {
		t.Fatalf("AllowedTools = %v, want the 7 built-ins plus Skill", AllowedTools())
	}
	if got := CheckInit(Observation{InitTools: AllowedTools()}); len(got) != 0 {
		t.Errorf("the allowlist itself was flagged: %v", got)
	}
	// The init list must EQUAL the allowlist: an extra tool is flagged too.
	if got := CheckInit(Observation{InitTools: append(AllowedTools(), "WebFetch")}); len(got) == 0 {
		t.Error("an extra tool beside Skill was not flagged")
	}
}
