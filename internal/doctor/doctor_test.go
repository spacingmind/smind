package doctor

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type fakeFS struct {
	files map[string]string
	dirs  map[string]bool
}

func newFS() *fakeFS { return &fakeFS{files: map[string]string{}, dirs: map[string]bool{}} }

func (f *fakeFS) mkdir(p string) {
	for ; p != "/" && p != "."; p = filepath.Dir(p) {
		f.dirs[p] = true
	}
}

func (f *fakeFS) write(p, s string) {
	f.files[p] = s
	f.mkdir(filepath.Dir(p))
}

type ent struct{ name string }

func (e ent) Name() string               { return e.name }
func (e ent) IsDir() bool                { return true }
func (e ent) Type() fs.FileMode          { return fs.ModeDir }
func (e ent) Info() (fs.FileInfo, error) { return nil, os.ErrInvalid }

func (f *fakeFS) readDir(p string) ([]os.DirEntry, error) {
	if !f.dirs[p] {
		return nil, os.ErrNotExist
	}
	names := map[string]bool{}
	for d := range f.dirs {
		if filepath.Dir(d) == p {
			names[filepath.Base(d)] = true
		}
	}
	var out []os.DirEntry
	var keys []string
	for n := range names {
		keys = append(keys, n)
	}
	sort.Strings(keys)
	for _, n := range keys {
		out = append(out, ent{n})
	}
	return out, nil
}

func (f *fakeFS) readFile(p string) ([]byte, error) {
	s, ok := f.files[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(s), nil
}

func (f *fakeFS) env(r int, vars map[string]string) Env {
	return Env{
		SmindHome: "/smind", Home: "/home/u", RetentionDays: r,
		Getenv:   func(k string) string { return vars[k] },
		ReadFile: f.readFile, ReadDir: f.readDir,
	}
}

func byID(fs []Finding, id string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.ID == id {
			out = append(out, f)
		}
	}
	return out
}

func one(t *testing.T, fs []Finding, id string) Finding {
	t.Helper()
	got := byID(fs, id)
	if len(got) != 1 {
		t.Fatalf("want one %s finding, got %+v", id, fs)
	}
	return got[0]
}

func TestClaudeUnsetIsWarnAtDefaultRetention(t *testing.T) {
	f := newFS()
	f.mkdir("/home/u/.claude")
	g := one(t, ProviderRetention(f.env(30, nil)), "claude.cleanupPeriodDays")
	if g.Level != LevelWarn || !strings.Contains(g.Message, "30") || !strings.Contains(g.Fix, "cleanupPeriodDays") {
		t.Fatalf("got %+v", g)
	}
}

func TestClaudeSetLowerWarns(t *testing.T) {
	f := newFS()
	f.write("/home/u/.claude/settings.json", `{"cleanupPeriodDays": 7}`)
	g := one(t, ProviderRetention(f.env(30, nil)), "claude.cleanupPeriodDays")
	if g.Level != LevelWarn || !strings.Contains(g.Message, "/home/u/.claude/settings.json") || !strings.Contains(g.Message, "7 days") {
		t.Fatalf("got %+v", g)
	}
}

func TestClaudeSetHigherIsOk(t *testing.T) {
	f := newFS()
	f.write("/home/u/.claude/settings.json", `{"cleanupPeriodDays": 90}`)
	g := one(t, ProviderRetention(f.env(30, nil)), "claude.cleanupPeriodDays")
	if g.Level != LevelOK || g.Fix != "" {
		t.Fatalf("got %+v", g)
	}
}

func TestClaudeLocalOverridesUser(t *testing.T) {
	f := newFS()
	f.write("/home/u/.claude/settings.json", `{"cleanupPeriodDays": 7}`)
	f.write("/home/u/.claude/settings.local.json", `{"cleanupPeriodDays": 400}`)
	g := one(t, ProviderRetention(f.env(30, nil)), "claude.cleanupPeriodDays")
	if g.Level != LevelOK || !strings.Contains(g.Message, "400") {
		t.Fatalf("got %+v", g)
	}
}

func TestClaudeRetentionZeroIsInfo(t *testing.T) {
	f := newFS()
	f.mkdir("/home/u/.claude")
	g := one(t, ProviderRetention(f.env(0, nil)), "claude.cleanupPeriodDays")
	if g.Level != LevelInfo || g.Fix != "" {
		t.Fatalf("got %+v", g)
	}
}

func TestClaudeInvalidSettingsIsInfoNotAWarning(t *testing.T) {
	f := newFS()
	f.write("/home/u/.claude/settings.json", `{not json`)
	fs := ProviderRetention(f.env(30, nil))
	if s := one(t, fs, "claude.settings"); s.Level != LevelInfo || !strings.Contains(s.Message, "could not read") {
		t.Fatalf("got %+v", s)
	}
	if g := one(t, fs, "claude.cleanupPeriodDays"); g.Level != LevelWarn {
		t.Fatalf("default rule must still apply: %+v", g)
	}
}

func TestClaudeInvalidValueTreatedAsUnset(t *testing.T) {
	for _, v := range []string{`0`, `-1`, `"x"`, `2.5`} {
		f := newFS()
		f.write("/home/u/.claude/settings.json", `{"cleanupPeriodDays": `+v+`}`)
		g := one(t, ProviderRetention(f.env(30, nil)), "claude.cleanupPeriodDays")
		if g.Level != LevelWarn || !strings.Contains(g.Message, "30 days (the default)") {
			t.Fatalf("%s: %+v", v, g)
		}
	}
}

func TestInstanceRootsAreChecked(t *testing.T) {
	f := newFS()
	f.write("/smind/providers/x/claude/settings.json", `{"cleanupPeriodDays": 5}`)
	g := one(t, ProviderRetention(f.env(30, nil)), "claude.cleanupPeriodDays")
	if g.Root != "/smind/providers/x/claude" || g.Level != LevelWarn || !strings.Contains(g.Message, "5 days") {
		t.Fatalf("got %+v", g)
	}
}

func TestNoRootNoFinding(t *testing.T) {
	if got := ProviderRetention(newFS().env(30, nil)); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestCodexIsInfoOnly(t *testing.T) {
	f := newFS()
	f.mkdir("/home/u/.codex")
	f.mkdir("/smind/providers/c/codex")
	got := ProviderRetention(f.env(30, nil))
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	for _, g := range got {
		if g.ID != "codex.retention" || g.Level != LevelInfo || g.Fix != "" {
			t.Fatalf("got %+v", g)
		}
	}
}

func TestEnvVarsPickTheRoot(t *testing.T) {
	f := newFS()
	f.mkdir("/home/u/.claude")
	f.mkdir("/home/u/.codex")
	f.write("/cfg/claude/settings.json", `{"cleanupPeriodDays": 90}`)
	f.mkdir("/cfg/codex")
	got := ProviderRetention(f.env(30, map[string]string{"CLAUDE_CONFIG_DIR": "/cfg/claude", "CODEX_HOME": "/cfg/codex"}))
	if len(got) != 2 || got[0].Root != "/cfg/claude" || got[1].Root != "/cfg/codex" {
		t.Fatalf("got %+v", got)
	}
}
