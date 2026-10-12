//go:build windows

package taskrunner

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/claudecli"
)

// TestRunner_RunPrompt_ClaudeNative_ThroughTheWindowsLauncher drives a whole
// turn the way the daemon does on Windows: the SDK starts smind.exe (this
// test binary) as the CLI, which in launcher mode runs the real claude.exe
// (a copy of this test binary under that name, playing the fake CLI) inside
// a Job Object -- the SDK's JSON stdio protocol has to survive the extra hop.
func TestRunner_RunPrompt_ClaudeNative_ThroughTheWindowsLauncher(t *testing.T) {
	t.Parallel()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The launcher only runs a file called claude.exe.
	target := filepath.Join(t.TempDir(), "claude.exe")
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}

	wm, task := newTestTask(t, "")
	r := New(wm) // the real newClaudeClient: no WithCLIPath of its own
	r.claudeLaunch = func() (string, []string, error) {
		return self, []string{claudecli.LaunchEnv + "=" + target}, nil
	}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", "", events)
	}()
	got := withoutUsage(drainEvents(events))
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	if len(got) != 3 || got[0].Text != "hello " || got[1].Text != "from claude" || got[2].Type != EventTypeDone {
		t.Fatalf("events through the launcher = %+v, want hello / from claude / done", got)
	}
	if _, ok := got[2].Raw.(claudecode.ResultMessage); !ok {
		t.Fatalf("event[2].Raw = %#v, want claudecode.ResultMessage", got[2].Raw)
	}
}
