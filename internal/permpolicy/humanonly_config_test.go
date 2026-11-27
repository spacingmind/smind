package permpolicy

import (
	"slices"
	"strings"
	"testing"
)

func Test_human_only_table_names_config_set(t *testing.T) {
	t.Parallel()
	var row *humanOnlyCommand
	for i := range humanOnlyCLI {
		if slices.Equal(humanOnlyCLI[i].path, []string{"config"}) {
			row = &humanOnlyCLI[i]
		}
	}
	if row == nil {
		t.Fatal("humanOnlyCLI has no config row")
	}
	if !slices.Equal(row.verbs, []string{"set", "unset"}) || !row.keyFilter || row.bare || row.flag != "" {
		t.Errorf("config row = %+v; want verbs set,unset, keyFilter, not bare", *row)
	}
	if !slices.Contains(HumanOnlyRPCs, "config.set") {
		t.Error(`HumanOnlyRPCs lacks "config.set"`)
	}
	for _, c := range []struct {
		args string
		want bool
	}{
		{"config set fleet.enabled true", true},
		{"config unset server.port", true},
		{"config --x set k v", true},
		{"config set", true},
		{"config get", false},
		{"config get server.port", false},
		{"config", false},
		{"config --help", false},
	} {
		if got := HumanOnlyCLI(splitWords(c.args)); got != c.want {
			t.Errorf("HumanOnlyCLI(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

func Test_config_cli_key_filter_is_empty_by_default(t *testing.T) {
	if len(agentSafeKeys) != 0 {
		t.Fatalf("agentSafeKeys = %v; a key needs a reason and a test of its own", agentSafeKeys)
	}
	// Not parallel: it edits the package-level map.
	agentSafeKeys["scratch.key"] = "test only"
	t.Cleanup(func() { delete(agentSafeKeys, "scratch.key") })
	if HumanOnlyCLI([]string{"config", "set", "scratch.key", "v"}) {
		t.Error("a listed key is still human-only: the filter does not filter")
	}
	if !HumanOnlyCLI([]string{"config", "set", "other.key", "v"}) {
		t.Error("an unlisted key is not human-only")
	}
	if !HumanOnlyCLI([]string{"config", "set"}) {
		t.Error("a missing key is not human-only")
	}
	if !HumanOnlyCLI([]string{"config", "set", "$(echo scratch.key)", "v"}) {
		t.Error("a computed key is not human-only")
	}
}

func Test_floor_denies_config_set_wrapped(t *testing.T) {
	t.Parallel()
	var cases []floorCase
	for _, c := range []string{"smind config set fleet.enabled true", "smind config unset plugins.enabled"} {
		rest := strings.TrimPrefix(c, "smind ")
		for _, w := range []string{
			"%s",
			"bash -c '%s'",
			"sh -c '%s'",
			"bash -lc \"%s\"",
			"env X=1 %s",
			"command %s",
			"echo fleet | xargs %s",
			"/usr/local/bin/%s",
			"su -c '%s' someone",
			"sh <<EOF\n%s\nEOF",
			"echo '%s' | sh",
		} {
			cases = append(cases, floorCase{strings.Replace(w, "%s", c, 1), "no-self-approve"})
		}
		cases = append(cases,
			floorCase{`"smind" ` + rest, "no-self-approve"},
			floorCase{"$(which smind) " + rest, "no-self-approve"},
			floorCase{"smind $(echo config) " + rest[len("config "):], "no-self-approve"},
			floorCase{"smind config $(echo set) fleet.enabled true", "no-self-approve"},
		)
	}
	checkFloorCases(t, cases)
}

func Test_floor_allows_config_get_and_lookalikes(t *testing.T) {
	t.Parallel()
	var cases []floorCase
	for _, c := range []string{
		"smind config get", "smind config get server.port", "smind config", "smind config --help",
		"npm config set registry x", "git config set", "echo smind config set x y",
		"bash -c 'smind config get server.port'", "grep -rn 'smind config set' docs",
	} {
		cases = append(cases, floorCase{c, ""})
	}
	checkFloorCases(t, cases)
}
