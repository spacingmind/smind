package wsapi

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const webEventsHookPath = "../../web/packages/ui/src/hooks/use-daemon-events.ts"

// TestWebSubscribedTopicsAreAllAllowlisted guards the web client's single
// events.subscribe call: the daemon rejects the whole call if any topic is not
// in knownTopics, so one stray topic silences every live update.
func TestWebSubscribedTopicsAreAllAllowlisted(t *testing.T) {
	src, err := os.ReadFile(webEventsHookPath)
	if err != nil {
		t.Fatalf("cannot read the web events hook %s: %v", webEventsHookPath, err)
	}
	arrays := regexp.MustCompile(`(?s)const\s+([A-Z_]*TOPICS)\s*=\s*\[(.*?)\]`).FindAllStringSubmatch(string(src), -1)
	literal := regexp.MustCompile(`"([^"]+)"`)
	spread := regexp.MustCompile(`\.\.\.([A-Z_]+TOPICS)`)

	seen := map[string]bool{}
	declared := []string{}
	var finalBody string
	for _, m := range arrays {
		name, body := m[1], m[2]
		if name == "TOPICS" {
			finalBody = body
			continue
		}
		declared = append(declared, name)
		for _, l := range literal.FindAllStringSubmatch(body, -1) {
			seen[l[1]] = true
		}
	}
	if finalBody == "" {
		t.Fatalf("final `const TOPICS = [...]` not found in %s", webEventsHookPath)
	}
	for _, l := range literal.FindAllStringSubmatch(finalBody, -1) {
		seen[l[1]] = true
	}

	var missing []string
	for topic := range seen {
		if !knownTopics[topic] {
			missing = append(missing, topic)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("web subscribes to topics the daemon rejects: [%s] -- the whole events.subscribe would fail", strings.Join(missing, ", "))
	}

	spread2 := map[string]bool{}
	for _, m := range spread.FindAllStringSubmatch(finalBody, -1) {
		spread2[m[1]] = true
	}
	for _, name := range declared {
		if !spread2[name] {
			t.Errorf("%s is declared but never spread into the final TOPICS array", name)
		}
	}

	if len(seen) < 25 {
		t.Errorf("parsed only %d distinct topics from %s; the parser is likely broken", len(seen), webEventsHookPath)
	}
	for _, want := range []string{"fleet.hosts.changed", "task.status"} {
		if !seen[want] {
			t.Errorf("expected topic %q among the parsed web topics", want)
		}
	}
}

// Publishing on mutation is covered by TestFleetHostsChangedPublishesOnMutation.
func TestFleetHostsChangedTopicIsKnownAndPublished(t *testing.T) {
	if TopicFleetHostsChanged != "fleet.hosts.changed" {
		t.Fatalf("TopicFleetHostsChanged = %q, want fleet.hosts.changed", TopicFleetHostsChanged)
	}
	if !knownTopics[TopicFleetHostsChanged] {
		t.Fatal("fleet.hosts.changed is not in knownTopics")
	}
}
