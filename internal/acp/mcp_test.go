package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// lastMcpServersSeen asks the fake agent (scripted via its "mcp=..." argv)
// what raw mcpServers JSON its most recent session/new|load|resume carried.
func lastMcpServersSeen(t *testing.T, c *Client) json.RawMessage {
	t.Helper()
	raw, err := c.conn.call(context.Background(), "_test/last_mcp_servers", nil)
	if err != nil {
		t.Fatalf("_test/last_mcp_servers error = %v", err)
	}
	var res struct {
		McpServers json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("decode _test/last_mcp_servers response: %v", err)
	}
	return res.McpServers
}

// TestClient_NewSessionSendsMcpServers proves NewSession forwards its
// mcpServers argument verbatim on the wire: a stdio entry (args array, env
// as an array of {name,value} objects -- not a K/V object) and an http
// entry (headers likewise) both round-trip through the fake agent's
// recorded raw JSON.
func TestClient_NewSessionSendsMcpServers(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mcp := []any{
		McpServerStdio{Type: "stdio", Name: "playwright", Command: "/usr/local/bin/npx", Args: []string{"-y", "@playwright/mcp"}, Env: []NameValue{{Name: "API_KEY", Value: "k"}}},
		McpServerHttp{Type: "http", Name: "pplx", URL: "https://mcp.pplx.ai/mcp", Headers: []NameValue{{Name: "Authorization", Value: "Bearer t"}}},
	}
	if _, _, err := c.NewSession(ctx, cwd, mcp); err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	got := lastMcpServersSeen(t, c)
	want := `[` +
		`{"type":"stdio","name":"playwright","command":"/usr/local/bin/npx","args":["-y","@playwright/mcp"],"env":[{"name":"API_KEY","value":"k"}]},` +
		`{"type":"http","name":"pplx","url":"https://mcp.pplx.ai/mcp","headers":[{"name":"Authorization","value":"Bearer t"}]}` +
		`]`
	if string(got) != want {
		t.Fatalf("mcpServers = %s, want %s", got, want)
	}
}

// TestClient_LoadSessionAndResumeSendMcpServers proves both resume RPCs
// carry the mcpServers argument too, not just session/new.
func TestClient_LoadSessionAndResumeSendMcpServers(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mcp := []any{McpServerStdio{Type: "stdio", Name: "s", Command: "/bin/x"}}
	sid, _, err := c.NewSession(ctx, cwd, mcp)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if _, err := c.LoadSession(ctx, sid, cwd, mcp); err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if got := string(lastMcpServersSeen(t, c)); !strings.Contains(got, `"name":"s"`) {
		t.Fatalf("LoadSession mcpServers = %s, want the stdio entry", got)
	}
	if _, err := c.ResumeSession(ctx, sid, cwd, mcp); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if got := string(lastMcpServersSeen(t, c)); !strings.Contains(got, `"name":"s"`) {
		t.Fatalf("ResumeSession mcpServers = %s, want the stdio entry", got)
	}
}

// TestClient_NilMcpServersSendsEmptyArray proves a nil slice serializes as
// the literal [] -- never null -- on every session-opening RPC.
func TestClient_NilMcpServersSendsEmptyArray(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := c.NewSession(ctx, cwd, nil); err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if got := string(lastMcpServersSeen(t, c)); got != "[]" {
		t.Fatalf("NewSession mcpServers = %s, want []", got)
	}

	sid, _, err := c.NewSession(ctx, cwd, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if _, err := c.LoadSession(ctx, sid, cwd, nil); err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if got := string(lastMcpServersSeen(t, c)); got != "[]" {
		t.Fatalf("LoadSession mcpServers = %s, want []", got)
	}
	if _, err := c.ResumeSession(ctx, sid, cwd, nil); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if got := string(lastMcpServersSeen(t, c)); got != "[]" {
		t.Fatalf("ResumeSession mcpServers = %s, want []", got)
	}
}

// TestClient_McpCapabilityFlags proves SupportsMcpStdio/Http/SSE decode
// both capability shapes: v2 (top-level "mcp" key, transports as objects)
// and v1 ("mcpCapabilities" booleans, stdio unconditionally mandatory --
// including the real glm-acp-agent's live-captured response shape).
func TestClient_McpCapabilityFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		caps      string
		wantStdio bool
		wantHTTP  bool
		wantSSE   bool
	}{
		{"real glm v1 http only", `{"loadSession":true,"mcpCapabilities":{"http":true},"promptCapabilities":{},"sessionCapabilities":{}}`, true, true, false},
		{"v1 no mcp key", `{}`, true, false, false},
		{"v1 http false", `{"mcpCapabilities":{"http":false}}`, true, false, false},
		{"v1 http null", `{"mcpCapabilities":{"http":null}}`, true, false, false},
		{"v1 sse true", `{"mcpCapabilities":{"sse":true}}`, true, false, true},
		{"v2 stdio+http objects", `{"mcp":{"stdio":{},"http":{}}}`, true, true, false},
		{"v2 empty object", `{"mcp":{}}`, false, false, false},
		{"v2 http only", `{"mcp":{"http":{}}}`, false, true, false},
		{"v2 false values", `{"mcp":{"stdio":false,"http":false,"sse":false}}`, false, false, false},
		{"v2 null values", `{"mcp":{"stdio":null,"sse":null}}`, false, false, false},
		{"malformed", `{"mcp":"yes"`, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{AgentCapabilities: json.RawMessage(tt.caps)}
			if got := c.SupportsMcpStdio(); got != tt.wantStdio {
				t.Errorf("SupportsMcpStdio() = %v, want %v", got, tt.wantStdio)
			}
			if got := c.SupportsMcpHttp(); got != tt.wantHTTP {
				t.Errorf("SupportsMcpHttp() = %v, want %v", got, tt.wantHTTP)
			}
			if got := c.SupportsMcpSSE(); got != tt.wantSSE {
				t.Errorf("SupportsMcpSSE() = %v, want %v", got, tt.wantSSE)
			}
		})
	}

	t.Run("fake agent v1 mcp=none advertises nothing", func(t *testing.T) {
		t.Parallel()
		c, err := New([]string{fakeAgentPath, "mcp=none"})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.Initialize(ctx); err != nil {
			t.Fatalf("Initialize() error = %v", err)
		}
		if strings.Contains(string(c.AgentCapabilities), "mcp") {
			t.Fatalf("agentCapabilities = %s, want no mcp key", c.AgentCapabilities)
		}
		// v1 baseline: stdio still supported with nothing advertised.
		if !c.SupportsMcpStdio() || c.SupportsMcpHttp() || c.SupportsMcpSSE() {
			t.Fatalf("v1 no-advertisement support = stdio:%v http:%v sse:%v, want true/false/false", c.SupportsMcpStdio(), c.SupportsMcpHttp(), c.SupportsMcpSSE())
		}
	})

	t.Run("fake agent v1 mcp=stdio,http,sse emits v1 shape", func(t *testing.T) {
		t.Parallel()
		c, err := New([]string{fakeAgentPath, "mcp=stdio,http,sse"})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.Initialize(ctx); err != nil {
			t.Fatalf("Initialize() error = %v", err)
		}
		// stdio ignored in v1 emission; http/sse become booleans.
		if got := string(c.AgentCapabilities); !strings.Contains(got, `"mcpCapabilities":{"http":true,"sse":true}`) {
			t.Fatalf("agentCapabilities = %s, want v1 mcpCapabilities http+sse", got)
		}
		if !c.SupportsMcpStdio() || !c.SupportsMcpHttp() || !c.SupportsMcpSSE() {
			t.Fatal("v1 http,sse advertisement should report all three supported (stdio by baseline)")
		}
	})

	t.Run("fake agent v2 mcp2=stdio,http,sse emits object shape", func(t *testing.T) {
		t.Parallel()
		c, err := New([]string{fakeAgentPath, "mcp2=stdio,http,sse"})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.Initialize(ctx); err != nil {
			t.Fatalf("Initialize() error = %v", err)
		}
		if !c.SupportsMcpStdio() || !c.SupportsMcpHttp() || !c.SupportsMcpSSE() {
			t.Fatalf("agentCapabilities = %s, want all mcp transports", c.AgentCapabilities)
		}
	})

	t.Run("fake agent v2 mcp2= emits empty mcp object", func(t *testing.T) {
		t.Parallel()
		c, err := New([]string{fakeAgentPath, "mcp2="})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.Initialize(ctx); err != nil {
			t.Fatalf("Initialize() error = %v", err)
		}
		if c.SupportsMcpStdio() || c.SupportsMcpHttp() || c.SupportsMcpSSE() {
			t.Fatalf("agentCapabilities = %s, want no transports supported", c.AgentCapabilities)
		}
	})
}
