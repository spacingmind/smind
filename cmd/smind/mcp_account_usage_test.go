package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// The account_usage tool is deliberately touch-only (AC53): it has no
// refresh/mode input, and every wsapi call it makes must be exactly
// {mode:"touch"} (+accountIds when given) -- an orchestrating agent must
// never be able to force a provider call. The tests below drive it through
// a recording stand-in daemon that echoes every account.usage request back,
// so the exact params on the wire can be asserted independently of the
// daemon-side implementation.

// fakeUsageDaemon is an httptest server whose /ws speaks just enough of
// wsapi's envelope protocol to answer wsclient requests: every method
// returns {"ok":true}, while each account.usage request's params are
// recorded (and returned as the result so the tool's decode path runs).
type fakeUsageDaemon struct {
	srv *httptest.Server

	mu      sync.Mutex
	calls   []json.RawMessage
	results map[string]any
}

// newFakeUsageDaemon stands up the recording daemon and dials it with the
// same wsclient the MCP server uses.
func newFakeUsageDaemon(t *testing.T) (*fakeUsageDaemon, *wsclient.Client) {
	t.Helper()
	f := &fakeUsageDaemon{results: map[string]any{}}
	var upgrader = websocket.Upgrader{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var env struct {
				ID     string          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := ws.ReadJSON(&env); err != nil {
				return
			}
			if env.Method == "account.usage" {
				f.mu.Lock()
				f.calls = append(f.calls, env.Params)
				f.mu.Unlock()
			}
			result, _ := json.Marshal(f.resultFor(env.Method, env.Params))
			_ = ws.WriteJSON(map[string]any{
				"id":     env.ID,
				"result": json.RawMessage(result),
			})
		}
	}))
	t.Cleanup(f.srv.Close)

	host := f.srv.URL[len("http://"):]
	client, err := wsclient.Dial(context.Background(), host, "test-token")
	if err != nil {
		t.Fatalf("wsclient.Dial() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return f, client
}

// resultFor returns the canned result for a method. account.usage echoes
// the recorded params back under "echo" so the tool's decode path runs on
// real JSON; anything else answers a generic ok.
func (f *fakeUsageDaemon) resultFor(method string, params json.RawMessage) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case "account.usage":
		if override, ok := f.results[method]; ok {
			return override
		}
		return map[string]any{"enabled": true, "ttlSeconds": 300, "reports": []any{}, "echo": json.RawMessage(params)}
	case "usage.summary":
		return []any{}
	}
	return map[string]any{"ok": true}
}

// usageCalls returns the recorded account.usage params, one per call.
func (f *fakeUsageDaemon) usageCalls() []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]json.RawMessage(nil), f.calls...)
}

// newAccountUsageSession builds an MCP client session whose wsclient
// points at the recording fake daemon, using the same in-memory transport
// wiring as newMCPSession.
func newAccountUsageSession(t *testing.T) (*mcp.ClientSession, *fakeUsageDaemon) {
	t.Helper()
	f, client := newFakeUsageDaemon(t)
	server := newMCPServer(client)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), t1, nil); err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, f
}

// TestAccountUsageTool_IsTouchOnly pins AC53's MCP seam: every call sends
// exactly {mode:"touch"} on the wire (never "refresh"/"cached"/anything
// else), and repeated tool calls stay touch -- the call budget itself is
// the daemon-side poller's, not the tool's.
func TestAccountUsageTool_IsTouchOnly(t *testing.T) {
	cs, f := newAccountUsageSession(t)

	var out struct {
		Enabled    bool     `json:"enabled"`
		TTLSeconds int64    `json:"ttlSeconds"`
		Reports    []any    `json:"reports"`
		Echo       struct { // the fake daemon's echo of the wire params
			Mode       string   `json:"mode"`
			AccountIDs []int64  `json:"accountIds"`
			Extra      []string `json:"-"`
		} `json:"echo"`
	}
	for i := 0; i < 10; i++ {
		if isErr := callMCPTool(t, cs, "account_usage", map[string]any{}, &out); isErr {
			t.Fatalf("account_usage call %d returned an error result", i+1)
		}
	}
	calls := f.usageCalls()
	if len(calls) != 10 {
		t.Fatalf("account.usage called %d times, want 10 (one RPC per tool call)", len(calls))
	}
	for i, raw := range calls {
		var params map[string]any
		if err := json.Unmarshal(raw, &params); err != nil {
			t.Fatalf("call %d params = %s: %v", i+1, raw, err)
		}
		if len(params) != 1 || params["mode"] != "touch" {
			t.Fatalf("call %d params = %s, want exactly {mode:touch}", i+1, raw)
		}
	}
}

// TestAccountUsageTool_AccountIdsForwarded pins the one input the tool
// does take: accountIds rides along with mode "touch" and nothing else.
func TestAccountUsageTool_AccountIdsForwarded(t *testing.T) {
	cs, f := newAccountUsageSession(t)

	var out struct{}
	_ = out
	if isErr := callMCPTool(t, cs, "account_usage", map[string]any{"accountIds": []int64{3, 7}}, nil); isErr {
		t.Fatal("account_usage(accountIds) returned an error result")
	}
	calls := f.usageCalls()
	if len(calls) != 1 {
		t.Fatalf("account.usage called %d times, want 1", len(calls))
	}
	var params map[string]any
	if err := json.Unmarshal(calls[0], &params); err != nil {
		t.Fatalf("params = %s: %v", calls[0], err)
	}
	if len(params) != 2 || params["mode"] != "touch" {
		t.Fatalf("params = %s, want exactly {mode:touch, accountIds:[3,7]}", calls[0])
	}
	ids, _ := json.Marshal(params["accountIds"])
	if string(ids) != "[3,7]" {
		t.Fatalf("accountIds = %s, want [3,7]", ids)
	}
}

// TestAccountUsageTool_InCatalogAndInstructions pins the catalog and
// instructions coverage: the tool is listed with its read-only annotation,
// and the orchestration guide names it (the instructions diff test also
// enforces this, but the plan calls it out per tool).
func TestAccountUsageTool_InCatalogAndInstructions(t *testing.T) {
	cs, _ := newAccountUsageSession(t)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "account_usage" {
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("account_usage annotations = %+v, want ReadOnlyHint true", tool.Annotations)
		}
		init := cs.InitializeResult()
		if init == nil || init.Instructions == "" || !strings.Contains(init.Instructions, "account_usage") {
			t.Fatal("instructions do not name account_usage")
		}
		return
	}
	t.Fatal("tools/list does not contain account_usage")
}

// TestAccountUsageTool_ReadOnly pins the read-only shape from both sides:
// the tool annotation (above) and the input schema -- accountIds is its
// only property, with nothing write-ish (no mode, no refresh, no force).
func TestAccountUsageTool_ReadOnly(t *testing.T) {
	cs, _ := newAccountUsageSession(t)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "account_usage" {
			continue
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		raw, _ := json.Marshal(tool.InputSchema)
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("input schema = %s: %v", raw, err)
		}
		if len(schema.Properties) != 1 {
			t.Fatalf("account_usage input properties = %v, want exactly [accountIds]", schema.Properties)
		}
		if _, ok := schema.Properties["accountIds"]; !ok {
			t.Fatalf("account_usage input properties = %v, want accountIds", schema.Properties)
		}
		for _, banned := range []string{"mode", "refresh", "force", "flush"} {
			if _, ok := schema.Properties[banned]; ok {
				t.Fatalf("account_usage input schema has write-ish property %q", banned)
			}
		}
		return
	}
	t.Fatal("tools/list does not contain account_usage")
}

// TestAccountUsageTool_NoRefreshInputForRunScope pins that a caller trying
// to smuggle a refresh through (mode:"refresh") gets nowhere: the property
// is not in the schema, so the SDK rejects it before the handler runs --
// and even if a client ignored additionalProperties and sent it anyway,
// the handler never forwards it (only mode:"touch" ever reaches the wire,
// as the touch-only test asserts).
func TestAccountUsageTool_NoRefreshInputForRunScope(t *testing.T) {
	cs, f := newAccountUsageSession(t)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "account_usage",
		Arguments: map[string]any{"mode": "refresh"},
	})
	if err != nil {
		t.Fatalf("CallTool(account_usage, mode:refresh) error = %v", err)
	}
	if !res.IsError {
		t.Fatal("account_usage with mode:refresh: IsError = false, want schema rejection")
	}
	if got := len(f.usageCalls()); got != 0 {
		t.Fatalf("account.usage called %d times after a rejected mode:refresh, want 0", got)
	}
}

// TestAccountUsageTool_UsageSummaryUnchanged pins that usage_summary's
// input shape and daemon method are untouched by the new tool: still
// scope/groupBy/since/until, still usage.summary.
func TestAccountUsageTool_UsageSummaryUnchanged(t *testing.T) {
	cs, f := newAccountUsageSession(t)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "usage_summary",
		Arguments: map[string]any{
			"scope":   "proxy",
			"groupBy": "account",
			"since":   "2026-10-01",
			"until":   "2026-10-02",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(usage_summary) error = %v", err)
	}
	if res.IsError {
		t.Fatal("usage_summary returned an error result")
	}
	// The fake daemon records only account.usage; usage.summary must not
	// have touched it, and the call must not have surfaced as account.usage.
	if got := len(f.usageCalls()); got != 0 {
		t.Fatalf("account.usage called %d times by usage_summary, want 0", got)
	}
}

// TestMCPAccountUsagePassesCooldownThrough: report rows are relayed as
// generic maps, so the daemon's optional cooldown key reaches the agent.
func TestMCPAccountUsagePassesCooldownThrough(t *testing.T) {
	cs, f := newAccountUsageSession(t)
	f.mu.Lock()
	f.results["account.usage"] = map[string]any{"enabled": true, "ttlSeconds": 300, "reports": []any{
		map[string]any{"accountId": 1, "cooldown": map[string]any{"until": "2026-10-12T13:00:00Z", "scope": "account", "reason": "rejected", "window": "weekly"}},
	}}
	f.mu.Unlock()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "account_usage", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("CallTool() = %v, %+v", err, res)
	}
	b, _ := json.Marshal(res.StructuredContent)
	for _, want := range []string{`"cooldown"`, `"until":"2026-10-12T13:00:00Z"`, `"window":"weekly"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %s in %s", want, b)
		}
	}
}
