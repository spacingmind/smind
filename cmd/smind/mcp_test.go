package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Test harness for the MCP tool surface (docs/plans/active/mcp-server.md
// steps 1-2). It runs each tool against a real in-process daemon -- the
// same fake-agent-backed wsapi.Handler setup newConfigOptionTestEnv
// builds for the CLI tests -- driven through the SDK's own client type
// over an in-memory transport, so the full initialize -> tools/list ->
// tools/call MCP round trip is exercised, schema validation included.

// newMCPSession stands up the test daemon (newConfigOptionTestEnv), dials
// it with the same wsclient the CLI uses, builds the MCP server over that
// client, and connects an MCP client session to it. The caller must keep
// the returned client alive for the session's lifetime.
func newMCPSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)

	t.Setenv("SMIND_DAEMON_TEST_URL", srvURL)

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
	return cs
}

// daemonURLForTest returns the test daemon's URL recorded by
// newMCPSession, so a test can open its own wsclient for direct-RPC
// setup/assertions alongside the MCP session.
func daemonURLForTest(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(os.Getenv("SMIND_DAEMON_TEST_URL"))
	if err != nil || u.Port() == "" {
		t.Fatalf("no test daemon URL recorded (SMIND_DAEMON_TEST_URL=%q)", os.Getenv("SMIND_DAEMON_TEST_URL"))
	}
	return u.String()
}

// jsonUnmarshalStrict decodes raw JSON into out, failing loudly on a type
// mismatch rather than silently zeroing fields.
func jsonUnmarshalStrict(raw json.RawMessage, out any) error {
	return json.Unmarshal(raw, out)
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
		if err := jsonUnmarshalStrict(data, out); err != nil {
			t.Fatalf("CallTool(%s): decode structured output: %v", name, err)
		}
	}
	return res.IsError
}
