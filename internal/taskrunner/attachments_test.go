package taskrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

const attachBlock = "<attachment>ctx-block-sentinel</attachment>"

func attachCtx(dirs ...string) context.Context {
	return WithPromptAttachments(context.Background(), PromptAttachments{
		Render: func(native bool) ([]PromptImage, []string) {
			if native {
				panic("native rendering is not used in this PR")
			}
			return nil, []string{attachBlock, "second-block"}
		},
		AddDirs: dirs,
	})
}

func runWithCtx(t *testing.T, ctx context.Context, r *Runner, task store.Task, p Provider, prompt string) []Event {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, p, prompt, denyAllDecider{}, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	return got
}

func wantPrompt(prompt string) string { return prompt + "\n\n" + attachBlock + "\n\n" + "second-block" }

func TestPromptAttachmentsFrom_RoundTrip(t *testing.T) {
	t.Parallel()
	if _, ok := PromptAttachmentsFrom(context.Background()); ok {
		t.Fatal("empty ctx has attachments")
	}
	if pa, ok := PromptAttachmentsFrom(attachCtx("d")); !ok || len(pa.AddDirs) != 1 {
		t.Fatalf("got %+v %v", pa, ok)
	}
}

func TestPromptInput_FallbackAllRunners(t *testing.T) {
	t.Parallel()
	t.Run("acp", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "")
		recordPrompts(t, task)
		runWithCtx(t, attachCtx(), glmRunnerWithCaps(wm, "loadSession"), task, ProviderGLM, "hello")
		got := promptBlocks(t, task)
		if len(got) != 1 || len(got[0]) != 1 || got[0][0] != wantPrompt("hello") {
			t.Fatalf("acp prompt blocks = %q", got)
		}
	})
	t.Run("claude", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "echo-args")
		runWithCtx(t, attachCtx(), claudeNativeRunner(t, wm), task, ProviderClaudeNative, "hello")
		line := readFile(t, filepath.Join(*task.WorktreePath, "prompt-line"))
		if !strings.Contains(line, "hello") || !strings.Contains(line, "ctx-block-sentinel") || !strings.Contains(line, "second-block") {
			t.Fatalf("claude prompt line = %s", line)
		}
	})
	t.Run("codex", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "")
		recordPrompts(t, task)
		runWithCtx(t, attachCtx(), codexRunner(wm), task, ProviderCodexNative, "hello")
		line := readFile(t, filepath.Join(*task.WorktreePath, "turn-input"))
		if !strings.Contains(line, "hello") || !strings.Contains(line, "ctx-block-sentinel") || !strings.Contains(line, "second-block") {
			t.Fatalf("codex turn input = %s", line)
		}
	})
	t.Run("none", func(t *testing.T) {
		t.Parallel()
		wm, task := newTestTask(t, "")
		recordPrompts(t, task)
		runWithCtx(t, context.Background(), glmRunnerWithCaps(wm, "loadSession"), task, ProviderGLM, "hello")
		if got := promptBlocks(t, task); len(got) != 1 || got[0][0] != "hello" {
			t.Fatalf("prompt blocks = %q", got)
		}
	})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPromptInput_ClaudeAddDirsForSet(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	set, skills := t.TempDir(), t.TempDir()
	ctx := WithDelivery(attachCtx(set), Delivery{AddDirs: []string{skills}})
	runWithCtx(t, ctx, claudeNativeRunner(t, wm), task, ProviderClaudeNative, "hello")
	args := strings.Join(readArgs(t, task), "\n")
	for _, d := range []string{set, skills} {
		if !strings.Contains(args, d) {
			t.Fatalf("--add-dir lacks %q in args %q", d, args)
		}
	}
}

func TestPromptInput_SlashCommandNotAppended(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	set := t.TempDir()
	events := runWithCtx(t, attachCtx(set), claudeNativeRunner(t, wm), task, ProviderClaudeNative, "/compact now")
	line := readFile(t, filepath.Join(*task.WorktreePath, "prompt-line"))
	if strings.Contains(line, "ctx-block-sentinel") || !strings.Contains(line, "/compact now") {
		t.Fatalf("slash prompt line = %s", line)
	}
	if strings.Contains(strings.Join(readArgs(t, task), "\n"), set) {
		t.Fatal("slash command still adds the attachment dir")
	}
	noted := false
	for _, e := range events {
		if e.Type == EventTypeSessionNote && e.Text == attachmentSlashNote {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("no slash-command session note in %+v", events)
	}
}

// TestACPEcho_ImageChunkNotPersisted: an agent that echoes an image
// user_message_chunk does not get its base64 data into any event (events are
// what run_events stores and run.logs serves).
func TestACPEcho_ImageChunkNotPersisted(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-image")
	got := runWithCtx(t, context.Background(), glmRunnerWithCaps(wm, "loadSession"), task, ProviderGLM, "hi")
	sawReply := false
	for _, e := range got {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "ECHOED-IMAGE-SENTINEL-DATA") {
			t.Fatalf("event carries the echoed image data: %s", b)
		}
		if e.Type == EventTypeText && e.Text == "reply after the echo" {
			sawReply = true
		}
	}
	if !sawReply {
		t.Fatalf("the text reply after the echo was lost: %+v", got)
	}
}
