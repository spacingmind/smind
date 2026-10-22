package main

import (
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestDefaultModelProvidersMatchTaskrunner keeps config.DefaultModelProviders
// (config cannot import taskrunner) in step with the providers the daemon
// actually supports.
func TestDefaultModelProvidersMatchTaskrunner(t *testing.T) {
	want := map[string]bool{}
	for _, p := range taskrunner.SupportedProviders() {
		want[string(p.ID)] = true
	}
	got := map[string]bool{}
	for _, id := range config.DefaultModelProviders {
		got[id] = true
	}
	for id := range want {
		if !got[id] {
			t.Errorf("provider %q is supported but missing from config.DefaultModelProviders", id)
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("config.DefaultModelProviders has %q, which taskrunner does not support", id)
		}
	}
}
