package spike

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// Fixed, non-existent paths: canonicalisation then only Cleans them, so the
// expected strings below are plain literals, not a re-run of the formula.
const (
	litArm  = "/nonexistent-p0/arm"
	litProj = "/nonexistent-p0/proj"
	litHome = "/nonexistent-home"
)

func decode(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("settings are not valid JSON: %v", err)
	}
	return m
}

func TestSpike_SettingsJSONShape(t *testing.T) {
	t.Setenv("HOME", litHome)
	raw, err := SettingsJSONWith(litArm, litProj, SettingsOptions{ReadMode: ReadDenyList, ExtraDenyRead: []string{"/nonexistent-p0-real/checkout"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{
	  "permissions": {"deny": ["Edit(//nonexistent-p0/proj/**)", "Write(//nonexistent-p0/proj/**)", "NotebookEdit(//nonexistent-p0/proj/**)"]},
	  "sandbox": {
	    "enabled": true,
	    "failIfUnavailable": true,
	    "allowUnsandboxedCommands": false,
	    "autoAllowBashIfSandboxed": true,
	    "network": {"strictAllowlist": true, "allowedDomains": [], "allowLocalBinding": false},
	    "filesystem": {
	      "allowWrite": ["/nonexistent-p0/arm"],
	      "denyWrite": ["/nonexistent-p0/proj"],
	      "denyRead": ["/nonexistent-home", "/nonexistent-p0", "/nonexistent-p0-real/checkout"],
	      "allowRead": ["/nonexistent-p0/arm", "/nonexistent-p0/proj"]
	    }
	  }
	}`
	if !reflect.DeepEqual(decode(t, raw), decode(t, want)) {
		t.Errorf("settings differ from the literal expectation:\n got  %s\n want %s", raw, want)
	}
	if strings.Contains(raw, `"allowUnsandboxedCommands":true`) {
		t.Error("allowUnsandboxedCommands:true must never appear")
	}
}

func TestSpike_SettingsJSONAllowListMode(t *testing.T) {
	t.Setenv("HOME", litHome)
	raw, err := SettingsJSONWith(litArm, litProj, SettingsOptions{ReadMode: ReadAllowList})
	if err != nil {
		t.Fatal(err)
	}
	fs := decode(t, raw)["sandbox"].(map[string]any)["filesystem"].(map[string]any)
	if got := fs["denyRead"]; !reflect.DeepEqual(got, []any{"/"}) {
		t.Errorf("allow-list denyRead = %v, want [/]", got)
	}
	allow := map[string]bool{}
	for _, p := range fs["allowRead"].([]any) {
		allow[p.(string)] = true
	}
	for _, want := range []string{litArm, litProj, "/usr", "/bin", "/System", "/dev"} {
		if !allow[want] {
			t.Errorf("allowRead lacks %s: %v", want, fs["allowRead"])
		}
	}
	if allow[litHome] || allow["/"] {
		t.Errorf("allowRead must not re-open $HOME or /: %v", fs["allowRead"])
	}
}

func TestSpike_SettingsJSONAblationsOnlyRemoveTheirKey(t *testing.T) {
	t.Setenv("HOME", litHome)
	full, _ := SettingsJSONWith(litArm, litProj, SettingsOptions{})
	for name, opt := range map[string]SettingsOptions{
		"network": {OmitNetworkKeys: true}, "denyWrite": {OmitDenyWrite: true}, "permissions": {OmitPermissionsDeny: true},
	} {
		raw, err := SettingsJSONWith(litArm, litProj, opt)
		if err != nil {
			t.Fatal(err)
		}
		if raw == full {
			t.Errorf("%s ablation changed nothing", name)
		}
	}
	raw, _ := SettingsJSONWith(litArm, litProj, SettingsOptions{OmitNetworkKeys: true})
	if strings.Contains(raw, "strictAllowlist") || strings.Contains(raw, "allowLocalBinding") || !strings.Contains(raw, `"allowedDomains":[]`) {
		t.Errorf("network ablation must drop exactly the two keys: %s", raw)
	}
}

func TestSpike_SettingsJSONFailsClosed(t *testing.T) {
	for name, home := range map[string]string{"empty": "", "relative": "rel/home"} {
		t.Run("home "+name, func(t *testing.T) {
			t.Setenv("HOME", home)
			if raw, err := SettingsJSON(litArm, litProj, nil); err == nil {
				t.Fatalf("deny-list settings were built without a usable $HOME: %s", raw)
			}
		})
	}
	t.Setenv("HOME", litHome)
	for name, tc := range map[string][2]string{
		"relative arm": {"rel/arm", litProj}, "relative projection": {litArm, "rel/proj"},
		"dotdot arm": {"/nonexistent-p0/../arm", litProj}, "dotdot projection": {litArm, "/nonexistent-p0/x/../proj"},
		"star": {litArm, "/nonexistent-p0/pr*oj"}, "question": {"/nonexistent-p0/a?m", litProj},
		"bracket": {litArm, "/nonexistent-p0/p[r]oj"}, "paren": {litArm, "/nonexistent-p0/pr(oj"},
		"newline": {"/nonexistent-p0/a\nrm", litProj},
	} {
		if raw, err := SettingsJSON(tc[0], tc[1], nil); !errors.Is(err, ErrBadPath) {
			t.Errorf("%s: want ErrBadPath, got err=%v raw=%s", name, err, raw)
		}
	}
	if _, err := SettingsJSON(litArm, litProj, []string{"/nonexistent-p0/x*"}); !errors.Is(err, ErrBadPath) {
		t.Errorf("glob in extraDenyRead accepted: %v", err)
	}
	if _, err := SettingsJSON(litArm, litProj, []string{"relative"}); !errors.Is(err, ErrBadPath) {
		t.Errorf("relative extraDenyRead accepted: %v", err)
	}
}

func TestSpike_SettingsJSONCanonicalisesPaths(t *testing.T) {
	t.Setenv("HOME", litHome)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realDir := filepath.Join(root, "real", "arm")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "real"), link); err != nil {
		t.Fatal(err)
	}
	// Symlinked prefix, trailing slash and a not-yet-existing tail.
	raw, err := SettingsJSON(link+"/arm/", link+"/not-yet/proj/", []string{link + "/arm"})
	if err != nil {
		t.Fatal(err)
	}
	fs := decode(t, raw)["sandbox"].(map[string]any)["filesystem"].(map[string]any)
	if got := fs["allowWrite"]; !reflect.DeepEqual(got, []any{realDir}) {
		t.Errorf("allowWrite = %v, want the symlink-resolved %s", got, realDir)
	}
	if got := fs["denyWrite"]; !reflect.DeepEqual(got, []any{filepath.Join(root, "real", "not-yet", "proj")}) {
		t.Errorf("denyWrite = %v (not-yet-existing tail must be Cleaned onto the resolved prefix)", got)
	}
	deny := fs["denyRead"].([]any)
	if len(deny) != 3 || deny[0] != litHome || deny[1] != filepath.Join(root, "real") || deny[2] != realDir {
		t.Errorf("denyRead = %v, want [home, resolved parent, resolved extra]", deny)
	}
	if strings.Contains(raw, link) {
		t.Errorf("the symlink path leaked into the settings: %s", raw)
	}
}

func TestSpike_SettingsJSONDefaultMode(t *testing.T) {
	t.Setenv("HOME", litHome)
	got, err := SettingsJSON(litArm, litProj, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := SettingsJSONWith(litArm, litProj, SettingsOptions{ReadMode: DefaultReadMode})
	if got != want {
		t.Errorf("SettingsJSON is not DefaultReadMode:\n%s\n%s", got, want)
	}
}

func TestSpike_LauncherScrubsEnv(t *testing.T) {
	const canary = "SMIND_CANARY_SECRET"
	t.Setenv(canary, "CANARY-not-a-real-secret")
	dir := t.TempDir()

	// Stub "CLI": dumps its environment, one NAME=VALUE per line, then its argv.
	stub := filepath.Join(dir, "stub-cli")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n/usr/bin/env\necho \"ARGS:$*\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	allow := []string{"PATH=/usr/bin:/bin", "HOME=/tmp/private home", "LANG=en_US.UTF-8", "USER=someone", "TRICKY=it's $(touch " + filepath.Join(dir, "pwned") + ")"}
	launcher, err := WriteLauncher(dir, stub, allow)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(launcher, "--flag", "a b").Output() // inherits the canary
	if err != nil {
		t.Fatalf("launcher failed: %v", err)
	}
	text := string(out)
	if strings.Contains(text, canary) || strings.Contains(text, "CANARY-not-a-real-secret") {
		t.Fatalf("canary leaked through the launcher:\n%s", text)
	}
	// PWD, SHLVL and _ are minted by the stub's own /bin/sh, not inherited.
	want := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "TRICKY": true, "USER": true, "PWD": true, "SHLVL": true, "_": true}
	got := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "ARGS:") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	for k := range got {
		if !want[k] {
			t.Errorf("unexpected env var %s reached the CLI", k)
		}
	}
	if got["HOME"] != "/tmp/private home" || got["TRICKY"] != "it's $(touch "+filepath.Join(dir, "pwned")+")" {
		t.Errorf("values were not passed verbatim: %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Fatal("an env value was interpreted by the shell")
	}
	if !strings.Contains(text, "ARGS:--flag a b") {
		t.Errorf("argv not forwarded:\n%s", text)
	}
}

func TestSpike_LauncherRejectsBadEntries(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"NOEQUALS", "1BAD=x", "A B=x", "=x"} {
		if _, err := WriteLauncher(dir, "/bin/true", []string{bad}); err == nil {
			t.Errorf("entry %q was accepted", bad)
		}
	}
	if _, err := WriteLauncher(dir, "relative/claude", nil); err == nil {
		t.Error("relative CLI path accepted")
	}
}

func TestSpike_LauncherRefusesUnsafeTargets(t *testing.T) {
	newDir := func(mode os.FileMode) string {
		d := filepath.Join(t.TempDir(), "d")
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(d, mode); err != nil {
			t.Fatal(err)
		}
		return d
	}
	// A normal 0700 dir works, and the file is created 0700.
	d := newDir(0o700)
	path, err := WriteLauncher(d, "/bin/true", []string{"PATH=/usr/bin"})
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o700 {
		t.Errorf("launcher mode = %v, want 0700", fi.Mode().Perm())
	}
	// Second write to the same dir must not overwrite (O_EXCL).
	if _, err := WriteLauncher(d, "/bin/true", nil); err == nil {
		t.Error("an existing launcher file was overwritten")
	}
	// A pre-planted file (not ours to follow) in a fresh dir.
	d = newDir(0o700)
	if err := os.WriteFile(filepath.Join(d, "claude-launcher.sh"), []byte("planted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteLauncher(d, "/bin/true", nil); err == nil {
		t.Error("a pre-planted file was accepted")
	}
	// A pre-planted symlink must be refused, and its target must stay untouched.
	d = newDir(0o700)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(d, "claude-launcher.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteLauncher(d, "/bin/true", nil); err == nil {
		t.Error("a pre-planted symlink was followed")
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("symlink target was overwritten: %q", b)
	}
	// Group/world-writable dir.
	for _, mode := range []os.FileMode{0o770, 0o707, 0o777} {
		if _, err := WriteLauncher(newDir(mode), "/bin/true", nil); err == nil {
			t.Errorf("dir mode %v accepted", mode)
		}
	}
	// A symlinked dir.
	real := newDir(0o700)
	linkDir := filepath.Join(t.TempDir(), "linkdir")
	if err := os.Symlink(real, linkDir); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteLauncher(linkDir, "/bin/true", nil); err == nil {
		t.Error("a symlinked launcher dir was accepted")
	}
}

func TestSpike_LedgerRefusesOverBudget(t *testing.T) {
	l := &Ledger{Path: filepath.Join(t.TempDir(), "sub", "ledger.jsonl")}
	for i := 0; i < MaxCalls; i++ {
		seq, err := l.Begin("call")
		if err != nil {
			t.Fatalf("call %d refused early: %v", i+1, err)
		}
		if seq != i+1 {
			t.Fatalf("Begin returned seq %d, want %d", seq, i+1)
		}
		if err := l.Record(Entry{Purpose: "call", CostUSD: 0.01}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.Begin("one too many"); !errors.Is(err, ErrBudget) {
		t.Fatalf("call %d (over MaxCalls=%d): want ErrBudget, got %v", MaxCalls+1, MaxCalls, err)
	}

	l2 := &Ledger{Path: filepath.Join(t.TempDir(), "ledger.jsonl")}
	spent := MaxUSD - perCallReserveUSD + 0.01
	if err := l2.Record(Entry{Purpose: "expensive", CostUSD: spent}); err != nil {
		t.Fatal(err)
	}
	_, err := l2.Begin("would pass the USD cap")
	if !errors.Is(err, ErrBudget) || !strings.Contains(err.Error(), fmt.Sprintf("%.4f", spent)) {
		t.Fatalf("cost gate: want ErrBudget naming the %.4f spent, got %v", spent, err)
	}
	if MaxCalls != 24 || MaxUSD != 0.80 {
		t.Errorf("caps are %d/%.2f, the hardening round authorised 24/0.80", MaxCalls, MaxUSD)
	}
}

func TestSpike_ArmPolicyDecisions(t *testing.T) {
	root := t.TempDir()
	arm := filepath.Join(root, "arm")
	proj := filepath.Join(root, "proj")
	sibling := filepath.Join(root, "sibling")
	for _, d := range []string{arm, proj, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sibling, "REAL.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sibling, filepath.Join(arm, "link")); err != nil {
		t.Fatal(err)
	}
	p := &ArmPolicy{ArmDir: arm, ReadOnlyDirs: []string{proj}, AllowBash: func(c string) bool { return c == "allowed-cmd" }}
	cases := []struct {
		name  string
		tool  string
		input map[string]any
		want  bool
	}{
		{"read inside", "Read", map[string]any{"file_path": filepath.Join(arm, "a.txt")}, true},
		{"read relative inside", "Read", map[string]any{"file_path": "a.txt"}, true},
		{"read outside", "Read", map[string]any{"file_path": filepath.Join(sibling, "REAL.txt")}, false},
		{"read traversal", "Read", map[string]any{"file_path": "../sibling/REAL.txt"}, false},
		{"read via symlink", "Read", map[string]any{"file_path": filepath.Join(arm, "link", "REAL.txt")}, false},
		{"read projection", "Read", map[string]any{"file_path": filepath.Join(proj, "s.txt")}, true},
		{"write projection", "Write", map[string]any{"file_path": filepath.Join(proj, "s.txt")}, false},
		{"write inside", "Write", map[string]any{"file_path": filepath.Join(arm, "n.txt")}, true},
		{"write new nested inside", "Write", map[string]any{"file_path": filepath.Join(arm, "d", "e", "n.txt")}, true},
		{"edit outside", "Edit", map[string]any{"file_path": filepath.Join(sibling, "REAL.txt")}, false},
		{"notebook outside", "NotebookEdit", map[string]any{"notebook_path": filepath.Join(root, "x.ipynb")}, false},
		{"grep no path", "Grep", map[string]any{"pattern": "x"}, true},
		{"grep outside", "Grep", map[string]any{"pattern": "x", "path": sibling}, false},
		{"glob absolute pattern", "Glob", map[string]any{"pattern": sibling + "/*"}, false},
		{"glob traversal pattern", "Glob", map[string]any{"pattern": "../*"}, false},
		{"glob inside", "Glob", map[string]any{"pattern": "*.txt"}, true},
		{"bash default deny", "Bash", map[string]any{"command": "ls"}, false},
		{"bash allowed by test", "Bash", map[string]any{"command": "allowed-cmd"}, true},
		{"webfetch", "WebFetch", map[string]any{"url": "http://x"}, false},
		{"unknown tool", "Mystery", nil, false},
	}
	for _, tc := range cases {
		got, _, _, _, _, err := p.Decide(context.Background(), claudecode.CanUseToolRequest{ToolName: tc.tool, Input: tc.input})
		if err != nil || got != tc.want {
			t.Errorf("%s: allow=%v err=%v, want %v", tc.name, got, err, tc.want)
		}
	}
	if n := len(p.Records()); n != len(cases) {
		t.Errorf("recorded %d requests, want %d", n, len(cases))
	}
}

func TestSpike_AllowlistEnv(t *testing.T) {
	have := map[string]string{"PATH": "/usr/bin", "HOME": "/h", "USER": "u", "SMIND_CANARY_SECRET": "CANARY-x", "GITHUB_TOKEN": "t", "LOGNAME": "u", "SHELL": "/bin/sh"}
	got := AllowlistEnv(func(n string) (string, bool) { v, ok := have[n]; return v, ok })
	names := map[string]bool{}
	for _, kv := range got {
		n, _, _ := strings.Cut(kv, "=")
		names[n] = true
	}
	for _, want := range []string{"PATH", "HOME", "USER", "ENABLE_CLAUDEAI_MCP_SERVERS"} {
		if !names[want] {
			t.Errorf("%s missing from the allowlist env", want)
		}
	}
	for _, bad := range []string{"SMIND_CANARY_SECRET", "GITHUB_TOKEN", "LOGNAME", "SHELL", "LANG"} {
		if names[bad] {
			t.Errorf("%s must not be in the allowlist env (absent or not allowlisted)", bad)
		}
	}
}
