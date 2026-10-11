package main

import (
	"slices"
	"testing"

	"github.com/spacingmind/smind/internal/config"
)

func TestGLMCommandOption_MaxTurns(t *testing.T) {
	t.Parallel()
	cfg := config.Default()

	if _, ok := glmCommandOverride(cfg, ""); ok {
		t.Error("zero maxTurns: want no override")
	}

	cfg.Agents.GLM.MaxTurns = 42
	if _, ok := glmCommandOverride(cfg, "/path/to/fake-agent"); ok {
		t.Error("SMIND_ACP_COMMAND set: env override must win")
	}

	cmd, ok := glmCommandOverride(cfg, "")
	if !ok {
		t.Fatal("positive maxTurns: want an override")
	}
	n := len(cmd)
	if n < 2 || !slices.Equal(cmd[n-2:], []string{"--max-turns", "42"}) {
		t.Errorf("command = %v, want trailing --max-turns 42", cmd)
	}
}
