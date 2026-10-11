package acp

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestGLMCommand_DefaultsPinnedVersion guards against pinning a
// glm-acp-agent older than 1.11.0, which reports only the last model
// call's usage in PromptResponse.usage.
func TestGLMCommand_DefaultsPinnedVersion(t *testing.T) {
	t.Parallel()
	cmd := GLMCommand()
	var version string
	for _, a := range cmd {
		if v, ok := strings.CutPrefix(a, "glm-acp-agent@"); ok {
			version = v
		}
	}
	if version == "" {
		t.Fatalf("GLMCommand() = %v has no glm-acp-agent@X.Y.Z arg", cmd)
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		t.Fatalf("version %q is not X.Y.Z", version)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		n[i] = v
	}
	if n[0] < 1 || (n[0] == 1 && n[1] < 11) {
		t.Fatalf("glm-acp-agent@%s < 1.11.0 reports last-call-only usage", version)
	}
	if slices.Contains(cmd, "--max-turns") {
		t.Fatalf("GLMCommand() = %v must not carry --max-turns", cmd)
	}
}

func TestGLMCommand_MaxTurnsFlag(t *testing.T) {
	t.Parallel()
	base := GLMCommand()
	for _, n := range []int{0, -3} {
		if got := GLMCommandWithMaxTurns(n); !slices.Equal(got, base) {
			t.Errorf("GLMCommandWithMaxTurns(%d) = %v, want %v", n, got, base)
		}
	}
	got := GLMCommandWithMaxTurns(7)
	want := append(slices.Clone(base), "--max-turns", "7")
	if !slices.Equal(got, want) {
		t.Errorf("GLMCommandWithMaxTurns(7) = %v, want %v", got, want)
	}
}
