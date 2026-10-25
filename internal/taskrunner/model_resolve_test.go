package taskrunner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

func TestResolveModel(t *testing.T) {
	t.Parallel()
	reg := providers.New()
	for _, id := range []string{"claude-own", "claude-inherit"} {
		if err := reg.Register(providers.Spec{ID: id, Label: id, Extends: providers.IDClaudeNative}); err != nil {
			t.Fatal(err)
		}
	}
	r := New(nil, WithProviderRegistry(reg),
		WithDefaultModel(ProviderClaudeNative, "claude-haiku-5-5"), WithDefaultModel("claude-own", "claude-sonnet-5-5"))
	bare := New(nil, WithProviderRegistry(reg))
	for _, tc := range []struct {
		name     string
		r        *Runner
		provider Provider
		asked    string
		want     string
		refused  bool
	}{
		{"requested wins", r, ProviderClaudeNative, "opus", "opus", false},
		{"configured default", r, ProviderClaudeNative, "", "claude-haiku-5-5", false},
		{"default keyword passes through", r, ProviderClaudeNative, ClaudeModelDefault, ClaudeModelDefault, false},
		{"instance own default", r, "claude-own", "", "claude-sonnet-5-5", false},
		{"instance falls back to base", r, "claude-inherit", "", "claude-haiku-5-5", false},
		{"claude, nothing resolves", bare, ProviderClaudeNative, "", "", true},
		{"instance, nothing resolves", bare, "claude-own", "", "", true},
		{"glm keeps its own", bare, ProviderGLM, "", "", false},
		{"codex keeps its own", bare, ProviderCodexNative, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.r.ResolveModel(tc.provider, tc.asked)
			var nm *NoModelError
			if errors.As(err, &nm) != tc.refused || (err != nil && !tc.refused) {
				t.Fatalf("ResolveModel(%q, %q) error = %v, refused want %v", tc.provider, tc.asked, err, tc.refused)
			}
			if got != tc.want {
				t.Fatalf("ResolveModel(%q, %q) = %q, want %q", tc.provider, tc.asked, got, tc.want)
			}
			if tc.refused && nm.Base != ProviderClaudeNative {
				t.Fatalf("refusal names %q, want the base provider's setting", nm.Base)
			}
		})
	}
}

// Every claude launcher refuses an empty model before the CLI is spawned: the
// last line of defence behind ResolveModel (mutation: let it omit --model).
func TestUnpinnedClaude_LauncherRefusesEmptyModel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		run  func(r *Runner, taskID int64, events chan Event) error
	}{
		{"RunPrompt", func(r *Runner, id int64, ev chan Event) error {
			return r.RunPrompt(t.Context(), id, id, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", "", ev)
		}},
		{"RunPromptIsolated", func(r *Runner, id int64, ev chan Event) error {
			return r.RunPromptIsolated(t.Context(), id, id, ProviderClaudeNative, "hi", "", IsolationOptions{SystemPrompt: "s"}, ev)
		}},
		{"RunPromptProbe", func(r *Runner, id int64, ev chan Event) error {
			return r.RunPromptProbe(t.Context(), id, id, ProviderClaudeNative, "hi", "", ProbeOptions{}, ev)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wm, task := newTestTask(t, "echo-args")
			if err := os.RemoveAll(filepath.Join(*task.WorktreePath, ".git")); err != nil {
				t.Fatal(err)
			}
			events := make(chan Event, 8)
			var nm *NoModelError
			if err := tc.run(claudeNativeRunner(t, wm), task.ID, events); !errors.As(err, &nm) {
				t.Fatalf("error = %v, want *NoModelError", err)
			}
			if _, err := os.Stat(filepath.Join(*task.WorktreePath, "args")); err == nil {
				t.Fatal("the CLI was spawned for a refused launch")
			}
		})
	}
}
