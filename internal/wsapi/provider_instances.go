package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/fsutil"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// maxDirSuggestions caps provider.dirSuggestions.
const maxDirSuggestions = 20

// dirLister is the only filesystem access provider.dirSuggestions has: list
// one directory's entries (names and types, from the directory read itself).
// It is a seam so a test can prove nothing else is touched.
type dirLister interface {
	ReadDir(name string) ([]fs.DirEntry, error)
}

type osDirLister struct{}

func (osDirLister) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }

// dirSuggestion is one provider.dirSuggestions entry. Names and paths only:
// never a file count, size or content.
type dirSuggestion struct {
	// Name is the directory's name under the daemon user's home (".claude-work").
	Name string `json:"name"`
	// Dir is its absolute path, the value provider.addInstance takes.
	Dir string `json:"dir"`
	// Extends is the provider the name suggests: claude-native for ~/.claude*,
	// codex-native for ~/.codex*.
	Extends string `json:"extends"`
	// SuggestedID and SuggestedLabel are a starting point for the add form.
	SuggestedID    string `json:"suggestedId"`
	SuggestedLabel string `json:"suggestedLabel"`
}

type dirSuggestionsResult struct {
	Suggestions []dirSuggestion `json:"suggestions"`
}

var suggestPrefixes = []struct{ prefix, extends string }{
	{".claude", providers.IDClaudeNative},
	{".codex", providers.IDCodexNative},
}

// effectiveBaseDir is the directory a launch of the built-in provider uses
// for its allowlisted env key (configured, else the daemon's environment,
// else the CLI default under home).
func effectiveBaseDir(runner *taskrunner.Runner, home, base string) string {
	keys := config.ProviderEnvKeys[base]
	if len(keys) == 0 {
		return ""
	}
	if v := runner.ProviderEnv(taskrunner.Provider(base))[keys[0].Key]; v != "" {
		return filepath.Clean(v)
	}
	if v := os.Getenv(keys[0].Key); v != "" {
		return filepath.Clean(v)
	}
	return filepath.Join(home, keys[0].DefaultDir)
}

// suggestDirs lists the ~/.claude* and ~/.codex* directories under home that
// are not already in use: not a built-in provider's effective directory and
// not an existing instance's. Only home itself is read, and only its entry
// names and types; symlinks and files are skipped, so nothing inside a
// suggested directory is ever opened.
func suggestDirs(runner *taskrunner.Runner, lister dirLister, home string) ([]dirSuggestion, error) {
	entries, err := lister.ReadDir(home)
	if err != nil {
		return nil, fmt.Errorf("provider.dirSuggestions: read home: %w", err)
	}
	taken := map[string]bool{}
	for _, base := range []string{providers.IDClaudeNative, providers.IDCodexNative} {
		taken[effectiveBaseDir(runner, home, base)] = true
	}
	for _, s := range runner.Providers().List() {
		if !s.IsInstance() {
			continue
		}
		for _, v := range s.Env {
			taken[filepath.Clean(v)] = true
		}
	}
	ids := map[string]bool{}
	for _, s := range runner.Providers().List() {
		ids[s.ID] = true
	}
	var out []dirSuggestion
	for _, e := range entries {
		if !e.IsDir() || fsutil.IsLink(e.Type()) {
			continue
		}
		for _, p := range suggestPrefixes {
			if !strings.HasPrefix(e.Name(), p.prefix) {
				continue
			}
			dir := filepath.Join(home, e.Name())
			if taken[dir] {
				break
			}
			id := suggestID(strings.TrimPrefix(e.Name(), "."), ids)
			out = append(out, dirSuggestion{Name: e.Name(), Dir: dir, Extends: p.extends, SuggestedID: id, SuggestedLabel: suggestLabel(e.Name(), p.prefix)})
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) > maxDirSuggestions {
		out = out[:maxDirSuggestions]
	}
	return out, nil
}

func suggestID(base string, used map[string]bool) string {
	id := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return '-'
	}, base)
	if id == "" || providers.ReservedID(id) || used[id] {
		id = "x-" + id
	}
	return id
}

func suggestLabel(name, prefix string) string {
	product := "Claude"
	if prefix == ".codex" {
		product = "Codex"
	}
	rest := strings.Trim(strings.TrimPrefix(name, prefix), "-_.")
	if rest == "" {
		return product
	}
	return product + " " + rest
}

// handleProviderDirSuggestions is provider.dirSuggestions: the candidates for
// a new provider instance, found by directory name only. Humans only.
func handleProviderDirSuggestions(runner *taskrunner.Runner, lister dirLister) handlerFunc {
	return func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.dirSuggestions is not available to agents"}
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("provider.dirSuggestions: %w", err)
		}
		out, err := suggestDirs(runner, lister, home)
		if err != nil {
			return nil, err
		}
		if out == nil {
			out = []dirSuggestion{}
		}
		return dirSuggestionsResult{Suggestions: out}, nil
	}
}

// reloadProviders re-reads config.yaml and replaces the registry's custom
// providers (ACP entries and instances) with providers.custom, atomically: an
// invalid file changes nothing and the error names the field. Secrets of
// instances the new catalog dropped are pruned (their error carries no
// value, and a prune that fails leaves only orphaned rows behind, not a bad
// registry, so it is ignored).
func reloadProviders(runner *taskrunner.Runner) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := runner.Providers().SetCustom(config.ProviderSpecs(cfg.Providers.Custom)); err != nil {
		return err
	}
	_, _ = runner.PruneProviderSecrets()
	return nil
}

// publishProvidersChanged tells clients the provider catalog changed (id ""
// when more than one provider may have).
func publishProvidersChanged(bus *eventBus, runner *taskrunner.Runner, id string) providerUpdatedPayload {
	res := providerUpdatedPayload{ID: id}
	if id != "" {
		res = newProviderUpdatedPayload(runner, id)
	}
	if bus != nil {
		bus.Publish(Event{Topic: TopicProviderUpdated, Payload: res})
	}
	return res
}

// handleProviderReload is provider.reload: apply the providers.custom section
// of config.yaml to the running daemon (a hand edit, without a restart).
// Humans only.
func handleProviderReload(runner *taskrunner.Runner, bus *eventBus) handlerFunc {
	return func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.reload is not available to agents"}
		}
		providerSetDefaultModelMu.Lock()
		defer providerSetDefaultModelMu.Unlock()
		if err := reloadProviders(runner); err != nil {
			return nil, fmt.Errorf("provider.reload: %w", err)
		}
		publishProvidersChanged(bus, runner, "")
		return providerListResultFor(runner, nil), nil
	}
}

// handleProviderAddInstance is provider.addInstance {extends, id, label, dir}:
// add an instance of claude-native or codex-native with its own config
// directory. It writes config.yaml providers.custom through the config
// writer (same validation as startup), then applies it live. Humans only: a
// run-scoped (agent) caller is refused.
func handleProviderAddInstance(runner *taskrunner.Runner, bus *eventBus) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.addInstance is not available to agents"}
		}
		var p struct {
			Extends string `json:"extends"`
			ID      string `json:"id"`
			Label   string `json:"label"`
			Dir     string `json:"dir"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("provider.addInstance: invalid params: %w", err)
		}
		providerSetDefaultModelMu.Lock()
		defer providerSetDefaultModelMu.Unlock()
		entry, err := config.AddProviderInstance(p.Extends, p.ID, p.Label, p.Dir)
		if err != nil {
			return nil, fmt.Errorf("provider.addInstance: %w", err)
		}
		if err := reloadProviders(runner); err != nil {
			return nil, fmt.Errorf("provider.addInstance: saved, but applying it failed: %w", err)
		}
		return publishProvidersChanged(bus, runner, entry.ID), nil
	}
}

// modelParam is one model of a provider.updateInstance models list.
type modelParam struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
}

func customModels(in *[]modelParam) *[]config.CustomModel {
	if in == nil {
		return nil
	}
	out := make([]config.CustomModel, len(*in))
	for i, m := range *in {
		out[i] = config.CustomModel{ID: m.ID, Label: m.Label, Description: m.Description, Default: m.Default}
	}
	return &out
}

// handleProviderUpdateInstance is provider.updateInstance: patch an
// instance's models, additionalModels, disallowedTools and
// inheritClaudeConfig in config.yaml (a byte-level patch of that entry only),
// then apply it live. An absent field is left alone, an empty one removes
// the key. Humans only: a run-scoped (agent) caller is refused.
func handleProviderUpdateInstance(runner *taskrunner.Runner, bus *eventBus) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.updateInstance is not available to agents"}
		}
		var p struct {
			ID                  string        `json:"id"`
			Models              *[]modelParam `json:"models"`
			AdditionalModels    *[]modelParam `json:"additionalModels"`
			DisallowedTools     *[]string     `json:"disallowedTools"`
			InheritClaudeConfig *bool         `json:"inheritClaudeConfig"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("provider.updateInstance: invalid params: %w", err)
		}
		if p.ID == "" {
			return nil, errors.New("provider.updateInstance: id is required")
		}
		providerSetDefaultModelMu.Lock()
		defer providerSetDefaultModelMu.Unlock()
		_, changed, err := config.UpdateProviderInstance(p.ID, config.InstanceUpdate{
			Models:              customModels(p.Models),
			AdditionalModels:    customModels(p.AdditionalModels),
			DisallowedTools:     p.DisallowedTools,
			InheritClaudeConfig: p.InheritClaudeConfig,
		})
		if err != nil {
			return nil, fmt.Errorf("provider.updateInstance: %w", err)
		}
		if !changed {
			return newProviderUpdatedPayload(runner, p.ID), nil
		}
		if err := reloadProviders(runner); err != nil {
			return nil, fmt.Errorf("provider.updateInstance: saved, but applying it failed: %w", err)
		}
		return publishProvidersChanged(bus, runner, p.ID), nil
	}
}
