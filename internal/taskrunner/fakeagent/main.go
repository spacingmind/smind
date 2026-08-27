// Command fakeagent is a minimal scripted ACP agent used only by
// internal/taskrunner's tests, so Runner's GLM path can be exercised
// end-to-end without depending on npx/network access. Unlike
// internal/acp/fakeagent (which exists to prove internal/acp's own
// streaming/fs/permission plumbing in detail), this script only needs to
// prove that Runner wires a real ACP subprocess up correctly and translates
// its updates -- so it skips the release-gating and fs exercises entirely,
// but does include a permission scenario (see runPromptScript's
// "permission" case) since proving Runner's PermissionDecider wiring needs
// a real session/request_permission round trip.
//
// The scenario to run is read from a "scenario" file in the session's cwd
// (the task's worktree) rather than an environment variable, since the
// worktree path is the one piece of per-test state naturally available to
// both the test and the spawned agent. Scenario "hang" streams one chunk
// then blocks forever, for proving context cancellation actually stops a
// running turn; "slow" streams five chunks with a real 300ms delay between
// each, for proving a caller observes genuinely incremental delivery rather
// than a reply buffered until the end; "permission" issues a real
// session/request_permission call and streams back which option was chosen,
// for proving Runner's PermissionDecider wiring end to end; anything else
// (including no file at all) runs the default two-chunk scripted reply.
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const sessionID = "fake-session-1"

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var (
	writeMu sync.Mutex
	nextID  int64

	pendingMu sync.Mutex
	pending   = map[int64]chan message{}
)

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

// call sends a request from the agent to the client (e.g.
// session/request_permission) and blocks for the matching response.
func call(method string, params any) message {
	raw, _ := json.Marshal(params)
	id := atomic.AddInt64(&nextID, 1)
	idJSON, _ := json.Marshal(id)

	ch := make(chan message, 1)
	pendingMu.Lock()
	pending[id] = ch
	pendingMu.Unlock()

	writeMessage(message{JSONRPC: "2.0", ID: idJSON, Method: method, Params: raw})
	return <-ch
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
	switch {
	case msg.Method == "initialize":
		respond(msg.ID, map[string]any{
			"protocolVersion":   1,
			"agentCapabilities": map[string]any{},
		})
	case msg.Method == "session/new":
		var params struct {
			Cwd string `json:"cwd"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		*sessionCwd = params.Cwd
		respond(msg.ID, map[string]any{"sessionId": sessionID})
	case msg.Method == "session/prompt":
		go runPromptScript(msg, *sessionCwd)
	case msg.Method == "" && len(msg.ID) > 0:
		// A response to a call() this agent itself sent (e.g. the
		// session/request_permission round trip in runPromptScript).
		var id int64
		if err := json.Unmarshal(msg.ID, &id); err == nil {
			pendingMu.Lock()
			ch, ok := pending[id]
			delete(pending, id)
			pendingMu.Unlock()
			if ok {
				ch <- msg
			}
		}
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

	if scenario == "permission" {
		// Deliberately issues the session/request_permission call as the
		// very first thing, with no preceding session/update: a preceding
		// text chunk would race the permission request in any test that
		// observes both through internal/runs.Registry, since ACP
		// dispatches an inbound request (session/request_permission) on
		// its own goroutine, decoupled from the notification-forwarding
		// pipeline a session/update chunk goes through (see acp/rpc.go's
		// handleLine) -- there's no causal ordering between them on the
		// wire, only a schedule-dependent one. The resolved event and
		// this scenario's final chunk, by contrast, *are* causally
		// ordered (this process cannot call sessionUpdate below until it
		// has received the permission response, which the client only
		// sends after recording the resolution), so that ordering is safe
		// for tests to assert on.
		permResp := call("session/request_permission", map[string]any{
			"sessionId": sessionID,
			"toolCall":  map[string]any{"toolCallId": "tc-1", "title": "Run a risky command"},
			"options": []map[string]any{
				{"optionId": "allow-1", "name": "Allow", "kind": "allow_once"},
				{"optionId": "deny-1", "name": "Deny", "kind": "reject_once"},
			},
		})
		optionID := ""
		if permResp.Error == nil {
			var result struct {
				Outcome struct {
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			}
			_ = json.Unmarshal(permResp.Result, &result)
			optionID = result.Outcome.OptionID
		}
		sessionUpdate("chose:" + optionID)
		respond(promptMsg.ID, map[string]any{"stopReason": "end_turn"})
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
