package main

import (
	"testing"

	"github.com/spacingmind/smind/internal/config"
)

func Test_config_cli_known_keys_match_the_table(t *testing.T) {
	known := map[string]bool{}
	for _, k := range config.KnownKeys() {
		known[k] = true
	}
	for _, ck := range config.ConfigKeys() {
		if ck.Writable && !known[ck.Key] {
			t.Errorf("writable key %q is not a CLI known key", ck.Key)
		}
	}
	for _, k := range []string{"fleet.enabled", "plugins.enabled", "learning.enabled", "automations.paused"} {
		if !known[k] {
			t.Errorf("%q is not among config.KnownKeys()", k)
		}
	}
}
