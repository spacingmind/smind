package main

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/wsapi"
)

var (
	tickRe  = regexp.MustCompile("`([^`]+)`")
	parenRe = regexp.MustCompile(`\([^)]*\)`)
)

func sortedSet(items []string) string {
	s := append([]string(nil), items...)
	sort.Strings(s)
	return strings.Join(s, ",")
}

// TestDocs_CapabilityTable_MatchesCode keeps the plugin docs' capability table
// and the observable event topics equal to wsapi.PluginCapabilityTable and
// wsapi.PluginEventTopics: a method added to (or dropped from) the code without
// the docs fails here.
func TestDocs_CapabilityTable_MatchesCode(t *testing.T) {
	root := repoRoot(t)
	want := map[string]string{}
	for capName, ms := range wsapi.PluginCapabilityTable {
		var names []string
		for _, m := range ms {
			names = append(names, m.Name)
		}
		want[capName] = sortedSet(names)
	}
	for _, page := range []string{"plugins.md", "plugins-authoring.md"} {
		md := readFile(t, root+"/public-docs/"+page)
		got := map[string]string{}
		h := strings.Index(md, "| Capability | Methods |")
		if h < 0 {
			t.Fatalf("public-docs/%s has no capability table", page)
		}
		for _, line := range strings.Split(md[h:], "\n")[2:] {
			if !strings.HasPrefix(line, "|") {
				break
			}
			cells := strings.SplitN(strings.Trim(line, "| "), "|", 2)
			if len(cells) != 2 {
				continue
			}
			capName := strings.Trim(strings.TrimSpace(cells[0]), "`")
			methods := parenRe.ReplaceAllString(cells[1], "")
			var names []string
			for _, tk := range tickRe.FindAllStringSubmatch(methods, -1) {
				names = append(names, tk[1])
			}
			got[capName] = sortedSet(names)
		}
		for capName, w := range want {
			if got[capName] != w {
				t.Errorf("public-docs/%s: capability %q lists %q, the code has %q", page, capName, got[capName], w)
			}
		}
		for capName := range got {
			if _, ok := want[capName]; !ok {
				t.Errorf("public-docs/%s: table row for %q, which is not in PluginCapabilityTable", page, capName)
			}
		}
	}

	// Event topics: the authoring page lists exactly the code's allowlist
	// between its markers; the plugins page must name every one of them.
	authoring := readFile(t, root+"/public-docs/plugins-authoring.md")
	i := strings.Index(authoring, "<!-- BEGIN EVENT TOPICS -->")
	j := strings.Index(authoring, "<!-- END EVENT TOPICS -->")
	if i < 0 || j < i {
		t.Fatal("plugins-authoring.md lost its EVENT TOPICS markers")
	}
	var docTopics []string
	for _, tk := range tickRe.FindAllStringSubmatch(authoring[i:j], -1) {
		docTopics = append(docTopics, tk[1])
	}
	if sortedSet(docTopics) != sortedSet(wsapi.PluginEventTopics) {
		t.Errorf("plugins-authoring.md event topics %q differ from the code %q", sortedSet(docTopics), sortedSet(wsapi.PluginEventTopics))
	}
	plugins := readFile(t, root+"/public-docs/plugins.md")
	for _, topic := range wsapi.PluginEventTopics {
		if !strings.Contains(plugins, "`"+topic+"`") {
			t.Errorf("public-docs/plugins.md does not name the observable topic %q", topic)
		}
	}
}
