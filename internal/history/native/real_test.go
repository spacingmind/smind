package native

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealSessionSmoke runs the readers on a real session file of a development
// machine and logs COUNTS only (never text). Opt in:
//
//	SMIND_REAL_CLAUDE_SESSION=<projects/<dir>/<id>.jsonl> or
//	SMIND_REAL_CODEX_ROLLOUT=<sessions/.../rollout-...jsonl>
//	go test ./internal/history/native -run RealSessionSmoke -v
//
// It takes the session's first prompt and its time as the "run", so a pass means
// the real format still parses and the turn matches by hash and window.
func TestRealSessionSmoke(t *testing.T) {
	if p := os.Getenv("SMIND_REAL_CLAUDE_SESSION"); p != "" {
		realClaude(t, p)
	}
	if p := os.Getenv("SMIND_REAL_CODEX_ROLLOUT"); p != "" {
		realCodex(t, p)
	}
	if os.Getenv("SMIND_REAL_CLAUDE_SESSION") == "" && os.Getenv("SMIND_REAL_CODEX_ROLLOUT") == "" {
		t.Skip("no real session given")
	}
}

func realClaude(t *testing.T, path string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var first claudeRec
	var prompt string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var rec claudeRec
		if json.Unmarshal(sc.Bytes(), &rec) != nil || rec.Type != "user" || rec.IsSidechain || rec.IsMeta {
			continue
		}
		if text, ok := claudePrompt(rec.Message.Content); ok {
			first, prompt = rec, text
			break
		}
	}
	if prompt == "" {
		t.Skip("no prompt record")
	}
	ts, _ := time.Parse(time.RFC3339Nano, first.Timestamp)
	// the dir layout: <root>/projects/<dir>/<id>.jsonl; the cwd is recorded in a record, not needed here
	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	var cwd string
	f.Seek(0, 0)
	sc = bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var r struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.Cwd != "" {
			cwd = r.Cwd
			break
		}
	}
	ref := Ref{Provider: "claude-native", Harness: HarnessClaudeCode, SessionID: id, ConfigDir: root, Cwd: cwd, PromptSha256: sha(prompt),
		TurnWindow: TurnWindow{StartedAt: ts, FinishedAt: ts.Add(time.Minute)}}
	rb := New(Config{Roots: func(string, string) []string { return []string{root} }, Timeout: 10 * time.Second})
	t0 := time.Now()
	res := rb.Rebuild(context.Background(), Request{Ref: ref, Requester: RequesterHuman, ExpectedToolCalls: -1})
	t.Logf("claude: cold rebuild %v for %d bytes (probe size %d)", time.Since(t0), res.FileSize, rb.Probe(ref).Size)
	counts := map[int]int{}
	for _, e := range res.Events {
		counts[int(e.Type)]++
	}
	t.Logf("claude: reason %q (%s); %d events; by type %v", res.Reason, res.Detail, len(res.Events), counts)
}

func realCodex(t *testing.T, path string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var prompt string
	var started time.Time
	var turn string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var rec codexRec
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if rec.Type == "event_msg" && rec.Payload.Type == "task_started" && turn == "" {
			turn, started = rec.Payload.TurnID, time.Unix(rec.Payload.StartedAt, 0).UTC()
		}
		if rec.Type == "event_msg" && rec.Payload.Type == "item_completed" && rec.Payload.Item.Type == "UserMessage" && prompt == "" {
			for _, c := range rec.Payload.Item.Content {
				prompt += c.Text
			}
		}
	}
	base := filepath.Base(path)
	i := strings.LastIndex(base, "-")
	_ = i
	id := strings.TrimSuffix(base, ".jsonl")
	if k := strings.Index(id, "rollout-"); k == 0 {
		parts := strings.SplitN(id, "-", 7) // rollout, YYYY, MM, DDThh, mm, ss, <thread id>
		if len(parts) == 7 {
			id = parts[6]
		}
	}
	root := path
	for k := 0; k < 5 && root != "/"; k++ {
		root = filepath.Dir(root)
	}
	ref := Ref{Provider: "codex-native", Harness: HarnessCodex, SessionID: id, CodexHome: root, PromptSha256: sha(prompt),
		TurnWindow: TurnWindow{StartedAt: started, FinishedAt: started.Add(time.Minute)}}
	rb := New(Config{Roots: func(string, string) []string { return []string{root} }, Timeout: 10 * time.Second})
	res := rb.Rebuild(context.Background(), Request{Ref: ref, Requester: RequesterHuman, ExpectedToolCalls: -1})
	t.Logf("codex: reason %q (%s); %d events", res.Reason, res.Detail, len(res.Events))
}
