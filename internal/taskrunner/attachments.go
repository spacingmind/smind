package taskrunner

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// PromptImage is an image the runner may send natively.
type PromptImage struct {
	Name, MimeType, Path string
	Size                 int64
	// data holds the file's bytes once attachmentParts has read them.
	data []byte
}

// PromptAttachments is a run's prompt attachments (ADR-0038), attached to its
// ctx so RunPrompt's signature stays unchanged. internal/taskrunner does not
// import internal/attach: the registry closes over the attachment store.
type PromptAttachments struct {
	// Render returns the images the runner may send natively (empty unless
	// native) and the text blocks to append to the prompt.
	Render func(native bool) (images []PromptImage, context []string)
	// AddDirs are directories the agent must be able to read (the set dir).
	AddDirs []string
}

type promptAttachmentsKey struct{}

// WithPromptAttachments returns ctx carrying pa.
func WithPromptAttachments(ctx context.Context, pa PromptAttachments) context.Context {
	return context.WithValue(ctx, promptAttachmentsKey{}, pa)
}

// PromptAttachmentsFrom returns the attachments attached to ctx, if any.
func PromptAttachmentsFrom(ctx context.Context) (PromptAttachments, bool) {
	pa, ok := ctx.Value(promptAttachmentsKey{}).(PromptAttachments)
	return pa, ok
}

// attachmentSlashNote is the session note sent when a slash command could not
// carry its attachments.
const attachmentSlashNote = "attachments were not sent with a slash command"

// attachmentParts renders the run's attachments for a runner that sends
// images natively iff native. It returns the images that were read OK (their
// bytes loaded into img.data) and the text blocks to put in the prompt: the
// rendered context plus, for every image that could not be read, a session
// note and the same path sentence the text fallback uses -- an unreadable
// attachment never fails the turn.
func (r *Runner) attachmentParts(ctx context.Context, native bool, events chan<- Event) (images []PromptImage, blocks []string) {
	pa, ok := PromptAttachmentsFrom(ctx)
	if !ok || pa.Render == nil {
		return nil, nil
	}
	imgs, blocks := pa.Render(native)
	for _, img := range imgs {
		data, err := os.ReadFile(img.Path)
		if err != nil {
			r.sendSessionNote(ctx, events, fmt.Sprintf("could not read attachment %s: %v", img.Name, err))
			blocks = append(blocks, imagePathNote(img))
			continue
		}
		img.data = data
		images = append(images, img)
	}
	return images, blocks
}

// imagePathNote is the text fallback for an image that cannot be sent
// natively (same wording as internal/attach's non-native render).
func imagePathNote(img PromptImage) string {
	return fmt.Sprintf(
		"The user attached an image: %s (%s, %d bytes). You cannot view images directly in this session; it is saved at %s for any tool that can read it.",
		img.Name, img.MimeType, img.Size, img.Path)
}

// attachmentsFor returns the prompt text and native images for one runner
// (ADR-0038 section 3). Each runner decides native itself and renders once:
// the rendered context blocks follow the user's prompt. A claude-native slash
// command is sent unchanged, because appended text would change what the CLI
// parses as the command's arguments.
func (r *Runner) attachmentsFor(ctx context.Context, provider Provider, prompt string, native bool, events chan<- Event) (string, []PromptImage) {
	images, blocks := r.attachmentParts(ctx, native, events)
	if len(blocks) == 0 && len(images) == 0 {
		return prompt, nil
	}
	if r.isClaude(provider) && strings.HasPrefix(prompt, "/") && len(images) == 0 {
		r.sendSessionNote(ctx, events, attachmentSlashNote)
		return prompt, nil
	}
	if len(blocks) == 0 {
		return prompt, images
	}
	return prompt + "\n\n" + strings.Join(blocks, "\n\n"), images
}

// claudePrompt sends one claude-native turn. Without attachments it is
// exactly client.Prompt(prompt). Images ride as native blocks, text first; for
// a slash command the CLI only expands it when it is the last block, so the
// context text (if any) and the images go before it. A slash command with
// nothing to send natively still goes out unchanged, with the session note.
func (r *Runner) claudePrompt(ctx context.Context, client claudeBackend, prompt string, updates chan<- claudecode.Message, events chan<- Event) (claudecode.ResultMessage, error) {
	images, blocks := r.attachmentParts(ctx, true, events)
	slash := strings.HasPrefix(prompt, "/")
	if slash && len(images) == 0 {
		if len(blocks) > 0 {
			r.sendSessionNote(ctx, events, attachmentSlashNote)
		}
		return client.Prompt(ctx, prompt, updates)
	}
	joined := strings.Join(blocks, "\n\n")
	if len(images) == 0 {
		if joined != "" {
			prompt += "\n\n" + joined
		}
		return client.Prompt(ctx, prompt, updates)
	}
	var out []claudecode.PromptBlock
	if slash {
		if joined != "" {
			out = append(out, claudecode.PromptText(joined))
		}
	} else {
		text := prompt
		if joined != "" {
			text += "\n\n" + joined
		}
		out = append(out, claudecode.PromptText(text))
	}
	for _, img := range images {
		out = append(out, claudecode.PromptImage(img.MimeType, base64.StdEncoding.EncodeToString(img.data)))
	}
	if slash {
		out = append(out, claudecode.PromptText(prompt))
	}
	return client.PromptBlocks(ctx, out, updates)
}

// attachmentAddDirs returns the extra directories the claude-native CLI needs
// for this run's attachments; none for a slash command (nothing was sent).
func attachmentAddDirs(ctx context.Context, prompt string) []string {
	pa, ok := PromptAttachmentsFrom(ctx)
	if !ok || strings.HasPrefix(prompt, "/") {
		return nil
	}
	return pa.AddDirs
}
