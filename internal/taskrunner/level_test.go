package taskrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

func levelCatalog() ProviderInfo {
	return ProviderInfo{ID: ProviderCodexNative, ModelsDiscovered: true, Models: []ModelInfo{
		{ID: "gpt-a", Default: true, Efforts: []EffortInfo{{ID: "low"}, {ID: "medium"}, {ID: "xhigh"}}, DefaultEffort: "medium"},
		{ID: "gpt-b", Efforts: []EffortInfo{{ID: "low"}}},
		{ID: "gpt-none"},
	}}
}

func TestThinkingLevelSyntax(t *testing.T) {
	t.Parallel()
	for _, ok := range []ThinkingLevel{"", "off", "xhigh", "level_2", "a.b-c"} {
		if !ok.IsValid() {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []ThinkingLevel{"High", "-x", "1x", "a b", "a;b", ThinkingLevel(strings.Repeat("a", 33))} {
		if bad.IsValid() {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestResolveLevel(t *testing.T) {
	t.Parallel()
	claude := ProviderInfo{ID: ProviderClaudeNative}
	acp := ProviderInfo{ID: ProviderGLM}
	cases := []struct {
		name     string
		catalog  ProviderInfo
		kind     providers.Runner
		model    string
		level    ThinkingLevel
		explicit bool
		want     ThinkingLevel
		wantNote bool
		wantErr  string
	}{
		{"unspecified", levelCatalog(), providers.RunnerCodex, "gpt-a", "", true, "", false, ""},
		{"codex supported explicit", levelCatalog(), providers.RunnerCodex, "gpt-a", "xhigh", true, "xhigh", false, ""},
		{"codex default model used", levelCatalog(), providers.RunnerCodex, "", "xhigh", true, "xhigh", false, ""},
		{"codex unsupported explicit names the list", levelCatalog(), providers.RunnerCodex, "gpt-b", "xhigh", true, "", false, "supported: low"},
		{"codex off explicit rejected with list", levelCatalog(), providers.RunnerCodex, "gpt-a", "off", true, "", false, "supported: low, medium, xhigh"},
		{"codex unsupported inherited dropped", levelCatalog(), providers.RunnerCodex, "gpt-b", "xhigh", false, "", true, ""},
		{"codex old standard inherited dropped", levelCatalog(), providers.RunnerCodex, "gpt-a", "standard", false, "", true, ""},
		{"codex model without efforts: any non-claude level passes", levelCatalog(), providers.RunnerCodex, "gpt-none", "max", true, "max", false, ""},
		{"codex model without efforts: claude word rejected", levelCatalog(), providers.RunnerCodex, "gpt-none", "extended", true, "", false, "not discovered"},
		{"codex undiscovered: claude word dropped when inherited", ProviderInfo{ID: ProviderCodexNative}, providers.RunnerCodex, "", "standard", false, "", true, ""},
		{"codex undiscovered: effort passes through", ProviderInfo{ID: ProviderCodexNative}, providers.RunnerCodex, "", "high", true, "high", false, ""},
		{"claude supported", claude, providers.RunnerClaude, "sonnet", "extended", true, "extended", false, ""},
		{"claude codex effort explicit rejected", claude, providers.RunnerClaude, "sonnet", "medium", true, "", false, "supported: off, standard, extended"},
		{"claude codex effort inherited dropped", claude, providers.RunnerClaude, "sonnet", "xhigh", false, "", true, ""},
		{"acp drops with a note", acp, providers.RunnerACP, "", "high", true, "", true, ""},
	}
	for _, tc := range cases {
		got, note, err := ResolveLevel(tc.catalog, tc.kind, tc.model, tc.level, tc.explicit)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want || (note != "") != tc.wantNote {
			t.Errorf("%s: got (%q, %q, %v), want level %q note=%v", tc.name, got, note, err, tc.want, tc.wantNote)
		}
	}
}

// runCodexLevel runs one codex turn on a discovered catalog with the
// fake's turn/start recording on, returning the events and the recorded
// turn/start params ("" when none).
func runCodexLevel(t *testing.T, level ThinkingLevel, model string) ([]Event, string) {
	t.Helper()
	wm, task := newTestTask(t, "reply")
	r := codexRunner(wm)
	r.acpModeProbe = true
	waitForDiscoveredModels(t, r, ProviderCodexNative)
	rec := filepath.Join(*task.WorktreePath, "record-prompt-blocks")
	if err := os.WriteFile(rec, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderCodexNative, "hi", nil, PermissionSettings{}, level, model, events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(*task.WorktreePath, "turn-input"))
	return got, string(raw)
}

func TestRunCodexAppliesSupportedEffort(t *testing.T) {
	t.Parallel()
	_, turn := runCodexLevel(t, "high", "gpt-fake-a")
	if !strings.Contains(turn, `"effort":"high"`) {
		t.Fatalf("turn/start = %s, want effort high on the wire", turn)
	}
}

func TestRunCodexUnspecifiedEffortSendsNothing(t *testing.T) {
	t.Parallel()
	events, turn := runCodexLevel(t, "", "gpt-fake-a")
	if turn == "" || strings.Contains(turn, "effort") {
		t.Fatalf("turn/start = %q, want a turn without an effort key", turn)
	}
	for _, e := range events {
		if e.Type == EventTypeSessionNote && strings.Contains(e.Text, "reasoning level") {
			t.Fatalf("unexpected note %q", e.Text)
		}
	}
}

// An inherited level the model does not list never reaches Codex and never
// fails the run: it is dropped with a session note.
func TestRunCodexDropsUnsupportedInheritedEffort(t *testing.T) {
	t.Parallel()
	for _, level := range []ThinkingLevel{"xhigh", "standard"} {
		events, turn := runCodexLevel(t, level, "gpt-fake-b")
		if turn == "" || strings.Contains(turn, "effort") {
			t.Fatalf("%s: turn/start = %q, want no effort", level, turn)
		}
		noted := false
		for _, e := range events {
			noted = noted || (e.Type == EventTypeSessionNote && strings.Contains(e.Text, "not supported"))
		}
		if !noted {
			t.Fatalf("%s: no drop note in %+v", level, events)
		}
	}
}

func TestRunClaudeRejectsCodexEffortLevel(t *testing.T) {
	t.Parallel()
	got, note, err := ResolveLevel(ProviderInfo{ID: ProviderClaudeNative}, providers.RunnerClaude, "sonnet", "medium", false)
	if err != nil || got != "" || note == "" {
		t.Fatalf("inherited codex effort on claude = (%q, %q, %v), want dropped with note", got, note, err)
	}
}
