package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrowserReachDriftEveryProviderHasARow pins AC1: every registered
// built-in provider, every compatible recipe and the three class keys have a
// reach row, so a provider or recipe added without a row fails here.
func TestBrowserReachDriftEveryProviderHasARow(t *testing.T) {
	t.Parallel()
	for _, s := range New().List() {
		if !IsBuiltin(s.ID) {
			continue
		}
		if _, ok := browserReach[s.ID]; !ok {
			t.Errorf("%s: registered built-in provider without a browserReach row", s.ID)
		}
	}
	for _, r := range Recipes() {
		if _, ok := recipeReach[r.ID]; !ok {
			t.Errorf("recipe %s: no recipeReach row", r.ID)
		}
	}
	for _, k := range []string{ReachClassCustomACP, ReachClassPlugin, ReachClassInstance} {
		if _, ok := browserReach[k]; !ok {
			t.Errorf("class key %s: no browserReach row", k)
		}
	}
}

// TestBrowserReachNoStaleRows is the other half of AC1: every browserReach key
// is a built-in id or a class key, every recipeReach key a recipe id, so a
// removed provider or recipe leaves a row that fails here.
func TestBrowserReachNoStaleRows(t *testing.T) {
	t.Parallel()
	classes := map[string]bool{ReachClassCustomACP: true, ReachClassPlugin: true, ReachClassInstance: true}
	for id := range browserReach {
		if !classes[id] && !IsBuiltin(id) {
			t.Errorf("browserReach[%q]: neither a built-in provider id nor a class key", id)
		}
	}
	recipeIDs := map[string]bool{}
	for _, r := range Recipes() {
		recipeIDs[r.ID] = true
	}
	for id := range recipeReach {
		if !recipeIDs[id] {
			t.Errorf("recipeReach[%q]: no recipe of that id in Recipes()", id)
		}
	}
}

// TestBrowserReachRecipeHarnessConsistent: a recipe's row agrees with its
// harness (claude-code never no, codex-app-server always no, acp never no),
// and a recipe's screenshots stay unknown: the model behind the endpoint is
// not the base's, so smind must not send it images.
func TestBrowserReachRecipeHarnessConsistent(t *testing.T) {
	t.Parallel()
	for _, r := range Recipes() {
		row, ok := recipeReach[r.ID]
		if !ok {
			continue // TestBrowserReachDriftEveryProviderHasARow failed
		}
		switch r.Harness {
		case "claude-code":
			if row.Reach == ReachNo {
				t.Errorf("%s: harness claude-code but reach is %s", r.ID, ReachNo)
			}
		case "codex-app-server":
			if row.Reach != ReachNo {
				t.Errorf("%s: harness codex-app-server but reach is %s, want %s", r.ID, row.Reach, ReachNo)
			}
		case "acp":
			if row.Reach == ReachNo {
				t.Errorf("%s: harness acp but reach is %s", r.ID, ReachNo)
			}
		default:
			t.Errorf("%s: harness %q is none of claude-code, codex-app-server, acp", r.ID, r.Harness)
		}
		if row.Images != "unknown" {
			t.Errorf("%s: recipe row Images = %q, want unknown", r.ID, row.Images)
		}
	}
}

// TestBrowserReachResolution pins AC2: how a spec resolves to a row.
func TestBrowserReachResolution(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		spec Spec
		want Reach
	}{
		{"claude-native gets the row", Spec{ID: IDClaudeNative}, ReachYes},
		{"codex-native does not", Spec{ID: IDCodexNative}, ReachNo},
		{"a plugin provider is no even with NoMCPServers false", Spec{ID: "x/y", Plugin: "x", Runner: RunnerACP}, ReachNo},
		{"NoMCPServers wins over the table", Spec{ID: IDOpenCode, NoMCPServers: true}, ReachNo},
		{"a recipe instance uses its recipe's row", Spec{ID: "my-glm", Extends: IDClaudeNative, Recipe: "zai-glm"}, ReachConditional},
		{"a plain instance uses the instance class row", Spec{ID: "my-oc", Extends: IDOpenCode}, ReachConditional},
		{"an instance of a no provider stays no", Spec{ID: "my-codex", Extends: IDCodexNative}, ReachNo},
		{"a custom ACP entry uses the custom class row", Spec{ID: "mine", Custom: true, Runner: RunnerACP}, ReachConditional},
		{"a recipe cannot lift a no base", Spec{ID: "x", Extends: IDCodexNative, Custom: true, Recipe: "deepseek"}, ReachNo},
		{"an unknown provider is no", Spec{ID: "nobody"}, ReachNo},
		{"a plugin id wins over a built-in id", Spec{ID: IDClaudeNative, Plugin: "p"}, ReachNo},
	} {
		if got := BrowserReachOf(tc.spec).Reach; got != tc.want {
			t.Errorf("%s: BrowserReachOf(%+v).Reach = %s, want %s", tc.name, tc.spec, got, tc.want)
		}
	}
}

// TestBrowserImagesInstanceIsUnknown: the effective image capability comes
// from the row (an instance, a plugin, every recipe: unknown), else the
// spec's own, with "" read as unknown.
func TestBrowserImagesInstanceIsUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spec Spec
		want string
	}{
		{Spec{ID: IDClaudeNative, Images: "yes"}, "yes"},
		{Spec{ID: IDOpenCode, Images: "yes"}, "yes"},
		{Spec{ID: IDGLM, Images: "no"}, "no"},
		{Spec{ID: "i", Extends: IDClaudeNative, Images: "yes"}, "unknown"},
		{Spec{ID: "x/y", Plugin: "x", Images: "yes"}, "unknown"},
		{Spec{ID: IDKimi, Images: ""}, "unknown"},
	} {
		if got := BrowserImages(tc.spec); got != tc.want {
			t.Errorf("BrowserImages(%+v) = %q, want %q", tc.spec, got, tc.want)
		}
	}
}

// TestBrowserUnavailableMessageNamesTheProvider: the message says which agent
// it is about and why, in the fixed shape of AC5.
func TestBrowserUnavailableMessageNamesTheProvider(t *testing.T) {
	t.Parallel()
	spec := Spec{ID: IDCodexNative, Label: "Codex"}
	msg := BrowserUnavailableMessage(spec)
	prefix := "the browser is not available for this agent (Codex): "
	if !strings.HasPrefix(msg, prefix) {
		t.Fatalf("BrowserUnavailableMessage(%+v) = %q, want it to start with %q", spec, msg, prefix)
	}
	if why := BrowserReachOf(spec).Why; !strings.Contains(msg, why) {
		t.Errorf("message %q does not contain the row's why %q", msg, why)
	}
}

// TestBrowserDocsNameEveryNoProvider: public-docs/browser.md says which agents
// cannot use the browser, so every built-in provider whose row is no is named
// there by its label, and the fixed refusal phrase appears.
func TestBrowserDocsNameEveryNoProvider(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile(filepath.Join("..", "..", "public-docs", "browser.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	rowOf := func(label string) string {
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "| "+label+" |") {
				return line
			}
		}
		return ""
	}
	for _, id := range BuiltinIDs() {
		s, ok := New().Get(id)
		if !ok {
			t.Fatalf("%s: built-in id missing from the registry", id)
		}
		// Every built-in has a table row in the doc, and a no row says no.
		row := rowOf(s.Label)
		if row == "" {
			t.Errorf("public-docs/browser.md has no table row for %s (%q)", id, s.Label)
			continue
		}
		if BrowserReachOf(s).Reach == ReachNo && !strings.HasPrefix(strings.TrimPrefix(row, "| "+s.Label+" | "), "no |") {
			t.Errorf("public-docs/browser.md row for %s must say no: %q", id, row)
		}
	}
	// Every recipe has a table row (its own, or the shared recipe row), and a recipe
	// whose row is no has its OWN row saying no.
	const sharedRecipeRow = "| compatible-endpoint providers made from a recipe"
	for _, rc := range Recipes() {
		if recipeReach[rc.ID].Reach == ReachNo {
			row := rowOf(rc.Label)
			if row == "" || !strings.HasPrefix(strings.TrimPrefix(row, "| "+rc.Label+" | "), "no") {
				t.Errorf("public-docs/browser.md needs its own row saying no for recipe %s (%q): %q", rc.ID, rc.Label, row)
			}
			continue
		}
		if !strings.Contains(text, sharedRecipeRow) {
			t.Errorf("public-docs/browser.md has no shared recipe row for %s", rc.ID)
		}
	}
	// The three class rows, each with the wording of its table row.
	for key, prefix := range map[string]string{
		ReachClassCustomACP: "| providers you add yourself under `providers.custom` | offered",
		ReachClassPlugin:    "| plugin providers | never",
		ReachClassInstance:  "| a provider that extends another (an instance, with no recipe) | offered",
	} {
		if !strings.Contains(text, prefix) {
			t.Errorf("public-docs/browser.md has no row for %s starting %q", key, prefix)
		}
	}
	if !strings.Contains(text, "the browser is not available for this agent") {
		t.Error(`public-docs/browser.md does not contain "the browser is not available for this agent"`)
	}
}

// TestReachTableNeverWidensTheOldRule pins AC7. Before the table every built-in
// provider got the browser row except codex-native. The EXACT set the table offers
// is written out here, so widening or narrowing it is a deliberate test edit.
func TestReachTableNeverWidensTheOldRule(t *testing.T) {
	t.Parallel()
	want := map[string]bool{IDClaudeNative: true, IDGLM: true, IDKimi: true, IDOpenCode: true, IDCopilot: true, IDCline: true, IDGoose: true, IDCursor: true}
	reg := New()
	got := map[string]bool{}
	for _, s := range reg.List() {
		if IsBuiltin(s.ID) && BrowserReachOf(s).Offered() {
			got[s.ID] = true
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("%s is offered the browser and is not in the expected set", id)
		}
	}
	for id := range want {
		if !got[id] {
			t.Errorf("%s is expected to be offered the browser and is not", id)
		}
	}
	// What is built on a provider never gets more than the provider: an instance of
	// codex-native with any recipe or none, of a custom ACP entry that takes no MCP
	// servers, and a plugin provider or no-MCP agent in any form.
	custom := Spec{ID: "mine", Custom: true, Runner: RunnerACP, NoMCPServers: true}
	no := []Spec{
		{ID: "i", Extends: IDCodexNative, Custom: true, Runner: RunnerCodex},
		{ID: "ic", Extends: "mine", Custom: true, Runner: RunnerACP, NoMCPServers: true},
		custom,
		{ID: "p/a", Plugin: "p", Runner: RunnerACP},
		{ID: "p/b", Plugin: "p", Extends: IDOpenCode},
		{ID: "n", Extends: IDOpenCode, NoMCPServers: true},
	}
	for _, rc := range Recipes() {
		no = append(no, Spec{ID: "ir-" + rc.ID, Extends: IDCodexNative, Custom: true, Recipe: rc.ID, Runner: RunnerCodex})
	}
	for _, s := range no {
		if BrowserReachOf(s).Offered() {
			t.Errorf("%+v is offered the browser", s)
		}
	}
	// An instance of a custom ACP entry that DOES take MCP servers is offered, as the entry is.
	if !BrowserReachOf(Spec{ID: "ic2", Extends: "mine", Custom: true, Runner: RunnerACP}).Offered() {
		t.Error("an instance of an offered custom ACP entry should be offered (not guaranteed)")
	}
}

// An instance of another provider made from the Codex-harness recipe is told the
// recipe's harness as the reason, not "Codex takes no per-run MCP servers" about a
// provider that is not Codex.
func TestBrowserReachRecipeHarnessReason(t *testing.T) {
	t.Parallel()
	spec := Spec{ID: "cc-resp", Label: "Claude via responses", Extends: IDClaudeNative, Custom: true, Recipe: "openai-responses-compatible", Runner: RunnerClaude}
	row := BrowserReachOf(spec)
	if row.Reach != ReachNo {
		t.Fatalf("reach = %s, want no", row.Reach)
	}
	if !strings.Contains(row.Why, "Codex app-server harness") || strings.Contains(row.Why, "Codex takes") {
		t.Errorf("why = %q: must name the recipe's harness", row.Why)
	}
	if msg := BrowserUnavailableMessage(spec); !strings.HasPrefix(msg, "the browser is not available for this agent (Claude via responses): ") {
		t.Errorf("message = %q", msg)
	}
}
