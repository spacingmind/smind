---
v: 1
milestone: "Providers epic PR3: claude-code recipes + vendor rate-limit profiles"
base: origin/develop
planRef: docs/plans/active/providers-epic.md#pr3-claude-code-recipe-catalog-data--vendor-rate-limit-profiles-after-pr1--pr2
acs:
  - id: AC3-1
    text: "Z.AI is added from its recipe with only a key; the key is stored in the secret store, never in config.yaml, a response, an event or argv."
    covers: { tests: [TestAddFromRecipe_WritesInstanceAndSecret, TestAddFromRecipe_SecretWriteOnly, TestRecipeEntry_ZAIOnlyNeedsKey, TestAddRecipeInstance_WritesConfigNoKey], frames: [] }
  - id: AC3-2
    text: "A run against a fake Anthropic-compatible server shows the base URL env, --disallowedTools WebSearch, --model and a private CLAUDE_CONFIG_DIR; the sentinel key is the bearer token and not in argv."
    covers: { tests: [TestRecipe_ZAI_RunAgainstFakeAnthropicServer, TestRecipe_PrivateConfigDirSet], frames: [] }
  - id: AC3-3
    text: "A GLM-worded 429 classifies as rate limited with the UTC+8 reset for the zai vendor only, and the cooldown lands on the instance id, not on claude-native."
    covers: { tests: [TestRateLimit_VendorProfile_GLMOnInstance, TestRateLimit_InstanceCooldownLeavesBaseRunning, TestVendor_CompatibleInstanceIsGenericUnlessSet], frames: [] }
  - id: AC3-4
    text: "A 429 from an unknown vendor classifies through the generic patterns."
    covers: { tests: [TestRateLimit_GenericUnknownVendor, TestRateLimit_ClaudeTextOnlyForAnthropic], frames: [] }
  - id: AC3-5
    text: "Every recipe has source and verifiedAt; inferred rows with no verifiedAt are flagged in provider.recipes."
    covers: { tests: [TestRecipes_AllParseAndValidate, TestRecipe_InferredRowsFlagged, TestRecipesRPC_ListsCatalog], frames: [] }
  - id: AC3-6
    text: "No literal secret exists in the catalog; a thinking: hidden recipe exposes no thinking control (run ignores the level, provider.list says hidden)."
    covers: { tests: [TestRecipe_NoSecretLiteralsInCatalog, TestRecipe_ThinkingHiddenHidesOption, TestProviderList_RecipeFields], frames: [] }
  - id: AC3-7
    text: "With inheritClaudeConfig on, no private dir is set and ~/.claude is used."
    covers: { tests: [TestRecipe_InheritToggleUsesDefaultConfigDir], frames: [] }
  - id: AC3-8
    text: "provider.addFromRecipe is human-only, idempotent for the same recipe, and refuses an id used by something else."
    covers: { tests: [TestAddFromRecipe_RefusesAgentCaller, TestAddFromRecipe_Idempotent, TestAddFromRecipe_IDCollision, TestAddFromRecipe_MissingKeyWritesNothing], frames: [] }
tests:
  - { name: TestRecipes_AllParseAndValidate, run: "go test ./internal/providers -run '^TestRecipes_AllParseAndValidate$' -count=1", timeoutSec: 120 }
  - { name: TestRecipe_NoSecretLiteralsInCatalog, run: "go test ./internal/providers -run '^TestRecipe_NoSecretLiteralsInCatalog$' -count=1", timeoutSec: 120 }
  - { name: TestRecipe_InferredRowsFlagged, run: "go test ./internal/providers -run '^TestRecipe_InferredRowsFlagged$' -count=1", timeoutSec: 120 }
  - { name: TestVendor_CompatibleInstanceIsGenericUnlessSet, run: "go test ./internal/providers -run '^TestVendor_CompatibleInstanceIsGenericUnlessSet$' -count=1", timeoutSec: 120 }
  - { name: TestRateLimit_VendorProfile_GLMOnInstance, run: "go test ./internal/runs/ratelimit -run '^TestRateLimit_VendorProfile_GLMOnInstance$' -count=1", timeoutSec: 120 }
  - { name: TestRateLimit_GenericUnknownVendor, run: "go test ./internal/runs/ratelimit -run '^TestRateLimit_GenericUnknownVendor$' -count=1", timeoutSec: 120 }
  - { name: TestRateLimit_ClaudeTextOnlyForAnthropic, run: "go test ./internal/runs/ratelimit -run '^TestRateLimit_ClaudeTextOnlyForAnthropic$' -count=1", timeoutSec: 120 }
  - { name: TestRateLimit_InstanceCooldownLeavesBaseRunning, run: "go test ./internal/runs -run '^TestRateLimit_InstanceCooldownLeavesBaseRunning$' -count=1", timeoutSec: 180 }
  - { name: TestRecipeEntry_ZAIOnlyNeedsKey, run: "go test ./internal/config -run '^TestRecipeEntry_ZAIOnlyNeedsKey$' -count=1", timeoutSec: 120 }
  - { name: TestAddRecipeInstance_WritesConfigNoKey, run: "go test ./internal/config -run '^TestAddRecipeInstance_WritesConfigNoKey$' -count=1", timeoutSec: 120 }
  - { name: TestRecipesRPC_ListsCatalog, run: "go test ./internal/wsapi -run '^TestRecipesRPC_ListsCatalog$' -count=1", timeoutSec: 300 }
  - { name: TestAddFromRecipe_WritesInstanceAndSecret, run: "go test ./internal/wsapi -run '^TestAddFromRecipe_WritesInstanceAndSecret$' -count=1", timeoutSec: 300 }
  - { name: TestAddFromRecipe_SecretWriteOnly, run: "go test ./internal/wsapi -run '^TestAddFromRecipe_SecretWriteOnly$' -count=1", timeoutSec: 300 }
  - { name: TestAddFromRecipe_RefusesAgentCaller, run: "go test ./internal/wsapi -run '^TestAddFromRecipe_RefusesAgentCaller$' -count=1", timeoutSec: 300 }
  - { name: TestAddFromRecipe_Idempotent, run: "go test ./internal/wsapi -run '^TestAddFromRecipe_Idempotent$' -count=1", timeoutSec: 300 }
  - { name: TestAddFromRecipe_IDCollision, run: "go test ./internal/wsapi -run '^TestAddFromRecipe_IDCollision$' -count=1", timeoutSec: 300 }
  - { name: TestAddFromRecipe_MissingKeyWritesNothing, run: "go test ./internal/wsapi -run '^TestAddFromRecipe_MissingKeyWritesNothing$' -count=1", timeoutSec: 300 }
  - { name: TestRecipe_ZAI_RunAgainstFakeAnthropicServer, run: "go test ./internal/taskrunner -run '^TestRecipe_ZAI_RunAgainstFakeAnthropicServer$' -count=1", timeoutSec: 300 }
  - { name: TestRecipe_PrivateConfigDirSet, run: "go test ./internal/taskrunner -run '^TestRecipe_PrivateConfigDirSet$' -count=1", timeoutSec: 300 }
  - { name: TestRecipe_InheritToggleUsesDefaultConfigDir, run: "go test ./internal/taskrunner -run '^TestRecipe_InheritToggleUsesDefaultConfigDir$' -count=1", timeoutSec: 300 }
  - { name: TestRecipe_ThinkingHiddenHidesOption, run: "go test ./internal/taskrunner -run '^TestRecipe_ThinkingHiddenHidesOption$' -count=1", timeoutSec: 300 }
  - { name: TestProviderList_RecipeFields, run: "go test ./internal/taskrunner -run '^TestProviderList_RecipeFields$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/providers/recipes.yaml, change: "new-file: give the zai-glm ANTHROPIC_AUTH_TOKEN env kind literal and value sentinel-not-real", expectFail: [TestRecipe_NoSecretLiteralsInCatalog] }
  - { id: M2, file: internal/runs/ratelimit/ratelimit.go, change: "run the GLM branch for every vendor, not only zai", expectFail: [TestRateLimit_VendorProfile_GLMOnInstance] }
  - { id: M3, file: internal/runs/registry.go, change: "pass no Vendor (or the base id) to ratelimit.Classify", expectFail: [TestRateLimit_InstanceCooldownLeavesBaseRunning] }
  - { id: M4, file: internal/taskrunner/claude_config_dir.go, change: "new-file: return nil entries always (drop the private dir)", expectFail: [TestRecipe_PrivateConfigDirSet, TestRecipe_ZAI_RunAgainstFakeAnthropicServer] }
  - { id: M5, file: internal/wsapi/provider_recipes.go, change: "new-file: remove the runScoped refusal in provider.addFromRecipe", expectFail: [TestAddFromRecipe_RefusesAgentCaller] }
  - { id: M6, file: internal/wsapi/provider_recipes.go, change: "new-file: put the pasted key into the result JSON", expectFail: [TestAddFromRecipe_SecretWriteOnly, TestAddFromRecipe_WritesInstanceAndSecret] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
limits: { budgetUsd: 2, maxMinutes: 40 }
---

# PR3 validation contract

Fake servers and sentinel strings only; no real key and no model call. The acceptance criteria and the design are in `docs/plans/active/providers-epic.md` section PR3 and ADR-0046 sections 5 and 8.
