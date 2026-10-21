//go:build !windows

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

func prepDirs(t *testing.T) (root, arm, priv, proj string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	arm, priv, proj = filepath.Join(root, "arm"), filepath.Join(root, "priv"), filepath.Join(root, "proj")
	for _, d := range []string{arm, priv, proj} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root, arm, priv, proj
}

func TestSandboxLaunch_OptionsShape(t *testing.T) {
	_, arm, priv, proj := prepDirs(t)
	argsFile := filepath.Join(priv, "argv.txt")
	l := &Launch{ArmDir: arm, ProjectionDir: proj, Tools: AllowedTools(), Policy: &Policy{ArmDir: arm}, prepared: true}
	var err error
	if l.SettingsJSON, err = Settings(arm, proj); err != nil {
		t.Fatal(err)
	}
	l.CLIPath = fakeCLI(t, priv, map[string]string{"SANDBOX_FAKE_ARGS": argsFile})

	c, err := claudecode.New(arm, l.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the stand-in CLI recorded no argv: %v", err)
	}
	argv := strings.Split(string(b), "\n")
	has := func(a string) bool {
		for _, x := range argv {
			if x == a {
				return true
			}
		}
		return false
	}
	follows := func(flag, value string) bool {
		for i, x := range argv {
			if x == flag && i+1 < len(argv) && argv[i+1] == value {
				return true
			}
			if x == flag+"="+value {
				return true
			}
		}
		return false
	}
	for flag, value := range map[string]string{
		"--tools": "Bash,Read,Edit,Write,Glob,Grep,NotebookEdit,Skill", "--permission-mode": "acceptEdits",
		"--permission-prompts": "none", "--settings": l.SettingsJSON, "--add-dir": proj, "--plugin-dir": proj,
	} {
		if !follows(flag, value) {
			t.Errorf("argv lacks %s %s:\n%s", flag, value, strings.Join(argv, " "))
		}
	}
	for _, f := range []string{"--setting-sources=", "--strict-mcp-config", "--disallowedTools"} {
		if !has(f) {
			t.Errorf("argv lacks %s:\n%s", f, strings.Join(argv, " "))
		}
	}
	for _, f := range []string{"--permission-prompt-tool", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "--mcp-config", "--disable-slash-commands"} {
		for _, x := range argv {
			if x == f || strings.HasPrefix(x, f+"=") {
				t.Errorf("argv must not contain %s", f)
			}
		}
	}
	// Without a projection there is no --add-dir at all.
	l.ProjectionDir = ""
	_ = os.Remove(argsFile)
	c, err = claudecode.New(arm, l.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	b, _ = os.ReadFile(argsFile)
	for _, f := range []string{"--add-dir", "--plugin-dir"} {
		if strings.Contains(string(b), f) {
			t.Errorf("%s present without a projection:\n%s", f, b)
		}
	}
}

func TestSandboxLaunch_PrepareValidates(t *testing.T) {
	if !Supported() {
		t.Skip("macOS only")
	}
	root, arm, priv, proj := prepDirs(t)
	cli := fakeCLI(t, root, nil)
	good := Config{ArmDir: arm, ProjectionDir: proj, PrivateDir: priv, RealCLI: cli}
	bad := map[string]func(c *Config){
		"relative arm":    func(c *Config) { c.ArmDir = "arm" },
		"relative cli":    func(c *Config) { c.RealCLI = "claude" },
		"private in arm":  func(c *Config) { c.PrivateDir = filepath.Join(arm, "p") },
		"arm in private":  func(c *Config) { c.ArmDir = filepath.Join(priv, "a") },
		"same dir":        func(c *Config) { c.PrivateDir = arm },
		"private in proj": func(c *Config) { c.PrivateDir = filepath.Join(proj, "p") },
		"glob in arm":     func(c *Config) { c.ArmDir = filepath.Join(root, "a*m") },
	}
	for name, mut := range bad {
		c := good
		mut(&c)
		if l, err := Prepare(c); err == nil {
			t.Errorf("%s: accepted (%v)", name, l)
		}
	}
	l, err := Prepare(good)
	if err != nil {
		t.Fatal(err)
	}
	if l.Policy == nil || l.PidFile == "" || filepath.Dir(l.PidFile) != priv || len(l.Tools) != 8 {
		t.Errorf("launch = %+v", l)
	}
	// An empty projection is legal.
	_, arm2, priv2, _ := prepDirs(t)
	if l, err := Prepare(Config{ArmDir: arm2, PrivateDir: priv2, RealCLI: cli}); err != nil || l.ProjectionDir != "" {
		t.Errorf("no-projection prepare: %v %+v", err, l)
	}
}

func TestSandboxLaunch_UnsupportedOffMac(t *testing.T) {
	if Supported() {
		t.Skip("darwin")
	}
	if _, err := Prepare(Config{}); err == nil {
		t.Error("Prepare must fail off macOS")
	}
}

func TestSandboxLogs_NoSecrets(t *testing.T) {
	if !Supported() {
		t.Skip("macOS only")
	}
	fakes := map[string]string{
		"FAKE_API_KEY": "fakekeyvalue111", "FAKE_AUTH_TOKEN": "faketokenvalue222", "FAKE_SECRET": "fakesecretvalue333",
		"FAKE_PASSWORD": "fakepasswordvalue444", "FAKE_CREDENTIAL": "fakecredvalue555",
	}
	for k, v := range fakes {
		t.Setenv(k, v)
	}
	// Every env var in the test process whose name looks secret.
	re := regexp.MustCompile(`(?i)(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL)`)
	secrets := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, _ := strings.Cut(kv, "="); re.MatchString(k) && len(v) >= 6 {
			secrets[k] = v
		}
	}
	_, arm, priv, proj := prepDirs(t)
	l, err := Prepare(Config{ArmDir: arm, ProjectionDir: proj, PrivateDir: priv, RealCLI: fakeCLI(t, t.TempDir(), nil)})
	if err != nil {
		t.Fatal(err)
	}
	launcher, _ := os.ReadFile(l.CLIPath)
	texts := map[string]string{"launcher": string(launcher), "settings": l.SettingsJSON, "string": l.String()}
	for _, o := range []any{l, *l} {
		texts["sprint"] = texts["sprint"] + fmtSprint(o)
	}
	for name, text := range texts {
		for k, v := range secrets {
			if strings.Contains(text, v) {
				t.Errorf("%s contains the value of %s", name, k)
			}
		}
	}
	if len(secrets) < len(fakes) {
		t.Errorf("only %d secret-looking vars seen", len(secrets))
	}
}

func fmtSprint(v any) string { return fmt.Sprint(v) }

func TestSandboxLaunch_UnpreparedHasNoOptions(t *testing.T) {
	for name, l := range map[string]*Launch{"nil": nil, "zero": {}, "literal": {ArmDir: "/x", CLIPath: "/y", Tools: AllowedTools(), Policy: &Policy{}}} {
		if l.Prepared() || l.Options() != nil {
			t.Errorf("%s launch: Prepared=%v options=%d", name, l.Prepared(), len(l.Options()))
		}
	}
	if !Supported() {
		return
	}
	_, arm, priv, _ := prepDirs(t)
	l, err := Prepare(Config{ArmDir: arm, PrivateDir: priv, RealCLI: fakeCLI(t, t.TempDir(), nil)})
	if err != nil || !l.Prepared() || len(l.Options()) == 0 {
		t.Errorf("prepared launch: %v prepared=%v", err, l.Prepared())
	}
}
