package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if !strings.Contains(after, "\n\n    # claude-b: the personal one") {
		t.Errorf("the blank line before claude-b's head comment is gone:\n%s", after)
	}
	const claudeALastKey = "      label: 'Claude A'\n"
	expectedExact := strings.Replace(original, claudeALastKey,
		claudeALastKey+"      additionalModels:\n        - id: glm-5.1\n      disallowedTools:\n        - WebSearch\n", 1)
	if after != expectedExact {
		t.Errorf("file differs from the expected exact output:\n%s\nwant:\n%s", after, expectedExact)
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

// A hand-written env path that filepath.Clean would rewrite (a trailing "/sub/..")
// must not trip the pre-write safety net: validation normalises the in-memory
// entry, and the check compares the file's raw value against a pristine copy.
func TestUpdateInstance_UncleanHandWrittenValuesAreNotAnEditorChange(t *testing.T) {
	claudeDir := filepath.ToSlash(filepath.Join(t.TempDir(), "hand-claude"))
	rawDir := claudeDir + "/sub/.."
	original := `providers:
  custom:
    - id: hand
      extends: claude-native
      env:
        CLAUDE_CONFIG_DIR: '` + rawDir + `'
`
	useInstanceHome(t, original)
	if _, changed, err := UpdateProviderInstance("hand", InstanceUpdate{InheritClaudeConfig: boolPtr(true)}); err != nil || !changed {
		t.Fatalf("UpdateProviderInstance = changed %v, err %v", changed, err)
	}
	after := instanceYAML(t)
	if !strings.Contains(after, "CLAUDE_CONFIG_DIR: '"+rawDir+"'") {
		t.Errorf("the hand-written env value was rewritten:\n%s", after)
	}
	if !strings.Contains(after, "inheritClaudeConfig: true") {
		t.Errorf("the edit did not land:\n%s", after)
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

func TestUpdateInstance_NoTrailingNewline(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), "nn-claude")
	original := "providers:\n  custom:\n    - id: nn\n      extends: claude-native\n      env:\n        CLAUDE_CONFIG_DIR: " + claudeDir
	useInstanceHome(t, original)
	_, changed, err := UpdateProviderInstance("nn", InstanceUpdate{DisallowedTools: toolsPtr("WebSearch")})
	if err != nil || !changed {
		t.Fatalf("UpdateProviderInstance = changed %v, err %v", changed, err)
	}
	after := instanceYAML(t)
	if !strings.HasPrefix(after, original+"\n") {
		t.Errorf("original bytes not preserved:\n%s", after)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers.Custom[0].DisallowedTools; len(got) != 1 || got[0] != "WebSearch" {
		t.Errorf("disallowedTools = %v", got)
	}
}

func TestUpdateInstance_InvalidFieldsNamed(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), "iv-claude")
	codexDir := filepath.Join(t.TempDir(), "iv-codex")
	content := `providers:
  custom:
    - id: cl
      extends: claude-native
      env:
        CLAUDE_CONFIG_DIR: ` + claudeDir + `
    - id: cx
      extends: codex-native
      endpoint:
        kind: openai-responses
        baseUrl: http://127.0.0.1:1
      models:
        - id: gpt-x
      env:
        CODEX_HOME: ` + codexDir + `
    - id: cx2
      extends: codex-native
      env:
        CODEX_HOME: ` + codexDir + `2
`
	cases := []struct {
		name string
		id   string
		u    InstanceUpdate
		want string
	}{
		{"models and additionalModels", "cl", InstanceUpdate{
			Models:           modelsPtr(CustomModel{ID: "a"}),
			AdditionalModels: modelsPtr(CustomModel{ID: "b"}),
		}, "additionalModels"},
		{"disallowedTools on codex", "cx2", InstanceUpdate{DisallowedTools: toolsPtr("WebSearch")}, "disallowedTools"},
		{"empty tool name", "cl", InstanceUpdate{DisallowedTools: toolsPtr("")}, "disallowedTools[0]"},
		{"bad model id", "cl", InstanceUpdate{Models: modelsPtr(CustomModel{ID: "-bad"})}, "models[0].id"},
		{"duplicate model ids", "cl", InstanceUpdate{Models: modelsPtr(CustomModel{ID: "a"}, CustomModel{ID: "a"})}, "duplicate model id"},
		{"inherit on codex", "cx2", InstanceUpdate{InheritClaudeConfig: boolPtr(true)}, "inheritClaudeConfig"},
		{"endpoint instance without models", "cx", InstanceUpdate{Models: modelsPtr()}, "models"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useInstanceHome(t, content)
			before := instanceYAML(t)
			_, changed, err := UpdateProviderInstance(tc.id, tc.u)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "internal edit check failed") {
				t.Errorf("refused by the post-patch safety net, not the first validation: %v", err)
			}
			if changed {
				t.Error("changed = true on error")
			}
			if after := instanceYAML(t); after != before {
				t.Errorf("file changed on error:\n%s", after)
			}
		})
	}
}

func TestUpdateInstance_UnknownIDAndNonInstance(t *testing.T) {
	useInstanceHome(t, `providers:
  custom:
    - id: acp
      command: /bin/true
    - id: inst
      extends: claude-native
`)
	before := instanceYAML(t)
	u := InstanceUpdate{DisallowedTools: toolsPtr("WebSearch")}
	if _, _, err := UpdateProviderInstance("nope", u); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown id: err = %v", err)
	}
	if _, _, err := UpdateProviderInstance("acp", u); err == nil || !strings.Contains(err.Error(), "not an instance") {
		t.Errorf("non-instance: err = %v", err)
	}
	if after := instanceYAML(t); after != before {
		t.Errorf("file changed:\n%s", after)
	}
}

func TestUpdateInstance_IdempotentNoop(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), "id-claude")
	path := useInstanceHome(t, `providers:
  custom:
    - id: same
      extends: claude-native
      env:
        CLAUDE_CONFIG_DIR: `+claudeDir+`
      disallowedTools:
        - WebSearch
`)
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	before := instanceYAML(t)
	_, changed, err := UpdateProviderInstance("same", InstanceUpdate{
		DisallowedTools:     toolsPtr("WebSearch"),
		InheritClaudeConfig: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("changed = true for an identical update")
	}
	if after := instanceYAML(t); after != before {
		t.Errorf("bytes changed:\n%s", after)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(old) {
		t.Errorf("mtime = %v, want %v", st.ModTime(), old)
	}
}

func TestUpdateInstance_NothingToUpdate(t *testing.T) {
	useInstanceHome(t, "providers:\n  custom:\n    - id: n\n      extends: claude-native\n")
	before := instanceYAML(t)
	_, changed, err := UpdateProviderInstance("n", InstanceUpdate{})
	if err == nil || !strings.Contains(err.Error(), "nothing to update") || changed {
		t.Fatalf("changed %v, err %v", changed, err)
	}
	if after := instanceYAML(t); after != before {
		t.Errorf("file changed:\n%s", after)
	}
}

func TestUpdateInstance_FlowStyleRefused(t *testing.T) {
	useInstanceHome(t, "providers:\n  custom:\n    - {id: x, extends: claude-native}\n")
	before := instanceYAML(t)
	_, changed, err := UpdateProviderInstance("x", InstanceUpdate{DisallowedTools: toolsPtr("WebSearch")})
	if err == nil || !strings.Contains(err.Error(), "edit config.yaml by hand") || changed {
		t.Fatalf("changed %v, err %v", changed, err)
	}
	if after := instanceYAML(t); after != before {
		t.Errorf("file changed:\n%s", after)
	}
}

// instanceFields is the four editable fields of one entry, for step checks.
type instanceFields struct {
	models, additional []string
	tools              []string
	inherit            bool
}

func modelIDs(ms []CustomModel) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func checkInstanceStep(t *testing.T, step string, id string, want instanceFields) string {
	t.Helper()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("%s: Load: %v", step, err)
	}
	var got *CustomProvider
	for i := range cfg.Providers.Custom {
		if cfg.Providers.Custom[i].ID == id {
			got = &cfg.Providers.Custom[i]
		}
	}
	if got == nil {
		t.Fatalf("%s: %q missing", step, id)
	}
	if g, w := strings.Join(modelIDs(got.Models), ","), strings.Join(want.models, ","); g != w {
		t.Errorf("%s: models = %q, want %q", step, g, w)
	}
	if g, w := strings.Join(modelIDs(got.AdditionalModels), ","), strings.Join(want.additional, ","); g != w {
		t.Errorf("%s: additionalModels = %q, want %q", step, g, w)
	}
	if g, w := strings.Join(got.DisallowedTools, ","), strings.Join(want.tools, ","); g != w {
		t.Errorf("%s: disallowedTools = %q, want %q", step, g, w)
	}
	if got.InheritClaudeConfig != want.inherit {
		t.Errorf("%s: inheritClaudeConfig = %v, want %v", step, got.InheritClaudeConfig, want.inherit)
	}
	file := instanceYAML(t)
	// Count only the edited entry: a following entry has keys of its own.
	own, _, _ := strings.Cut(file, "    # the second entry")
	for _, key := range []string{"models", "additionalModels", "disallowedTools", "inheritClaudeConfig"} {
		if n := strings.Count(own, "\n      "+key+":"); n > 1 {
			t.Errorf("%s: key %q appears %d times:\n%s", step, key, n, file)
		}
	}
	return file
}

func runSeveralEditSteps(t *testing.T, id string) {
	t.Helper()
	steps := []struct {
		name string
		u    InstanceUpdate
		want instanceFields
	}{
		{"add two lists", InstanceUpdate{
			AdditionalModels: modelsPtr(CustomModel{ID: "glm-5.1"}, CustomModel{ID: "x"}),
			DisallowedTools:  toolsPtr("WebSearch", "Bash(rm:*)"),
		}, instanceFields{additional: []string{"glm-5.1", "x"}, tools: []string{"WebSearch", "Bash(rm:*)"}}},
		{"remove a middle key, add another", InstanceUpdate{
			Models:           modelsPtr(CustomModel{ID: "m"}),
			AdditionalModels: modelsPtr(),
		}, instanceFields{models: []string{"m"}, tools: []string{"WebSearch", "Bash(rm:*)"}}},
		{"replace tools and set inherit", InstanceUpdate{
			DisallowedTools:     toolsPtr("A"),
			InheritClaudeConfig: boolPtr(true),
		}, instanceFields{models: []string{"m"}, tools: []string{"A"}, inherit: true}},
		{"remove everything", InstanceUpdate{
			Models:              modelsPtr(),
			DisallowedTools:     toolsPtr(),
			InheritClaudeConfig: boolPtr(false),
		}, instanceFields{}},
	}
	for _, s := range steps {
		_, changed, err := UpdateProviderInstance(id, s.u)
		if err != nil || !changed {
			t.Fatalf("%s: changed %v, err %v", s.name, changed, err)
		}
		checkInstanceStep(t, s.name, id, s.want)
	}
}

func TestUpdateInstance_SeveralEditsInOneCall(t *testing.T) {
	t.Run("generated entry", func(t *testing.T) {
		useInstanceHome(t, "")
		if _, err := AddProviderInstance("claude-native", "claude-work", "Claude Work", t.TempDir()); err != nil {
			t.Fatal(err)
		}
		runSeveralEditSteps(t, "claude-work")
	})
	t.Run("hand-written entry with comments and a following entry", func(t *testing.T) {
		dirA := filepath.Join(t.TempDir(), "hw-a")
		dirB := filepath.Join(t.TempDir(), "hw-b")
		second := `    # the second entry
    - id: other
      extends: claude-native
      env:
        CLAUDE_CONFIG_DIR: ` + dirB + `
      disallowedTools:
        - Keep # me
`
		useInstanceHome(t, `providers:
  custom:
    # first, hand-written
    - id: first
      # which harness
      extends: claude-native
      # where its login lives
      env:
        CLAUDE_CONFIG_DIR: `+dirA+`
      label: 'First'
`+second)
		runSeveralEditSteps(t, "first")
		if after := instanceYAML(t); !strings.HasSuffix(after, second) {
			t.Errorf("the second entry changed:\n%s", after)
		}
	})
}
