// Command fakeagent is a minimal, scripted ACP agent used only by
// internal/acp's tests. It speaks just enough real JSON-RPC-over-stdio ACP
// to drive one full initialize -> session/new -> session/prompt turn,
// including an fs/read_text_file callback and a session/request_permission
// callback, so most of internal/acp's tests run offline without depending
// on npx/network access.
//
// Its prompt script waits for a "_test/release" notification from the
// client before continuing past the first streamed chunk; tests use that
// gate to prove updates are forwarded incrementally rather than buffered.
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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

	releaseOnce sync.Once
	releaseCh   = make(chan struct{})

	// mcpCaps is scripted via an "mcp=stdio,http,sse" os.Args entry (any
	// subset; "mcp=none" or absent advertises no mcp capability key at
	// all). "mcp=" emits the v1 shape (mcpCapabilities booleans; stdio is
	// implicitly mandatory in v1 so a stdio entry is ignored), "mcp2="
	// emits the v2 object shape ("mcp":{"stdio":{},...}; an empty list
	// emits "mcp":{}).
	mcpCaps      []string
	mcpCaps2     bool
	mcpCaps2List []string

	// lastMcpServers holds the raw mcpServers JSON the most recent
	// session/new|load|resume carried, served back on
	// "_test/last_mcp_servers" so tests can assert the exact wire shape.
	lastMcpServers = []byte("[]")
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

// call sends a request from the agent to the client (e.g.
// fs/read_text_file) and blocks for the matching response.
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

func respond(id json.RawMessage, result any) {
	raw, _ := json.Marshal(result)
	writeMessage(message{JSONRPC: "2.0", ID: id, Result: raw})
}

func main() {
	for _, a := range os.Args[1:] {
		if v, ok := strings.CutPrefix(a, "mcp2="); ok {
			mcpCaps2 = true
			if v != "" && v != "none" {
				mcpCaps2List = strings.Split(v, ",")
			}
		} else if v, ok := strings.CutPrefix(a, "mcp="); ok && v != "none" {
			mcpCaps = strings.Split(v, ",")
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
		caps := map[string]any{
			"loadSession":         true,
			"sessionCapabilities": map[string]any{"resume": map[string]any{}},
		}
		if mcpCaps2 {
			mcp := map[string]any{}
			for _, c := range mcpCaps2List {
				mcp[c] = map[string]any{}
			}
			caps["mcp"] = mcp
		} else if len(mcpCaps) > 0 {
			mcpCapsObj := map[string]any{}
			for _, c := range mcpCaps {
				switch c {
				case "http", "sse":
					mcpCapsObj[c] = true
				}
			}
			if len(mcpCapsObj) > 0 {
				caps["mcpCapabilities"] = mcpCapsObj
			}
		}
		respond(msg.ID, map[string]any{
			"protocolVersion":   1,
			"agentCapabilities": caps,
		})
	case msg.Method == "session/new":
		var params struct {
			Cwd        string          `json:"cwd"`
			McpServers json.RawMessage `json:"mcpServers"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		recordMcpServers(params.McpServers)
		*sessionCwd = params.Cwd
		// Scripts a select-kind option (with its own enumerated choices,
		// like GLM's real thinking-level tiers) and a boolean-kind option
		// (no choices at all) -- proving Client.NewSession decodes both
		// shapes, including a select's Options list, intact and in order.
		respond(msg.ID, map[string]any{
			"sessionId": sessionID,
			// Mirrors GLM's user-reported advertised modes (ADR-0019).
			"modes": map[string]any{
				"currentModeId": "default",
				"availableModes": []map[string]any{
					{"id": "default", "name": "Default"},
					{"id": "accept_edits", "name": "Accept Edits", "description": "Edits run without prompting"},
					{"id": "bypass_permissions", "name": "Bypass Permissions"},
				},
			},
			"configOptions": []map[string]any{
				{
					"configId":     "thinking-level",
					"name":         "Thinking Level",
					"description":  "How much the model reasons before responding",
					"category":     "thought_level",
					"type":         "select",
					"currentValue": "medium",
					"options": []map[string]any{
						{"value": "minimal", "name": "Minimal"},
						{"value": "low", "name": "Low"},
						{"value": "medium", "name": "Medium"},
						{"value": "high", "name": "High", "description": "Reasons the longest"},
					},
				},
				{
					"configId":     "web-search",
					"name":         "Web Search",
					"type":         "boolean",
					"currentValue": true,
				},
			},
		})
	case msg.Method == "session/load" || msg.Method == "session/resume":
		var params struct {
			SessionID  string          `json:"sessionId"`
			Cwd        string          `json:"cwd"`
			McpServers json.RawMessage `json:"mcpServers"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		if params.SessionID != sessionID {
			writeMessage(message{JSONRPC: "2.0", ID: msg.ID, Error: &rpcError{
				Code: -32000, Message: "unknown session: " + params.SessionID,
			}})
			return
		}
		recordMcpServers(params.McpServers)
		*sessionCwd = params.Cwd
		respond(msg.ID, map[string]any{
			"configOptions": []map[string]any{{
				"configId":     "thinking-level",
				"name":         "Thinking Level",
				"type":         "select",
				"currentValue": "medium",
			}},
		})
	case msg.Method == "session/set_config_option":
		handleSetConfigOption(msg)
	case msg.Method == "session/set_mode":
		var params struct {
			SessionID string `json:"sessionId"`
			ModeID    string `json:"modeId"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		switch params.ModeID {
		case "default", "accept_edits", "bypass_permissions":
			respond(msg.ID, map[string]any{})
		default:
			writeMessage(message{JSONRPC: "2.0", ID: msg.ID, Error: &rpcError{Code: -32602, Message: "unknown mode: " + params.ModeID}})
		}
	case msg.Method == "session/prompt":
		go runPromptScript(msg, *sessionCwd)
	case msg.Method == "_test/last_mcp_servers":
		respond(msg.ID, map[string]any{"mcpServers": json.RawMessage(lastMcpServers)})
	case msg.Method == "_test/release":
		releaseOnce.Do(func() { close(releaseCh) })
	case msg.Method == "" && len(msg.ID) > 0:
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

func recordMcpServers(raw json.RawMessage) {
	if len(raw) == 0 {
		raw = json.RawMessage("[]")
	}
	lastMcpServers = append([]byte(nil), raw...)
}

func sessionUpdate(update map[string]any) {
	notify("session/update", map[string]any{
		"sessionId": sessionID,
		"update":    update,
	})
}

func textChunk(text string) map[string]any {
	return map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]any{"type": "text", "text": text},
	}
}

func runPromptScript(promptMsg message, cwd string) {
	sessionUpdate(textChunk("Hello, "))

	<-releaseCh

	readResp := call("fs/read_text_file", map[string]any{
		"sessionId": sessionID,
		"path":      cwd + "/hello.txt",
	})
	fileContent := ""
	if readResp.Error == nil {
		var result struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(readResp.Result, &result)
		fileContent = result.Content
	}
	sessionUpdate(textChunk("read:" + fileContent))

	permResp := call("session/request_permission", map[string]any{
		"sessionId": sessionID,
		"toolCall":  map[string]any{"toolCallId": "tc-1"},
		"options": []map[string]any{
			{"optionId": "allow-1", "name": "Allow", "kind": "allow_once"},
			{"optionId": "reject-1", "name": "Reject", "kind": "reject_once"},
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
	sessionUpdate(textChunk("permission:" + optionID))

	sessionUpdate(textChunk("world!"))

	respond(promptMsg.ID, map[string]any{"stopReason": "end_turn"})
}
