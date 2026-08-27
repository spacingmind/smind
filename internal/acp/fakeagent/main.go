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
