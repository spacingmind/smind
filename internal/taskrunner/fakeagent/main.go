// Command fakeagent is a minimal scripted ACP agent used only by
// internal/taskrunner's tests, so Runner's GLM path can be exercised
// end-to-end without depending on npx/network access. Unlike
// internal/acp/fakeagent (which exists to prove internal/acp's own
// streaming/fs/permission plumbing in detail), this script only needs to
// prove that Runner wires a real ACP subprocess up correctly and translates
// its updates -- so it skips the release-gating, fs, and permission
// exercises entirely.
//
// The scenario to run is read from a "scenario" file in the session's cwd
// (the task's worktree) rather than an environment variable, since the
// worktree path is the one piece of per-test state naturally available to
// both the test and the spawned agent. Scenario "hang" streams one chunk
// then blocks forever, for proving context cancellation actually stops a
// running turn; "slow" streams five chunks with a real 300ms delay between
// each, for proving a caller observes genuinely incremental delivery rather
// than a reply buffered until the end; anything else (including no file at
// all) runs the default two-chunk scripted reply.
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const sessionID = "fake-session-1"

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

var writeMu sync.Mutex

func writeMessage(msg message) {
	data, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	data = append(data, '\n')

	writeMu.Lock()
	defer writeMu.Unlock()
	os.Stdout.Write(data)
}

func notify(method string, params any) {
	raw, _ := json.Marshal(params)
	writeMessage(message{JSONRPC: "2.0", Method: method, Params: raw})
}

func respond(id json.RawMessage, result any) {
	raw, _ := json.Marshal(result)
	writeMessage(message{JSONRPC: "2.0", ID: id, Result: raw})
}

func main() {
	reader := bufio.NewReaderSize(os.Stdin, 1<<20)
	var sessionCwd string

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			var msg message
			if jsonErr := json.Unmarshal([]byte(line), &msg); jsonErr == nil {
				handle(msg, &sessionCwd)
			}
		}
		if err != nil {
			return
		}
	}
}

func handle(msg message, sessionCwd *string) {
	switch msg.Method {
	case "initialize":
		respond(msg.ID, map[string]any{
			"protocolVersion":   1,
			"agentCapabilities": map[string]any{},
		})
	case "session/new":
		var params struct {
			Cwd string `json:"cwd"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		*sessionCwd = params.Cwd
		respond(msg.ID, map[string]any{"sessionId": sessionID})
	case "session/prompt":
		go runPromptScript(msg, *sessionCwd)
	}
}

func sessionUpdate(text string) {
	notify("session/update", map[string]any{
		"sessionId": sessionID,
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": text},
		},
	})
}

func runPromptScript(promptMsg message, cwd string) {
	scenario := "reply"
	if data, err := os.ReadFile(filepath.Join(cwd, "scenario")); err == nil {
		scenario = strings.TrimSpace(string(data))
	}

	if scenario == "hang" {
		sessionUpdate("before hang")
		time.Sleep(time.Hour)
		return
	}

	if scenario == "slow" {
		// Streams several chunks with a real delay between each, so a
		// caller (e.g. a manual CLI smoke test) can observe genuinely
		// incremental delivery -- not just receipt of the whole reply
		// after the fact -- by timing when each one arrives.
		for i, chunk := range []string{"one ", "two ", "three ", "four ", "five"} {
			if i > 0 {
				time.Sleep(300 * time.Millisecond)
			}
			sessionUpdate(chunk)
		}
		respond(promptMsg.ID, map[string]any{"stopReason": "end_turn"})
		return
	}

	sessionUpdate("Hello, ")
	sessionUpdate("world!")
	respond(promptMsg.ID, map[string]any{"stopReason": "end_turn"})
}
