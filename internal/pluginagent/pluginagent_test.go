package pluginagent

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

func TestPluginAgent_BaseEnv_BuiltNeverInherited(t *testing.T) {
	environ := []string{
		"SMIND_TOKEN=s1",
		"ANTHROPIC_API_KEY=s2",
		"HOME=/real",
		"PATH=/evil",
		"LANG=C",
		"TZ=UTC",
	}
	env := BaseEnv(environ, "/h", "/t")
	want := []string{"HOME=/h", "LANG=C", "PATH=" + SafePath(environ), "TMPDIR=/t", "TZ=UTC"}
	if runtime.GOOS == "windows" {
		want = []string{"HOME=/h", "LANG=C", "PATH=" + SafePath(environ), "TEMP=/t", "TMP=/t", "TZ=UTC", "USERPROFILE=/h"}
	}
	got := append([]string(nil), env...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("env = %v, want exactly %v", got, want)
	}
	for _, leak := range []string{"s1", "s2", "/evil", "/real"} {
		if strings.Contains(strings.Join(env, "\n"), leak) {
			t.Fatalf("env leaks %q: %v", leak, env)
		}
	}
}

func TestPluginAgent_ConfinedExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks and the executable bit differ on Windows")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bin", "agent"), []byte("x"), 0o755)
	writeFile(t, filepath.Join(root, "bin", "plain"), []byte("x"), 0o644)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "evil"), []byte("x"), 0o755)
	os.Symlink(filepath.Join(outside, "evil"), filepath.Join(root, "bin", "link"))
	os.Symlink(outside, filepath.Join(root, "dirlink"))
	for rel, wantErr := range map[string]string{
		"bin/agent": "", "bin/plain": "not executable", "bin/missing": "not found", "bin/link": "symlink", "dirlink/evil": "symlink", "bin": "regular",
	} {
		_, err := ConfinedExecutable(root, rel)
		if wantErr == "" && err != nil || wantErr != "" && (err == nil || !strings.Contains(err.Error(), wantErr)) {
			t.Errorf("%s: err = %v, want %q", rel, err, wantErr)
		}
	}
}

func TestPluginAgent_ResolveBareName_FixedPathOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh is a shell script; the fixed PATH differs on Windows")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mytool"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	t.Setenv("PATH", dir)
	spec := providers.Spec{ID: "demo/tool", Plugin: "demo", CommandForm: "bare", Command: []string{"mytool"}}
	if _, err := ResolveCommand(spec); err == nil {
		t.Fatal("a name found only on the daemon's PATH resolved")
	}
	spec.Command = []string{"sh"}
	argv, err := ResolveCommand(spec)
	if err != nil {
		t.Fatalf("ResolveCommand(sh): %v", err)
	}
	if !filepath.IsAbs(argv[0]) {
		t.Fatalf("argv[0] = %q, want an absolute path", argv[0])
	}
	if _, err := os.Stat(argv[0]); err != nil {
		t.Fatalf("resolved sh does not exist: %v", err)
	}
}

func TestPluginAgent_LaunchEnv_Table(t *testing.T) {
	newSpec := func(id string) providers.Spec {
		return providers.Spec{
			ID: id, Plugin: "acme", PluginData: t.TempDir(),
			Env:         map[string]string{"DEMO": "1", "PATH": "/evil", "HOME": "/evil"},
			SecretSlots: []providers.SecretSlot{{Name: "A_KEY", Required: true}, {Name: "B_KEY"}},
		}
	}
	find := func(env []string, name string) string {
		for _, kv := range env {
			if strings.HasPrefix(kv, name+"=") {
				return kv
			}
		}
		return ""
	}
	daemonEnv := []string{"HOME=/real-daemon", "PATH=/evil", "LANG=C", "SMIND_TOKEN=s1"}

	t.Run("secrets resolved, env filtered", func(t *testing.T) {
		spec := newSpec("acme/agent")
		env, err := LaunchEnv(spec, EnvOptions{
			Environ: daemonEnv,
			Secret: func(name string) (string, bool) {
				if name == "A_KEY" {
					return "v", true
				}
				return "", false
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if find(env, "DEMO") != "DEMO=1" || find(env, "A_KEY") != "A_KEY=v" || find(env, "B_KEY") != "" ||
			find(env, "PATH") == "PATH=/evil" || find(env, "HOME") == "HOME=/evil" {
			t.Fatalf("env = %v", env)
		}
		if find(env, "PATH") != "PATH="+SafePath(daemonEnv) {
			t.Fatalf("PATH = %q, want the fixed SafePath", find(env, "PATH"))
		}
		homeDir := filepath.Join(spec.PluginData, "providers", "agent", "home")
		if find(env, "HOME") != "HOME="+homeDir {
			t.Fatalf("HOME = %q, want %q", find(env, "HOME"), homeDir)
		}
		fi, err := os.Stat(homeDir)
		if err != nil || !fi.IsDir() {
			t.Fatalf("home dir: %v", err)
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
			t.Fatalf("home mode = %o, want 0700", fi.Mode().Perm())
		}
		if strings.Contains(strings.Join(env, "\n"), "s1") {
			t.Fatalf("env leaks the daemon environment: %v", env)
		}
	})

	t.Run("required secret missing", func(t *testing.T) {
		for _, secret := range []func(string) (string, bool){nil, func(string) (string, bool) { return "", false }} {
			_, err := LaunchEnv(newSpec("acme/agent"), EnvOptions{Environ: daemonEnv, Secret: secret})
			if err == nil || !strings.Contains(err.Error(), "A_KEY") {
				t.Fatalf("err = %v, want a missing A_KEY error", err)
			}
		}
	})

	t.Run("skip secrets", func(t *testing.T) {
		env, err := LaunchEnv(newSpec("acme/agent"), EnvOptions{Environ: daemonEnv, SkipSecrets: true})
		if err != nil {
			t.Fatal(err)
		}
		if find(env, "A_KEY") != "" || find(env, "B_KEY") != "" {
			t.Fatalf("env = %v, want no secrets", env)
		}
	})

	t.Run("inherit home", func(t *testing.T) {
		spec := newSpec("acme/agent")
		spec.InheritHome = true
		env, err := LaunchEnv(spec, EnvOptions{
			Environ:  daemonEnv,
			RealHome: "/real",
			Secret: func(name string) (string, bool) {
				if name == "A_KEY" {
					return "v", true
				}
				return "", false
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if find(env, "HOME") != "HOME=/real" {
			t.Fatalf("HOME = %q, want /real", find(env, "HOME"))
		}
		if joined := strings.Join(env, "\n"); strings.Contains(joined, "s1") || strings.Contains(joined, "/real-daemon") {
			t.Fatalf("env inherits the daemon environment: %v", env)
		}
	})

	t.Run("unsafe provider id", func(t *testing.T) {
		for _, id := range []string{"acme/Agent", "acme/../x"} {
			spec := newSpec(id)
			if _, err := LaunchEnv(spec, EnvOptions{Environ: daemonEnv}); err == nil {
				t.Fatalf("id %q accepted", id)
			}
			entries, err := os.ReadDir(spec.PluginData)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("id %q created directories under the plugin data: %v", id, entries)
			}
		}
	})
}

func writeFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
}

// TestPluginAgent_RunVersion_ArgvAndEnv: the probe executes the resolved
// program with ONLY the version flag (never the ACP arguments), cwd the
// plugin root, and the built environment.
func TestPluginAgent_RunVersion_ArgvAndEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	t.Setenv("SMIND_PROBE_SENTINEL", "leak-77")
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "probe.txt")
	script := "#!/bin/sh\n{ echo \"ARGV=$*\"; echo \"PWD=$(pwd -P)\"; env; } > '" + out + "'\necho 'tool 3.4.5'\n"
	if err := os.WriteFile(filepath.Join(root, "agent"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := providers.Spec{ID: "acme/agent", Plugin: "acme", Command: []string{"agent", "--acp", "--verbose"}, CommandForm: "path", PluginRoot: root, PluginData: t.TempDir()}
	got := RunVersion(spec, "--version", os.Environ())
	if !strings.Contains(got, "3.4.5") {
		t.Fatalf("version output = %q", got)
	}
	raw, _ := os.ReadFile(out)
	dump := string(raw)
	if !strings.Contains(dump, "ARGV=--version\n") {
		t.Errorf("argv must be only the version flag:\n%s", dump)
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	if !strings.Contains(dump, "PWD="+realRoot+"\n") {
		t.Errorf("cwd must be the plugin root %s:\n%s", realRoot, dump)
	}
	if strings.Contains(dump, "SMIND_PROBE_SENTINEL") || strings.Contains(dump, "leak-77") {
		t.Errorf("the daemon's environment reached the probe:\n%s", dump)
	}
	// A broken command (missing file) returns "" and runs nothing.
	spec.Command = []string{"missing"}
	if got := RunVersion(spec, "--version", os.Environ()); got != "" {
		t.Errorf("a missing command returned %q", got)
	}
}
