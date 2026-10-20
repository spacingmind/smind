package spike

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

func TestSpike_SettingsJSONShape(t *testing.T) {
	arm, proj := "/tmp/x/arm", "/tmp/x/proj"
	raw := SettingsJSON(arm, proj)
	var got struct {
		Sandbox struct {
			Enabled                  *bool `json:"enabled"`
			FailIfUnavailable        *bool `json:"failIfUnavailable"`
			AllowUnsandboxedCommands *bool `json:"allowUnsandboxedCommands"`
			AutoAllowBashIfSandboxed *bool `json:"autoAllowBashIfSandboxed"`
			Network                  struct {
				StrictAllowlist   *bool     `json:"strictAllowlist"`
				AllowedDomains    *[]string `json:"allowedDomains"`
				AllowLocalBinding *bool     `json:"allowLocalBinding"`
			} `json:"network"`
			Filesystem struct {
				AllowWrite []string `json:"allowWrite"`
				DenyWrite  []string `json:"denyWrite"`
				DenyRead   []string `json:"denyRead"`
				AllowRead  []string `json:"allowRead"`
			} `json:"filesystem"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("settings are not valid JSON: %v", err)
	}
	s := got.Sandbox
	for name, p := range map[string]*bool{
		"enabled": s.Enabled, "failIfUnavailable": s.FailIfUnavailable,
		"allowUnsandboxedCommands": s.AllowUnsandboxedCommands,
		"network.strictAllowlist":  s.Network.StrictAllowlist,
	} {
		if p == nil {
			t.Fatalf("required key %s is missing", name)
		}
	}
	if !*s.Enabled || !*s.FailIfUnavailable || !*s.Network.StrictAllowlist {
		t.Errorf("enabled, failIfUnavailable and strictAllowlist must all be true: %s", raw)
	}
	if *s.AllowUnsandboxedCommands {
		t.Error("allowUnsandboxedCommands must be false")
	}
	if strings.Contains(raw, `"allowUnsandboxedCommands":true`) {
		t.Error("raw settings contain allowUnsandboxedCommands:true")
	}
	if s.Network.AllowedDomains == nil || len(*s.Network.AllowedDomains) != 0 {
		t.Errorf("allowedDomains must be present and empty: %s", raw)
	}
	if s.Network.AllowLocalBinding == nil || *s.Network.AllowLocalBinding {
		t.Error("allowLocalBinding must be present and false")
	}
	if len(s.Filesystem.AllowWrite) != 1 || s.Filesystem.AllowWrite[0] != arm {
		t.Errorf("allowWrite must be exactly the arm dir, got %v", s.Filesystem.AllowWrite)
	}
	if len(s.Filesystem.DenyWrite) != 1 || s.Filesystem.DenyWrite[0] != proj {
		t.Errorf("denyWrite must be exactly the projection dir, got %v", s.Filesystem.DenyWrite)
	}
	if len(s.Filesystem.DenyRead) == 0 || len(s.Filesystem.AllowRead) == 0 {
		t.Errorf("denyRead and allowRead must both be set: %s", raw)
	}
	var perms struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	_ = json.Unmarshal([]byte(raw), &perms)
	for _, tool := range []string{"Edit", "Write", "NotebookEdit"} {
		want := tool + "(/" + proj + "/**)"
		found := false
		for _, d := range perms.Permissions.Deny {
			found = found || d == want
		}
		if !found {
			t.Errorf("permissions.deny lacks %q: %v", want, perms.Permissions.Deny)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		found := false
		for _, p := range s.Filesystem.DenyRead {
			found = found || p == home
		}
		if !found {
			t.Errorf("denyRead must contain the real $HOME %q, got %v", home, s.Filesystem.DenyRead)
		}
	}
	for _, p := range append(append(append([]string{}, s.Filesystem.AllowWrite...), s.Filesystem.AllowRead...), s.Filesystem.DenyRead...) {
		if !filepath.IsAbs(p) {
			t.Errorf("sandbox path %q is not absolute", p)
		}
	}
}

func TestSpike_SettingsJSONRejectsRelativePath(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic for a relative arm dir")
		}
	}()
	SettingsJSON("rel/arm", "/tmp/proj")
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

func TestSpike_LedgerRefusesOverBudget(t *testing.T) {
	l := &Ledger{Path: filepath.Join(t.TempDir(), "sub", "ledger.jsonl")}
	for i := 0; i < MaxCalls; i++ {
		if err := l.Begin("call"); err != nil {
			t.Fatalf("call %d refused early: %v", i+1, err)
		}
		if err := l.Record(Entry{Purpose: "call", CostUSD: 0.01}); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Begin("one too many"); !errors.Is(err, ErrBudget) {
		t.Fatalf("13th call: want ErrBudget, got %v", err)
	}

	l2 := &Ledger{Path: filepath.Join(t.TempDir(), "ledger.jsonl")}
	if err := l2.Record(Entry{Purpose: "expensive", CostUSD: MaxUSD - perCallReserveUSD + 0.01}); err != nil {
		t.Fatal(err)
	}
	err := l2.Begin("would pass 0.60")
	if !errors.Is(err, ErrBudget) || !strings.Contains(err.Error(), "0.66") {
		t.Fatalf("cost gate: want ErrBudget naming totals, got %v", err)
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
