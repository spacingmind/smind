package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/spacingmind/smind/internal/providers"
)

// ProvidersConfig is the `providers:` section: one block per built-in provider
// id (ProviderConfig) plus `custom`, the list of providers added by config
// (ADR-0042, plugins plan P0): ACP agents, and instances of claude-native or
// codex-native with their own config directory.
type ProvidersConfig struct {
	ByID   map[string]ProviderConfig `yaml:",inline"`
	Custom []CustomProvider          `yaml:"custom"`
}

// CustomProvider is one providers.custom entry.
//
// An ACP provider sets Command (+ Args, Env, Models, Modes). An instance sets
// Extends (any built-in provider, or an earlier ACP entry) and overlays it
// (ADR-0046): Command (a different executable; Args only for an ACP base),
// Models (replace the base's list) or AdditionalModels (merged into it),
// DisallowedTools (Claude Code), and Env — typed values against the runner's
// per-harness allowlist (ADR-0046 section 3): a literal, a `secret:` reference
// or a passthrough; a key never sits inline in config.yaml. It runs the
// extended provider's code, side by side with it.
type CustomProvider struct {
	ID               string              `yaml:"id"`
	Label            string              `yaml:"label"`
	Extends          string              `yaml:"extends"`
	Command          string              `yaml:"command"`
	Args             []string            `yaml:"args"`
	Env              map[string]EnvValue `yaml:"env"`
	Models           []CustomModel       `yaml:"models"`
	AdditionalModels []CustomModel       `yaml:"additionalModels"`
	DisallowedTools  []string            `yaml:"disallowedTools"`
	Modes            []CustomMode        `yaml:"modes"`
}

// CustomModel is one entry of a custom ACP provider's model list.
type CustomModel struct {
	ID          string `yaml:"id"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
	Default     bool   `yaml:"default"`
}

// CustomMode is one entry of a custom ACP provider's permission modes.
type CustomMode struct {
	ID           string `yaml:"id"`
	Label        string `yaml:"label"`
	Description  string `yaml:"description"`
	AutoApproves bool   `yaml:"autoApproves"`
}

var (
	customIDRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	envNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	maxLabelLen = 80
	// maxToolNameLen bounds one disallowedTools entry.
	maxToolNameLen = 200
)

// ValidateCustomProviders checks every providers.custom entry and returns the
// first problem with the offending field named (`providers.custom[2].command:
// required`). It normalises instance env paths in place (~/ expanded, cleaned),
// like providers.<id>.env. Env values are never echoed in an error.
func ValidateCustomProviders(list []CustomProvider) error {
	seen := map[string]bool{}
	// bases are the ids an instance may extend: every built-in, then each
	// earlier non-instance entry (an ACP provider), mapped to its runner.
	bases := map[string]providers.Runner{}
	for _, id := range providers.BuiltinIDs() {
		bases[id] = providers.BuiltinRunner(id)
	}
	for i := range list {
		p := &list[i]
		field := func(f string) string { return fmt.Sprintf("providers.custom[%d].%s", i, f) }
		p.ID = strings.TrimSpace(p.ID)
		switch {
		case p.ID == "":
			return fmt.Errorf("%s: required", field("id"))
		case !customIDRe.MatchString(p.ID):
			return fmt.Errorf("%s: %q must be 1-64 characters of a-z, 0-9, . _ - and start with a letter or digit", field("id"), p.ID)
		case providers.ReservedID(p.ID):
			return fmt.Errorf("%s: %q is reserved (a built-in provider or a reserved prefix)", field("id"), p.ID)
		case seen[p.ID]:
			return fmt.Errorf("%s: duplicate id %q", field("id"), p.ID)
		}
		seen[p.ID] = true
		if len(p.Label) > maxLabelLen {
			return fmt.Errorf("%s: longer than %d characters", field("label"), maxLabelLen)
		}
		if p.Extends != "" {
			if err := validateInstance(p, bases, field); err != nil {
				return err
			}
			continue
		}
		if err := validateACPEntry(p, field); err != nil {
			return err
		}
		bases[p.ID] = providers.RunnerACP
	}
	return nil
}

func validateInstance(p *CustomProvider, bases map[string]providers.Runner, field func(string) string) error {
	runner, ok := bases[p.Extends]
	if !ok {
		return fmt.Errorf("%s: %q must be a built-in provider or an earlier providers.custom ACP entry", field("extends"), p.Extends)
	}
	if len(p.Modes) > 0 {
		return fmt.Errorf("%s: not allowed with extends", field("modes"))
	}
	if err := validateInstanceCommand(p, runner, field); err != nil {
		return err
	}
	if len(p.Models) > 0 && len(p.AdditionalModels) > 0 {
		return fmt.Errorf("%s: not allowed together with models (models replaces the list, additionalModels extends it)", field("additionalModels"))
	}
	if err := validateModelList(p.Models, field, "models"); err != nil {
		return err
	}
	if err := validateModelList(p.AdditionalModels, field, "additionalModels"); err != nil {
		return err
	}
	if len(p.DisallowedTools) > 0 && runner != providers.RunnerClaude {
		return fmt.Errorf("%s: only applies to an instance of %s", field("disallowedTools"), providers.IDClaudeNative)
	}
	for j, tool := range p.DisallowedTools {
		if err := validateToolName(tool); err != nil {
			return fmt.Errorf("%s: %w", field(fmt.Sprintf("disallowedTools[%d]", j)), err)
		}
	}
	for key, value := range p.Env {
		clean, err := ValidateInstanceEnv(runner, key, value)
		if err != nil {
			return fmt.Errorf("%s: %w", field("env."+key), err)
		}
		p.Env[key] = clean
	}
	return nil
}

// validateInstanceCommand checks an instance's command override: an ACP base
// takes a command plus args, claude-native and codex-native only the
// executable (no args: their launch is not a free-form argv).
func validateInstanceCommand(p *CustomProvider, runner providers.Runner, field func(string) string) error {
	if p.Command == "" {
		if len(p.Args) > 0 {
			return fmt.Errorf("%s: needs command", field("args"))
		}
		return nil
	}
	if strings.ContainsAny(p.Command, "\x00\n\r") {
		return fmt.Errorf("%s: must not contain NUL or newline characters", field("command"))
	}
	if runner != providers.RunnerACP && len(p.Args) > 0 {
		return fmt.Errorf("%s: not allowed for an instance of %s (only command, the executable)", field("args"), p.Extends)
	}
	for j, a := range p.Args {
		if strings.ContainsRune(a, 0) {
			return fmt.Errorf("%s: must not contain NUL characters", field(fmt.Sprintf("args[%d]", j)))
		}
	}
	return nil
}

// validateToolName checks one disallowedTools entry (a Claude Code tool name
// or rule such as "WebSearch" or "Bash(rm:*)"): non-empty, bounded, printable.
func validateToolName(tool string) error {
	switch {
	case strings.TrimSpace(tool) == "":
		return fmt.Errorf("required")
	case len(tool) > maxToolNameLen:
		return fmt.Errorf("longer than %d characters", maxToolNameLen)
	}
	for _, c := range tool {
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("must not contain control characters")
		}
	}
	return nil
}

// validateModelList checks a models / additionalModels list: ids required,
// syntax-valid and unique.
func validateModelList(models []CustomModel, field func(string) string, name string) error {
	ids := map[string]bool{}
	for j, m := range models {
		f := func(s string) string { return field(fmt.Sprintf("%s[%d].%s", name, j, s)) }
		if m.ID == "" {
			return fmt.Errorf("%s: required", f("id"))
		}
		if err := ValidateModelName(m.ID); err != nil {
			return fmt.Errorf("%s: %w", f("id"), err)
		}
		if ids[m.ID] {
			return fmt.Errorf("%s: duplicate model id %q", f("id"), m.ID)
		}
		ids[m.ID] = true
	}
	return nil
}

func validateACPEntry(p *CustomProvider, field func(string) string) error {
	if strings.TrimSpace(p.Command) == "" {
		return fmt.Errorf("%s: required", field("command"))
	}
	if strings.ContainsAny(p.Command, "\x00\n\r") {
		return fmt.Errorf("%s: must not contain NUL or newline characters", field("command"))
	}
	for j, a := range p.Args {
		if strings.ContainsRune(a, 0) {
			return fmt.Errorf("%s: must not contain NUL characters", field(fmt.Sprintf("args[%d]", j)))
		}
	}
	for key, value := range p.Env {
		clean, err := ValidateInstanceEnv(providers.RunnerACP, key, value)
		if err != nil {
			return fmt.Errorf("%s: %w", field("env."+key), err)
		}
		p.Env[key] = clean
	}
	if len(p.AdditionalModels) > 0 {
		return fmt.Errorf("%s: only applies to an instance (use models)", field("additionalModels"))
	}
	if len(p.DisallowedTools) > 0 {
		return fmt.Errorf("%s: only applies to an instance of %s", field("disallowedTools"), providers.IDClaudeNative)
	}
	if err := validateModelList(p.Models, field, "models"); err != nil {
		return err
	}
	ids := map[string]bool{}
	for j, m := range p.Modes {
		f := func(s string) string { return field(fmt.Sprintf("modes[%d].%s", j, s)) }
		if strings.TrimSpace(m.ID) == "" {
			return fmt.Errorf("%s: required", f("id"))
		}
		if ids[m.ID] {
			return fmt.Errorf("%s: duplicate mode id %q", f("id"), m.ID)
		}
		ids[m.ID] = true
	}
	return nil
}

// ProviderSpecs converts validated entries to registry specs. Literal env
// values go to Spec.Env (empty literals kept: some vendors need NAME=""),
// `secret:` references to Spec.SecretEnv (env NAME -> secret name) and
// passthrough names to Spec.PassthroughEnv, sorted.
func ProviderSpecs(list []CustomProvider) []providers.Spec {
	out := make([]providers.Spec, 0, len(list))
	for _, p := range list {
		s := providers.Spec{ID: p.ID, Label: strings.TrimSpace(p.Label), Extends: p.Extends}
		for name, v := range p.Env {
			switch {
			case v.Secret != "":
				if s.SecretEnv == nil {
					s.SecretEnv = map[string]string{}
				}
				s.SecretEnv[name] = v.Secret
			case v.Passthrough:
				s.PassthroughEnv = append(s.PassthroughEnv, name)
			default:
				if s.Env == nil {
					s.Env = map[string]string{}
				}
				s.Env[name] = v.Literal
			}
		}
		sort.Strings(s.PassthroughEnv)
		if p.Extends == "" {
			s.Runner = providers.RunnerACP
			s.Command = append([]string{p.Command}, p.Args...)
			s.Models = specModels(p.Models)
			for _, m := range p.Modes {
				s.Modes = append(s.Modes, providers.Mode{ID: m.ID, Label: orID(m.Label, m.ID), Description: m.Description, AutoApproves: m.AutoApproves})
			}
		} else {
			if p.Command != "" {
				s.Command = append([]string{p.Command}, p.Args...)
			}
			s.DisallowedTools = append([]string(nil), p.DisallowedTools...)
			if len(p.Models) > 0 {
				s.Models, s.ModelsMode = specModels(p.Models), providers.ModelsReplace
			} else if len(p.AdditionalModels) > 0 {
				s.Models, s.ModelsMode = specModels(p.AdditionalModels), providers.ModelsAdd
			}
		}
		out = append(out, s)
	}
	return out
}

func specModels(list []CustomModel) []providers.Model {
	var out []providers.Model
	for _, m := range list {
		out = append(out, providers.Model{ID: m.ID, Label: orID(m.Label, m.ID), Description: m.Description, Default: m.Default})
	}
	return out
}

func orID(label, id string) string {
	if label != "" {
		return label
	}
	return id
}

// AddProviderInstance appends an instance entry (extends + its config
// directory) to providers.custom in config.yaml through the atomic,
// comment-preserving writer, after validating the resulting list with
// ValidateCustomProviders (duplicate or reserved id, bad path, ...). dir is
// stored in the extended provider's allowlisted env key (CLAUDE_CONFIG_DIR /
// CODEX_HOME), "~/" expanded. It returns the stored entry.
func AddProviderInstance(extends, id, label, dir string) (CustomProvider, error) {
	keys := ProviderEnvKeys[extends]
	if len(keys) == 0 || (extends != providers.IDClaudeNative && extends != providers.IDCodexNative) {
		return CustomProvider{}, fmt.Errorf("extends %q: must be %s or %s", extends, providers.IDClaudeNative, providers.IDCodexNative)
	}
	entry := CustomProvider{ID: strings.TrimSpace(id), Label: strings.TrimSpace(label), Extends: extends}
	if strings.TrimSpace(dir) != "" {
		clean, err := ValidateProviderEnv(extends, keys[0].Key, strings.TrimSpace(dir))
		if err != nil {
			return CustomProvider{}, err
		}
		entry.Env = map[string]EnvValue{keys[0].Key: {Literal: clean}}
	}
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("id"), str(entry.ID)}}
	if entry.Label != "" {
		node.Content = append(node.Content, str("label"), str(entry.Label))
	}
	node.Content = append(node.Content, str("extends"), str(extends))
	if entry.Env != nil {
		env := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str(keys[0].Key), str(entry.Env[keys[0].Key].Literal)}}
		node.Content = append(node.Content, str("env"), env)
	}
	err := mutate(func(root *yaml.Node) error {
		seq := find(root, []string{"providers", "custom"})
		if seq == nil || seq.Kind != yaml.SequenceNode {
			seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			setNode(root, []string{"providers", "custom"}, seq)
			seq = find(root, []string{"providers", "custom"})
		}
		seq.Content = append(seq.Content, node)
		var all []CustomProvider
		if err := seq.Decode(&all); err != nil {
			return fmt.Errorf("providers.custom: %w", err)
		}
		return ValidateCustomProviders(all)
	})
	return entry, err
}
