package taskrunner

import (
	"context"
	"strings"
)

// PromptImage is an image the runner may send natively.
type PromptImage struct {
	Name, MimeType, Path string
	Size                 int64
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

// promptWithAttachments is the text/path fallback every runner uses in this
// PR (native image blocks come later): the rendered context blocks follow the
// user's prompt. A claude-native slash command is sent unchanged, because
// appended text would change what the CLI parses as the command's arguments.
func (r *Runner) promptWithAttachments(ctx context.Context, provider Provider, prompt string, events chan<- Event) string {
	pa, ok := PromptAttachmentsFrom(ctx)
	if !ok || pa.Render == nil {
		return prompt
	}
	_, blocks := pa.Render(false)
	if len(blocks) == 0 {
		return prompt
	}
	if provider == ProviderClaudeNative && strings.HasPrefix(prompt, "/") {
		r.sendSessionNote(ctx, events, attachmentSlashNote)
		return prompt
	}
	return prompt + "\n\n" + strings.Join(blocks, "\n\n")
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
