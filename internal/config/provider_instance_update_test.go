package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useInstanceHome points SMIND_HOME at a fresh temp dir with config.yaml
// (0600) as its only content, and returns the config path.
func useInstanceHome(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	for _, k := range []string{EnvListen, EnvHostnames, EnvPassword} {
		t.Setenv(k, "")
	}
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func instanceYAML(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func modelsPtr(ms ...CustomModel) *[]CustomModel {
	if ms == nil {
		ms = []CustomModel{}
	}
	return &ms
}

func toolsPtr(ts ...string) *[]string {
	if ts == nil {
		ts = []string{}
	}
	return &ts
}

func boolPtr(b bool) *bool { return &b }

func TestUpdateInstance_LeavesOtherEntryByteIdentical(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "claude-a")
	dirB := filepath.Join(t.TempDir(), "claude-b")
	original := `# my smind config
server:
  port: 4650 # keep this
providers:
  custom:
    # claude-a: the work account
    - id: claude-a
      extends: claude-native
      env:
        CLAUDE_CONFIG_DIR: ` + dirA + `
      label: 'Claude A'

    # claude-b: the personal one
    - id: claude-b
      extends: claude-native
      env:
        CLAUDE_CONFIG_DIR: ` + dirB + `
      label: 'Claude B' # personal
`
	useInstanceHome(t, original)
	_, changed, err := UpdateProviderInstance("claude-a", InstanceUpdate{
		AdditionalModels: modelsPtr(CustomModel{ID: "glm-5.1"}),
		DisallowedTools:  toolsPtr("WebSearch"),
	})
	if err != nil || !changed {
		t.Fatalf("UpdateProviderInstance = changed %v, err %v", changed, err)
	}
	after := instanceYAML(t)
	head := strings.Index(original, "    - id: claude-a")
	tail := strings.Index(original, "    # claude-b")
	if head < 0 || tail < 0 {
		t.Fatal("setup: markers not found in the original")
	}
	if prefix := original[:head]; !strings.HasPrefix(after, prefix) {
		t.Errorf("bytes before claude-a's first line changed:\n%s", after)
	}
	if !strings.HasSuffix(after, original[tail:]) {
		t.Errorf("bytes from claude-b's head comment to EOF changed:\n%s", after)
	}
	block := after[len(original[:head]) : len(after)-len(original[tail:])]
	for _, want := range []string{"additionalModels:", "disallowedTools:"} {
		if !strings.Contains(block, want) {
			t.Errorf("claude-a's block lacks %q:\n%s", want, block)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers.Custom) != 2 {
		t.Fatalf("providers.custom = %+v", cfg.Providers.Custom)
	}
	a, b := cfg.Providers.Custom[0], cfg.Providers.Custom[1]
	if len(a.AdditionalModels) != 1 || a.AdditionalModels[0].ID != "glm-5.1" {
		t.Errorf("claude-a additionalModels = %+v", a.AdditionalModels)
	}
	if strings.Join(a.DisallowedTools, ",") != "WebSearch" {
		t.Errorf("claude-a disallowedTools = %q", a.DisallowedTools)
	}
	if len(b.AdditionalModels) != 0 || len(b.DisallowedTools) != 0 || b.Env["CLAUDE_CONFIG_DIR"].Literal != dirB {
		t.Errorf("claude-b changed: %+v", b)
	}
}

func TestUpdateInstance_HandWrittenEntryWithComments(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), "hand-claude")
	original := `# hand-edited config
providers:
  custom:

    - id: hand
      extends: 'claude-native'   # quoted on purpose
      label: 'Hand written'
      # the models I actually use
      models:
        - id: 'sonnet-x'
        # and one more
        - id: 'haiku-x'
      env:
        CLAUDE_CONFIG_DIR: '` + claudeDir + `'
`
	useInstanceHome(t, original)
	_, changed, err := UpdateProviderInstance("hand", InstanceUpdate{InheritClaudeConfig: boolPtr(true)})
	if err != nil || !changed {
		t.Fatalf("UpdateProviderInstance = changed %v, err %v", changed, err)
	}
	after := instanceYAML(t)
	kept := []string{
		"# hand-edited config",
		"# quoted on purpose",
		"# the models I actually use",
		"# and one more",
		"extends: 'claude-native'   # quoted on purpose",
		"label: 'Hand written'",
		"- id: 'sonnet-x'",
		"- id: 'haiku-x'",
		"inheritClaudeConfig: true",
	}
	for _, want := range kept {
		if !strings.Contains(after, want) {
			t.Errorf("config.yaml lost %q:\n%s", want, after)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := cfg.Providers.Custom[0]
	if !entry.InheritClaudeConfig || len(entry.Models) != 2 || entry.Models[0].ID != "sonnet-x" || entry.Models[1].ID != "haiku-x" {
		t.Errorf("hand = %+v", entry)
	}
}

func TestUpdateInstance_InsertRemoveReplaceFields(t *testing.T) {
	useInstanceHome(t, `providers:
  custom:
    - id: inst
      extends: claude-native
      models:
        - id: m1
          label: M1
        - id: m2
`)
	entry := func(t *testing.T) CustomProvider {
		t.Helper()
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Providers.Custom[0]
	}

	_, changed, err := UpdateProviderInstance("inst", InstanceUpdate{
		Models: modelsPtr(CustomModel{ID: "r1", Default: true}, CustomModel{ID: "r2"}),
	})
	if err != nil || !changed {
		t.Fatalf("replace models = changed %v, err %v", changed, err)
	}
	if body := instanceYAML(t); strings.Contains(body, "m1") || !strings.Contains(body, "id: r1") {
		t.Errorf("models not replaced:\n%s", body)
	}
	if e := entry(t); len(e.Models) != 2 || e.Models[0].ID != "r1" || !e.Models[0].Default || e.Models[1].ID != "r2" || len(e.AdditionalModels) != 0 {
		t.Errorf("after replace = %+v", e)
	}

	_, changed, err = UpdateProviderInstance("inst", InstanceUpdate{
		Models:           modelsPtr(),
		AdditionalModels: modelsPtr(CustomModel{ID: "a1"}),
		DisallowedTools:  toolsPtr("Bash"),
	})
	if err != nil || !changed {
		t.Fatalf("switch to additionalModels = changed %v, err %v", changed, err)
	}
	body := instanceYAML(t)
	if strings.Contains(body, "models:") && !strings.Contains(body, "additionalModels:") {
		t.Errorf("the models key survived the switch:\n%s", body)
	}
	if !strings.Contains(body, "additionalModels:") || !strings.Contains(body, "disallowedTools:") {
		t.Errorf("switch did not write both keys:\n%s", body)
	}
	if e := entry(t); len(e.Models) != 0 || len(e.AdditionalModels) != 1 || e.AdditionalModels[0].ID != "a1" || strings.Join(e.DisallowedTools, ",") != "Bash" {
		t.Errorf("after switch = %+v", e)
	}

	_, changed, err = UpdateProviderInstance("inst", InstanceUpdate{DisallowedTools: toolsPtr()})
	if err != nil || !changed {
		t.Fatalf("remove disallowedTools = changed %v, err %v", changed, err)
	}
	if body := instanceYAML(t); strings.Contains(body, "disallowedTools") {
		t.Errorf("disallowedTools survived the removal:\n%s", body)
	}
	if e := entry(t); len(e.DisallowedTools) != 0 {
		t.Errorf("after removal = %+v", e)
	}

	if _, changed, err := UpdateProviderInstance("inst", InstanceUpdate{InheritClaudeConfig: boolPtr(true)}); err != nil || !changed {
		t.Fatalf("inherit true = changed %v, err %v", changed, err)
	}
	if body := instanceYAML(t); !strings.Contains(body, "inheritClaudeConfig: true") {
		t.Errorf("inheritClaudeConfig not written:\n%s", body)
	}
	if e := entry(t); !e.InheritClaudeConfig {
		t.Errorf("inheritClaudeConfig did not load: %+v", e)
	}
	if _, changed, err := UpdateProviderInstance("inst", InstanceUpdate{InheritClaudeConfig: boolPtr(false)}); err != nil || !changed {
		t.Fatalf("inherit false = changed %v, err %v", changed, err)
	}
	if body := instanceYAML(t); strings.Contains(body, "inheritClaudeConfig") {
		t.Errorf("inheritClaudeConfig survived false:\n%s", body)
	}
	if e := entry(t); e.InheritClaudeConfig {
		t.Errorf("inheritClaudeConfig still set: %+v", e)
	}
}

func TestUpdateInstance_LastEntryBeforeOtherSection(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), "last-claude")
	original := `providers:
  custom:
    - id: last
      extends: claude-native
      env:
        CLAUDE_CONFIG_DIR: ` + claudeDir + `

# the server section, kept by hand
server:
  port: 4700
`
	useInstanceHome(t, original)
	_, changed, err := UpdateProviderInstance("last", InstanceUpdate{
		AdditionalModels: modelsPtr(CustomModel{ID: "glm-4.7"}),
	})
	if err != nil || !changed {
		t.Fatalf("UpdateProviderInstance = changed %v, err %v", changed, err)
	}
	after := instanceYAML(t)
	tail := strings.Index(original, "# the server section")
	if tail < 0 {
		t.Fatal("setup: section comment not found")
	}
	if !strings.HasSuffix(after, original[tail:]) {
		t.Errorf("the following section changed:\n%s", after)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers.Custom) != 1 || len(cfg.Providers.Custom[0].AdditionalModels) != 1 || cfg.Providers.Custom[0].AdditionalModels[0].ID != "glm-4.7" {
		t.Fatalf("providers.custom = %+v", cfg.Providers.Custom)
	}
	if cfg.Server.Port != 4700 {
		t.Errorf("server.port = %d", cfg.Server.Port)
	}
}
