package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// recipeEnvView is one env entry of a provider.recipes row. A secret or
// secretDefault value is blanked: a placeholder is never sent either, only
// the store ever holds values.
type recipeEnvView struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// recipeModelView is one model of a provider.recipes row.
type recipeModelView struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
}

// recipeView is one catalog row of provider.recipes, plus what the add form
// needs: needsBaseUrl (a userBaseURL env or the codex harness) and needsKey
// (an env the user must paste a key into).
type recipeView struct {
	ID                string            `json:"id"`
	Label             string            `json:"label"`
	Description       string            `json:"description"`
	Harness           string            `json:"harness"`
	EndpointKind      string            `json:"endpointKind"`
	Vendor            string            `json:"vendor"`
	Env               []recipeEnvView   `json:"env"`
	Models            []recipeModelView `json:"models"`
	ModelsRequired    bool              `json:"modelsRequired"`
	DisallowedTools   []string          `json:"disallowedTools"`
	Thinking          string            `json:"thinking"`
	Notes             []string          `json:"notes"`
	Docs              string            `json:"docs"`
	Source            string            `json:"source"`
	VerifiedAt        string            `json:"verifiedAt"`
	Unverified        bool              `json:"unverified"`
	UnverifiedAspects []string          `json:"unverifiedAspects"`
	NeedsBaseURL      bool              `json:"needsBaseUrl"`
	NeedsKey          bool              `json:"needsKey"`
}

type recipesResult struct {
	Recipes []recipeView `json:"recipes"`
}

// recipeNeedsKey reports whether the row has an env the user must paste a
// key into (kind secret; a secretDefault row stores its own placeholder and
// asks for nothing).
func recipeNeedsKey(rec providers.Recipe) bool {
	for _, e := range rec.Env {
		if e.Kind == "secret" {
			return true
		}
	}
	return false
}

// recipeNeedsBaseURL reports whether the row needs a URL from the user: a
// userBaseURL env, or the codex harness whose endpoint.baseUrl carries it.
func recipeNeedsBaseURL(rec providers.Recipe) bool {
	if rec.Harness == "codex-app-server" {
		return true
	}
	for _, e := range rec.Env {
		if e.Kind == "userBaseURL" {
			return true
		}
	}
	return false
}

func newRecipeView(rec providers.Recipe) recipeView {
	v := recipeView{
		ID:                rec.ID,
		Label:             rec.Label,
		Description:       rec.Description,
		Harness:           rec.Harness,
		EndpointKind:      rec.EndpointKind,
		Vendor:            rec.Vendor,
		Env:               make([]recipeEnvView, 0, len(rec.Env)),
		Models:            make([]recipeModelView, 0, len(rec.Models)),
		ModelsRequired:    rec.ModelsRequired,
		DisallowedTools:   append([]string{}, rec.DisallowedTools...),
		Thinking:          rec.Thinking,
		Notes:             append([]string{}, rec.Notes...),
		Docs:              rec.Docs,
		Source:            rec.Source,
		VerifiedAt:        rec.VerifiedAt,
		Unverified:        rec.IsUnverified(),
		UnverifiedAspects: append([]string{}, rec.Unverified...),
		NeedsBaseURL:      recipeNeedsBaseURL(rec),
		NeedsKey:          recipeNeedsKey(rec),
	}
	for _, e := range rec.Env {
		value := e.Value
		if e.Kind == "secret" || e.Kind == "secretDefault" {
			value = ""
		}
		v.Env = append(v.Env, recipeEnvView{Name: e.Name, Kind: e.Kind, Value: value})
	}
	for _, m := range rec.Models {
		v.Models = append(v.Models, recipeModelView{ID: m.ID, Label: m.Label, Default: m.Default})
	}
	return v
}

// handleProviderRecipes is provider.recipes: the catalog rows a client
// offers as add-provider templates. Read-only, no params, any caller.
func handleProviderRecipes() handlerFunc {
	return func(_ context.Context, _ *requestContext, _ json.RawMessage) (any, error) {
		recs := providers.Recipes()
		if recs == nil {
			return nil, fmt.Errorf("provider.recipes: the catalog did not load: %w", providers.RecipesErr())
		}
		out := make([]recipeView, 0, len(recs))
		for _, rec := range recs {
			out = append(out, newRecipeView(rec))
		}
		return recipesResult{Recipes: out}, nil
	}
}

// addFromRecipeSecret is one secret provider.addFromRecipe stored: the name
// and the store's fingerprint, never the value.
type addFromRecipeSecret struct {
	Name        string `json:"name"`
	Set         bool   `json:"set"`
	Fingerprint string `json:"fingerprint"`
}

type addFromRecipeResult struct {
	ID       string                `json:"id"`
	Created  bool                  `json:"created"`
	NeedsKey bool                  `json:"needsKey"`
	Secrets  []addFromRecipeSecret `json:"secrets"`
}

// handleProviderAddFromRecipe is provider.addFromRecipe {recipe, id?, label?,
// baseUrl?, models?, secrets?}: instantiate a catalog recipe as a
// providers.custom entry (config.AddRecipeInstance), apply it live and store
// the pasted key — or, for a keyless local row, the recipe's placeholder —
// as the instance's secret. Humans only. A key is required up front for
// rows that need one (before config is touched), never goes in config.yaml,
// and never appears in a result, event or error.
func handleProviderAddFromRecipe(runner *taskrunner.Runner, db *store.Store, bus *eventBus) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.addFromRecipe is not available to agents"}
		}
		var p struct {
			Recipe  string            `json:"recipe"`
			ID      string            `json:"id"`
			Label   string            `json:"label"`
			BaseURL string            `json:"baseUrl"`
			Models  []string          `json:"models"`
			Secrets map[string]string `json:"secrets"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("provider.addFromRecipe: invalid params")
		}
		rec, ok := providers.RecipeByID(strings.TrimSpace(p.Recipe))
		if !ok {
			return nil, fmt.Errorf("provider.addFromRecipe: unknown recipe %q", p.Recipe)
		}
		key := strings.TrimSpace(p.Secrets[config.RecipeSecretName])
		if recipeNeedsKey(rec) {
			if err := validProviderSecretValue(key); err != nil {
				return nil, fmt.Errorf("provider.addFromRecipe: a key is required (secrets.%s)", config.RecipeSecretName)
			}
		}
		providerSetDefaultModelMu.Lock()
		defer providerSetDefaultModelMu.Unlock()
		res, err := config.AddRecipeInstance(rec, config.RecipeInput{ID: p.ID, Label: p.Label, BaseURL: p.BaseURL, Models: p.Models})
		if err != nil {
			return nil, fmt.Errorf("provider.addFromRecipe: %w", err)
		}
		if err := reloadProviders(runner); err != nil {
			return nil, fmt.Errorf("provider.addFromRecipe: saved, but applying it failed: %w", err)
		}
		stored := map[string]string{}
		if key != "" {
			stored[config.RecipeSecretName] = key // a pasted key wins
		} else {
			for name, value := range res.PlaceholderSecrets {
				stored[name] = value
			}
		}
		names := make([]string, 0, len(stored))
		for name := range stored {
			names = append(names, name)
		}
		sort.Strings(names)
		secrets := make([]addFromRecipeSecret, 0, len(names))
		for _, name := range names {
			if err := db.SetProviderSecret(res.Entry.ID, name, stored[name]); err != nil {
				return nil, fmt.Errorf("provider.addFromRecipe: store failed")
			}
			secrets = append(secrets, addFromRecipeSecret{Name: name, Set: true, Fingerprint: store.SecretFingerprint(stored[name])})
		}
		publishProvidersChanged(bus, runner, res.Entry.ID)
		return addFromRecipeResult{ID: res.Entry.ID, Created: res.Created, Secrets: secrets}, nil
	}
}
