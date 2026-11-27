package config

import (
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Test_config_allowlist_defaults_match_the_built_in_defaults keeps the
// table's Default column honest: it is what the daemon uses when the key is
// absent, so it must equal what Default() holds for that yaml path.
func Test_config_allowlist_defaults_match_the_built_in_defaults(t *testing.T) {
	data, err := yaml.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	root := doc.Content[0]
	for _, ck := range ConfigKeys() {
		n := find(root, strings.Split(ck.Key, "."))
		got := ""
		if n != nil {
			got = n.Value
		} else if ck.Kind != kindBool {
			continue // omitempty drops the zero value of a non-bool
		} else {
			got = "false" // omitempty drops a false bool
		}
		want := ck.Default
		if ck.Kind == kindFloat {
			f, _ := strconv.ParseFloat(want, 64)
			g, _ := strconv.ParseFloat(got, 64)
			if f != g {
				t.Errorf("%s: table default %q, Default() has %q", ck.Key, want, got)
			}
			continue
		}
		if ck.Key == "server.listen" || ck.Key == "server.hostnames" {
			continue // derived from the port / empty list
		}
		if want != got {
			t.Errorf("%s: table default %q, Default() has %q", ck.Key, want, got)
		}
	}
}
