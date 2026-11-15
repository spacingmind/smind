package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// Test harness for the browser MCP child: no daemon, no browser -- the
// seven tools are wired to a wsapi stand-in that records every request
// and answers with a test-chosen envelope, so the tests pin exactly what
// the child puts on the wire and exactly how it converts what comes back.

// fakeBrowserReply is what the fake daemon answers one RPC with: either a
// result payload or an error message.
type fakeBrowserReply struct {
	result any    // the wsapi result payload
	errMsg string // non-empty: the envelope carries this rpc error
}

// fakeBrowserDaemon is a wsapi stand-in shaped like the validator tests'
// fakeVerdictDaemon: it records every request (fakeDaemonCall) and answers
// each with the reply the test last installed.
func fakeBrowserDaemon(t *testing.T) (*httptest.Server, <-chan fakeDaemonCall, func(fakeBrowserReply)) {
	t.Helper()
	calls := make(chan fakeDaemonCall, 32)
	var mu sync.Mutex
	reply := fakeBrowserReply{result: map[string]any{"content": []any{}, "isError": false}}
	setReply := func(r fakeBrowserReply) {
		mu.Lock()
		defer mu.Unlock()
		reply = r
	}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var env struct {
				ID     string          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(data, &env); err != nil || env.ID == "" {
				continue
			}
			var call fakeDaemonCall
			call.method = env.Method
			_ = json.Unmarshal(env.Params, &call.params)
			calls <- call
			mu.Lock()
			ans := reply
			mu.Unlock()
			var out []byte
			if ans.errMsg != "" {
				out, _ = json.Marshal(map[string]any{"id": env.ID, "error": map[string]any{"message": ans.errMsg}})
			} else {
				out, _ = json.Marshal(map[string]any{"id": env.ID, "result": ans.result})
			}
			if err := ws.WriteMessage(websocket.TextMessage, out); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, calls, setReply
}

// browserSessionOver wires a browser-child MCP session whose tools talk
// to the given wsclient.
func browserSessionOver(t *testing.T, client *wsclient.Client) *mcp.ClientSession {
	t.Helper()
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := newBrowserMCPServer(client, "tok-1").Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// browserSession is a browser-child session over the fake daemon, plus
// the daemon's recorded calls and reply setter.
func browserSession(t *testing.T) (*mcp.ClientSession, <-chan fakeDaemonCall, func(fakeBrowserReply)) {
	t.Helper()
	srv, calls, setReply := fakeBrowserDaemon(t)
	client, err := wsclient.Dial(context.Background(), strings.TrimPrefix(srv.URL, "http://"), "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return browserSessionOver(t, client), calls, setReply
}

// browserCatalogue is the pinned seven-tool catalogue, sorted.
var browserCatalogue = []string{
	"browser_close", "browser_console", "browser_open", "browser_screenshot",
	"browser_snapshot", "browser_tabs", "browser_wait",
}

// TestBrowserMCP_ToolSetIsPinned pins the child's catalogue: exactly the
// seven tools, no eighth, and none of the acting tools (evaluate, click,
// fill, ...) the browser child deliberately does not expose.
func TestBrowserMCP_ToolSetIsPinned(t *testing.T) {
	cs, _, _ := browserSession(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
		if tool.Annotations == nil || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
			t.Errorf("%s annotations = %+v, want OpenWorldHint true", tool.Name, tool.Annotations)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(browserCatalogue, ",") {
		t.Fatalf("browser catalogue = %v, want exactly %v", got, browserCatalogue)
	}
	// open and close act on a tab; every other tool is a read.
	for _, tool := range res.Tools {
		want := tool.Name != "browser_open" && tool.Name != "browser_close"
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != want {
			t.Errorf("%s ReadOnlyHint = %v, want %v", tool.Name, tool.Annotations, want)
		}
	}
	// The settings surface is human-only: no tool of the child may reach it.
	for _, name := range got {
		for _, bad := range []string{"settings", "enable", "disable", "allow", "data", "clear"} {
			if strings.Contains(name, bad) {
				t.Errorf("tool %q looks like the human-only browser settings surface", name)
			}
		}
	}
	for _, forbidden := range []string{
		"browser_evaluate", "browser_upload", "browser_download", "browser_click",
		"browser_fill", "browser_type", "browser_press", "browser_scroll",
		"browser_back", "browser_forward", "browser_reload",
	} {
		if slices.Contains(got, forbidden) {
			t.Errorf("browser catalogue contains %s: the child exposes reads plus open/close only", forbidden)
		}
	}
}

// browserToolSchemas decodes every tool's input schema from tools/list.
func browserToolSchemas(t *testing.T, cs *mcp.ClientSession) map[string]map[string]any {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]map[string]any, len(res.Tools))
	for _, tool := range res.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s: decode schema: %v", tool.Name, err)
		}
		out[tool.Name] = schema
	}
	return out
}

// prop returns one property node of a decoded schema.
func prop(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	props, _ := schema["properties"].(map[string]any)
	node, _ := props[name].(map[string]any)
	if node == nil {
		t.Fatalf("schema has no %s property: %v", name, schema)
	}
	return node
}

// TestBrowserMCP_SchemasAreStrict: every input schema rejects unknown
// properties (the go-sdk struct default), and the caps the daemon's broker
// expects are visible to the model up front.
func TestBrowserMCP_SchemasAreStrict(t *testing.T) {
	cs, _, _ := browserSession(t)
	schemas := browserToolSchemas(t, cs)
	if len(schemas) != len(browserCatalogue) {
		t.Fatalf("catalogue size = %d, want %d", len(schemas), len(browserCatalogue))
	}
	for name, schema := range schemas {
		if schema["additionalProperties"] != false {
			t.Errorf("%s: additionalProperties = %v, want false", name, schema["additionalProperties"])
		}
	}
	open := schemas["browser_open"]
	required := false
	for _, r := range open["required"].([]any) {
		required = required || r == "url"
	}
	if !required {
		t.Errorf("browser_open required = %v, want url", open["required"])
	}
	if prop(t, open, "url")["maxLength"] != float64(2048) {
		t.Errorf("browser_open url maxLength = %v, want 2048", prop(t, open, "url")["maxLength"])
	}
	for _, name := range []string{"browser_close", "browser_snapshot", "browser_screenshot"} {
		if got := prop(t, schemas[name], "tab")["maxLength"]; got != float64(128) {
			t.Errorf("%s tab maxLength = %v, want 128", name, got)
		}
	}
	wait := schemas["browser_wait"]
	for _, c := range []struct {
		field string
		key   string
		want  float64
	}{
		{"text", "maxLength", 200},
		{"url", "maxLength", 2048},
		{"timeoutSeconds", "minimum", 1},
		{"timeoutSeconds", "maximum", 15},
	} {
		if got := prop(t, wait, c.field)[c.key]; got != c.want {
			t.Errorf("browser_wait %s %s = %v, want %v", c.field, c.key, got, c.want)
		}
	}
	if wait["required"] != nil {
		t.Errorf("browser_wait required = %v, want none (every field optional)", wait["required"])
	}
}

// TestBrowserMCP_ForwardsToBrowserCall: every tool is exactly one
// browser.call round trip carrying {tool, args}, with the args the typed
// input marshals to -- nothing added, nothing renamed.
func TestBrowserMCP_ForwardsToBrowserCall(t *testing.T) {
	cs, calls, _ := browserSession(t)
	ctx := context.Background()
	for _, c := range []struct {
		tool string
		args map[string]any
		want map[string]any
	}{
		{"browser_open", map[string]any{"url": "http://127.0.0.1:1/now"}, map[string]any{"url": "http://127.0.0.1:1/now"}},
		{"browser_tabs", nil, map[string]any{}},
		{"browser_close", nil, map[string]any{}},
		{"browser_close", map[string]any{"tab": "tab-2"}, map[string]any{"tab": "tab-2"}},
		{"browser_snapshot", map[string]any{"tab": "tab-2"}, map[string]any{"tab": "tab-2"}},
		{"browser_screenshot", map[string]any{"tab": "tab-2"}, map[string]any{"tab": "tab-2"}},
		{"browser_console", nil, map[string]any{}},
		{"browser_wait", map[string]any{"text": "Ready", "url": "http://127.0.0.1:1/done", "timeoutSeconds": 5},
			map[string]any{"text": "Ready", "url": "http://127.0.0.1:1/done", "timeoutSeconds": float64(5)}},
	} {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
		if err != nil {
			t.Fatalf("%s: CallTool error = %v", c.tool, err)
		}
		if res.IsError {
			t.Fatalf("%s: tool error: %s", c.tool, mcpToolErrorText(res))
		}
		select {
		case got := <-calls:
			if got.method != "browser.call" {
				t.Fatalf("%s: daemon method = %s, want browser.call", c.tool, got.method)
			}
			if got.params["tool"] != c.tool {
				t.Fatalf("%s: daemon params = %v, want tool %q", c.tool, got.params, c.tool)
			}
			if got.params["token"] != "tok-1" {
				t.Fatalf("%s: the run's browser capability must ride every call: %v", c.tool, got.params)
			}
			args, ok := got.params["args"].(map[string]any)
			if !ok {
				t.Fatalf("%s: daemon args = %T, want a JSON object", c.tool, got.params["args"])
			}
			if !reflect.DeepEqual(args, c.want) {
				t.Fatalf("%s: daemon args = %v, want exactly %v", c.tool, args, c.want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the daemon never saw the call", c.tool)
		}
	}
}

// TestBrowserMCP_ConvertsTextAndImageResults: the daemon's envelope rides
// through -- text verbatim, images base64-decoded with their mime type,
// unknown items replaced by the fixed unsupported line (never passed
// through), IsError copied, and no text of the child's own added.
func TestBrowserMCP_ConvertsTextAndImageResults(t *testing.T) {
	cs, _, setReply := browserSession(t)
	ctx := context.Background()

	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	setReply(fakeBrowserReply{result: browserCallResult{Content: []browserContent{
		{Type: "text", Text: "The page says hello"},
		{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(png)},
		{Type: "audio", Data: "never-passed-through"},
	}}})
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "browser_snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("browser_snapshot came back a tool error: %s", mcpToolErrorText(res))
	}
	if len(res.Content) != 3 {
		t.Fatalf("content items = %d, want the three converted items", len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "The page says hello" {
		t.Errorf("first item = %#v, want the daemon's text verbatim", res.Content[0])
	}
	img, ok := res.Content[1].(*mcp.ImageContent)
	if !ok || img.MIMEType != "image/png" || !bytes.Equal(img.Data, png) {
		t.Errorf("second item = %#v, want a png image with the decoded bytes", res.Content[1])
	}
	other, ok := res.Content[2].(*mcp.TextContent)
	if !ok || other.Text != "unsupported content type from the daemon" {
		t.Errorf("third item = %#v, want the fixed unsupported line", res.Content[2])
	}

	// The envelope's error flag surfaces as IsError, with the daemon's own
	// text and nothing added.
	setReply(fakeBrowserReply{result: browserCallResult{
		Content: []browserContent{{Type: "text", Text: "not found"}},
		IsError: true,
	}})
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "browser_open", Arguments: map[string]any{"url": "http://127.0.0.1:1/x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("a daemon isError envelope must surface as IsError")
	}
	if txt := mcpToolErrorText(res); txt != "not found" {
		t.Errorf("error text = %q, want the daemon's text verbatim", txt)
	}
}

// TestBrowserMCP_DaemonErrorIsAToolError: an RPC-level daemon rejection
// becomes a tool error carrying the daemon's message, labelled with the
// tool's own name (callWS) -- not a protocol error.
func TestBrowserMCP_DaemonErrorIsAToolError(t *testing.T) {
	cs, _, setReply := browserSession(t)
	setReply(fakeBrowserReply{errMsg: "host example.net is not allowed by the policy"})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "browser_open",
		Arguments: map[string]any{"url": "http://127.0.0.1:1/"},
	})
	if err != nil {
		t.Fatalf("a daemon rejection must be a tool error, not a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatal("the daemon's rejection did not surface as a tool error")
	}
	txt := mcpToolErrorText(res)
	if !strings.Contains(txt, "host example.net is not allowed by the policy") {
		t.Errorf("error text = %q, want the daemon's message", txt)
	}
	if !strings.Contains(txt, "browser_open:") {
		t.Errorf("error text = %q, want it labelled with the tool's name", txt)
	}
}

// TestBrowserMCP_RequiresRunID: without SMIND_RUN_ID the child refuses
// (exit 1) before dialing anything -- a browser belongs to a run, never
// to whoever happens to start the process.
func TestBrowserMCP_RequiresRunID(t *testing.T) {
	t.Setenv("SMIND_RUN_ID", "")
	t.Setenv("SMIND_HOME", t.TempDir())

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	code := cmdMcpBrowser(nil)
	w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)

	if code != 1 {
		t.Fatalf("cmdMcpBrowser without SMIND_RUN_ID = %d, want 1", code)
	}
	if !strings.Contains(string(out), "SMIND_RUN_ID is required") {
		t.Fatalf("stderr = %q, want the SMIND_RUN_ID refusal", out)
	}
	if strings.Contains(string(out), "daemon") {
		t.Errorf("stderr = %q: the refusal came after a dial attempt; the run id check must come first", out)
	}
}

// TestBrowserMCP_NoArguments: the child takes no arguments, exactly like
// `mcp serve`.
func TestBrowserMCP_NoArguments(t *testing.T) {
	t.Setenv("SMIND_RUN_ID", "run-1")
	t.Setenv("SMIND_HOME", t.TempDir())
	if code := cmdMcpBrowser([]string{"extra"}); code != 2 {
		t.Fatalf("cmdMcpBrowser with an argument = %d, want 2 (usage)", code)
	}
}

// TestBrowserMCP_NotInTheSmindServerCatalogue: the browser tools live
// only in the smind-browser child; the main `smind` server must not grow
// a browser_ tool of its own.
func TestBrowserMCP_NotInTheSmindServerCatalogue(t *testing.T) {
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := newMCPServer(nil).Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	for _, name := range mcpToolNames(t, cs) {
		if strings.HasPrefix(name, "browser_") {
			t.Errorf("the smind server lists %s: browser tools live only in the %s child", name, browserMCPName)
		}
	}
}

func TestBrowserMCP_RequiresTheBrowserToken(t *testing.T) {
	t.Setenv("SMIND_RUN_ID", "run-1")
	t.Setenv("SMIND_BROWSER_TOKEN", "")
	t.Setenv("SMIND_HOME", t.TempDir())
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	code := cmdMcpBrowser(nil)
	w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	if code != 1 || !strings.Contains(string(out), "SMIND_BROWSER_TOKEN is required") || strings.Contains(string(out), "daemon") {
		t.Fatalf("a child without its capability must fail closed before dialling: code=%d %q", code, out)
	}
}
