package config

import (
	"strings"
	"testing"
)

func TestProviderDefaultModelLoad(t *testing.T) {
	useHome(t, "providers:\n  claude-native:\n    defaultModel: sonnet\n")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.DefaultModel("claude-native"); got != "sonnet" {
		t.Fatalf("DefaultModel(claude-native) = %q, want sonnet", got)
	}
	if got := cfg.DefaultModel("glm"); got != "" {
		t.Fatalf("DefaultModel(glm) = %q, want empty (unset keeps today's behaviour)", got)
	}
}

func TestProviderDefaultModelAbsentIsToday(t *testing.T) {
	useHome(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, id := range DefaultModelProviders {
		if got := cfg.DefaultModel(id); got != "" {
			t.Errorf("DefaultModel(%s) = %q, want empty", id, got)
		}
	}
}

func TestProviderDefaultModelLoadRejects(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"unknown provider", "providers:\n  claudee-native:\n    defaultModel: sonnet\n", "unknown provider"},
		{"leading dash", "providers:\n  glm:\n    defaultModel: \"--evil\"\n", "must not begin with '-'"},
		{"bad character", "providers:\n  glm:\n    defaultModel: \"a b\"\n", "only letters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useHome(t, tt.yaml)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestProviderDefaultModelSetGetUnset(t *testing.T) {
	path := useHome(t, "# keep me\nserver:\n  port: 5000\n")
	key := "providers.claude-native.defaultModel"
	if err := Set(key, "claude-sonnet-5-5"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got, err := Get(key); err != nil || got != "claude-sonnet-5-5" {
		t.Fatalf("Get() = %q, %v, want claude-sonnet-5-5", got, err)
	}
	if body := readFile(t, path); !strings.Contains(body, "# keep me") || !strings.Contains(body, "port: 5000") {
		t.Fatalf("config.yaml lost unrelated content:\n%s", body)
	}
	cfg, err := Load()
	if err != nil || cfg.DefaultModel("claude-native") != "claude-sonnet-5-5" {
		t.Fatalf("Load() = %+v, %v, want the stored default", cfg.Providers, err)
	}
	if err := Unset(key); err != nil {
		t.Fatalf("Unset() error = %v", err)
	}
	if got, _ := Get(key); got != "" {
		t.Fatalf("Get() after Unset = %q, want empty", got)
	}
}

func TestProviderDefaultModelSetValidates(t *testing.T) {
	useHome(t, "")
	for _, bad := range []string{"", "  ", "--model", "a b", strings.Repeat("a", 101)} {
		if err := Set("providers.glm.defaultModel", bad); err == nil {
			t.Errorf("Set(%q) error = nil, want rejected", bad)
		}
	}
	if err := Set("providers.nope.defaultModel", "x"); err == nil || !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("Set(unknown provider) error = %v, want unknown config key", err)
	}
}
