package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// An aliased section must not be replaced by an empty mapping (reviewer 2).
func Test_config_set_refuses_an_aliased_section(t *testing.T) {
	body := "base: &x\n  enabled: false\n  allowUnattendedModes: true\n  maxBudgetUsd: 9\nautomations: *x\n"
	path := writeConfig(t, body)
	if err := Set("automations.enabled", "true"); err == nil || !strings.Contains(err.Error(), "anchor or alias") {
		t.Fatalf("Set through an alias = %v, want a refusal", err)
	}
	if err := Unset("automations.enabled"); err == nil {
		t.Fatal("Unset through an alias was accepted")
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Fatalf("config.yaml changed:\n%s", got)
	}
}

func Test_config_effective_bool_spellings(t *testing.T) {
	for in, want := range map[string]string{"yes": "true", "on": "true", "True": "true", "no": "false", "off": "false", "false": "false"} {
		writeConfig(t, "plugins:\n  enabled: "+in+"\n")
		ev, err := Effective("plugins.enabled")
		if err != nil || ev.Value != want || ev.Source != "file" {
			t.Errorf("enabled: %s -> %+v (%v), want %s", in, ev, err, want)
		}
	}
}

// An unparsable file is an error, not "the defaults" (reviewer 2).
func Test_config_effective_unreadable_file_is_an_error(t *testing.T) {
	writeConfig(t, "learning: [unclosed\n")
	_, err := Effective("learning.enabled")
	if !errors.Is(err, ErrConfigUnreadable) {
		t.Fatalf("Effective on an unparsable file = %v, want ErrConfigUnreadable", err)
	}
}

func Test_config_audit_caps_fields_and_keeps_generations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	old := auditMaxBytes
	auditMaxBytes = 600
	t.Cleanup(func() { auditMaxBytes = old })
	huge := strings.Repeat("k", 1<<20)
	if err := AppendAudit(AuditEntry{Key: huge, Outcome: huge, Caller: "run"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, auditFileName))
	if len(b) > 1200 {
		t.Fatalf("one refused call wrote %d bytes: fields are not capped", len(b))
	}
	for i := 0; i < 40; i++ {
		if err := AppendAudit(AuditEntry{Key: "plugins.enabled", Outcome: "ok", Caller: "human"}); err != nil {
			t.Fatal(err)
		}
	}
	for g := 1; g <= auditGenerations; g++ {
		if _, err := os.Stat(filepath.Join(home, auditFileName+"."+string(rune('0'+g)))); err != nil {
			t.Errorf("generation .%d missing: %v", g, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, auditFileName+"."+string(rune('0'+auditGenerations+1)))); err == nil {
		t.Errorf("more than %d generations kept", auditGenerations)
	}
}

// Aliases and merge keys are resolved the way the loader does (reviewer 3).
func Test_config_effective_resolves_aliases_and_merge_keys(t *testing.T) {
	writeConfig(t, "base: &a\n  enabled: true\nautomations: *a\n")
	if ev, err := Effective("automations.enabled"); err != nil || ev.Value != "true" || ev.Source != "file" {
		t.Errorf("alias section: %+v (%v), want true from file", ev, err)
	}
	writeConfig(t, "x: &t true\nbrowser:\n  disabled: *t\n")
	if ev, err := Effective("browser.disabled"); err != nil || ev.Value != "true" {
		t.Errorf("alias leaf: %+v (%v), want true", ev, err)
	}
	writeConfig(t, "defaults: &d\n  automations:\n    enabled: true\n    maxBudgetUsd: 5\n<<: *d\n")
	if ev, err := Effective("automations.enabled"); err != nil || ev.Value != "true" {
		t.Errorf("merge key: %+v (%v), want true", ev, err)
	}
}

// A merge key above the path: a fresh section would shadow the merged one and
// drop its siblings, so the edit is refused (reviewer 3).
func Test_config_set_refuses_under_a_merge_key(t *testing.T) {
	body := "defaults: &d\n  automations:\n    enabled: true\n    maxBudgetUsd: 5\n<<: *d\n"
	path := writeConfig(t, body)
	if err := Set("automations.paused", "true"); err == nil || !strings.Contains(err.Error(), "merge key") {
		t.Fatalf("Set under a merge key = %v, want a refusal", err)
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Fatalf("config.yaml changed:\n%s", got)
	}
}
