package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// TestBeforeStartEditsReachTheLaunch_ClaudeNative: the validated edits of a
// plugin's run.beforeStart hook (LaunchEdits on the context) reach the claude
// CLI's environment and MCP config, and never replace what is already there.
func TestBeforeStartEditsReachTheLaunch_ClaudeNative(t *testing.T) {
	t.Setenv("PRESET_VAR", "daemon")
	r, _, task := newClaudeMcpEnv(t)
	ctx := WithLaunchEdits(context.Background(), LaunchEdits{
		Env: map[string]string{"ACME_MODE": "strict", "PRESET_VAR": "plugin"},
		Mcp: []store.McpServer{{Name: "acme__files", Transport: "stdio", Command: "go", Args: `["version"]`, Env: `{}`, Enabled: true, OwnerPlugin: "acme"}},
	})
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{}, "", testClaudeModel, events)
	}()
	var notes []string
	for e := range events {
		if e.Type == EventTypeSessionNote {
			notes = append(notes, e.Text)
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	env, err := os.ReadFile(filepath.Join(*task.WorktreePath, "env-all"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "ACME_MODE=strict") {
		t.Fatalf("the hook's env must reach the CLI: %s", env)
	}
	if !strings.Contains(string(env), "PRESET_VAR=daemon") || strings.Contains(string(env), "PRESET_VAR=plugin") {
		t.Fatalf("an existing key is never overridden: %s", env)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "PRESET_VAR") {
		t.Fatalf("the skipped key is named in a session note: %q", notes)
	}
	cfg, err := os.ReadFile(filepath.Join(*task.WorktreePath, "mcp-config-content"))
	if err != nil || !strings.Contains(string(cfg), `"acme__files"`) {
		t.Fatalf("the hook's MCP server must be in the config: %s %v", cfg, err)
	}
}

func TestHookEnvEntries_SkipsExistingAndDaemonKeys(t *testing.T) {
	t.Setenv("FROM_DAEMON", "1")
	ctx := WithLaunchEdits(context.Background(), LaunchEdits{Env: map[string]string{"B": "2", "A": "1", "FROM_DAEMON": "x", "IN_LIST": "y"}})
	add, note := hookEnvEntries(ctx, []string{"IN_LIST=z"})
	if strings.Join(add, ",") != "A=1,B=2" {
		t.Fatalf("add = %v", add)
	}
	if !strings.Contains(note, "FROM_DAEMON") || !strings.Contains(note, "IN_LIST") {
		t.Fatalf("note = %q", note)
	}
	if add, note := hookEnvEntries(context.Background(), nil); add != nil || note != "" {
		t.Fatalf("no edits: %v %q", add, note)
	}
}
