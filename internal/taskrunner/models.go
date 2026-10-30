package taskrunner

import (
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/providers"
)

// ModelInfo is one model a provider offers (ADR-0025): ID is the string the
// provider itself takes (Claude's --model value, an ACP model config
// option's value, a Codex model id); Label/Description are its own words.
// Default marks the provider's current/default model when it reports one.
type ModelInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
	// Source says where the entry came from (additive wire field):
	// "catalog" (smind's static Claude list), "config" (an instance's or a
	// custom provider's configured models) or "discovered" (the agent's own
	// list, learned by a probe).
	Source string `json:"source,omitempty"`
}

// Model sources (ModelInfo.Source).
const (
	ModelSourceCatalog    = "catalog"
	ModelSourceConfig     = "config"
	ModelSourceDiscovered = "discovered"
)

// acpModelConfigCategory is ACP's SessionConfigOption category for the
// model selector.
const acpModelConfigCategory = "model"

// maxModelLen bounds a model string's length.
const maxModelLen = 100

// ClaudeModelDefault is the synthetic choice of the claude-native `model`
// config option that means "the provider default": selecting it resets the
// live session's model (Client.SetModel(nil)), and passing it as a run's
// model is the same as passing none.
const ClaudeModelDefault = "default"

// claudeModels is the static claude-native model catalog: aliases the CLI
// resolves itself, plus the current full ids. A convenience, not an
// allowlist -- see ValidateModel.
func claudeModels() []ModelInfo {
	return stampSource([]ModelInfo{
		{ID: "sonnet", Label: "Sonnet (latest)", Description: "Alias for the latest Sonnet"},
		{ID: "opus", Label: "Opus (latest)", Description: "Alias for the latest Opus"},
		{ID: "haiku", Label: "Haiku (latest)", Description: "Alias for the latest Haiku"},
		{ID: "claude-fable-5-1", Label: "Fable 5.1"},
		{ID: "claude-opus-5-5", Label: "Opus 5.5"},
		{ID: "claude-sonnet-5-5", Label: "Sonnet 5.5"},
		{ID: "claude-haiku-5-5", Label: "Haiku 5.5"},
	}, ModelSourceCatalog)
}

func stampSource(models []ModelInfo, source string) []ModelInfo {
	for i := range models {
		models[i].Source = source
	}
	return models
}

// staticModels is s's built-in model catalog. Claude's static list is the
// default; an instance's models replace it, or (additionalModels) are merged
// into it by id. A custom ACP provider's configured list is served as is;
// ACP and Codex are otherwise discovered at runtime (Runner.ProviderCatalog).
func staticModels(s providers.Spec) []ModelInfo {
	if s.Runner == providers.RunnerClaude {
		if len(s.Models) == 0 {
			return claudeModels()
		}
		if s.ModelsMode == providers.ModelsAdd {
			return mergeModelInfos(claudeModels(), configModels(s.Models))
		}
	}
	return configModels(s.Models)
}

func configModels(models []providers.Model) []ModelInfo {
	var out []ModelInfo
	for _, m := range models {
		out = append(out, ModelInfo{ID: m.ID, Label: m.Label, Description: m.Description, Default: m.Default, Source: ModelSourceConfig})
	}
	return out
}

// mergeModelInfos is base plus extra by id: an extra entry replaces a base
// entry of the same id in place, new ids append.
func mergeModelInfos(base, extra []ModelInfo) []ModelInfo {
	out := append([]ModelInfo(nil), base...)
	for _, m := range extra {
		replaced := false
		for i := range out {
			if out[i].ID == m.ID {
				out[i], replaced = m, true
				break
			}
		}
		if !replaced {
			out = append(out, m)
		}
	}
	return out
}

// ValidateModelSyntax checks a model string's shape: empty (the provider
// default) is fine; otherwise 1-100 characters from [A-Za-z0-9._:/@\[\]-],
// not beginning with '-'. The string reaches `--model <name>` as argv and
// JSON fields, never a shell.
func ValidateModelSyntax(model string) error {
	if model == "" {
		return nil
	}
	if len(model) > maxModelLen {
		return fmt.Errorf("invalid model: longer than %d characters", maxModelLen)
	}
	if model[0] == '-' {
		return fmt.Errorf("invalid model %q: must not begin with '-'", model)
	}
	for _, c := range model {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune("._:/@[]-", c):
		default:
			return fmt.Errorf("invalid model %q: only letters, digits and . _ : / @ [ ] - are allowed", model)
		}
	}
	return nil
}

// ValidateModel checks model against catalog (Runner.ProviderCatalog's
// entry for the run's provider). A bad shape is always rejected; an id
// missing from the catalog only when catalog.ModelsDiscovered -- the Claude
// catalog is static and any syntax-valid id passes, and an undiscovered
// ACP/Codex catalog passes the string through for the backend to judge.
func ValidateModel(catalog ProviderInfo, model string) error {
	if err := ValidateModelSyntax(model); err != nil {
		return err
	}
	if model == "" || !catalog.ModelsDiscovered {
		return nil
	}
	ids := make([]string, len(catalog.Models))
	for i, m := range catalog.Models {
		if m.ID == model {
			return nil
		}
		ids[i] = m.ID
	}
	return fmt.Errorf("invalid model %q for provider %q (valid: %s)", model, catalog.ID, strings.Join(ids, ", "))
}

// modelCatalog is one provider's discovered models. configID names the ACP
// config option they came from (runACP applies a model with
// session/set_config_option on it); current is the option's current value.
type modelCatalog struct {
	models   []ModelInfo
	configID string
	current  string
}

// deriveACPModels builds a catalog from a session's advertised config
// options: the first select option with category "model" and choices.
func deriveACPModels(options []acp.ConfigOption) (modelCatalog, bool) {
	for _, o := range options {
		if o.Category != acpModelConfigCategory || o.Type != "select" || len(o.Options) == 0 {
			continue
		}
		c := modelCatalog{configID: o.ConfigID, current: configCurrentValue(o.CurrentValue)}
		for _, so := range o.Options {
			c.models = append(c.models, ModelInfo{ID: so.Value, Label: so.Name, Description: so.Description, Default: so.Value == c.current, Source: ModelSourceDiscovered})
		}
		return c, true
	}
	return modelCatalog{}, false
}
