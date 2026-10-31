package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func useHome(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	for _, k := range []string{EnvListen, EnvHostnames, EnvPassword} {
		t.Setenv(k, "")
	}
	if content != "" {
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(home, "config.yaml")
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConfigSetGetUnsetRoundTripKeepsComments(t *testing.T) {
	path := useHome(t, `# smind config -- edited by hand
server:
  # the port people ask about
  port: 5000 # default is 4648
orchestration:
  maxDepth: 3 # keep shallow
worktrees:
  setupTimeoutSeconds: 700
`)
	if err := Set("server.listen", "0.0.0.0:5000"); err != nil {
		t.Fatal(err)
	}
	if err := Set("server.hostnames", "nas.lan, .example.com"); err != nil {
		t.Fatal(err)
	}
	if err := Set("server.port", "5000"); err != nil {
		t.Fatal(err)
	}
	if v, _ := Get("server.listen"); v != "0.0.0.0:5000" {
		t.Errorf("listen = %q", v)
	}
	if v, _ := Get("server.hostnames"); v != "nas.lan,.example.com" {
		t.Errorf("hostnames = %q", v)
	}
	if v, _ := Get("server.port"); v != "5000" {
		t.Errorf("port = %q", v)
	}

	got := readFile(t, path)
	for _, keep := range []string{"# smind config -- edited by hand", "# the port people ask about", "# default is 4648", "maxDepth: 3 # keep shallow", "setupTimeoutSeconds: 700"} {
		if !strings.Contains(got, keep) {
			t.Errorf("edit lost %q:\n%s", keep, got)
		}
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("the edited file must load: %v", err)
	}
	if cfg.Server.Listen != "0.0.0.0:5000" || len(cfg.Server.Hostnames) != 2 || cfg.Orchestration.MaxDepth != 3 {
		t.Fatalf("loaded %+v", cfg.Server)
	}

	if err := Unset("server.listen"); err != nil {
		t.Fatal(err)
	}
	if err := Unset("server.hostnames"); err != nil {
		t.Fatal(err)
	}
	if err := Unset("server.hostnames"); err != nil { // idempotent
		t.Fatal(err)
	}
	got = readFile(t, path)
	if strings.Contains(got, "listen") || strings.Contains(got, "hostnames") || !strings.Contains(got, "# the port people ask about") {
		t.Fatalf("after unset:\n%s", got)
	}
	if v, _ := Get("server.listen"); v != "" {
		t.Errorf("listen after unset = %q", v)
	}
}

func TestConfigSetAutomationsEnabled(t *testing.T) {
	path := useHome(t, `# keep me
server:
  port: 5000
`)
	if err := Set("automations.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if v, _ := Get("automations.enabled"); v != "true" {
		t.Errorf("Get = %q, want \"true\"", v)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "# keep me") {
		t.Errorf("edit lost the comment:\n%s", got)
	}
	if !strings.Contains(got, "enabled: true") {
		t.Errorf("edit did not write enabled:\n%s", got)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("the edited file must load: %v", err)
	}
	if !cfg.Automations.Enabled {
		t.Errorf("Automations.Enabled = false, want true")
	}
	if err := Set("automations.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if v, _ := Get("automations.enabled"); v != "false" {
		t.Errorf("Get after set false = %q", v)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("the edited file must load: %v", err)
	}
	if cfg.Automations.Enabled {
		t.Errorf("Automations.Enabled = true, want false")
	}
}

func TestConfigSetAutomationsEnabledRejectsNonBool(t *testing.T) {
	path := useHome(t, "# keep me\nserver:\n  port: 4648\n")
	before := readFile(t, path)
	for _, v := range []string{"yes", "1", ""} {
		err := Set("automations.enabled", v)
		if err == nil || !strings.Contains(err.Error(), "automations.enabled") {
			t.Errorf("Set(%q) = %v, want an error naming the key", v, err)
		}
	}
	if readFile(t, path) != before {
		t.Fatal("a rejected edit changed the file")
	}
}

func TestConfigSetCreatesFileOwnerOnly(t *testing.T) {
	path := useHome(t, "")
	if err := Set("server.port", "4700"); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	if !strings.Contains(readFile(t, path), "port: 4700") {
		t.Fatal(readFile(t, path))
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".config.yaml.*")); len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

func TestConfigSetRejectsUnknownKey(t *testing.T) {
	path := useHome(t, "server:\n  port: 4648\n")
	before := readFile(t, path)
	for _, key := range []string{"server.bogus", "port", "orchestration.maxDepth", "server", "relay.legacyDevices", ""} {
		err := Set(key, "1")
		if err == nil || !strings.Contains(err.Error(), "unknown config key") || !strings.Contains(err.Error(), "server.listen") {
			t.Errorf("Set(%q) = %v, want an unknown-key error listing the known keys", key, err)
		}
		if _, err := Get(key); err == nil {
			t.Errorf("Get(%q) succeeded", key)
		}
		if err := Unset(key); err == nil {
			t.Errorf("Unset(%q) succeeded", key)
		}
	}
	if readFile(t, path) != before {
		t.Fatal("a rejected edit changed the file")
	}
}

func TestConfigSetValidatesValues(t *testing.T) {
	path := useHome(t, "")
	for key, vals := range map[string][]string{
		"server.port":      {"0", "70000", "abc", "", "-1"},
		"server.listen":    {"", "4648", "host-only", "1.2.3.4:", "1.2.3.4:99999", "a b:1"},
		"server.hostnames": {"", "http://x.com", "x.com:4648", "*.x.com", "x..com", "-x.com", ".com", ".localhost.", "."},
	} {
		for _, v := range vals {
			if err := Set(key, v); err == nil {
				t.Errorf("Set(%s, %q) succeeded", key, v)
			}
		}
	}
	if err := Set("server.passwordHash", "$2a$12$whatever"); err == nil || !strings.Contains(err.Error(), "smind set-password") {
		t.Errorf("passwordHash must not be settable here: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("rejected edits must not create the file")
	}
	// port and listen may not name different ports.
	if err := Set("server.listen", "0.0.0.0:5001"); err != nil {
		t.Fatal(err)
	}
	if err := Set("server.port", "5002"); err == nil || !strings.Contains(err.Error(), "different ports") {
		t.Fatalf("conflicting port/listen: %v", err)
	}
}

func TestPasswordHashIsNeverPrinted(t *testing.T) {
	path := useHome(t, "")
	if err := SetPasswordHash("$2a$12$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012345"); err != nil {
		t.Fatal(err)
	}
	v, err := Get("server.passwordHash")
	if err != nil || v != "<set>" {
		t.Fatalf("Get = %q, %v", v, err)
	}
	all, _ := GetAll()
	for _, kv := range all {
		if strings.Contains(kv[1], "$2a$") {
			t.Fatalf("GetAll leaked the hash: %v", kv)
		}
	}
	if err := SetPasswordHash("hunter2hunter2"); err == nil {
		t.Fatal("SetPasswordHash stored a non-bcrypt value")
	}
	if err := Unset("server.passwordHash"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, path), "passwordHash") {
		t.Fatal("unset left the hash")
	}
}

func TestEnvOverridesConfig(t *testing.T) {
	useHome(t, "server:\n  listen: 127.0.0.1:4648\n  hostnames: [file.example.com]\n  passwordHash: \"$2a$12$filehashfilehashfilehashOuoZxuuuuuuuuuuuuuuuuuuuuuuuuuuu\"\n")
	t.Setenv(EnvListen, "0.0.0.0:4700")
	t.Setenv(EnvHostnames, "A.example.com, .b.example.com")
	t.Setenv(EnvPassword, "container-password-123")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != "0.0.0.0:4700" || cfg.Server.ClientPort() != 4700 {
		t.Errorf("listen = %q port %d", cfg.Server.Listen, cfg.Server.ClientPort())
	}
	if got := strings.Join(cfg.Server.Hostnames, "|"); got != "a.example.com|.b.example.com" {
		t.Errorf("hostnames = %q", got)
	}
	if cfg.Server.PasswordPlain != "container-password-123" || !cfg.Server.HasPassword() {
		t.Errorf("password env not applied")
	}
	// The env never reaches config.yaml.
	if err := Set("server.hostnames", "x.example.com"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, Path()), "container-password") || strings.Contains(readFile(t, Path()), "0.0.0.0") {
		t.Fatalf("an environment override leaked into config.yaml:\n%s", readFile(t, Path()))
	}
}

func TestListenHelpers(t *testing.T) {
	cases := []struct {
		listen    string
		port      int
		loopback  bool
		companion string
		wantErr   bool
	}{
		{"", 0, true, "", false},
		{"127.0.0.1:4648", 0, true, "", false},
		{"localhost:5000", 0, true, "", false},
		{"[::1]:5000", 0, true, "127.0.0.1:5000", false},
		{"127.0.0.2:5000", 0, true, "127.0.0.1:5000", false},
		{"0.0.0.0:4648", 0, false, "", false},
		{":4648", 0, false, "", false},
		{"[::]:4648", 0, false, "", false},
		{"192.168.1.10:5000", 0, false, "127.0.0.1:5000", false},
		{"nas.lan:5000", 0, false, "127.0.0.1:5000", false},
		{"0.0.0.0:5000", 4648, false, "", false}, // default port + other listen port: listen wins
		{"0.0.0.0:5000", 6000, false, "", true},  // explicit disagreeing port
		{"nope", 0, false, "", true},
		{"1.2.3.4:0", 0, false, "", true},
	}
	for _, c := range cases {
		s := ServerConfig{Port: c.port, Listen: c.listen}
		_, _, err := s.ListenHostPort()
		if (err != nil) != c.wantErr {
			t.Errorf("%+v: err = %v, wantErr %v", s, err, c.wantErr)
			continue
		}
		if c.wantErr {
			continue
		}
		if s.ListenIsLoopback() != c.loopback || s.LoopbackCompanion() != c.companion {
			t.Errorf("%+v: loopback=%v companion=%q, want %v %q", s, s.ListenIsLoopback(), s.LoopbackCompanion(), c.loopback, c.companion)
		}
	}
	if (ServerConfig{Listen: "0.0.0.0:5000"}).ClientPort() != 5000 || (ServerConfig{}).ClientPort() != 4648 || (ServerConfig{Listen: "junk"}).ClientPort() != 4648 {
		t.Error("ClientPort")
	}
}

func TestLoadRejectsBadServerSection(t *testing.T) {
	useHome(t, "server:\n  listen: nonsense\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "server.listen") {
		t.Fatalf("Load = %v", err)
	}
	useHome(t, "server:\n  hostnames: [\"http://bad\"]\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "hostnames") {
		t.Fatalf("Load = %v", err)
	}
}

func TestConfigSetEditsThroughASymlinkAndKeepsItPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	link := useHome(t, "")
	real := filepath.Join(t.TempDir(), "dotfiles-config.yaml")
	if err := os.WriteFile(real, []byte("# managed elsewhere\nserver:\n  port: 4648\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := Set("server.port", "4701"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}
	got := readFile(t, real)
	if !strings.Contains(got, "4701") || !strings.Contains(got, "# managed elsewhere") {
		t.Fatalf("target file:\n%s", got)
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestEnvStillAppliesWhenTheFileDoesNotParse(t *testing.T) {
	useHome(t, "server: [this is not a mapping\n")
	t.Setenv(EnvListen, "0.0.0.0:4700")
	t.Setenv(EnvPassword, "container-password-123")
	cfg, err := Load()
	if err == nil {
		t.Fatal("Load accepted a broken file")
	}
	if cfg.Server.Listen != "0.0.0.0:4700" || cfg.Server.PasswordPlain == "" {
		t.Fatalf("the environment was dropped along with the file: %+v", cfg.Server)
	}
}

func TestBindsNeverDualStackAnIPv4Literal(t *testing.T) {
	for listen, want := range map[string]string{
		"0.0.0.0:4648": "tcp4", "192.168.1.5:4648": "tcp4", "127.0.0.1:4648": "tcp4", "localhost:4648": "tcp4",
		"[::]:4648": "tcp", ":4648": "tcp", "[fd00::1]:4648": "tcp6", "nas.lan:4648": "tcp",
	} {
		b, err := ServerConfig{Listen: listen}.Binds()
		if err != nil || b[0].Network != want {
			t.Errorf("%s: %v %v, want primary network %s", listen, b, err, want)
		}
	}
}

func TestEnvStillAppliesWhenTheFileCannotBeRead(t *testing.T) {
	path := useHome(t, "")
	if err := os.Mkdir(path, 0o700); err != nil { // a directory where config.yaml should be: ReadFile fails, not "not exist"
		t.Fatal(err)
	}
	t.Setenv(EnvListen, "0.0.0.0:4700")
	t.Setenv(EnvPassword, "container-password-123")
	cfg, err := Load()
	if err == nil {
		t.Fatal("Load accepted an unreadable config")
	}
	if cfg.Server.Listen != "0.0.0.0:4700" || cfg.Server.PasswordPlain == "" {
		t.Fatalf("the environment was dropped with the unreadable file: %+v", cfg.Server)
	}
}
