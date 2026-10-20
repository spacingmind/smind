package spike

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	if len(s.Filesystem.DenyRead) == 0 || len(s.Filesystem.AllowRead) == 0 {
		t.Errorf("denyRead and allowRead must both be set: %s", raw)
	}
	for _, p := range append(append(append([]string{}, s.Filesystem.AllowWrite...), s.Filesystem.AllowRead...), s.Filesystem.DenyRead...) {
		if p != "~" && !filepath.IsAbs(p) {
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
	allow := []string{"PATH=/usr/bin:/bin", "HOME=/tmp/private home", "LANG=en_US.UTF-8", "TRICKY=it's $(touch " + filepath.Join(dir, "pwned") + ")"}
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
	want := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "TRICKY": true, "PWD": true, "SHLVL": true, "_": true}
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
	if err := l2.Record(Entry{Purpose: "expensive", CostUSD: 0.56}); err != nil {
		t.Fatal(err)
	}
	err := l2.Begin("would pass 0.60")
	if !errors.Is(err, ErrBudget) || !strings.Contains(err.Error(), "0.56") {
		t.Fatalf("cost gate: want ErrBudget naming totals, got %v", err)
	}
}
