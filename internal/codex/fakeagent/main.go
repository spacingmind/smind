// Command fakeagent is a minimal, scripted `codex app-server` speaker used
// by internal/codex's own tests and (mirroring
// internal/taskrunner/fakeagent's exact role for GLM/Kimi) by
// internal/taskrunner's Codex-native tests, so both can be exercised
// end-to-end without depending on a real `codex` binary/network access. It
// speaks just enough of the real app-server dialect -- no "jsonrpc" field,
// an initialize/initialized handshake, thread/start, turn/start
// acknowledged immediately then completed asynchronously via a later
// turn/completed notification, item/agentMessage/delta streaming -- to
// drive one full turn.
//
// The scenario to run is read from a "scenario" file in the thread's cwd
// (the task's worktree), same mechanism and reasoning as
// internal/taskrunner/fakeagent: the worktree path is the one piece of
// per-test state naturally available to both the test and the spawned
// agent. Scenario "permission" issues a real
// item/commandExecution/requestApproval call (which also doubles as this
// package's ordering-proof gate: the call blocks until the test responds,
// exactly like the release-gate internal/acp/fakeagent uses a dedicated
// notification for) and streams back which decision was chosen; anything
// else (including no file at all) runs the default two-delta scripted
// reply, matching internal/taskrunner/fakeagent's default shape so
// GLM/Kimi/Codex-native tests can share the same event-count assertions.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	threadID = "fake-thread-1"
	turnID   = "fake-turn-1"
)

// threadStateMu guards loadedThreads/archivedThreads, this fake agent's
// bookkeeping for the thread/loaded/list, thread/resume,
// thread/archive, thread/unarchive round trip a resume test drives --
// separate from writeMu/pendingMu, which guard the wire transport itself.
var (
	threadStateMu   sync.Mutex
	loadedThreads   = map[string]bool{}
	archivedThreads = map[string]bool{}
)

type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
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
	writeMessage(message{Method: method, Params: raw})
}

func respond(id json.RawMessage, result any) {
	raw, _ := json.Marshal(result)
	writeMessage(message{ID: id, Result: raw})
}

// call sends a request from the agent to the client (e.g. an approval
// request) and blocks for the matching response.
func call(method string, params any) message {
	raw, _ := json.Marshal(params)
	id := atomic.AddInt64(&nextID, 1)
	idJSON, _ := json.Marshal(id)

	ch := make(chan message, 1)
	pendingMu.Lock()
	pending[id] = ch
	pendingMu.Unlock()

	writeMessage(message{ID: idJSON, Method: method, Params: raw})
	return <-ch
}

// noModelList (the "nomodellist" argument) makes model/list answer
// method-not-found, like an older app-server.
var noModelList bool

// recordThreadModel writes the `model` a thread/start or thread/resume
// carried to a "thread-model" file in cwd ("" when absent), so tests can
// assert the field was (not) sent.
func recordThreadModel(cwd, model string) {
	if cwd == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(cwd, "thread-model"), []byte(model), 0o644)
}

// recordThreadInstructions writes the `developerInstructions` a thread/start
// or thread/resume carried to a "thread-instructions" file in cwd ("" when
// absent).
func recordThreadInstructions(cwd, text string) {
	if cwd == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(cwd, "thread-instructions"), []byte(text), 0o644)
}

func main() {
	for _, a := range os.Args[1:] {
		if a == "nomodellist" {
			noModelList = true
		}
	}
	reader := bufio.NewReaderSize(os.Stdin, 1<<20)
	var threadCwd string

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			var msg message
			if jsonErr := json.Unmarshal([]byte(line), &msg); jsonErr == nil {
				handle(msg, &threadCwd)
			}
		}
		if err != nil {
			return
		}
	}
}

func handle(msg message, threadCwd *string) {
	switch {
	case msg.Method == "initialize":
		respond(msg.ID, map[string]any{})
	case msg.Method == "initialized":
		// Notification, no response.
	case msg.Method == "thread/start":
		var params struct {
			Cwd            string `json:"cwd"`
			ApprovalPolicy string `json:"approvalPolicy"`
			Sandbox        string `json:"sandbox"`
			Model          string `json:"model"`

			DeveloperInstructions string `json:"developerInstructions"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		*threadCwd = params.Cwd
		threadStateMu.Lock()
		loadedThreads[threadID] = true
		threadStateMu.Unlock()
		recordThreadInitMethod(params.Cwd, "thread/start")
		recordThreadPolicy(params.Cwd, params.ApprovalPolicy, params.Sandbox)
		recordThreadModel(params.Cwd, params.Model)
		recordThreadInstructions(params.Cwd, params.DeveloperInstructions)
		respond(msg.ID, map[string]any{"thread": map[string]any{"id": threadID}})
	case msg.Method == "thread/loaded/list":
		threadStateMu.Lock()
		ids := make([]string, 0, len(loadedThreads))
		for id := range loadedThreads {
			ids = append(ids, id)
		}
		threadStateMu.Unlock()
		respond(msg.ID, map[string]any{"data": ids, "nextCursor": nil})
	case msg.Method == "thread/archive":
		var params struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		threadStateMu.Lock()
		archivedThreads[params.ThreadID] = true
		delete(loadedThreads, params.ThreadID)
		threadStateMu.Unlock()
		respond(msg.ID, map[string]any{"thread": map[string]any{"id": params.ThreadID}})
	case msg.Method == "thread/unarchive":
		var params struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		threadStateMu.Lock()
		wasArchived := archivedThreads[params.ThreadID]
		delete(archivedThreads, params.ThreadID)
		threadStateMu.Unlock()
		if !wasArchived {
			writeMessage(message{ID: msg.ID, Error: &rpcError{
				Code: -32600, Message: fmt.Sprintf("no archived rollout found for thread id %s", params.ThreadID),
			}})
			return
		}
		respond(msg.ID, map[string]any{"thread": map[string]any{"id": params.ThreadID}})
	case msg.Method == "thread/resume":
		var params struct {
			ThreadID       string `json:"threadId"`
			Cwd            string `json:"cwd"`
			ApprovalPolicy string `json:"approvalPolicy"`
			Sandbox        string `json:"sandbox"`
			Model          string `json:"model"`

			DeveloperInstructions string `json:"developerInstructions"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		recordThreadPolicy(params.Cwd, params.ApprovalPolicy, params.Sandbox)
		recordThreadModel(params.Cwd, params.Model)
		recordThreadInstructions(params.Cwd, params.DeveloperInstructions)
		threadStateMu.Lock()
		isArchived := archivedThreads[params.ThreadID]
		isKnown := params.ThreadID == threadID
		threadStateMu.Unlock()
		if isArchived {
			writeMessage(message{ID: msg.ID, Error: &rpcError{
				Code:    -32600,
				Message: fmt.Sprintf("session %s is archived. Run `codex unarchive %s` to unarchive it first.", params.ThreadID, params.ThreadID),
			}})
			return
		}
		if !isKnown {
			writeMessage(message{ID: msg.ID, Error: &rpcError{
				Code: -32600, Message: fmt.Sprintf("no rollout found for thread id %s", params.ThreadID),
			}})
			return
		}
		*threadCwd = params.Cwd
		threadStateMu.Lock()
		loadedThreads[params.ThreadID] = true
		threadStateMu.Unlock()
		recordThreadInitMethod(params.Cwd, "thread/resume")
		respond(msg.ID, map[string]any{"thread": map[string]any{"id": params.ThreadID}})
	case msg.Method == "model/list":
		if noModelList {
			writeMessage(message{ID: msg.ID, Error: &rpcError{Code: -32601, Message: "method not found: model/list"}})
			return
		}
		var params struct {
			Cursor string `json:"cursor"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		// Two pages, so a client's cursor loop is exercised; page two
		// carries a hidden model that must be skipped.
		if params.Cursor == "" {
			respond(msg.ID, map[string]any{
				"data": []map[string]any{
					{"id": "gpt-fake-a", "model": "gpt-fake-a", "displayName": "GPT Fake A", "description": "first", "hidden": false, "isDefault": true},
				},
				"nextCursor": "page2",
			})
			return
		}
		respond(msg.ID, map[string]any{
			"data": []map[string]any{
				{"id": "gpt-fake-b", "model": "gpt-fake-b", "displayName": "GPT Fake B", "description": "second", "hidden": false, "isDefault": false},
				{"id": "gpt-hidden", "model": "gpt-hidden", "displayName": "Hidden", "description": "", "hidden": true, "isDefault": false},
			},
			"nextCursor": nil,
		})
	case msg.Method == "turn/start":
		recordTurnInput(*threadCwd, msg.Params)
		respond(msg.ID, map[string]any{"turn": map[string]any{"id": turnID, "status": "inProgress"}})
		go runTurnScript(*threadCwd)
	case msg.Method == "" && len(msg.ID) > 0:
		// A response to a call() this agent itself sent (e.g. the
		// item/commandExecution/requestApproval round trip below).
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

// recordThreadInitMethod writes the method that started/resumed this
// thread to a "thread-init-method" file in cwd, overwriting any prior
// value -- the only way a test can observe whether Runner called
// thread/start or thread/resume (both return the same thread id in this
// fake agent).
func recordThreadInitMethod(cwd, method string) {
	if cwd == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(cwd, "thread-init-method"), []byte(method), 0o644)
}

func delta(text string) {
	notify("item/agentMessage/delta", map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"itemId":   "fake-item-1",
		"delta":    text,
	})
}

func turnCompleted(status string) {
	notify("turn/completed", map[string]any{
		"threadId": threadID,
		"turn":     map[string]any{"id": turnID, "status": status},
	})
}

func runTurnScript(cwd string) {
	scenario := "reply"
	if data, err := os.ReadFile(filepath.Join(cwd, "scenario")); err == nil {
		scenario = strings.TrimSpace(string(data))
	}

	if scenario == "hang" {
		// Streams one delta then blocks forever, for proving a cancelled
		// turn still leaves the thread resumable -- same shape as
		// internal/taskrunner/fakeagent's "hang" scenario.
		delta("before hang")
		time.Sleep(time.Hour)
		return
	}

	if scenario == "permission" {
		// Deliberately the very first thing, with no preceding delta --
		// same reasoning as internal/taskrunner/fakeagent's "permission"
		// scenario: an inbound request (item/commandExecution/
		// requestApproval) is dispatched on its own goroutine, decoupled
		// from the notification-forwarding pipeline a delta goes through
		// (see rpc.go's handleLine), so there's no causal wire ordering
		// between them to test against -- only a schedule-dependent one.
		approvalResp := call("item/commandExecution/requestApproval", map[string]any{
			"threadId": threadID,
			"turnId":   turnID,
			"itemId":   "fake-item-1",
			"command":  "echo hi",
			"cwd":      cwd,
		})
		decision := ""
		if approvalResp.Error == nil {
			var result struct {
				Decision string `json:"decision"`
			}
			_ = json.Unmarshal(approvalResp.Result, &result)
			decision = result.Decision
		}
		delta("decision:" + decision)
		turnCompleted("completed")
		return
	}

	if scenario == "token-usage" || scenario == "token-usage-turn2" {
		// A tool-looping turn: several model calls, each sending a
		// thread/tokenUsage/updated whose `last` is that one call and whose
		// `total` is the thread's running cumulative figure. A client must
		// charge the turn the growth of `total`, not the final `last` (one
		// call of several) and not the sum of the lasts' notifications.
		if scenario == "token-usage-turn2" {
			// The thread's second turn, in a new process: the total already
			// holds what the first turn's three calls added up to.
			seedTokenTotals(tokenCallsTurn1...)
			modelCall(tokenCallsTurn2[0])
			delta("Again.")
			turnCompleted("completed")
			return
		}
		modelCall(tokenCallsTurn1[0])
		delta("Hello, ")
		modelCall(tokenCallsTurn1[1])
		modelCall(tokenCallsTurn1[2])
		delta("world!")
		turnCompleted("completed")
		return
	}

	if scenario == "token-usage-partial" {
		// Only some fields on the wire and no modelContextWindow: the rest
		// must stay unreported.
		notify("thread/tokenUsage/updated", map[string]any{
			"threadId": threadID,
			"turnId":   turnID,
			"tokenUsage": map[string]any{
				"last":  map[string]any{"inputTokens": 4, "outputTokens": 1},
				"total": map[string]any{"inputTokens": 4, "outputTokens": 1},
			},
		})
		delta("Hello, ")
		turnCompleted("completed")
		return
	}

	delta("Hello, ")
	delta("world!")
	turnCompleted("completed")
}

// tokenCallsTurn1 are one tool-looping turn's three model calls; tokenCallsTurn2
// is the thread's next turn (one call).
var (
	tokenCallsTurn1 = []map[string]int{
		{"inputTokens": 10, "outputTokens": 2, "totalTokens": 12},
		{"inputTokens": 30, "cachedInputTokens": 20, "outputTokens": 7, "totalTokens": 37},
		{"inputTokens": 50, "cachedInputTokens": 40, "cacheWriteInputTokens": 5, "outputTokens": 8, "reasoningOutputTokens": 3, "totalTokens": 58},
	}
	tokenCallsTurn2 = []map[string]int{
		{"inputTokens": 60, "cachedInputTokens": 50, "outputTokens": 4, "totalTokens": 64},
	}
)

// tokenTotal is the thread's cumulative usage so far, summed over every model
// call made in it.
var tokenTotal = map[string]int{}

func seedTokenTotals(calls ...map[string]int) {
	for _, call := range calls {
		for k, v := range call {
			tokenTotal[k] += v
		}
	}
}

// modelCall adds one model call's usage to the thread total and sends the
// thread/tokenUsage/updated that call produces: `last` is the call alone,
// `total` the thread's cumulative figure including it.
func modelCall(last map[string]int) {
	seedTokenTotals(last)
	total := map[string]int{}
	for k, v := range tokenTotal {
		total[k] = v
	}
	notify("thread/tokenUsage/updated", map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"tokenUsage": map[string]any{
			"last":               last,
			"total":              total,
			"modelContextWindow": 200000,
		},
	})
}

// recordThreadPolicy writes the approvalPolicy/sandbox a thread/start or
// thread/resume carried to a "thread-policy" file in cwd
// ("<approvalPolicy> <sandbox>"), so tests can assert which Codex preset
// a mode sent.
func recordThreadPolicy(cwd, approvalPolicy, sandbox string) {
	if cwd == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(cwd, "thread-policy"), []byte(approvalPolicy+" "+sandbox), 0o644)
}

// recordTurnInput appends the raw turn/start params as one JSON line to
// "turn-input" in cwd. Opt-in: only when a "record-prompt-blocks" file exists
// in cwd, so ordinary runs leave their worktree clean.
func recordTurnInput(cwd string, params json.RawMessage) {
	if cwd == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(cwd, "record-prompt-blocks")); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(cwd, "turn-input"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(append([]byte(nil), params...), '\n'))
}
