package providers

import "fmt"

// Browser reach (ADR-0061 section 9, docs/plans/active/browser-for-agents-pr5.md):
// which providers get the `smind-browser` MCP row. ONE table decides it, with a
// row for every registered provider and every compatible recipe; a drift test
// fails when a provider has none. An unknown provider never gets the browser.

// Reach says whether the browser row is handed to a provider's agent.
type Reach string

const (
	// ReachYes: the row is built and handed to the agent.
	ReachYes Reach = "yes"
	// ReachConditional: handed over, but whether the agent (or the model behind
	// a compatible endpoint) uses it is not guaranteed.
	ReachConditional Reach = "conditional"
	// ReachNo: no row is built and no socket is opened.
	ReachNo Reach = "no"
)

// ReachRow is one row of the table.
type ReachRow struct {
	Reach Reach
	// Mechanism is how the row reaches the agent.
	Mechanism string
	// Why is one sentence a person can read (said for no and conditional).
	Why string
	// Images overrides the provider's catalogue Images for the browser's
	// screenshots: "" = use the catalogue; "yes" | "no" | "unknown".
	Images string
}

// Offered reports whether the row is handed to the agent.
func (r ReachRow) Offered() bool { return r.Reach == ReachYes || r.Reach == ReachConditional }

// Class keys for entries that have no fixed id.
const (
	ReachClassCustomACP = "class:custom-acp"
	ReachClassPlugin    = "class:plugin"
	ReachClassInstance  = "class:instance"
)

const (
	whyACP      = "an ACP agent takes the browser only if it advertises MCP support; smind cannot tell in advance"
	whyEndpoint = "this agent runs against a compatible endpoint; whether the model behind it calls MCP tools is up to that model"
	whyNoMCP    = "this agent cannot take MCP servers"
	// whyCodexAppServer names the harness, so an instance of another provider made
	// from a Codex-harness recipe is told the real reason.
	whyCodexAppServer = "this runs on the Codex app-server harness, which takes no per-run MCP servers"
)

var (
	reachClaude = ReachRow{Reach: ReachYes, Mechanism: "--mcp-config, bound to the Claude Code process id"}
	reachACP    = ReachRow{Reach: ReachConditional, Mechanism: "session/new mcpServers", Why: whyACP}
	reachRecipe = ReachRow{Reach: ReachConditional, Mechanism: "--mcp-config, bound to the Claude Code process id", Why: whyEndpoint, Images: "unknown"}
)

// browserReach is keyed by built-in provider id, then class key. A row for every
// built-in provider id is required by TestBrowserReachDriftEveryProviderHasARow.
var browserReach = map[string]ReachRow{
	IDClaudeNative: reachClaude,
	IDCodexNative:  {Reach: ReachNo, Mechanism: "none", Why: "Codex takes no per-run MCP servers"},
	IDGLM:          {Reach: ReachConditional, Mechanism: "session/new mcpServers", Why: whyACP + "; GLM cannot read images, so only the text tools are useful"},
	IDKimi:         reachACP,
	IDOpenCode:     reachACP,
	IDCopilot:      reachACP,
	IDCline:        reachACP,
	IDGoose:        reachACP,
	IDCursor:       reachACP,

	ReachClassCustomACP: {Reach: ReachConditional, Mechanism: "session/new mcpServers", Why: whyACP, Images: "unknown"},
	ReachClassPlugin:    {Reach: ReachNo, Mechanism: "none", Why: "a plugin provider never receives smind's own MCP servers", Images: "unknown"},
	ReachClassInstance:  {Reach: ReachConditional, Mechanism: "the launch path of the provider it extends", Why: whyEndpoint, Images: "unknown"},
}

// recipeReach is keyed by compatible-recipe id (recipes.yaml). A recipe's harness
// decides its row; TestBrowserReachRecipeHarnessConsistent pins that.
var recipeReach = map[string]ReachRow{
	"zai-glm":                       reachRecipe,
	"qwen-dashscope":                reachRecipe,
	"deepseek":                      reachRecipe,
	"kimi-moonshot":                 reachRecipe,
	"minimax":                       reachRecipe,
	"openrouter":                    reachRecipe,
	"ollama":                        reachRecipe,
	"lm-studio":                     reachRecipe,
	"litellm":                       reachRecipe,
	"anthropic-compatible":          reachRecipe,
	"openai-responses-compatible":   {Reach: ReachNo, Mechanism: "none", Why: whyCodexAppServer, Images: "unknown"},
	"chat-completions-via-opencode": {Reach: ReachConditional, Mechanism: "session/new mcpServers", Why: whyACP, Images: "unknown"},
}

// BrowserReachOf resolves the row for a registered spec: a plugin provider is
// always no; a spec that cannot take MCP servers is no whatever the table says;
// then the recipe row, the id row, the class row; an unknown provider is no.
func BrowserReachOf(spec Spec) ReachRow {
	if spec.Plugin != "" {
		return browserReach[ReachClassPlugin]
	}
	row := resolveReach(spec)
	if spec.NoMCPServers {
		row = ReachRow{Reach: ReachNo, Mechanism: "none", Why: whyNoMCP, Images: row.Images}
	}
	return row
}

func resolveReach(spec Spec) ReachRow {
	// A base that cannot take the row stays no, whatever recipe is named: nothing
	// ties a Recipe id to the base it is set on.
	if spec.Extends != "" && IsBuiltin(spec.Extends) && browserReach[spec.Extends].Reach == ReachNo {
		row := browserReach[spec.Extends]
		row.Images = "unknown"
		return row
	}
	if spec.Recipe != "" {
		if row, ok := recipeReach[spec.Recipe]; ok {
			return row
		}
	}
	if spec.Extends != "" {
		// An instance with no (known) recipe: a no base was handled above; every
		// other base (a built-in, or another custom entry) gets the FIXED instance
		// class row (offered, not guaranteed, images unknown: the model that would
		// read a screenshot is not the base's), not the base's own row.
		return browserReach[ReachClassInstance]
	}
	if row, ok := browserReach[spec.ID]; ok && IsBuiltin(spec.ID) {
		return row
	}
	if spec.Custom && spec.Runner == RunnerACP {
		return browserReach[ReachClassCustomACP]
	}
	return ReachRow{Reach: ReachNo, Mechanism: "none", Why: "smind has no browser entry for this provider", Images: "unknown"}
}

// BrowserImages is the image capability the browser uses for spec: the row's
// override, else the catalogue's. Only "yes" returns screenshots as images.
func BrowserImages(spec Spec) string {
	if img := BrowserReachOf(spec).Images; img != "" {
		return img
	}
	if spec.Images == "" {
		return "unknown"
	}
	return spec.Images
}

// BrowserUnavailableMessage is what a provider without the browser is told.
func BrowserUnavailableMessage(spec Spec) string {
	label := spec.Label
	if label == "" {
		label = spec.ID
	}
	return fmt.Sprintf("the browser is not available for this agent (%s): %s", label, BrowserReachOf(spec).Why)
}

// BrowserReachEntry is one provider's reach, for the settings view.
type BrowserReachEntry struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Reach  Reach  `json:"reach"`
	Why    string `json:"why,omitempty"`
	Images string `json:"images"`
}

// BrowserReachList is the reach of every provider in the registry, in display order.
func (r *Registry) BrowserReachList() []BrowserReachEntry {
	var out []BrowserReachEntry
	for _, s := range r.List() {
		row := BrowserReachOf(s)
		label := s.Label
		if label == "" {
			label = s.ID
		}
		out = append(out, BrowserReachEntry{ID: s.ID, Label: label, Reach: row.Reach, Why: row.Why, Images: BrowserImages(s)})
	}
	return out
}
