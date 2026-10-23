package config

import (
	"strings"
	"testing"
)

func TestSetProviderDefaultModel(t *testing.T) {
	const base = "# keep me\nserver:\n  port: 5000 # port comment\n"
	tests := []struct {
		name      string
		start     string
		provider  string
		model     string
		wantErr   string
		wantModel string // for glm/claude-native per provider below
	}{
		{name: "set", start: base, provider: "claude-native", model: "sonnet", wantModel: "sonnet"},
		{name: "change", start: base + "providers:\n  claude-native:\n    defaultModel: opus\n", provider: "claude-native", model: "sonnet", wantModel: "sonnet"},
		{name: "clear", start: base + "providers:\n  claude-native:\n    defaultModel: opus\n", provider: "claude-native", model: "  ", wantModel: ""},
		{name: "clear absent", start: base, provider: "glm", model: "", wantModel: ""},
		{name: "unknown provider", start: base, provider: "nope", model: "x", wantErr: `unknown provider "nope"`},
		{name: "unknown provider clear", start: base, provider: "nope", model: "", wantErr: `unknown provider "nope"`},
		{name: "invalid model", start: base, provider: "glm", model: "--evil", wantErr: "must not begin with '-'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := useHome(t, tt.start)
			err := SetProviderDefaultModel(tt.provider, tt.model)
			body := readFile(t, path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				if body != tt.start {
					t.Fatalf("file changed on error:\n%s", body)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.DefaultModel(tt.provider); got != tt.wantModel {
				t.Fatalf("DefaultModel = %q, want %q", got, tt.wantModel)
			}
			if !strings.Contains(body, "# keep me") || !strings.Contains(body, "port: 5000") || !strings.Contains(body, "# port comment") {
				t.Fatalf("comments/unrelated keys lost:\n%s", body)
			}
		})
	}
}
