//go:build !windows

package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCLIEnv switches the test binary into a stand-in `claude` CLI. The
// stand-in is configured by the wrapper script fakeCLI writes (the launcher
// scrubs the environment, so the config is baked into the wrapper):
//
//	SANDBOX_FAKE_ARGS   file the argv is written to
//	SANDBOX_FAKE_INIT   JSON object: the init system message body
//	SANDBOX_FAKE_RESULT JSON object: extra result message fields
//	SANDBOX_FAKE_WRITES JSON object path->content written when the prompt arrives
//	SANDBOX_FAKE_CALLS  JSON list of fakeCall: Bash tool_use + tool_result pairs to stream
//	SANDBOX_FAKE_TEXT   an assistant text message to stream
const fakeCLIEnv = "SANDBOX_FAKE_CLI"

func TestMain(m *testing.M) {
	if os.Getenv(fakeCLIEnv) == "1" {
		runFakeCLI()
		return
	}
	os.Exit(m.Run())
}

func runFakeCLI() {
	if p := os.Getenv("SANDBOX_FAKE_ARGS"); p != "" {
		_ = os.WriteFile(p, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(os.Stdout, "%s\n", b)
	}
	if !in.Scan() {
		os.Exit(1)
	}
	var req struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(in.Bytes(), &req)
	out(map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "success", "request_id": req.RequestID, "response": map[string]any{"success": true}}})
	if !in.Scan() { // the prompt; EOF means the client closed (argv-only tests)
		return
	}
	var writes map[string]string
	_ = json.Unmarshal([]byte(os.Getenv("SANDBOX_FAKE_WRITES")), &writes)
	for p, c := range writes {
		_ = os.WriteFile(p, []byte(c), 0o600)
	}
	calls := []fakeCall{}
	_ = json.Unmarshal([]byte(os.Getenv("SANDBOX_FAKE_CALLS")), &calls)
	init := map[string]any{"type": "system", "subtype": "init", "session_id": "s1"}
	var body map[string]any
	if json.Unmarshal([]byte(os.Getenv("SANDBOX_FAKE_INIT")), &body) == nil {
		for k, v := range body {
			init[k] = v
		}
	}
	out(init)
	for _, c := range calls {
		out(map[string]any{"type": "assistant", "session_id": "s1", "message": map[string]any{"role": "assistant", "model": "m",
			"content": []any{map[string]any{"type": "tool_use", "id": c.ID, "name": "Bash", "input": map[string]any{"command": c.Command}}}}})
		out(map[string]any{"type": "user", "session_id": "s1", "message": map[string]any{"role": "user",
			"content": []any{map[string]any{"type": "tool_result", "tool_use_id": c.ID, "content": c.Result, "is_error": c.IsError}}}})
	}
	if t := os.Getenv("SANDBOX_FAKE_TEXT"); t != "" {
		out(map[string]any{"type": "assistant", "session_id": "s1", "message": map[string]any{"role": "assistant", "model": "m",
			"content": []any{map[string]any{"type": "text", "text": t}}}})
	}
	res := map[string]any{"type": "result", "subtype": "success", "session_id": "s1", "result": "ok", "num_turns": 2}
	var extra map[string]any
	if json.Unmarshal([]byte(os.Getenv("SANDBOX_FAKE_RESULT")), &extra) == nil {
		for k, v := range extra {
			res[k] = v
		}
	}
	out(res)
	for in.Scan() {
	}
}

// fakeCLI writes an executable wrapper in dir that runs this test binary as
// the stand-in CLI with env (NAME=VALUE) set, and returns its path.
func fakeCLI(t *testing.T, dir string, env map[string]string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n" + fakeCLIEnv + "=1")
	for k, v := range env {
		b.WriteString(" " + k + "=" + shQuote(v))
	}
	b.WriteString(" exec " + shQuote(self) + " \"$@\"\n")
	p := filepath.Join(dir, "fake-claude")
	if err := os.WriteFile(p, []byte(b.String()), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeCall is one Bash call the stand-in streams.
type fakeCall struct {
	ID, Command, Result string
	IsError             bool
}
