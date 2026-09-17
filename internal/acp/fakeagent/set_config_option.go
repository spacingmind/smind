package main

import "encoding/json"

// handleSetConfigOption responds to session/set_config_option requests.
//
// The result echoes every request field the client is required to send
// back into the response's configOptions, so tests can assert the client
// used ACP's real wire field names: the echo only round-trips if this
// struct's json tags match the fields the client actually sent
// (sessionId, configId, and the flattened SessionConfigOptionValue's
// type/value of ACP's SetSessionConfigOptionRequest). The session id and
// type travel in the echoed option's description.
//
// The sentinel config id "no-such-option" gets an invalid-params error
// instead, so tests can prove an agent-side JSON-RPC error response
// surfaces as a Go error rather than a silent no-op.
func handleSetConfigOption(msg message) {
	var req struct {
		SessionID string `json:"sessionId"`
		ConfigID  string `json:"configId"`
		Type      string `json:"type"`
		Value     string `json:"value"`
	}
	_ = json.Unmarshal(msg.Params, &req)
	if req.ConfigID == "no-such-option" {
		writeMessage(message{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error:   &rpcError{Code: -32602, Message: "unknown config option: no-such-option"},
		})
		return
	}
	respond(msg.ID, map[string]any{
		"configOptions": []map[string]any{{
			"configId":     req.ConfigID,
			"name":         "Option " + req.ConfigID,
			"description":  req.SessionID + " " + req.Type,
			"currentValue": map[string]any{"type": "id", "value": req.Value},
		}},
	})
}
