package runs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const (
	seedSession = "11111111-2222-4333-8444-555555555555"
	seedPrompt  = "Fix the failing test"
)

// TestSeedReadContractFrames seeds an isolated home with one finished skeleton
// run per rebuild outcome, for the web frame pass of the history read contract
// (ADR-0053). It does nothing unless SMIND_SEED_HOME is set; the Claude session
// files go under SMIND_SEED_CLAUDE_DIR, which the daemon must be started with as
// CLAUDE_CONFIG_DIR.
func TestSeedReadContractFrames(t *testing.T) {
	home := os.Getenv("SMIND_SEED_HOME")
	if home == "" {
		t.Skip("SMIND_SEED_HOME not set")
	}
	claudeDir := os.Getenv("SMIND_SEED_CLAUDE_DIR")
	if claudeDir == "" {
		t.Fatal("SMIND_SEED_CLAUDE_DIR is required")
	}
	fixture, err := os.ReadFile("../history/native/testdata/claude/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(home, "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	wsDir := filepath.Join(home, "workspace")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := db.CreateWorkspace(store.Workspace{Path: wsDir, Title: "Read contract", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	projDir := filepath.Join(claudeDir, "projects", native.ClaudeProjectDir("/work/repo"))
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := func(session string) string { return filepath.Join(projDir, session+".jsonl") }
	write := func(session string, data []byte) {
		if err := os.WriteFile(sessionPath(session), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	started := time.Date(2026, 11, 5, 9, 10, 0, 0, time.UTC)
	fin := started.Add(10 * time.Second)
	sum := sha256.Sum256([]byte(seedPrompt))
	claudeRef := func(session string) string {
		b, _ := json.Marshal(native.Ref{Provider: "claude-native", Harness: native.HarnessClaudeCode, SessionID: session, ConfigDir: claudeDir, Cwd: "/work/repo",
			PromptSha256: hex.EncodeToString(sum[:]), TurnWindow: native.TurnWindow{StartedAt: started, FinishedAt: fin}})
		return string(b)
	}

	const (
		missingSession = "99999999-2222-4333-8444-555555555555"
		largeSession   = "22222222-2222-4333-8444-555555555555"
		driftSession   = "33333333-2222-4333-8444-555555555555"
	)
	cases := []struct {
		title, provider, ref string
		prepare              func()
	}{
		{"Rebuildable", "claude-native", claudeRef(seedSession), func() { write(seedSession, fixture) }},
		{"Session file gone", "claude-native", claudeRef(missingSession), func() {}},
		{"Too large", "claude-native", claudeRef(largeSession), func() {
			f, err := os.Create(sessionPath(largeSession))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := f.Truncate(65 << 20); err != nil {
				t.Fatal(err)
			}
		}},
		{"Format drift", "claude-native", claudeRef(driftSession), func() {
			write(driftSession, []byte(strings.ReplaceAll(string(fixture), `"tool_use_id":"toolu_B`, `"tool_use_id":"renamed_B`)))
		}},
		{"Cannot rebuild", "glm", "", func() {}},
	}
	for i, c := range cases {
		task, err := db.CreateTask(store.Task{WorkspaceID: ws.ID, Title: c.title, Status: "created"})
		if err != nil {
			t.Fatal(err)
		}
		chat, err := db.CreateChat(task.ID, c.title)
		if err != nil {
			t.Fatal(err)
		}
		id := "seed-run-" + string(rune('a'+i))
		if _, err := db.CreateRun(store.Run{ID: id, TaskID: task.ID, ChatID: chat.ID, Provider: c.provider, Prompt: seedPrompt, Status: "done", StartedAt: started, FinishedAt: &fin}); err != nil {
			t.Fatal(err)
		}
		for j, ev := range seedTurn() {
			d, err := EncodeStoredEvent(ev)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.AppendRunEvent(id, int64(j), d); err != nil {
				t.Fatal(err)
			}
		}
		if c.ref != "" {
			if err := db.SetRunNativeRef(id, c.ref); err != nil {
				t.Fatal(err)
			}
		}
		if err := Skeletonize(db, id); err != nil {
			t.Fatal(err)
		}
		c.prepare()
	}
}

// seedTurn is the Claude fixture turn as the live path stored it (the same
// events as hcLiveTurn in internal/wsapi/history_contract_test.go).
func seedTurn() []taskrunner.Event {
	heredoc := "cat > notes.txt <<'EOF'\nsecret-token-123\nEOF\necho written"
	msgs := []claudecode.Message{
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ThinkingBlock{Thinking: "need the failing test first"}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.TextBlock{Text: "I will run the tests. "}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ToolUseBlock{ID: "toolu_B1", Name: "Bash", Input: map[string]any{"command": heredoc, "description": "write notes"}}}},
		claudecode.UserMessage{Content: []claudecode.ContentBlock{claudecode.ToolResultBlock{ToolUseID: "toolu_B1", Content: json.RawMessage(`"written"`)}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ToolUseBlock{ID: "toolu_B2", Name: "Read", Input: map[string]any{"file_path": "/work/repo/main.go"}}}},
		claudecode.UserMessage{Content: []claudecode.ContentBlock{claudecode.ToolResultBlock{ToolUseID: "toolu_B2", Content: json.RawMessage(`[{"type":"text","text":"package main"}]`)}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.ToolUseBlock{ID: "toolu_B3", Name: "Bash", Input: map[string]any{"command": "go test ./..."}}}},
		claudecode.UserMessage{Content: []claudecode.ContentBlock{claudecode.ToolResultBlock{ToolUseID: "toolu_B3", Content: json.RawMessage(`"FAIL: boom"`), IsError: true}}},
		claudecode.AssistantMessage{Content: []claudecode.ContentBlock{claudecode.TextBlock{Text: "Done: fixed."}}},
	}
	var out []taskrunner.Event
	for _, m := range msgs {
		out = append(out, taskrunner.ClaudeMessageEvents(m)...)
	}
	return append(out, taskrunner.Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
}
