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
// for proving Runner's PermissionDecider wiring end to end; "permission-edit"
// is the same idea for an edit-kind request, with a synchronization chunk
// and a real delay before the request goes out so a test can reliably
// change the run's permission settings mid-run before it arrives; anything
// else (including no file at all) runs the default two-chunk scripted reply.
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

// capsMode is set from os.Args[1] (defaulting to "" -- no resume
// capability at all) so a test can choose what this agent's initialize
// response advertises: "loadSession" (session/load supported), "resume"
// (sessionCapabilities.resume supported), "both" (both, to prove
// newOrResumeACPSession's loadSession-first priority), or unset/anything
// else (neither -- the "this agent can't resume at all" fallback path).
var capsMode string

// modesMode is set from a "modes:<kind>" argument: "modes:session"
// advertises ACP SessionModeState in session/new (GLM-shaped
// default/accept_edits/bypass_permissions), "modes:config" advertises the
// same modes as a category-"mode" select config option instead; unset
// advertises no modes at all. session/set_mode (and a set_config_option
// on the "mode" config id) records the applied mode to a "session-mode"
// file in the session's cwd, so tests can observe it.
var modesMode string

var fakeModeIDs = []string{"default", "accept_edits", "bypass_permissions"}

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
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "modes:") {
			modesMode = strings.TrimPrefix(a, "modes:")
		} else {
			capsMode = a
		}
	}

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
		caps := map[string]any{}
		switch capsMode {
		case "loadSession":
			caps["loadSession"] = true
		case "resume":
			caps["sessionCapabilities"] = map[string]any{"resume": map[string]any{}}
		case "both":
			caps["loadSession"] = true
			caps["sessionCapabilities"] = map[string]any{"resume": map[string]any{}}
		}
		respond(msg.ID, map[string]any{
			"protocolVersion":   1,
			"agentCapabilities": caps,
		})
	case msg.Method == "session/new":
		var params struct {
			Cwd string `json:"cwd"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		*sessionCwd = params.Cwd
		recordSessionInitMethod(params.Cwd, "session/new")
		result := map[string]any{
			"sessionId":     sessionID,
			"configOptions": defaultConfigOptions(),
		}
		if modesMode == "session" {
			available := make([]map[string]any, len(fakeModeIDs))
			for i, id := range fakeModeIDs {
				available[i] = map[string]any{"id": id, "name": "Mode " + id}
			}
			result["modes"] = map[string]any{"currentModeId": "default", "availableModes": available}
		}
		respond(msg.ID, result)
	case msg.Method == "session/set_mode":
		var params struct {
			ModeID string `json:"modeId"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		if !knownFakeMode(params.ModeID) {
			writeMessage(message{JSONRPC: "2.0", ID: msg.ID, Error: &rpcError{Code: -32602, Message: "unknown mode: " + params.ModeID}})
			return
		}
		recordSessionMode(*sessionCwd, "set_mode:"+params.ModeID)
		respond(msg.ID, map[string]any{})
	case msg.Method == "session/load" || msg.Method == "session/resume":
		handleResumeSession(msg, sessionCwd)
	case msg.Method == "session/set_config_option":
		handleSetConfigOption(msg, *sessionCwd)
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

// defaultConfigOptions is a select-kind option with its own enumerated
// choices (mirrors GLM's real thinking-level tiers), so Runner/Registry/
// wsapi tests can drive a real config-option round trip end to end rather
// than asserting only against the non-ACP "not supported" path.
func defaultConfigOptions() []map[string]any {
	opts := []map[string]any{{
		"configId":     "thinking-level",
		"name":         "Thinking Level",
		"type":         "select",
		"currentValue": "medium",
		"options": []map[string]any{
			{"value": "minimal", "name": "Minimal"},
			{"value": "low", "name": "Low"},
			{"value": "medium", "name": "Medium"},
			{"value": "high", "name": "High"},
		},
	}}
	if modesMode == "config" {
		choices := make([]map[string]any, len(fakeModeIDs))
		for i, id := range fakeModeIDs {
			choices[i] = map[string]any{"value": id, "name": "Mode " + id}
		}
		opts = append(opts, map[string]any{
			"configId":     "mode",
			"name":         "Mode",
			"category":     "mode",
			"type":         "select",
			"currentValue": "default",
			"options":      choices,
		})
	}
	return opts
}

func knownFakeMode(id string) bool {
	for _, m := range fakeModeIDs {
		if m == id {
			return true
		}
	}
	return false
}

// recordSessionMode appends how a mode was applied to "session-mode" in
// cwd (one line per call).
func recordSessionMode(cwd, line string) {
	if cwd == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(cwd, "session-mode"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// recordSessionInitMethod writes the method that started/resumed this
// session to a "session-init-method" file in cwd, overwriting any prior
// value -- the only way a test can observe which of session/new,
// session/load, or session/resume Runner actually called (the wire
// protocol result looks the same either way).
func recordSessionInitMethod(cwd, method string) {
	if cwd == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(cwd, "session-init-method"), []byte(method), 0o644)
}

// handleResumeSession answers session/load and session/resume: it
// succeeds, with the same configOptions shape session/new returns, only
// for the one session id this agent ever hands out (sessionID) -- any
// other id (simulating a stale/unknown stored session) gets an RPC error,
// so a test can drive Runner's stale-session fallback path.
func handleResumeSession(msg message, sessionCwd *string) {
	var params struct {
		SessionID string `json:"sessionId"`
		Cwd       string `json:"cwd"`
	}
	_ = json.Unmarshal(msg.Params, &params)
	if params.SessionID != sessionID {
		writeMessage(message{JSONRPC: "2.0", ID: msg.ID, Error: &rpcError{
			Code: -32000, Message: "unknown session: " + params.SessionID,
		}})
		return
	}
	*sessionCwd = params.Cwd
	recordSessionInitMethod(params.Cwd, msg.Method)
	respond(msg.ID, map[string]any{"configOptions": defaultConfigOptions()})
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

	if scenario == "permission-edit" {
		// Unlike "permission" above, this scenario needs genuine
		// happens-before ordering, not a race: a test proving a mid-run
		// permission-settings switch (internal/runs.Registry.SetAutoAccept)
		// lands *before* this scenario's edit-kind session/request_permission
		// call needs a reliable signal to synchronize on first. The
		// preceding sessionUpdate + sleep gives the test time to observe
		// the "ready" chunk (causally ordered before this, via the
		// notification-forwarding pipeline) and make the switch
		// before the request actually goes out.
		sessionUpdate("ready")
		time.Sleep(300 * time.Millisecond)
		permResp := call("session/request_permission", map[string]any{
			"sessionId": sessionID,
			"toolCall": map[string]any{
				"toolCallId": "tc-edit-1",
				"kind":       "edit",
				"locations":  []map[string]any{{"path": "a.go"}},
			},
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

	if scenario == "mode-update" {
		// The agent switches its own mode mid-turn (ACP's
		// current_mode_update -- e.g. a plan-mode agent leaving plan) and
		// then asks for permission, so a test can prove the client's idea
		// of the current mode follows the agent's notification.
		notify("session/update", map[string]any{
			"sessionId": sessionID,
			"update":    map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": "accept_edits"},
		})
		time.Sleep(100 * time.Millisecond)
		call("session/request_permission", map[string]any{
			"sessionId": sessionID,
			"toolCall":  map[string]any{"toolCallId": "tc-mode", "title": "After mode update"},
			"options": []map[string]any{
				{"optionId": "allow-1", "name": "Allow", "kind": "allow_once"},
				{"optionId": "deny-1", "name": "Deny", "kind": "reject_once"},
			},
		})
		sessionUpdate("done")
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

	if scenario == "structured" {
		// Exercises every new ACP session/update kind
		// docs/decisions/0008-structured-run-events.md added support for:
		// a thought chunk, a user-message chunk, and a tool call reported
		// first as "tool_call" (running) then completed via a
		// "tool_call_update" -- proving Runner's acpEvent translates each
		// into its own taskrunner.EventType. Also sends a "plan" update, a
		// kind acpEvent doesn't otherwise recognize, proving it surfaces as
		// EventTypeRaw rather than being dropped
		// (docs/decisions/0010-preserve-unknown-acp-event-kinds.md).
		notify("session/update", map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "plan",
				"entries":       []any{map[string]any{"content": "write the tests", "status": "pending"}},
			},
		})
		notify("session/update", map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "agent_thought_chunk",
				"content":       map[string]any{"type": "text", "text": "thinking it over"},
			},
		})
		notify("session/update", map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "user_message_chunk",
				"content":       map[string]any{"type": "text", "text": "a synthesized user turn"},
			},
		})
		notify("session/update", map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    "tc-1",
				"title":         "Run tests",
				"kind":          "execute",
				"status":        "in_progress",
				"rawInput":      map[string]any{"command": "go test ./..."},
			},
		})
		notify("session/update", map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "tool_call_update",
				"toolCallId":    "tc-1",
				"status":        "completed",
				"content": []any{
					map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "ok"}},
				},
			},
		})
		sessionUpdate("done")
		respond(promptMsg.ID, map[string]any{"stopReason": "end_turn"})
		return
	}

	sessionUpdate("Hello, ")
	sessionUpdate("world!")
	respond(promptMsg.ID, map[string]any{"stopReason": "end_turn"})
}
