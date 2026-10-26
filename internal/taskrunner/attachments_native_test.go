package taskrunner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\nnative-image-bytes")

// imageCtx attaches one stored PNG (plus the given context blocks) to ctx;
// Render honours native the way internal/attach does: images only when native.
func imageCtx(t *testing.T, name string, blocks ...string) (context.Context, PromptImage) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	img := PromptImage{Name: name, MimeType: "image/png", Path: path, Size: int64(len(pngBytes))}
	return withImage(img, blocks...), img
}

func withImage(img PromptImage, blocks ...string) context.Context {
	return WithPromptAttachments(context.Background(), PromptAttachments{
		Render: func(native bool) ([]PromptImage, []string) {
			if native {
				return []PromptImage{img}, blocks
			}
			return nil, append(append([]string(nil), blocks...), imagePathNote(img))
		},
	})
}

type wireBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
	Path     string `json:"path"`
	Source   struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

func claudeContent(t *testing.T, line string) []wireBlock {
	t.Helper()
	var msg struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	var blocks []wireBlock
	if err := json.Unmarshal(msg.Message.Content, &blocks); err != nil {
		t.Fatalf("content is not an array: %s", msg.Message.Content)
	}
	return blocks
}

func TestClaudeNative_SendsImageBlocks(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	ctx, _ := imageCtx(t, "shot.png", "ctx-note")
	runWithCtx(t, ctx, claudeNativeRunner(t, wm), task, ProviderClaudeNative, "describe it")
	blocks := claudeContent(t, readFile(t, filepath.Join(*task.WorktreePath, "prompt-line")))
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[1].Type != "image" {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[0].Text != "describe it\n\nctx-note" {
		t.Fatalf("text = %q", blocks[0].Text)
	}
	src := blocks[1].Source
	if src.Type != "base64" || src.MediaType != "image/png" {
		t.Fatalf("source = %+v", src)
	}
	got, err := base64.StdEncoding.DecodeString(src.Data)
	if err != nil || string(got) != string(pngBytes) {
		t.Fatalf("image data does not decode to the stored bytes: %v", err)
	}
}

func TestClaudeNative_TextOnlyUnchanged(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	runWithCtx(t, attachCtx(), claudeNativeRunner(t, wm), task, ProviderClaudeNative, "hello")
	line := readFile(t, filepath.Join(*task.WorktreePath, "prompt-line"))
	if !strings.Contains(line, `"content":"hello\n\n`) {
		t.Fatalf("text-only turn is no longer a string content: %s", line)
	}
}

func TestClaudeNative_SlashCommandTextLast(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	ctx, _ := imageCtx(t, "shot.png", "ctx-note")
	events := runWithCtx(t, ctx, claudeNativeRunner(t, wm), task, ProviderClaudeNative, "/compact now")
	blocks := claudeContent(t, readFile(t, filepath.Join(*task.WorktreePath, "prompt-line")))
	if len(blocks) != 3 || blocks[0].Text != "ctx-note" || blocks[1].Type != "image" || blocks[2].Type != "text" || blocks[2].Text != "/compact now" {
		t.Fatalf("blocks = %+v", blocks)
	}
	for _, e := range events {
		if e.Type == EventTypeSessionNote && e.Text == attachmentSlashNote {
			t.Fatal("slash note emitted although images were sent natively")
		}
	}
}

func TestClaudeNative_UnreadableImageFallsBackToPath(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	missing := PromptImage{Name: "gone.png", MimeType: "image/png", Path: filepath.Join(t.TempDir(), "gone.png"), Size: 9}
	events := runWithCtx(t, withImage(missing), claudeNativeRunner(t, wm), task, ProviderClaudeNative, "look")
	line := readFile(t, filepath.Join(*task.WorktreePath, "prompt-line"))
	// The line is JSON: a Windows path's backslashes appear doubled in it.
	wantPath, _ := json.Marshal(missing.Path)
	if !strings.Contains(line, `"content":"look`) || !strings.Contains(line, strings.Trim(string(wantPath), `"`)) {
		t.Fatalf("no path fallback in %s", line)
	}
	noted := false
	for _, e := range events {
		if e.Type == EventTypeSessionNote && strings.Contains(e.Text, "could not read attachment gone.png") {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("no read-failure session note in %+v", events)
	}
}

func readACPBlocks(t *testing.T, worktree string) []wireBlock {
	t.Helper()
	data := readFile(t, filepath.Join(worktree, "prompt-blocks"))
	var blocks []wireBlock
	if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &blocks); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	return blocks
}

func TestACP_ImageBlockWhenCapabilityAdvertised(t *testing.T) {
	t.Setenv("FAKEAGENT_IMAGE_CAP", "1")
	wm, task := newTestTask(t, "")
	recordPrompts(t, task)
	ctx, _ := imageCtx(t, "shot.png")
	runWithCtx(t, ctx, glmRunnerWithCaps(wm, "loadSession"), task, ProviderKimi, "describe")
	blocks := readACPBlocks(t, *task.WorktreePath)
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[0].Text != "describe" {
		t.Fatalf("blocks = %+v", blocks)
	}
	if b := blocks[1]; b.Type != "image" || b.MimeType != "image/png" || b.Data != base64.StdEncoding.EncodeToString(pngBytes) {
		t.Fatalf("image block = %+v", b)
	}
}

func TestACP_NoImageBlockWithoutCapability(t *testing.T) {
	t.Setenv("FAKEAGENT_PROMPT_CAPS_EMPTY", "1")
	wm, task := newTestTask(t, "")
	recordPrompts(t, task)
	ctx, img := imageCtx(t, "shot.png")
	runWithCtx(t, ctx, glmRunnerWithCaps(wm, "loadSession"), task, ProviderGLM, "describe")
	blocks := readACPBlocks(t, *task.WorktreePath)
	if len(blocks) != 1 || blocks[0].Type != "text" || !strings.Contains(blocks[0].Text, img.Path) {
		t.Fatalf("blocks = %+v", blocks)
	}
}

func TestCodexNative_SendsLocalImage(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	recordPrompts(t, task)
	ctx, img := imageCtx(t, "shot.png")
	runWithCtx(t, ctx, codexRunner(wm), task, ProviderCodexNative, "describe")
	var got struct {
		Input []wireBlock `json:"input"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(readFile(t, filepath.Join(*task.WorktreePath, "turn-input")))), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Input) != 2 || got.Input[0].Type != "text" || got.Input[1].Type != "localImage" || got.Input[1].Path != img.Path {
		t.Fatalf("input = %+v", got.Input)
	}
}
