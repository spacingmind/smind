package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// handleSetConfigOption responds to session/set_config_option requests,
// mirroring internal/acp/fakeagent's own handler of the same name (same
// wire field names, same echo shape) so Runner/Registry/CLI tests can
// drive the set-config-option path end to end.
//
// The sentinel config id "no-such-option" gets an invalid-params error,
// so tests can prove an agent-side rejection surfaces as a Go error (and
// through wsapi/CLI as a printed message) rather than a silent no-op.
func handleSetConfigOption(msg message, cwd string) {
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
			Error:   &rpcError{Code: -32602, Message: "no such config option"},
		})
		return
	}
	if req.ConfigID == "model" {
		if strings.HasPrefix(req.Value, "bad-") {
			writeMessage(message{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Error:   &rpcError{Code: -32602, Message: "unsupported model: " + req.Value},
			})
			return
		}
		recordSessionModel(cwd, "set_config_option:model:"+req.Value)
	}
	if req.ConfigID == "mode" {
		recordSessionMode(cwd, "set_config_option:"+req.Value)
	}
	respond(msg.ID, map[string]any{
		"configOptions": []map[string]any{{
			"configId":     req.ConfigID,
			"name":         "Option " + req.ConfigID,
			"type":         "select",
			"currentValue": map[string]any{"type": "id", "value": req.Value},
		}},
	})
}

// recordSessionModel appends one line to "session-model" in cwd.
func recordSessionModel(cwd, line string) {
	if cwd == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(cwd, "session-model"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}
