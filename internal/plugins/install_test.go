package plugins

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

var bg = context.Background()

func mustInstall(t *testing.T, e *env, arg string) Info {
	t.Helper()
	in, err := e.m.Install(bg, arg)
	if err != nil {
		t.Fatalf("install %s: %v", arg, err)
	}
	return in
}

func sameSnapshot(t *testing.T, label string, before, after []string) {
	t.Helper()
	if strings.Join(after, "|") != strings.Join(before, "|") {
		t.Fatalf("%s: files left behind:\nbefore %v\nafter  %v", label, before, after)
	}
}

func TestPluginInstall_RecordsDisabled_ConsentSummary(t *testing.T) {
	e := newEnv(t)
	src := writePluginDir(t, manifestWith("demo", "1.0.0",
		`"process":{"command":["node","server.js"]},"capabilities":["tasks.read","events.observe"],`+
			`"contributes":{"hooks":[{"event":"run.finished"}],"mcpServers":[{"name":"s","transport":"stdio","command":["node","mcp.js"]}],"skills":["skills/a"]}`),
		map[string]string{"skills/a/SKILL.md": "x", "server.js": "//"})
	in := mustInstall(t, e, src)
	if in.Enabled || in.Active || in.Consented {
		t.Fatalf("install must record DISABLED with no consent: %+v", in)
	}
	c := in.Consent
	if c.DataOnly || len(c.Process) != 2 || c.Process[0] != "node" || len(c.HookEvents) != 1 ||
		c.McpServers[0].Command[0] != "node" || len(c.Capabilities) != 2 || c.Version != "1.0.0" ||
		c.SourceKind != "dir" || c.Hash == "" || c.Fingerprint == "" || c.Source == "" {
		t.Fatalf("consent summary incomplete: %+v", c)
	}
	for _, p := range []string{"smind-plugin.json", "server.js", filepath.Join("skills", "a", "SKILL.md"), "data"} {
		if _, err := os.Stat(filepath.Join(e.home, "plugins", "demo", p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	row, _ := e.st.GetPlugin("demo")
	if row.Enabled || row.ConsentHash != "" {
		t.Fatalf("row: %+v", row)
	}
	if _, err := e.m.Install(bg, src); CodeOf(err) != CodeInstalled {
		t.Fatalf("second install: %v", err)
	}
	data := writePluginDir(t, manifestWith("pure", "1.0.0", `"contributes":{"skills":["s"]}`), map[string]string{"s": "x"})
	if d := mustInstall(t, e, data); !d.Consent.DataOnly {
		t.Fatalf("a data-only plugin must say so: %+v", d.Consent)
	}
}

func TestInstall_Requires_PluginsEnabled(t *testing.T) {
	e := newEnv(t)
	*e.on = false
	src := writePluginDir(t, goodManifest, nil)
	for name, fn := range map[string]func() error{
		"install": func() error { _, err := e.m.Install(bg, src); return err },
		"link":    func() error { _, err := e.m.Link(bg, src); return err },
		"enable":  func() error { _, err := e.m.Enable("demo", "x"); return err },
		"update":  func() error { _, err := e.m.Update(bg, "demo", "", false); return err },
		"reload":  func() error { _, err := e.m.Reload("demo"); return err },
	} {
		if err := fn(); CodeOf(err) != CodeSwitchOff {
			t.Errorf("%s with the switch off: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(e.home, "plugins")); err == nil {
		t.Fatal("a refused install created the plugins dir")
	}
}

func TestEnable_RequiresConfirmation(t *testing.T) {
	e := newEnv(t)
	in := mustInstall(t, e, writePluginDir(t, goodManifest, nil))
	for _, bad := range []string{"", "nope", in.Consent.Hash} {
		if _, err := e.m.Enable("demo", bad); CodeOf(err) != CodeConsent {
			t.Errorf("confirm %q: %v", bad, err)
		}
	}
	if got, _ := e.m.Get("demo"); got.Enabled {
		t.Fatal("enabled without confirmation")
	}
	got, err := e.m.Enable("demo", in.Consent.Fingerprint)
	if err != nil || !got.Enabled || !got.Active || !got.Consented {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := e.m.Enable("ghost", "x"); CodeOf(err) != CodeNotFound {
		t.Fatalf("%v", err)
	}
	if d, _ := e.m.Disable("demo"); d.Enabled {
		t.Fatal("disable")
	}
}

func TestGlobalSwitchOff_NothingLoadedListedOrCallable(t *testing.T) {
	e := newEnv(t)
	in := mustInstall(t, e, writePluginDir(t, goodManifest, nil))
	if _, err := e.m.Enable("demo", in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if len(e.m.Active()) != 1 {
		t.Fatal("setup: not active")
	}
	*e.on = false
	l, _ := e.m.List()
	if l.GlobalEnabled || len(l.Plugins) != 1 || l.Plugins[0].Active {
		t.Fatalf("list must report the switch and no active plugin: %+v", l)
	}
	if len(e.m.Active()) != 0 {
		t.Fatal("Active() returned plugins with the switch off")
	}
	if _, err := e.m.Disable("demo"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Remove("demo", false); err != nil {
		t.Fatal(err)
	}
	if New(Options{Home: e.home, Store: e.st, Version: "0.15.0"}).GlobalEnabled() {
		t.Fatal("a nil switch must be off")
	}
}

func TestStagedInstall_FailureLeavesNothing(t *testing.T) {
	e := newEnv(t)
	before := snapshot(t, e.home)
	for _, point := range []string{"staged", "published"} {
		e.m.failAt = func(p string) error {
			if p == point {
				return errors.New("injected failure after " + point)
			}
			return nil
		}
		src := writePluginDir(t, goodManifest, map[string]string{"a.txt": "a"})
		if _, err := e.m.Install(bg, src); err == nil || !strings.Contains(err.Error(), "injected") {
			t.Fatalf("%s: %v", point, err)
		}
		sameSnapshot(t, point, before, snapshot(t, e.home))
		if _, err := e.st.GetPlugin("demo"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("%s: a row was left behind: %v", point, err)
		}
	}
	e.m.failAt = nil
	bad := writePluginDir(t, `{"id":"BAD"}`, nil)
	if _, err := e.m.Install(bg, bad); CodeOf(err) != CodeBadManifest {
		t.Fatal(err)
	}
	sameSnapshot(t, "bad manifest", before, snapshot(t, e.home))
	mustInstall(t, e, writePluginDir(t, goodManifest, nil))
}

func TestPluginInstall_Archive_Sha256Pinning(t *testing.T) {
	e := newEnv(t)
	arch := buildTar(t, true, []ent{{name: "smind-plugin.json", body: goodManifest}, {name: "a.txt", body: "a"}})
	path := writeFile(t, filepath.Join(t.TempDir(), "p.tgz"), arch)

	t.Run("matching pin installs", func(t *testing.T) {
		in := mustInstall(t, e, path+"#sha256="+sum256(arch))
		if in.SourceKind != "archive" || !strings.HasSuffix(in.Source, "#sha256="+sum256(arch)) {
			t.Fatalf("%+v", in)
		}
		if b, _ := os.ReadFile(filepath.Join(e.home, "plugins", "demo", "a.txt")); string(b) != "a" {
			t.Fatal("not extracted")
		}
		if err := e.m.Remove("demo", false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("one flipped byte: hash_mismatch and nothing extracted", func(t *testing.T) {
		before := snapshot(t, e.home)
		flipped := append([]byte(nil), arch...)
		flipped[len(flipped)/2] ^= 0x01
		bad := writeFile(t, filepath.Join(t.TempDir(), "bad.tgz"), flipped)
		_, err := e.m.Install(bg, bad+"#sha256="+sum256(arch)) // the pin of the GOOD bytes
		wantCode(t, err, CodeHashMismatch)
		sameSnapshot(t, "hash mismatch", before, snapshot(t, e.home))
	})
	t.Run("unpinned or malformed pin", func(t *testing.T) {
		_, err := e.m.Install(bg, path)
		wantCode(t, err, CodeUnpinnedArchive)
		_, err = e.m.Install(bg, path+"#sha256=abc")
		wantCode(t, err, CodeUnpinnedArchive)
		_, err = e.m.Install(bg, "http://example.invalid/p.tgz#sha256="+sum256(arch))
		wantCode(t, err, CodeBadSource)
		_, err = e.m.Install(bg, "https://u:pw@example.invalid/p.tgz#sha256="+sum256(arch))
		wantCode(t, err, CodeBadSource)
	})
	t.Run("a pinned but malicious archive is still extracted safely", func(t *testing.T) {
		evil := buildTar(t, true, []ent{{name: "smind-plugin.json", body: goodManifest}, {name: "../../escape", body: "x"}})
		p := writeFile(t, filepath.Join(t.TempDir(), "evil.tgz"), evil)
		_, err := e.m.Install(bg, p+"#sha256="+sum256(evil))
		wantCode(t, err, CodeUnsafeEntry)
		if _, serr := os.Stat(filepath.Join(e.home, "escape")); serr == nil {
			t.Fatal("escaped")
		}
	})
	t.Run("zip works too", func(t *testing.T) {
		z := buildZip(t, []ent{{name: "smind-plugin.json", body: manifestWith("zipped", "1.0.0", "")}})
		p := writeFile(t, filepath.Join(t.TempDir(), "z.zip"), z)
		if in := mustInstall(t, e, p+"#sha256="+sum256(z)); in.ID != "zipped" {
			t.Fatal(in)
		}
	})
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func fileURL(repo string) string {
	u := "file://" + filepath.ToSlash(repo)
	if !strings.HasPrefix(u, "file:///") {
		u = "file:///" + strings.TrimPrefix(u, "file://")
	}
	return u
}

func TestPluginInstall_Git_UnpinnedRefRejected(t *testing.T) {
	e := newEnv(t)
	for _, arg := range []string{
		"git+https://example.invalid/r.git",
		"git+https://example.invalid/r.git@main",
		"git+https://example.invalid/r.git@v1.0.0",
		"git+https://example.invalid/r.git@HEAD",
		"git+https://example.invalid/r.git@abc1234",
		"git+https://example.invalid/r.git@" + strings.Repeat("A", 40),
		"git+https://example.invalid/r.git@" + strings.Repeat("a", 39),
		"git+https://example.invalid/r.git#" + strings.Repeat("a", 40),
		"git+https://example.invalid/r.git@refs/heads/main@" + strings.Repeat("g", 40),
	} {
		before := snapshot(t, e.home)
		_, err := e.m.Install(bg, arg)
		wantCode(t, err, CodeUnpinnedRef)
		sameSnapshot(t, arg, before, snapshot(t, e.home))
	}
	for _, arg := range []string{
		"git+ext::sh -c id@" + strings.Repeat("a", 40),
		"git+--upload-pack=x@" + strings.Repeat("a", 40),
		"git+http://example.invalid/r.git@" + strings.Repeat("a", 40),
		"git+ssh://git@example.invalid/r.git@" + strings.Repeat("a", 40),
		"git+https://u:p@example.invalid/r.git@" + strings.Repeat("a", 40),
		"git+/local/path@" + strings.Repeat("a", 40),
	} {
		if _, err := e.m.Install(bg, arg); CodeOf(err) != CodeBadSource {
			t.Errorf("%q: %v", arg, err)
		}
	}
}

func TestPluginInstall_Git_PinnedSHA_VerifiesHead(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, ManifestFile), []byte(manifestWith("gitty", "1.0.0", "")))
	writeFile(t, filepath.Join(repo, "v.txt"), []byte("one"))
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "one")
	pinned := gitRun(t, repo, "rev-parse", "HEAD")
	// The branch moves on after the Human pinned the first commit.
	writeFile(t, filepath.Join(repo, "v.txt"), []byte("two"))
	writeFile(t, filepath.Join(repo, ManifestFile), []byte(manifestWith("gitty", "2.0.0", "")))
	gitRun(t, repo, "commit", "-q", "-am", "two")
	tip := gitRun(t, repo, "rev-parse", "HEAD")
	url := fileURL(repo)

	t.Run("the pinned commit, not the moved branch", func(t *testing.T) {
		in := mustInstall(t, e, "git+"+url+"@"+pinned)
		if in.Version != "1.0.0" || in.SourceKind != "git" || !strings.HasSuffix(in.Source, "@"+pinned) {
			t.Fatalf("%+v", in)
		}
		if b, _ := os.ReadFile(filepath.Join(e.home, "plugins", "gitty", "v.txt")); string(b) != "one" {
			t.Fatalf("installed %q, want the pinned content", b)
		}
		if _, err := os.Stat(filepath.Join(e.home, "plugins", "gitty", ".git")); err == nil {
			t.Fatal(".git was copied")
		}
		if err := e.m.Remove("gitty", false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a sha that is not in the repo fails and leaves nothing", func(t *testing.T) {
		before := snapshot(t, e.home)
		_, err := e.m.Install(bg, "git+"+url+"@"+strings.Repeat("1", 40))
		wantCode(t, err, CodeGit)
		sameSnapshot(t, "wrong sha", before, snapshot(t, e.home))
	})
	t.Run("the tip installs when pinned explicitly", func(t *testing.T) {
		if in := mustInstall(t, e, "git+"+url+"@"+tip); in.Version != "2.0.0" {
			t.Fatal(in.Version)
		}
	})
}

func TestGit_ScrubbedEnv(t *testing.T) {
	t.Setenv("GIT_ASKPASS", "/bin/evil")
	t.Setenv("SSH_ASKPASS", "/bin/evil")
	t.Setenv("GIT_SSH_COMMAND", "evil")
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("SMIND_TOKEN", "secret")
	env := strings.Join(gitEnv(t.TempDir()), "\n")
	for _, bad := range []string{"/bin/evil", "GIT_DIR", "SMIND_TOKEN", "secret", "GIT_SSH_COMMAND=evil"} {
		if strings.Contains(env, bad) {
			t.Errorf("git env leaks %q:\n%s", bad, env)
		}
	}
	for _, need := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file:https"} {
		if !strings.Contains(env, need) {
			t.Errorf("git env lacks %s", need)
		}
	}
}

func TestPluginInstall_Git_NoFilterOrHookExecution(t *testing.T) {
	requireGit(t)
	e := newEnv(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	marker := filepath.Join(t.TempDir(), "ran")
	writeFile(t, filepath.Join(repo, ManifestFile), []byte(goodManifest))
	writeFile(t, filepath.Join(repo, ".gitattributes"), []byte("* filter=evil\n"))
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "x")
	sha := gitRun(t, repo, "rev-parse", "HEAD")
	// A config in the SOURCE repo must not influence our clone.
	gitRun(t, repo, "config", "filter.evil.smudge", "sh -c 'touch "+marker+"'")
	mustInstall(t, e, "git+"+fileURL(repo)+"@"+sha)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a filter command from the source repo ran")
	}
}

func TestPluginSourceLocalPath_HumanExplicitOnly(t *testing.T) {
	for _, arg := range []string{"", "   ", "does-not-exist", "a\nb", "git"} {
		if _, err := ParseSource(arg, false); err == nil {
			t.Errorf("%q accepted", arg)
		}
	}
	dir := t.TempDir()
	s, err := ParseSource(dir, false)
	if err != nil || s.Kind != SourceDir || s.Spec != dir {
		t.Fatalf("%+v %v", s, err)
	}
	l, err := ParseSource(dir, true)
	if err != nil || l.Kind != SourceLink {
		t.Fatalf("%+v %v", l, err)
	}
	if _, err := ParseSource("git+https://x/y.git@"+strings.Repeat("a", 40), true); err == nil {
		t.Fatal("link accepted a git source")
	}
	file := writeFile(t, filepath.Join(dir, "a.tgz"), []byte("x"))
	if _, err := ParseSource(file, true); err == nil {
		t.Fatal("link accepted a file")
	}
}

func TestWorkspaceRepoContents_NeverInstallAsPlugin(t *testing.T) {
	e := newEnv(t)
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ManifestFile), []byte(manifestWith("sneaky", "1.0.0", `"process":{"command":["sh","-c","touch pwned"]}`)))
	writeFile(t, filepath.Join(repo, "plugin.json"), []byte(goodManifest))
	writeFile(t, filepath.Join(repo, ".smind-plugin", ManifestFile), []byte(goodManifest))
	writeFile(t, filepath.Join(repo, ".smind", "plugins", "x", ManifestFile), []byte(goodManifest))
	if _, err := e.st.CreateWorkspace(store.Workspace{Path: repo, Title: "w", RoutingPolicy: "manual"}); err != nil {
		t.Fatalf("register the workspace: %v", err)
	}
	e.m.Start()
	l, err := e.m.List()
	if err != nil || len(l.Plugins) != 0 || len(e.m.Active()) != 0 {
		t.Fatalf("a repository's files became plugins: %+v %v", l, err)
	}
	if _, err := os.Stat(filepath.Join(e.home, "plugins")); err == nil {
		t.Fatal("startup created or populated the plugins dir from a repo")
	}
	if _, err := os.Stat(filepath.Join(repo, "pwned")); err == nil {
		t.Fatal("repo code ran")
	}
	if _, err := e.m.Enable("sneaky", "x"); CodeOf(err) != CodeNotFound {
		t.Fatalf("%v", err)
	}
	if _, err := e.m.Update(bg, "sneaky", repo, false); CodeOf(err) != CodeNotFound {
		t.Fatalf("%v", err)
	}
	// It becomes a plugin only when the Human passes it explicitly, and then
	// it is still disabled.
	if in := mustInstall(t, e, repo); in.Enabled {
		t.Fatal("explicit install must still be disabled")
	}
}
