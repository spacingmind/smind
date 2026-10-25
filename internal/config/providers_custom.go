package config

import (
	"fmt"
	"regexp"
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
// Extends (claude-native or codex-native) and, in Env, only that provider's
// config-directory variable (CLAUDE_CONFIG_DIR / CODEX_HOME, the same
// allowlist as providers.<id>.env); it runs the extended provider's code, side
// by side with it.
type CustomProvider struct {
	ID      string            `yaml:"id"`
	Label   string            `yaml:"label"`
	Extends string            `yaml:"extends"`
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
	Models  []CustomModel     `yaml:"models"`
	Modes   []CustomMode      `yaml:"modes"`
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
)

// ValidateCustomProviders checks every providers.custom entry and returns the
// first problem with the offending field named (`providers.custom[2].command:
// required`). It normalises instance env paths in place (~/ expanded, cleaned),
// like providers.<id>.env. Env values are never echoed in an error.
func ValidateCustomProviders(list []CustomProvider) error {
	seen := map[string]bool{}
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
			if err := validateInstance(p, field); err != nil {
				return err
			}
			continue
		}
		if err := validateACPEntry(p, field); err != nil {
			return err
		}
	}
	return nil
}

func validateInstance(p *CustomProvider, field func(string) string) error {
	if p.Extends != providers.IDClaudeNative && p.Extends != providers.IDCodexNative {
		return fmt.Errorf("%s: %q must be %s or %s", field("extends"), p.Extends, providers.IDClaudeNative, providers.IDCodexNative)
	}
	switch {
	case p.Command != "":
		return fmt.Errorf("%s: not allowed with extends (an instance runs %s's own command)", field("command"), p.Extends)
	case len(p.Args) > 0:
		return fmt.Errorf("%s: not allowed with extends", field("args"))
	case len(p.Models) > 0:
		return fmt.Errorf("%s: not allowed with extends", field("models"))
	case len(p.Modes) > 0:
		return fmt.Errorf("%s: not allowed with extends", field("modes"))
	}
	for key, value := range p.Env {
		clean, err := ValidateProviderEnv(p.Extends, key, value)
		if err != nil {
			return fmt.Errorf("%s: %w", field("env."+key), err)
		}
		p.Env[key] = clean
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
		// Free-form env of the agent command: no allowlist, but the value is
		// never put in an error (it may be a token).
		if !envNameRe.MatchString(key) {
			return fmt.Errorf("%s: the name is not a valid environment variable name", field("env."+key))
		}
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("%s: the value must not contain NUL characters", field("env."+key))
		}
	}
	ids := map[string]bool{}
	for j, m := range p.Models {
		f := func(s string) string { return field(fmt.Sprintf("models[%d].%s", j, s)) }
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
	ids = map[string]bool{}
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

// ProviderSpecs converts validated entries to registry specs.
func ProviderSpecs(list []CustomProvider) []providers.Spec {
	out := make([]providers.Spec, 0, len(list))
	for _, p := range list {
		s := providers.Spec{ID: p.ID, Label: strings.TrimSpace(p.Label), Extends: p.Extends, Env: copyEnv(p.Env)}
		if p.Extends == "" {
			s.Runner = providers.RunnerACP
			s.Command = append([]string{p.Command}, p.Args...)
			for _, m := range p.Models {
				s.Models = append(s.Models, providers.Model{ID: m.ID, Label: orID(m.Label, m.ID), Description: m.Description, Default: m.Default})
			}
			for _, m := range p.Modes {
				s.Modes = append(s.Modes, providers.Mode{ID: m.ID, Label: orID(m.Label, m.ID), Description: m.Description, AutoApproves: m.AutoApproves})
			}
		}
		out = append(out, s)
	}
	return out
}

func orID(label, id string) string {
	if label != "" {
		return label
	}
	return id
}

func copyEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
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
		entry.Env = map[string]string{keys[0].Key: clean}
	}
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("id"), str(entry.ID)}}
	if entry.Label != "" {
		node.Content = append(node.Content, str("label"), str(entry.Label))
	}
	node.Content = append(node.Content, str("extends"), str(extends))
	if entry.Env != nil {
		env := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str(keys[0].Key), str(entry.Env[keys[0].Key])}}
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
