package acp

import "encoding/json"

// NameValue is one entry of an MCP server's env/headers list on ACP's
// session/new|load|resume wire (NewSessionRequest.mcpServers): both are
// arrays of {name, value} objects there, unlike the flat K/V maps some
// other protocols (e.g. Claude's --mcp-config) use.
type NameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// McpServerStdio is the wire shape of one stdio MCP server entry in a
// session/new|load|resume request's mcpServers array (ADR-0018).
type McpServerStdio struct {
	Type    string      `json:"type"`
	Name    string      `json:"name"`
	Command string      `json:"command"`
	Args    []string    `json:"args"`
	Env     []NameValue `json:"env"`
}

// McpServerHttp is the wire shape of one http (or sse) MCP server entry
// in a session/new|load|resume request's mcpServers array (ADR-0018).
type McpServerHttp struct {
	Type    string      `json:"type"`
	Name    string      `json:"name"`
	URL     string      `json:"url"`
	Headers []NameValue `json:"headers"`
}

// MarshalJSON keeps Args/Env as [] rather than null when unset: ACP's
// mcpServers schema types both as arrays, and some agents reject a null.
func (s McpServerStdio) MarshalJSON() ([]byte, error) {
	type alias McpServerStdio
	if s.Args == nil {
		s.Args = []string{}
	}
	if s.Env == nil {
		s.Env = []NameValue{}
	}
	return json.Marshal(alias(s))
}

// MarshalJSON keeps Headers as [] rather than null when unset.
func (h McpServerHttp) MarshalJSON() ([]byte, error) {
	type alias McpServerHttp
	if h.Headers == nil {
		h.Headers = []NameValue{}
	}
	return json.Marshal(alias(h))
}

// nonNilMcpServers normalizes a nil mcpServers slice to an empty []any so
// it always serializes as [] on the wire, never null.
func nonNilMcpServers(mcpServers []any) []any {
	if mcpServers == nil {
		return []any{}
	}
	return mcpServers
}
