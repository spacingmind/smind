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
	for _, id := range BuiltinIDs() {
		s, ok := New().Get(id)
		if !ok {
			t.Fatalf("%s: built-in id missing from the registry", id)
		}
		if BrowserReachOf(s).Reach != ReachNo {
			continue
		}
		if !strings.Contains(text, s.Label) {
			t.Errorf("public-docs/browser.md does not name %s by its label %q", id, s.Label)
		}
	}
	if !strings.Contains(text, "the browser is not available for this agent") {
		t.Error(`public-docs/browser.md does not contain "the browser is not available for this agent"`)
	}
}

// TestReachTableNeverWidensTheOldRule pins AC7: before the table, every built-in
// provider got the browser row except codex-native (and a plugin provider, and an
// agent that takes no MCP servers). The table must not offer it to anything else.
func TestReachTableNeverWidensTheOldRule(t *testing.T) {
	t.Parallel()
	reg := New()
	for _, s := range reg.List() {
		if !IsBuiltin(s.ID) {
			continue
		}
		oldRule := s.ID != IDCodexNative && s.Plugin == "" && !s.NoMCPServers
		if got := BrowserReachOf(s).Offered(); got && !oldRule {
			t.Errorf("%s: the table offers the browser where the old rule did not", s.ID)
		}
	}
}
