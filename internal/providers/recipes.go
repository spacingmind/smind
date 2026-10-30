package providers

import (
	"bytes"
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	yaml "gopkg.in/yaml.v3"
)

//go:embed recipes.yaml
var recipesYAML []byte

// RecipeEnv is one entry of a recipe's launch environment.
type RecipeEnv struct {
	Name string `yaml:"name"`
	// Kind says where Value comes from:
	//   literal       — Value is written to the env as-is
	//   secret        — the user pastes a key, stored in the secret store
	//                   (Value must be empty)
	//   secretDefault — a non-sensitive placeholder stored as the secret
	//                   automatically, with no key prompt
	//   userBaseURL   — the user supplies the URL; Value is a placeholder
	//                   shown in the UI, empty or an http(s) URL
	Kind  string `yaml:"kind"`
	Value string `yaml:"value"`
}

// RecipeModel is one entry of a recipe's model catalog.
type RecipeModel struct {
	ID      string `yaml:"id"`
	Label   string `yaml:"label"`
	Default bool   `yaml:"default"`
}

// Recipe is a catalog template for one providers.custom instance of a
// claude-native (or codex-native/acp) endpoint: the env overlay, model
// catalog and capability flags. Rows are curated by hand, so the catalog
// must never contain a key — secrets are named, never valued.
type Recipe struct {
	ID          string `yaml:"id"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`

	// Harness is the engine the instance runs on.
	Harness string `yaml:"harness"`
	// EndpointKind is the wire protocol the endpoint speaks.
	EndpointKind string `yaml:"endpointKind"`
	// Vendor is "zai" for smind-vendored rows, else "generic".
	Vendor string `yaml:"vendor"`

	Env    []RecipeEnv   `yaml:"env"`
	Models []RecipeModel `yaml:"models"`
	// ModelsRequired: the user must supply model ids (generic URL recipes).
	ModelsRequired  bool     `yaml:"modelsRequired"`
	DisallowedTools []string `yaml:"disallowedTools"`
	Thinking        string   `yaml:"thinking"` // "supported" | "hidden"
	Notes           []string `yaml:"notes"`
	Docs            string   `yaml:"docs"` // https URL
	Source          string   `yaml:"source"`
	VerifiedAt      string   `yaml:"verifiedAt"` // "" or YYYY-MM-DD
	Unverified      []string `yaml:"unverified"` // inferred aspects, shown as notes in the UI
}

var (
	recipesOnce     sync.Once
	recipesAll      []Recipe
	recipesLoadErr  error
	recipeIDRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)
	recipeEnvNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Recipes returns the embedded recipe catalog in display order, as a deep
// copy. A catalog that fails to parse or validate yields nil, with the
// error from RecipesErr (an embedded file cannot be half-bad at runtime,
// so loading is fail-closed, never a panic).
func Recipes() []Recipe {
	recipesOnce.Do(loadRecipes)
	if recipesLoadErr != nil {
		return nil
	}
	out := make([]Recipe, len(recipesAll))
	for i := range recipesAll {
		out[i] = cloneRecipe(recipesAll[i])
	}
	return out
}

// RecipesErr is the error that made Recipes return nil (nil on success).
func RecipesErr() error {
	recipesOnce.Do(loadRecipes)
	return recipesLoadErr
}

// RecipeByID returns the recipe with id.
func RecipeByID(id string) (Recipe, bool) {
	for _, r := range Recipes() {
		if r.ID == id {
			return r, true
		}
	}
	return Recipe{}, false
}

// IsUnverified reports whether the row's claims were never re-verified.
func (r Recipe) IsUnverified() bool { return r.VerifiedAt == "" }

func loadRecipes() {
	var doc struct {
		Recipes []Recipe `yaml:"recipes"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(recipesYAML))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		recipesLoadErr = fmt.Errorf("recipes.yaml: %w", err)
		return
	}
	seen := make(map[string]bool, len(doc.Recipes))
	for i, r := range doc.Recipes {
		if seen[r.ID] {
			recipesLoadErr = fmt.Errorf("recipes.yaml [%d]: duplicate recipe id %s", i, r.ID)
			return
		}
		seen[r.ID] = true
		if err := ValidateRecipe(r); err != nil {
			recipesLoadErr = fmt.Errorf("recipes.yaml [%d]: %w", i, err)
			return
		}
	}
	recipesAll = doc.Recipes
}

func cloneRecipe(r Recipe) Recipe {
	r.Env = append([]RecipeEnv(nil), r.Env...)
	r.Models = append([]RecipeModel(nil), r.Models...)
	r.DisallowedTools = append([]string(nil), r.DisallowedTools...)
	r.Notes = append([]string(nil), r.Notes...)
	r.Unverified = append([]string(nil), r.Unverified...)
	return r
}

func recipeOneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func credentialEnvName(name string) bool {
	for _, s := range []string{"_KEY", "_TOKEN", "_SECRET", "_PASSWORD"} {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

// ValidateRecipe checks one catalog row. Errors name the recipe id and the
// offending field, never a value.
func ValidateRecipe(r Recipe) error {
	if !recipeIDRe.MatchString(r.ID) {
		return fmt.Errorf("recipe id: must match ^[a-z0-9][a-z0-9-]{0,40}$")
	}
	if r.Label == "" {
		return fmt.Errorf("recipe %s: label: required", r.ID)
	}
	if !recipeOneOf(r.Harness, "claude-code", "codex-app-server", "acp") {
		return fmt.Errorf("recipe %s: harness: must be claude-code, codex-app-server or acp", r.ID)
	}
	if !recipeOneOf(r.EndpointKind, "anthropic", "openai-responses", "chat-completions", "local") {
		return fmt.Errorf("recipe %s: endpointKind: must be anthropic, openai-responses, chat-completions or local", r.ID)
	}
	if !recipeOneOf(r.Vendor, "", "zai", "generic") {
		return fmt.Errorf("recipe %s: vendor: must be empty, zai or generic", r.ID)
	}
	if !recipeOneOf(r.Thinking, "supported", "hidden") {
		return fmt.Errorf("recipe %s: thinking: must be supported or hidden", r.ID)
	}
	if !recipeOneOf(r.Source, "paseo-docs", "vendor-docs", "inferred") {
		return fmt.Errorf("recipe %s: source: must be paseo-docs, vendor-docs or inferred", r.ID)
	}
	if r.VerifiedAt != "" {
		if _, err := time.Parse("2006-01-02", r.VerifiedAt); err != nil {
			return fmt.Errorf("recipe %s: verifiedAt: must be empty or YYYY-MM-DD", r.ID)
		}
	}
	if !strings.HasPrefix(r.Docs, "https://") {
		return fmt.Errorf("recipe %s: docs: must be an https URL", r.ID)
	}
	seenEnv := make(map[string]bool, len(r.Env))
	for _, e := range r.Env {
		if !recipeEnvNameRe.MatchString(e.Name) {
			return fmt.Errorf("recipe %s: env name: must match ^[A-Za-z_][A-Za-z0-9_]*$", r.ID)
		}
		if seenEnv[e.Name] {
			return fmt.Errorf("recipe %s: env %s: duplicate name", r.ID, e.Name)
		}
		seenEnv[e.Name] = true
		if !recipeOneOf(e.Kind, "literal", "secret", "secretDefault", "userBaseURL") {
			return fmt.Errorf("recipe %s: env %s: kind must be literal, secret, secretDefault or userBaseURL", r.ID, e.Name)
		}
		switch {
		case e.Kind == "secret" && e.Value != "":
			return fmt.Errorf("recipe %s: env %s: a secret's value is pasted by the user, never stored in the catalog", r.ID, e.Name)
		case e.Kind == "userBaseURL" && e.Value != "" && !strings.HasPrefix(e.Value, "http://") && !strings.HasPrefix(e.Value, "https://"):
			return fmt.Errorf("recipe %s: env %s: a userBaseURL placeholder must be empty or an http(s) URL", r.ID, e.Name)
		case e.Kind == "literal" && e.Value != "" && credentialEnvName(e.Name):
			return fmt.Errorf("recipe %s: env %s: literal value looks like a credential", r.ID, e.Name)
		}
	}
	seenModel := make(map[string]bool, len(r.Models))
	defaults := 0
	for _, m := range r.Models {
		if m.ID == "" {
			return fmt.Errorf("recipe %s: models: id required", r.ID)
		}
		if seenModel[m.ID] {
			return fmt.Errorf("recipe %s: models %s: duplicate id", r.ID, m.ID)
		}
		seenModel[m.ID] = true
		if m.Default {
			defaults++
			if defaults > 1 {
				return fmt.Errorf("recipe %s: models: at most one default", r.ID)
			}
		}
	}
	if r.Harness == "claude-code" {
		// Third-party anthropic-compatible endpoints have no hosted
		// WebSearch; every claude-code row must say so.
		webSearch := false
		for _, tool := range r.DisallowedTools {
			if tool == "" {
				return fmt.Errorf("recipe %s: disallowedTools: empty entry", r.ID)
			}
			if tool == "WebSearch" {
				webSearch = true
			}
		}
		if !webSearch {
			return fmt.Errorf("recipe %s: disallowedTools: WebSearch must be present", r.ID)
		}
	}
	return nil
}
