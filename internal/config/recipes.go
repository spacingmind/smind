package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/spacingmind/smind/internal/providers"
)

// RecipeSecretName is the single secret name a recipe instance references:
// the pasted key (or the recipe's own placeholder) lives in the store under
// it, never in config.yaml.
const RecipeSecretName = "api-key"

// RecipeInput is what the caller adds on top of a recipe row: the instance
// identity and the values the row cannot know (a gateway URL, model ids).
type RecipeInput struct {
	ID      string
	Label   string
	BaseURL string
	Models  []string
}

// RecipeResult is what AddRecipeInstance did: the stored entry, whether it
// wrote one, the placeholder values to store as secrets when no key was
// pasted (secret name -> value), and the secret names the entry references,
// sorted.
type RecipeResult struct {
	Entry              CustomProvider
	Created            bool
	PlaceholderSecrets map[string]string
	SecretNames        []string
}

// BuildRecipeEntry materialises one recipe row as a providers.custom
// instance entry (pure: nothing is written, no key is ever embedded — a
// secret-kind env becomes a `secret: api-key` reference). Errors name the
// recipe and the missing input, never a value.
func BuildRecipeEntry(rec providers.Recipe, in RecipeInput) (CustomProvider, map[string]string, error) {
	switch rec.Harness {
	case "claude-code":
	case "codex-app-server":
		if strings.TrimSpace(in.BaseURL) == "" {
			return CustomProvider{}, nil, fmt.Errorf("recipe %q: baseUrl: required", rec.ID)
		}
	case "acp":
		// Only the chat-completions-via-OpenCode row is an ACP recipe.
		if rec.EndpointKind != string(providers.EndpointChatCompletions) {
			return CustomProvider{}, nil, fmt.Errorf("recipe %q: an acp recipe must be a chat-completions endpoint", rec.ID)
		}
		if strings.TrimSpace(in.BaseURL) == "" {
			return CustomProvider{}, nil, fmt.Errorf("recipe %q: baseUrl: required", rec.ID)
		}
	default: // a row the catalog loader would have rejected
		return CustomProvider{}, nil, fmt.Errorf("recipe %q: unsupported harness", rec.ID)
	}
	entry := CustomProvider{
		ID:              strings.TrimSpace(in.ID),
		Label:           strings.TrimSpace(in.Label),
		Recipe:          rec.ID,
		Vendor:          rec.Vendor,
		Thinking:        rec.Thinking,
		DisallowedTools: append([]string(nil), rec.DisallowedTools...),
	}
	if entry.ID == "" {
		entry.ID = rec.ID
	}
	if entry.Label == "" {
		entry.Label = rec.Label
	}
	switch rec.Harness {
	case "codex-app-server":
		entry.Extends = providers.IDCodexNative
		entry.Endpoint = &CustomEndpoint{Kind: "openai-responses", BaseURL: strings.TrimSpace(in.BaseURL)}
	case "acp":
		entry.Extends = providers.IDOpenCode
		entry.Endpoint = &CustomEndpoint{Kind: string(providers.EndpointChatCompletions), BaseURL: strings.TrimSpace(in.BaseURL)}
	default:
		entry.Extends = providers.IDClaudeNative
	}
	var placeholders map[string]string
	for _, e := range rec.Env {
		switch e.Kind {
		case "literal":
			setRecipeEnv(&entry, e.Name, EnvValue{Literal: e.Value})
		case "secret", "secretDefault":
			setRecipeEnv(&entry, e.Name, EnvValue{Secret: RecipeSecretName})
			if e.Kind == "secretDefault" {
				if placeholders == nil {
					placeholders = map[string]string{}
				}
				placeholders[RecipeSecretName] = e.Value
			}
		case "userBaseURL":
			if strings.TrimSpace(in.BaseURL) == "" {
				return CustomProvider{}, nil, fmt.Errorf("recipe %q: baseUrl: required", rec.ID)
			}
			// A codex or opencode recipe's URL rides on endpoint.baseUrl instead.
			if rec.Harness != "codex-app-server" && rec.Harness != "acp" {
				setRecipeEnv(&entry, e.Name, EnvValue{Literal: strings.TrimSpace(in.BaseURL)})
			}
		}
	}
	if len(in.Models) > 0 {
		for i, id := range in.Models {
			entry.Models = append(entry.Models, CustomModel{ID: strings.TrimSpace(id), Default: i == 0})
		}
	} else {
		for _, m := range rec.Models {
			entry.Models = append(entry.Models, CustomModel{ID: m.ID, Label: m.Label, Default: m.Default})
		}
	}
	if len(entry.Models) == 0 && rec.ModelsRequired {
		return CustomProvider{}, nil, fmt.Errorf("recipe %q: models: required", rec.ID)
	}
	return entry, placeholders, nil
}

func setRecipeEnv(entry *CustomProvider, name string, v EnvValue) {
	if entry.Env == nil {
		entry.Env = map[string]EnvValue{}
	}
	entry.Env[name] = v
}

// errRecipeInstanceExists aborts mutate before anything is written: the
// same recipe already owns this id.
var errRecipeInstanceExists = errors.New("recipe instance exists")

// AddRecipeInstance appends the entry BuildRecipeEntry materialises to
// providers.custom in config.yaml through the atomic, comment-preserving
// writer, after validating the resulting list (same as AddProviderInstance).
// It is idempotent: an existing entry with the same id from the same recipe
// is returned with Created false and the file untouched; an id used by
// another entry (or another recipe) is refused. The returned
// PlaceholderSecrets are values the caller should store as secrets when no
// key was pasted; a key never goes in the file.
func AddRecipeInstance(rec providers.Recipe, in RecipeInput) (RecipeResult, error) {
	entry, placeholders, err := BuildRecipeEntry(rec, in)
	if err != nil {
		return RecipeResult{}, err
	}
	var existing CustomProvider
	err = mutate(func(root *yaml.Node) error {
		seq := find(root, []string{"providers", "custom"})
		var all []CustomProvider
		if seq != nil && seq.Kind == yaml.SequenceNode {
			if err := seq.Decode(&all); err != nil {
				return fmt.Errorf("providers.custom: %w", err)
			}
		}
		for _, p := range all {
			if p.ID != entry.ID {
				continue
			}
			if p.Recipe == rec.ID {
				existing = p
				return errRecipeInstanceExists
			}
			return fmt.Errorf("provider id %q is already used", entry.ID)
		}
		if seq == nil || seq.Kind != yaml.SequenceNode {
			seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			setNode(root, []string{"providers", "custom"}, seq)
			seq = find(root, []string{"providers", "custom"})
		}
		seq.Content = append(seq.Content, recipeEntryNode(rec, entry))
		var merged []CustomProvider
		if err := seq.Decode(&merged); err != nil {
			return fmt.Errorf("providers.custom: %w", err)
		}
		return ValidateCustomProviders(merged)
	})
	if errors.Is(err, errRecipeInstanceExists) {
		return RecipeResult{Entry: existing, Created: false, SecretNames: recipeSecretNames(existing)}, nil
	}
	if err != nil {
		return RecipeResult{}, err
	}
	return RecipeResult{Entry: entry, Created: true, PlaceholderSecrets: placeholders, SecretNames: recipeSecretNames(entry)}, nil
}

// recipeSecretNames lists the secret names the entry's env references,
// sorted (a recipe entry references at most one).
func recipeSecretNames(entry CustomProvider) []string {
	var names []string
	for _, v := range entry.Env {
		if v.Secret != "" {
			names = append(names, v.Secret)
		}
	}
	sort.Strings(names)
	return names
}

// recipeEntryNode builds the entry's yaml node with the fields in a fixed
// order, like AddProviderInstance: id, label, extends, recipe, vendor,
// thinking, inheritClaudeConfig, env, endpoint, models, disallowedTools.
// Env is written in the recipe's catalog order; a literal stays a string
// (an empty one as an explicit "") and a secret reference as
// {secret: <name>} — a value never appears.
func recipeEntryNode(rec providers.Recipe, entry CustomProvider) *yaml.Node {
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
	boolean := func(v bool) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"} }
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("id"), str(entry.ID)}}
	if entry.Label != "" {
		node.Content = append(node.Content, str("label"), str(entry.Label))
	}
	node.Content = append(node.Content, str("extends"), str(entry.Extends), str("recipe"), str(entry.Recipe))
	if entry.Vendor != "" {
		node.Content = append(node.Content, str("vendor"), str(entry.Vendor))
	}
	if entry.Thinking != "" {
		node.Content = append(node.Content, str("thinking"), str(entry.Thinking))
	}
	if entry.InheritClaudeConfig {
		node.Content = append(node.Content, str("inheritClaudeConfig"), boolean(true))
	}
	if len(entry.Env) > 0 {
		env := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, e := range rec.Env {
			v, ok := entry.Env[e.Name]
			if !ok {
				continue
			}
			var val *yaml.Node
			if v.Secret != "" {
				val = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("secret"), str(v.Secret)}}
			} else {
				val = str(v.Literal)
			}
			env.Content = append(env.Content, str(e.Name), val)
		}
		node.Content = append(node.Content, str("env"), env)
	}
	if entry.Endpoint != nil {
		endpoint := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("kind"), str(entry.Endpoint.Kind), str("baseUrl"), str(entry.Endpoint.BaseURL)}}
		node.Content = append(node.Content, str("endpoint"), endpoint)
	}
	if len(entry.Models) > 0 {
		models := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, m := range entry.Models {
			mn := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("id"), str(m.ID)}}
			if m.Label != "" {
				mn.Content = append(mn.Content, str("label"), str(m.Label))
			}
			if m.Default {
				mn.Content = append(mn.Content, str("default"), boolean(true))
			}
			models.Content = append(models.Content, mn)
		}
		node.Content = append(node.Content, str("models"), models)
	}
	if len(entry.DisallowedTools) > 0 {
		tools := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, tool := range entry.DisallowedTools {
			tools.Content = append(tools.Content, str(tool))
		}
		node.Content = append(node.Content, str("disallowedTools"), tools)
	}
	return node
}
