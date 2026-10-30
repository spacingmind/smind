package taskrunner

// Shared fixtures for the OpenCode real-agent conformance tests
// (opencode_real_conformance_test.go): a fake OpenAI-compatible HTTP
// backend on 127.0.0.1 and a fully isolated OpenCode HOME/config, so the
// installed `opencode acp` binary can be driven through Runner with zero
// network access and zero cost. Every test skips itself when `opencode`
// is not on PATH.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// opencodeRealProvider is the custom provider id the conformance tests
// register: the real `opencode acp` command, with its environment pointing
// at the isolated dirs and the fake backend.
const opencodeRealProvider Provider = "opencode-real"

// fakeOpenAIKey is the only API key ever handed to the agent. It is
// deliberately not a valid key for anything: every Authorization header
// the fake backend sees must carry it, proving no real credential or
// built-in opencode endpoint was used.
const fakeOpenAIKey = "SENTINEL-NOT-A-KEY"

// requireOpenCodeOnPath skips the test unless the real opencode CLI is
// installed (npm i -g opencode-ai; the conformance run used 1.18.33).
func requireOpenCodeOnPath(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skipf("opencode is not on PATH (%v); skipping the real-agent conformance test", err)
	}
}

// openCodeRealCtx is the per-test context: 90s, generous for a node
// process boot plus one model round trip.
func openCodeRealCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 90*time.Second)
}

// fakeChatCompletion records one POST /chat/completions the fake backend
// served: what the last user message said, and whether the conversation
// history the agent sent already contained a tool result or an assistant
// reply (the resume test's proof that session/load carried context over).
type fakeChatCompletion struct {
	lastUser     string
	hasToolMsg   bool
	hasAssistant bool
	toolNames    []string
}

// fakeOpenAI is a local OpenAI-compatible SSE backend (a Go port of the
// spike's fake.mjs, /tmp/steward4/spike-acp/opencode/fake.mjs): GET
// .../models lists fake-model, POST .../chat/completions streams
// "FAKE reply done." -- or, when the last user message contains RUNTOOL
// and tools are offered with no tool result yet, one bash tool call; or,
// for SLOW, 40 chunks of "tick " at 250ms until the client goes away.
type fakeOpenAI struct {
	srv *httptest.Server

	mu    sync.Mutex
	auths []string
	chats []fakeChatCompletion
}

func (f *fakeOpenAI) URL() string { return f.srv.URL }

// chatCalls snapshots the served chat completions, oldest first.
func (f *fakeOpenAI) chatCalls() []fakeChatCompletion {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeChatCompletion(nil), f.chats...)
}

// authHeaders snapshots every Authorization header value seen ("" when a
// request carried none).
func (f *fakeOpenAI) authHeaders() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths...)
}

// assertOnlySentinelAuth fails the test if any request the backend served
// carried an Authorization header other than the sentinel key.
func (f *fakeOpenAI) assertOnlySentinelAuth(t *testing.T) {
	t.Helper()
	for _, a := range f.authHeaders() {
		if a != "" && a != "Bearer "+fakeOpenAIKey {
			t.Fatalf("fake backend saw Authorization %q, want only %q", a, "Bearer "+fakeOpenAIKey)
		}
	}
}

func fakeOpenAIServer(t *testing.T) *fakeOpenAI {
	t.Helper()
	f := &fakeOpenAI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.mu.Unlock()

		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"fake-model","object":"model"}]}`)
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var payload struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		call := fakeChatCompletion{}
		for _, m := range payload.Messages {
			switch m.Role {
			case "tool":
				call.hasToolMsg = true
			case "assistant":
				call.hasAssistant = true
			case "user":
				call.lastUser = string(m.Content)
			}
		}
		for _, tool := range payload.Tools {
			call.toolNames = append(call.toolNames, tool.Function.Name)
		}
		f.mu.Lock()
		f.chats = append(f.chats, call)
		f.mu.Unlock()

		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		chunk := func(delta map[string]any, finish string, usage bool) {
			obj := map[string]any{"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "fake-model"}
			choice := map[string]any{"index": 0, "delta": delta}
			if finish != "" {
				choice["finish_reason"] = finish
			}
			obj["choices"] = []any{choice}
			if usage {
				obj["usage"] = map[string]int{"prompt_tokens": 5, "completion_tokens": 5, "total_tokens": 10}
			}
			b, _ := json.Marshal(obj)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		done := func() {
			fmt.Fprint(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}

		toolsOffered := len(payload.Tools) > 0
		wantTool := toolsOffered && !call.hasToolMsg && strings.Contains(call.lastUser, "RUNTOOL")
		switch {
		case wantTool:
			chunk(map[string]any{
				"role": "assistant", "content": nil,
				"tool_calls": []any{map[string]any{
					"index": 0, "id": "call_1", "type": "function",
					"function": map[string]any{"name": "bash", "arguments": ""},
				}},
			}, "", false)
			args, _ := json.Marshal(map[string]string{"command": "echo hello-from-tool", "description": "echo test"})
			chunk(map[string]any{
				"tool_calls": []any{map[string]any{
					"index": 0, "function": map[string]any{"arguments": string(args)},
				}},
			}, "", false)
			chunk(map[string]any{}, "tool_calls", true)
			done()
		case strings.Contains(call.lastUser, "SLOW"):
			chunk(map[string]any{"role": "assistant", "content": "FAKE "}, "", false)
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for i := 0; i < 40; i++ {
				select {
				case <-ticker.C:
					chunk(map[string]any{"content": "tick "}, "", false)
				case <-r.Context().Done():
					return
				}
			}
			chunk(map[string]any{}, "stop", true)
			done()
		default:
			chunk(map[string]any{"role": "assistant", "content": "FAKE "}, "", false)
			for _, word := range []string{"reply ", "done."} {
				chunk(map[string]any{"content": word}, "", false)
			}
			chunk(map[string]any{}, "stop", true)
			done()
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// opencodeIsolation returns NAME=VALUE env entries that point the agent's
// HOME and every XDG dir at a throwaway directory, and writes the
// opencode.json configuring the "fake" provider (apiKey sentinel,
// baseURL <baseURL>/v1). Deliberately no "permission" block: the ask
// policy must come from the shipped OPENCODE_PERMISSION env (see
// opencodeRealRunner), which is what the conformance test exercises.
func opencodeIsolation(t *testing.T, baseURL string) []string {
	t.Helper()
	home := t.TempDir()
	cfg := `{
 "$schema": "https://opencode.ai/config.json",
 "model": "fake/fake-model",
 "small_model": "fake/fake-model",
 "autoupdate": false,
 "share": "disabled",
 "provider": {
  "fake": {
   "npm": "@ai-sdk/openai-compatible",
   "name": "Fake",
   "options": {
    "baseURL": "` + baseURL + `/v1",
    "apiKey": "` + fakeOpenAIKey + `"
   },
   "models": {
    "fake-model": {
     "name": "Fake Model",
     "tool_call": true
    }
   }
  }
 }
}`
	path := filepath.Join(home, "opencode.json")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write opencode.json: %v", err)
	}
	return []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"OPENCODE_CONFIG=" + path,
		"OPENCODE_DISABLE_AUTOUPDATE=1",
	}
}

// opencodeRealRunner registers opencodeRealProvider over the real
// `opencode acp` command with envEntries as its environment, plus the
// builtin opencode spec's own OPENCODE_PERMISSION value (the shipped ask
// policy: `{"bash":"ask","edit":"ask"}`) so the test runs exactly what a
// stock smind install would spawn.
func opencodeRealRunner(t *testing.T, wm *workspace.Manager, envEntries []string) *Runner {
	t.Helper()
	env := map[string]string{}
	for _, e := range envEntries {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	builtin, ok := providers.New().Get(providers.IDOpenCode)
	if !ok || builtin.Env["OPENCODE_PERMISSION"] == "" {
		t.Fatal("builtin opencode spec carries no OPENCODE_PERMISSION env")
	}
	env["OPENCODE_PERMISSION"] = builtin.Env["OPENCODE_PERMISSION"]

	r, _ := customRunner(t, wm, providers.Spec{
		ID: string(opencodeRealProvider), Label: "OpenCode (real)",
		Command: []string{"opencode", "acp"}, Env: env,
	})
	return r
}

// runOpenCodeRealPrompt drives one RunPrompt turn to completion and
// returns its events and error.
func runOpenCodeRealPrompt(ctx context.Context, r *Runner, task store.Task, prompt string, decider PermissionDecider) ([]Event, error) {
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, opencodeRealProvider, prompt, decider, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	return got, <-errCh
}
