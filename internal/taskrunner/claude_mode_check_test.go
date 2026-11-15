package taskrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner/fakeclaude"
)

func initFrame(field string) json.RawMessage {
	return json.RawMessage(`{"type":"system","subtype":"init","session_id":"s"` + field + `}`)
}

func TestClaudeModeMismatchNote(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		requested string
		raw       json.RawMessage
		want      []string // substrings; nil = no note
		notWant   []string
	}{
		{"auto honoured is silent", "auto", initFrame(`,"permissionMode":"auto"`), nil, nil},
		{"missing field (older CLI) is silent", "auto", initFrame(``), nil, nil},
		{"number is silent", "auto", initFrame(`,"permissionMode":7`), nil, nil},
		{"null is silent", "auto", initFrame(`,"permissionMode":null`), nil, nil},
		{"empty string is silent", "auto", initFrame(`,"permissionMode":""`), nil, nil},
		{"not JSON is silent", "auto", json.RawMessage(`not json`), nil, nil},
		{"empty request is silent", "", initFrame(`,"permissionMode":"default"`), nil, nil},
		{"auto downgraded to default", "auto", initFrame(`,"permissionMode":"default"`), []string{"Manual", "Auto mode", "permissions.disableAutoMode", "The mode shown for this run is the one requested"}, nil},
		{"bypass downgraded to default", "bypassPermissions", initFrame(`,"permissionMode":"default"`), []string{"permissions.disableBypassPermissionsMode"}, nil},
		{"acceptEdits downgraded to default", "acceptEdits", initFrame(`,"permissionMode":"default"`), []string{"Manual", "Accept File Edits"}, nil},
		{"more permissive than requested warns", "default", initFrame(`,"permissionMode":"bypassPermissions"`), []string{"Warning:", "Bypass", "smind did not change the mode"}, nil},
		{"unknown mode is reported, never echoed, never as more permissive", "auto", initFrame(`,"permissionMode":"futureMode"`), []string{"does not know"}, []string{"futureMode", "Warning:"}},
		{"a hostile mode string is not echoed", "auto", initFrame(`,"permissionMode":"\u001b[31mEVIL\n` + strings.Repeat("x", 5000) + `"`), []string{"does not know"}, []string{"EVIL", "\u001b", "xxxxxxxx"}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := claudeModeMismatchNote(c.requested, c.raw)
			if c.want == nil {
				if got != "" {
					t.Fatalf("note = %q, want none", got)
				}
				return
			}
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Fatalf("note %q lacks %q", got, w)
				}
			}
			for _, w := range c.notWant {
				if strings.Contains(got, w) {
					t.Fatalf("note %q must not contain %q", got, w)
				}
			}
		})
	}
}

func TestClaudeModeNoteNamesNoPathOrContent(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"type":"system","subtype":"init","permissionMode":"default","cwd":"/home/secret/project","settings":"SUPERSECRET"}`)
	got := claudeModeMismatchNote("auto", raw)
	if got == "" || strings.Contains(got, "/home/secret") || strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("note = %q: want a note without the init frame's other fields", got)
	}
}

// runModeInit runs one claude-native turn against the fake CLI's "mode-init"
// scenario and returns every event and the (unchanged) requested settings.
func runModeInit(t *testing.T, reported, requested string) []Event {
	t.Helper()
	if reported != "" {
		t.Setenv(fakeclaude.ReportedModeEnv, reported)
	}
	_, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("mode-init"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	r := claudeNativeRunner(t, wm)
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{Mode: requested}, "", testClaudeModel, events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	return got
}

func modeNotes(events []Event) []string {
	var notes []string
	for _, e := range events {
		if e.Type == EventTypeSessionNote && strings.Contains(e.Text, "Claude started in") {
			notes = append(notes, e.Text)
		}
	}
	return notes
}

func TestInitFrameModeMismatchNotesOnce(t *testing.T) {
	events := runModeInit(t, "default", ClaudeModeAuto)
	notes := modeNotes(events)
	if len(notes) != 1 || !strings.Contains(notes[0], "permissions.disableAutoMode") {
		t.Fatalf("mode notes = %v, want exactly one naming the auto-mode setting", notes)
	}
	for _, e := range events {
		// The run's stored mode is never rewritten: no permission_mode_changed from a mismatch.
		if e.Type == EventTypePermissionModeChanged {
			t.Fatalf("a mode mismatch must not emit %v (registry persists it and retries would re-send it)", e)
		}
	}
}

func TestInitFrameModeTwoInitFramesNoteOnce(t *testing.T) {
	t.Setenv(fakeclaude.RepeatInitEnv, "1")
	if notes := modeNotes(runModeInit(t, "default", ClaudeModeAuto)); len(notes) != 1 {
		t.Fatalf("mode notes = %v, want exactly one for two init frames in one spawn", notes)
	}
}

func TestInitFrameModeMatchIsSilent(t *testing.T) {
	if notes := modeNotes(runModeInit(t, "", ClaudeModeAuto)); len(notes) != 0 {
		t.Fatalf("matching mode produced notes %v", notes)
	}
}

func TestInitFrameModeMissingOrMalformedIsSilent(t *testing.T) {
	for _, reported := range []string{"-", "#"} {
		if notes := modeNotes(runModeInit(t, reported, ClaudeModeAuto)); len(notes) != 0 {
			t.Fatalf("reported %q produced notes %v, want none and the run to complete", reported, notes)
		}
	}
}

func TestInitFrameMorePermissiveWarns(t *testing.T) {
	notes := modeNotes(runModeInit(t, ClaudeModeBypass, ClaudeModeDefault))
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "Warning:") {
		t.Fatalf("mode notes = %v, want one warning", notes)
	}
}

func TestInitFrameModeCheckDefaultsToAcceptEdits(t *testing.T) {
	// No mode requested: the run asks the CLI for acceptEdits, so a CLI that reports default is a mismatch against it.
	notes := modeNotes(runModeInit(t, ClaudeModeDefault, ""))
	if len(notes) != 1 || !strings.Contains(notes[0], "Accept File Edits") {
		t.Fatalf("mode notes = %v, want one against acceptEdits", notes)
	}
}
