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

func runWith(t *testing.T, r *Runner, task store.Task, p Provider, prompt string, d *Delivery) []Event {
	t.Helper()
	ctx := context.Background()
	if d != nil {
		ctx = WithDelivery(ctx, *d)
	}
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

func TestDeliveryFrom_RoundTrip(t *testing.T) {
	t.Parallel()
	if _, ok := DeliveryFrom(context.Background()); ok {
		t.Fatal("empty ctx has a delivery")
	}
	d, ok := DeliveryFrom(WithDelivery(context.Background(), Delivery{Preface: "p"}))
	if !ok || d.Preface != "p" || d.Empty() {
		t.Fatalf("got %+v %v", d, ok)
	}
}

func TestClaudeRun_AddsProjectionDir(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	dir := t.TempDir()
	runWith(t, r, task, ProviderClaudeNative, "hi", &Delivery{AddDirs: []string{dir}})
	if v, ok := flagValue(readArgs(t, task), "--add-dir"); !ok || v != dir {
		t.Fatalf("--add-dir = %q (present %v), want %q", v, ok, dir)
	}
}

func TestClaudeRun_NoSkillsNoOption(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	runWith(t, r, task, ProviderClaudeNative, "hi", nil)
	baseline := readArgs(t, task)
	if _, ok := flagValue(baseline, "--add-dir"); ok {
		t.Fatalf("--add-dir present without delivery: %q", baseline)
	}
	// A fresh chat (the first run left a resume handle behind) with an empty
	// delivery must launch with the very same argv.
	wm2, task2 := newTestTask(t, "echo-args")
	runWith(t, claudeNativeRunner(t, wm2), task2, ProviderClaudeNative, "hi", &Delivery{})
	if got := readArgs(t, task2); strings.Join(got, "\n") != strings.Join(baseline, "\n") {
		t.Fatalf("empty delivery changed argv:\n%q\nvs\n%q", got, baseline)
	}
}

func TestReflector_IgnoresDelivery(t *testing.T) {
	t.Parallel()
	wm, task := newIsolatedTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	ctx := WithDelivery(context.Background(), Delivery{AddDirs: []string{t.TempDir()}, Preface: "x"})
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPromptIsolated(ctx, task.ID, task.ID, ProviderClaudeNative, "hi", "", IsolationOptions{SystemPrompt: "s"}, events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	assertIsolatedFlags(t, readArgs(t, task), "s")
}

// recordPrompts opts the task's fake ACP agent into logging prompt blocks.
func recordPrompts(t *testing.T, task store.Task) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "record-prompt-blocks"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// promptBlocks reads the fake agent's "prompt-blocks" log: one entry (a
// list of text blocks) per session/prompt.
func promptBlocks(t *testing.T, task store.Task) [][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "prompt-blocks"))
	if err != nil {
		t.Fatalf("read prompt-blocks: %v", err)
	}
	var out [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var blocks []struct{ Text string }
		if err := json.Unmarshal([]byte(line), &blocks); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		var texts []string
		for _, b := range blocks {
			texts = append(texts, b.Text)
		}
		out = append(out, texts)
	}
	return out
}

func TestACPRun_PrefacesFirstPromptOnly(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	recordPrompts(t, task)
	r := glmRunnerWithCaps(wm, "loadSession")
	d := &Delivery{Preface: "SKILL INDEX"}
	events := runWith(t, r, task, ProviderGLM, "first", d)
	runWith(t, r, task, ProviderGLM, "second", d)
	got := promptBlocks(t, task)
	if len(got) != 2 {
		t.Fatalf("prompts = %v", got)
	}
	if len(got[0]) != 2 || got[0][0] != "SKILL INDEX" || got[0][1] != "first" {
		t.Fatalf("first prompt blocks = %q, want [preface, user]", got[0])
	}
	if len(got[1]) != 1 || got[1][0] != "second" {
		t.Fatalf("resumed prompt blocks = %q, want only the user's text", got[1])
	}
	for _, e := range events {
		if strings.Contains(e.Text, "SKILL INDEX") {
			t.Fatalf("event leaks the preface: %+v", e)
		}
	}
}

// A session that could not be resumed is a new session: it gets the preface.
func TestACPRun_PrefacesResetSession(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	recordPrompts(t, task)
	r := glmRunnerWithCaps(wm, "none") // cannot resume => fresh session/new each run
	d := &Delivery{Preface: "IDX"}
	runWith(t, r, task, ProviderGLM, "a", d)
	runWith(t, r, task, ProviderGLM, "b", d)
	for i, blocks := range promptBlocks(t, task) {
		if len(blocks) != 2 || blocks[0] != "IDX" {
			t.Fatalf("prompt %d blocks = %q, want a preface", i, blocks)
		}
	}
}

func TestACPRun_NoDeliveryNoLeadingBlock(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	recordPrompts(t, task)
	r := glmRunnerWithCaps(wm, "loadSession")
	runWith(t, r, task, ProviderGLM, "only", nil)
	if got := promptBlocks(t, task); len(got) != 1 || len(got[0]) != 1 || got[0][0] != "only" {
		t.Fatalf("blocks = %q", got)
	}
}

func TestCodexRun_Unchanged(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := codexRunner(wm)
	got := runWith(t, r, task, ProviderCodexNative, "hi", &Delivery{AddDirs: []string{t.TempDir()}, Preface: "NOPE"})
	if len(got) == 0 || got[len(got)-1].Type != EventTypeDone {
		t.Fatalf("events = %+v", got)
	}
	for _, e := range got {
		if strings.Contains(e.Text, "NOPE") {
			t.Fatalf("codex event carries the preface: %+v", e)
		}
	}
}
