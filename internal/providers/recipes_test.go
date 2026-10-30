package providers

import (
	"regexp"
	"strings"
	"testing"
)

var recipeCatalogIDs = []string{
	"zai-glm", "qwen-dashscope", "deepseek", "kimi-moonshot", "minimax",
	"openrouter", "ollama", "lm-studio", "litellm", "anthropic-compatible",
	"openai-responses-compatible", "chat-completions-via-opencode",
}

func TestRecipes_AllParseAndValidate(t *testing.T) {
	t.Parallel()
	if err := RecipesErr(); err != nil {
		t.Fatalf("RecipesErr() = %v, want nil", err)
	}
	got := Recipes()
	if len(got) != len(recipeCatalogIDs) {
		t.Fatalf("len(Recipes()) = %d, want %d", len(got), len(recipeCatalogIDs))
	}
	for i, r := range got {
		if r.ID != recipeCatalogIDs[i] {
			t.Errorf("Recipes()[%d].ID = %q, want %q", i, r.ID, recipeCatalogIDs[i])
		}
		if err := ValidateRecipe(r); err != nil {
			t.Errorf("%s: ValidateRecipe: %v", r.ID, err)
		}
		if byID, ok := RecipeByID(r.ID); !ok || byID.ID != r.ID {
			t.Errorf("RecipeByID(%q) = %+v, %v", r.ID, byID, ok)
		}
	}
	if _, ok := RecipeByID("no-such-recipe"); ok {
		t.Error(`RecipeByID("no-such-recipe") found`)
	}
}

// credentialRe matches key shapes that must never appear anywhere in the
// catalog. The DashScope note's "sk-sp-..." ellipsis stays under the length.
var credentialRe = regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}|Bearer |AKIA[0-9A-Z]{8,}|ghp_[A-Za-z0-9]{8,}`)

func TestRecipe_NoSecretLiteralsInCatalog(t *testing.T) {
	t.Parallel()
	for _, r := range Recipes() {
		for _, e := range r.Env {
			switch {
			case e.Kind == "secret" && e.Value != "":
				t.Errorf("%s: env %s: a secret must not carry a value", r.ID, e.Name)
			case e.Kind == "literal" && credentialEnvName(e.Name) && e.Value != "":
				t.Errorf("%s: env %s: a credential-named env must stay empty", r.ID, e.Name)
			}
			if credentialRe.MatchString(e.Value) {
				t.Errorf("%s: env %s: value looks like a credential", r.ID, e.Name)
			}
		}
		for _, n := range r.Notes {
			if credentialRe.MatchString(n) {
				t.Errorf("%s: a note looks like a credential: %q", r.ID, n)
			}
		}
	}

	t.Run("literal token rejected", func(t *testing.T) {
		t.Parallel()
		r, ok := RecipeByID("zai-glm")
		if !ok {
			t.Fatal("zai-glm: missing from the catalog")
		}
		r.Env = append([]RecipeEnv(nil), r.Env...)
		for i := range r.Env {
			if r.Env[i].Name == "ANTHROPIC_AUTH_TOKEN" {
				r.Env[i].Kind, r.Env[i].Value = "literal", "sentinel-not-real"
			}
		}
		err := ValidateRecipe(r)
		if err == nil || !strings.Contains(err.Error(), "credential") {
			t.Fatalf("ValidateRecipe(literal token) err = %v, want a credential error", err)
		}
	})
}

func TestRecipe_InferredRowsFlagged(t *testing.T) {
	t.Parallel()
	inferred := map[string]bool{}
	for _, r := range Recipes() {
		if !r.IsUnverified() {
			t.Errorf("%s: IsUnverified() = false, want true (no row is re-verified yet)", r.ID)
		}
		if r.Source != "inferred" {
			continue
		}
		inferred[r.ID] = true
		if len(r.Unverified) == 0 {
			t.Errorf("%s: source inferred but no unverified aspects listed", r.ID)
		}
		if r.VerifiedAt != "" {
			t.Errorf("%s: source inferred but verifiedAt is set", r.ID)
		}
	}
	for _, id := range []string{"kimi-moonshot", "chat-completions-via-opencode"} {
		if !inferred[id] {
			t.Errorf("%s: expected source inferred", id)
		}
	}
}

func TestRecipe_ClaudeCodeRecipesDisallowWebSearch(t *testing.T) {
	t.Parallel()
	for _, r := range Recipes() {
		if r.Harness != "claude-code" {
			continue
		}
		if !recipeOneOf("WebSearch", r.DisallowedTools...) {
			t.Errorf("%s: claude-code recipe without WebSearch in disallowedTools", r.ID)
		}
	}
}

func TestRecipe_ZAIShape(t *testing.T) {
	t.Parallel()
	r, ok := RecipeByID("zai-glm")
	if !ok {
		t.Fatal("zai-glm: missing from the catalog")
	}
	base, ok := recipeEnvByName(r, "ANTHROPIC_BASE_URL")
	if !ok || base.Kind != "literal" || base.Value != "https://api.z.ai/api/anthropic" {
		t.Fatalf("zai-glm base URL env = %+v, ok = %v", base, ok)
	}
	tok, ok := recipeEnvByName(r, "ANTHROPIC_AUTH_TOKEN")
	if !ok || tok.Kind != "secret" || tok.Value != "" {
		t.Fatalf("zai-glm token env = %+v, ok = %v", tok, ok)
	}
	if r.Vendor != "zai" {
		t.Errorf("zai-glm vendor = %q, want zai", r.Vendor)
	}
	var def string
	for _, m := range r.Models {
		if m.Default {
			def = m.ID
		}
	}
	if def != "glm-5.3" {
		t.Errorf("zai-glm default model = %q, want glm-5.3", def)
	}
}

func recipeEnvByName(r Recipe, name string) (RecipeEnv, bool) {
	for _, e := range r.Env {
		if e.Name == name {
			return e, true
		}
	}
	return RecipeEnv{}, false
}

func TestRecipes_ReturnsCopy(t *testing.T) {
	t.Parallel()
	first := Recipes()
	if len(first) == 0 {
		t.Fatal("Recipes() is empty")
	}
	first[0].Label = "mutated"
	first[0].Env[0].Value = "mutated"
	first[0].Models[0].ID = "mutated"
	first[0].Notes[0] = "mutated"
	second := Recipes()
	if second[0].Label == "mutated" || second[0].Env[0].Value == "mutated" ||
		second[0].Models[0].ID == "mutated" || second[0].Notes[0] == "mutated" {
		t.Fatal("Recipes() leaked its internal storage to a caller mutation")
	}
}

func validTestRecipe() Recipe {
	return Recipe{
		ID: "test-recipe", Label: "Test",
		Harness: "claude-code", EndpointKind: "anthropic", Vendor: "generic",
		Env: []RecipeEnv{
			{Name: "ANTHROPIC_BASE_URL", Kind: "literal", Value: "https://api.example.com"},
			{Name: "ANTHROPIC_AUTH_TOKEN", Kind: "secret"},
		},
		DisallowedTools: []string{"WebSearch"},
		Thinking:        "hidden",
		Docs:            "https://docs.example.com/",
		Source:          "vendor-docs",
	}
}

func TestValidateRecipe_Rejects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		want   string
		mutate func(*Recipe)
	}{
		{"bad id", "id", func(r *Recipe) { r.ID = "Not A Recipe" }},
		{"bad harness", "harness", func(r *Recipe) { r.Harness = "smith" }},
		{"secret with value", "secret", func(r *Recipe) { r.Env[1].Value = "sentinel-not-real" }},
		{"duplicate env name", "duplicate", func(r *Recipe) {
			r.Env = append(r.Env, RecipeEnv{Name: "ANTHROPIC_BASE_URL", Kind: "literal", Value: "https://api2.example.com"})
		}},
		{"two defaults", "default", func(r *Recipe) {
			r.Models = []RecipeModel{{ID: "m1", Default: true}, {ID: "m2", Default: true}}
		}},
		{"claude-code without WebSearch", "WebSearch", func(r *Recipe) { r.DisallowedTools = []string{"Bash"} }},
		{"docs not https", "docs", func(r *Recipe) { r.Docs = "http://docs.example.com/" }},
	} {
		r := validTestRecipe()
		tc.mutate(&r)
		err := ValidateRecipe(r)
		if err == nil {
			t.Errorf("%s: ValidateRecipe accepted an invalid recipe", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), r.ID) && !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want the recipe id or %q", tc.name, err, tc.want)
		}
	}
	if err := ValidateRecipe(validTestRecipe()); err != nil {
		t.Fatalf("validTestRecipe: %v", err)
	}
}
