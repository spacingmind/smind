package wsapi

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// A session note replays through run.logs as a chunk; clients concatenate
// adjacent chunks, so it must end in a paragraph break or the next assistant
// text is glued onto it (UAT A-12).
func TestToRunLogEvent_SessionNoteIsItsOwnParagraph(t *testing.T) {
	got := toRunLogEvent(taskrunner.Event{Type: taskrunner.EventTypeSessionNote, Text: "uat does not support resuming a session"})
	if got.Type != "chunk" {
		t.Fatalf("Type = %q, want chunk (wire format unchanged)", got.Type)
	}
	if !strings.Contains(got.Text, "uat does not support resuming a session") {
		t.Fatalf("Text = %q lost the note", got.Text)
	}
	if !strings.HasSuffix(got.Text, "\n\n") {
		t.Fatalf("Text = %q must end with a paragraph break", got.Text)
	}
}
