package wsapi

import (
	"os"
	"regexp"
	"sort"
	"testing"
)

// The web UI's single events.subscribe list lives in
// web/packages/ui/src/hooks/use-daemon-events.ts as a set of NAME_TOPICS
// arrays spread into one final TOPICS array. The daemon rejects the whole
// subscribe if any single topic is unknown (parseTopics), so this test
// pins the two sides together: everything the web subscribes to must be a
// key of knownTopics.
const webTopicsHook = "../../web/packages/ui/src/hooks/use-daemon-events.ts"

func TestWebSubscribedTopicsAreAllAllowlisted(t *testing.T) {
	raw, err := os.ReadFile(webTopicsHook)
	if err != nil {
		t.Fatalf("cannot read %s: %v -- has the web UI's subscription hook moved?", webTopicsHook, err)
	}

	// [A-Z_]* rather than + so the final bare `const TOPICS = [...]` is
	// captured too; that array is handled separately below because its body
	// mixes "topic" literals with ...NAME_TOPICS spreads.
	arrayRe := regexp.MustCompile(`(?s)const\s+([A-Z_]*TOPICS)\s*=\s*\[(.*?)\]`)
	literalRe := regexp.MustCompile(`"([^"]+)"`)
	spreadRe := regexp.MustCompile(`\.\.\.([A-Z_]+TOPICS)`)

	declared := map[string][]string{} // NAME_TOPICS -> its topic literals
	var finalLiterals []string
	spread := map[string]bool{}
	for _, m := range arrayRe.FindAllStringSubmatch(string(raw), -1) {
		name, body := m[1], m[2]
		if name == "TOPICS" {
			for _, l := range literalRe.FindAllStringSubmatch(body, -1) {
				finalLiterals = append(finalLiterals, l[1])
			}
			for _, s := range spreadRe.FindAllStringSubmatch(body, -1) {
				spread[s[1]] = true
			}
			continue
		}
		for _, l := range literalRe.FindAllStringSubmatch(body, -1) {
			declared[name] = append(declared[name], l[1])
		}
	}
	if len(declared) == 0 {
		t.Fatalf("no NAME_TOPICS arrays parsed from %s -- the hook's shape changed", webTopicsHook)
	}

	distinct := map[string]bool{}
	var missing []string
	for name, topics := range declared {
		if !spread[name] {
			t.Errorf("%s is declared but never spread (...%s) into the final TOPICS array", name, name)
		}
		for _, topic := range topics {
			distinct[topic] = true
			if !knownTopics[topic] {
				missing = append(missing, topic)
			}
		}
	}
	for _, topic := range finalLiterals {
		distinct[topic] = true
		if !knownTopics[topic] {
			missing = append(missing, topic)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("web subscribes to topics the daemon rejects: %v -- the whole events.subscribe would fail", missing)
	}

	// Sanity bounds so a broken parser cannot pass vacuously.
	if len(distinct) < 25 {
		t.Fatalf("parsed %d distinct topics from %s, want at least 25 -- the parser is likely broken", len(distinct), webTopicsHook)
	}
	for _, want := range []string{"fleet.hosts.changed", "task.status"} {
		if !distinct[want] {
			t.Errorf("topic %q missing from the parsed subscription list", want)
		}
	}
}

func TestFleetHostsChangedTopicIsKnownAndPublished(t *testing.T) {
	if !knownTopics[TopicFleetHostsChanged] {
		t.Fatalf("knownTopics[%q] = false, want true", TopicFleetHostsChanged)
	}
	if TopicFleetHostsChanged != "fleet.hosts.changed" {
		t.Fatalf("TopicFleetHostsChanged = %q, want fleet.hosts.changed", TopicFleetHostsChanged)
	}

	// TestFleetHostsChangedPublishesOnMutation covers the fleet.* method
	// mutations; this pins the direct publish path the UI's FLEET_TOPICS
	// subscription relies on.
	e := newFleetEnv(t)
	sub := subscribeTopics(e.api, TopicFleetHostsChanged)
	e.api.FleetHostsChanged()
	n := 0
	for _, ev := range queued(sub) {
		if ev.Topic == TopicFleetHostsChanged && ev.Payload == nil {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("FleetHostsChanged published %d fleet.hosts.changed events, want 1", n)
	}
}
