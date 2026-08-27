// Package wsapi implements smind's WebSocket RPC API: a single persistent
// connection multiplexing many concurrent request/response exchanges plus
// server-pushed streaming events, used by the web UI (and, later, the
// terminal feature) for anything that needs bidirectional or streamed
// traffic rather than a one-shot HTTP call.
//
// One JSON object is sent per WebSocket text message, in one of four
// shapes:
//
//   - client -> server request:  {"id": "...", "method": "...", "params": ...}
//   - server -> client response: {"id": "...", "result": ...} or {"id": "...", "error": {"message": "..."}}
//   - server -> client event:    {"id": "...", "event": "...", "params": ...}
//   - client -> server cancel:   {"method": "task.cancel", "params": {"id": "..."}}
//
// A response terminates the request with the matching id; zero or more
// events may arrive on that same id first, for methods that stream (e.g.
// task.prompt). task.cancel has no id of its own -- it's fire-and-forget,
// not a request awaiting a response -- and asks the server to cancel the
// still-in-flight request named in its params.
package wsapi

import "encoding/json"

// envelope is the wire shape of every message this package sends or
// receives; requests, responses, events, and cancellations all fit this one
// struct, distinguished by which fields are present.
type envelope struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
}

// rpcError is the wire shape of a terminal error response.
type rpcError struct {
	Message string `json:"message"`
}

// cancelParams is the params shape of a task.cancel message.
type cancelParams struct {
	ID string `json:"id"`
}

func marshalOrNull(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return raw
}
