package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderEnvLoadAccepts(t *testing.T) {
	// Temp dirs, single-quoted in the YAML: a bare "/Users/x" is not absolute on
	// Windows, and its paths hold backslashes.
	claudeDir, codexDir := filepath.Join(t.TempDir(), ".claude-scopedocs"), filepath.Join(t.TempDir(), "codex")
	useHome(t, "providers:\n  claude-native:\n    env:\n      CLAUDE_CONFIG_DIR: '"+claudeDir+"'\n  codex-native:\n    env:\n      CODEX_HOME: '"+codexDir+"'\n")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.ProviderEnv("claude-native")["CLAUDE_CONFIG_DIR"]; got != claudeDir {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q", got)
	}
	if got := cfg.ProviderEnv("codex-native")["CODEX_HOME"]; got != codexDir {
		t.Fatalf("CODEX_HOME = %q", got)
	}
	if got := cfg.ProviderEnv("glm"); got != nil {
		t.Fatalf("glm env = %v, want none", got)
	}
}

func TestProviderEnvRejects(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"other key", "providers:\n  claude-native:\n    env:\n      ANTHROPIC_API_KEY: /x\n", "allowed: CLAUDE_CONFIG_DIR"},
		{"PATH", "providers:\n  claude-native:\n    env:\n      PATH: /usr/bin\n", "not allowed"},
		{"key of another provider", "providers:\n  claude-native:\n    env:\n      CODEX_HOME: /x\n", "not allowed"},
		{"provider without env keys", "providers:\n  glm:\n    env:\n      CLAUDE_CONFIG_DIR: /x\n", "no settable launch env"},
		{"relative", "providers:\n  claude-native:\n    env:\n      CLAUDE_CONFIG_DIR: rel/dir\n", "must be absolute"},
		{"NUL", "providers:\n  claude-native:\n    env:\n      CLAUDE_CONFIG_DIR: \"/a\\0b\"\n", "NUL or control"},
		{"newline", "providers:\n  claude-native:\n    env:\n      CLAUDE_CONFIG_DIR: \"/a\\nb\"\n", "NUL or control"},
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

func TestProviderEnvExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err := ValidateProviderEnv("claude-native", "CLAUDE_CONFIG_DIR", "~/.claude-scopedocs/")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".claude-scopedocs"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// Loading expands it too, so a launch never sees a literal "~".
	useHome(t, "providers:\n  claude-native:\n    env:\n      CLAUDE_CONFIG_DIR: ~/cfg\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ProviderEnv("claude-native")["CLAUDE_CONFIG_DIR"]; got != filepath.Join(home, "cfg") {
		t.Fatalf("loaded %q", got)
	}
}

func TestProviderEnvNeedNotExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "unmounted", "drive")
	if _, err := ValidateProviderEnv("claude-native", "CLAUDE_CONFIG_DIR", missing); err != nil {
		t.Fatalf("a missing directory must be accepted: %v", err)
	}
}

func TestSetProviderEnv(t *testing.T) {
	path := useHome(t, "# keep me\nserver:\n  port: 5000\n")
	dir := filepath.Join(t.TempDir(), "cfg")
	got, err := SetProviderEnv("claude-native", "CLAUDE_CONFIG_DIR", dir)
	if err != nil || got != dir {
		t.Fatalf("SetProviderEnv = %q, %v", got, err)
	}
	body := readFile(t, path)
	if !strings.Contains(body, "# keep me") || !strings.Contains(body, dir) {
		t.Fatalf("config.yaml:\n%s", body)
	}
	if _, err := SetProviderEnv("claude-native", "ANTHROPIC_API_KEY", "/x"); err == nil || !strings.Contains(err.Error(), "allowed: CLAUDE_CONFIG_DIR") {
		t.Fatalf("allowlist bypass: %v", err)
	}
	if _, err := SetProviderEnv("claude-native", "CLAUDE_CONFIG_DIR", "rel"); err == nil {
		t.Fatal("a relative path was accepted")
	}
	if _, err := SetProviderEnv("nope", "CLAUDE_CONFIG_DIR", dir); err == nil {
		t.Fatal("an unknown provider was accepted")
	}
	if _, err := SetProviderEnv("claude-native", "CLAUDE_CONFIG_DIR", " "); err != nil {
		t.Fatal(err)
	}
	if body := readFile(t, path); strings.Contains(body, dir) || !strings.Contains(body, "# keep me") {
		t.Fatalf("clear left the value or lost comments:\n%s", body)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
