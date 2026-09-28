package main

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Test harness for the MCP tool surface (docs/plans/active/mcp-server.md
// steps 1-2). It runs each tool against a real in-process daemon -- the
// same fake-agent-backed wsapi.Handler setup newConfigOptionTestEnv
// builds for the CLI tests -- driven through the SDK's own client type
// over an in-memory transport, so the full initialize -> tools/list ->
// tools/call MCP round trip is exercised, schema validation included.

// mcpTestEnv is one harness instance: the MCP client session plus the
// test daemon's URL, so tests can open their own wsclient for direct-RPC
// setup/assertions alongside the session.
type mcpTestEnv struct {
	cs     *mcp.ClientSession
	srvURL string
}

// newMCPSession stands up the test daemon (newConfigOptionTestEnv), dials
// it with the same wsclient the CLI uses, builds the MCP server over that
// client, and connects an MCP client session to it.
func newMCPSession(t *testing.T) *mcpTestEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)

	client := dialTestClient(t, srvURL)

	server := newMCPServer(client)
	// server.Connect's session is closed by cs.Close below (closing the
	// client end of the in-memory pipe ends the server's read loop); the
	// Server type itself has no Close.
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), t1, nil); err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return &mcpTestEnv{cs: cs, srvURL: srvURL}
}

// daemonURL returns the test daemon's URL, parsed.
func (e *mcpTestEnv) daemonURL() *url.URL {
	u, err := url.Parse(e.srvURL)
	if err != nil || u.Port() == "" {
		return nil
	}
	return u
}

// mcpToolNames returns the names in the server's tools/list response.
func mcpToolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	names := make([]string, len(res.Tools))
	for i, tool := range res.Tools {
		names[i] = tool.Name
	}
	return names
}

// callMCPTool calls name with args and returns its structured output
// (decoded into out) and isError.
func callMCPTool(t *testing.T, cs *mcp.ClientSession, name string, args any, out any) bool {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) error = %v", name, err)
	}
	if res.StructuredContent != nil && out != nil {
		// Over the wire the structured content arrives JSON-decoded (a
		// map/slice/...), so round-trip it through Marshal to reach out.
		data, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("CallTool(%s): marshal structured output: %v", name, err)
		}
		if err := decodeMCPInto(data, out); err != nil {
			t.Fatalf("CallTool(%s): decode structured output: %v", name, err)
		}
	}
	return res.IsError
}

// mcpToolErrorText concatenates a tool result's text content, for
// asserting on an error message's wording (e.g. that it carries the
// daemon's rejection reason or a specific prefix).
func mcpToolErrorText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
