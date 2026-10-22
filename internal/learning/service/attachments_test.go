package service

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/attach"
	"github.com/spacingmind/smind/internal/store"
)

// TestLearningWindow_MarkersOnly: a run's attachments reach the learning
// window as markers only -- never a GitHub title or body, a stored file name,
// or any bytes.
func TestLearningWindow_MarkersOnly(t *testing.T) {
	set := attach.Set{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Items: []attach.Item{
		{ID: "1", Kind: attach.KindImage, Name: "shot.png", MimeType: "image/png", Size: 99, File: "01-shot-STORED.png"},
		{ID: "2", Kind: attach.KindFileRef, Path: "src/a.go"},
		{ID: "3", Kind: attach.KindPR, Number: 7, Repo: "o/r", Title: "SECRET-TITLE", URL: "https://example.test/SECRET-URL"},
	}}
	wr := store.ChatWindowRun{PromptInWindow: true, Run: store.Run{ID: "r1", Prompt: "fix it", Origin: "human", Attachments: attach.EncodeSet(set)}}
	got := (&Service{}).windowRuns([]store.ChatWindowRun{wr})
	if len(got) != 1 || got[0].Prompt != "fix it" {
		t.Fatalf("windowRuns = %+v", got)
	}
	joined := strings.Join(got[0].Attachments, "\n")
	for _, want := range []string{"[attachment: image shot.png]", "[attachment: file_ref src/a.go]", "[attachment: pr #7]"} {
		if !strings.Contains(joined, want) {
			t.Errorf("markers lack %q: %s", want, joined)
		}
	}
	for _, leak := range []string{"SECRET-TITLE", "SECRET-URL", "STORED", "image/png"} {
		if strings.Contains(joined, leak) || strings.Contains(got[0].Prompt, leak) {
			t.Errorf("window input leaks %q", leak)
		}
	}
	// A prompt already reflected earlier is background: no markers either.
	wr.PromptInWindow = false
	if g := (&Service{}).windowRuns([]store.ChatWindowRun{wr}); len(g[0].Attachments) != 0 {
		t.Errorf("background prompt carries markers: %v", g[0].Attachments)
	}
}
