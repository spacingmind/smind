package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/validation"
	"github.com/spacingmind/smind/internal/wsclient"
)

// testVerdict is one well-formed pass verdict against contract 3, the
// payload every test here sends in some shape.
func testVerdict() validation.Verdict {
	return validation.Verdict{
		V:              1,
		ContractID:     3,
		ContractSha256: strings.Repeat("a", 64),
		CommitSHA:      strings.Repeat("b", 40),
		Status:         validation.StatusPass,
		Summary:        "ok",
		Items: []validation.Item{{
			Kind:     validation.KindTest,
			ID:       "TestA",
			Status:   validation.StatusPass,
			Evidence: []validation.Evidence{{Type: "excerpt", Text: "PASS"}},
		}},
	}
}

// callVerdictSubmitArgs calls validation_verdict_submit with args and
// returns the rejection text plus whether the call was rejected at all
// (an RPC-level error or a tool result with IsError both count).
func callVerdictSubmit(t *testing.T, cs *mcp.ClientSession, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "validation_verdict_submit", Arguments: args})
	switch {
	case err != nil:
		return err.Error(), true
	case res.IsError:
		return mcpToolErrorText(res), true
	default:
		return "", false
	}
}

// TestVerdictSubmit_RejectedPayloadsFromTheLog pins the fix: the two
// payload shapes a model actually sent against the old json.RawMessage
// input -- the verdict JSON as a string, and its bytes as an array of
// numbers (the shape the SDK used to advertise) -- are both rejected by
// the tool's schema before anything reaches the daemon, instead of dying
// in the daemon's unmarshal.
func TestVerdictSubmit_RejectedPayloadsFromTheLog(t *testing.T) {
	cs := validatorSession(t, validatorEnv{ValidationID: 7, ContractID: 3, Token: "tok"})
	raw, err := json.Marshal(testVerdict())
	if err != nil {
		t.Fatal(err)
	}
	nums := make([]any, len(raw))
	for i, b := range raw {
		nums[i] = float64(b)
	}
	for name, verdict := range map[string]any{
		"verdict JSON as a string":     string(raw),
		"verdict bytes as number list": nums,
	} {
		msg, rejected := callVerdictSubmit(t, cs, map[string]any{"verdict": verdict})
		if !rejected {
			t.Fatalf("%s: the call was accepted, want a rejection", name)
		}
		if !strings.Contains(msg, "validating /properties/verdict") {
			t.Errorf("%s: error = %q, want a schema rejection naming /properties/verdict", name, msg)
		}
		if strings.Contains(msg, "cannot unmarshal") {
			t.Errorf("%s: error = %q: the payload reached the daemon's unmarshal, want a schema rejection", name, msg)
		}
	}
}

// fakeDaemonCall is one request the fake verdict daemon below saw.
type fakeDaemonCall struct {
	method string
	params map[string]any
}

// fakeVerdictDaemon is a wsapi stand-in that records every request it
// receives and answers each with a passing, merge-ready validation. It
// exists to inspect exactly what the MCP layer put on the wire.
func fakeVerdictDaemon(t *testing.T) (*httptest.Server, <-chan fakeDaemonCall) {
	t.Helper()
	calls := make(chan fakeDaemonCall, 8)
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
			reply, _ := json.Marshal(map[string]any{"id": env.ID, "result": map[string]any{"id": 7, "status": "pass", "mergeReady": true}})
			if err := ws.WriteMessage(websocket.TextMessage, reply); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

// validatorSessionOver wires a validator MCP session whose tools talk to
// the given wsclient (validatorSession, but over a caller-chosen daemon).
func validatorSessionOver(t *testing.T, client *wsclient.Client, env validatorEnv) *mcp.ClientSession {
	t.Helper()
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := newValidatorMCPServer(client, env).Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// TestVerdictSubmit_ObjectReachesDaemon proves the happy path end to end:
// a JSON object argument survives the tool's schema, is re-marshalled by
// the typed handler, and reaches the daemon as validation.verdict.submit
// with a JSON object verdict -- ids, token and the item intact -- and the
// daemon's mergeReady comes back as the tool's structured result.
func TestVerdictSubmit_ObjectReachesDaemon(t *testing.T) {
	srv, calls := fakeVerdictDaemon(t)
	client, err := wsclient.Dial(context.Background(), strings.TrimPrefix(srv.URL, "http://"), "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	env := validatorEnv{ValidationID: 7, ContractID: 3, Token: "tok"}
	cs := validatorSessionOver(t, client, env)

	want := testVerdict()
	raw, _ := json.Marshal(want)
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatal(err)
	}
	msg, rejected := callVerdictSubmit(t, cs, map[string]any{"verdict": asMap})
	if rejected {
		t.Fatalf("verdict submit rejected: %s", msg)
	}

	var got fakeDaemonCall
	select {
	case got = <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon never saw the verdict submit")
	}
	if got.method != "validation.verdict.submit" {
		t.Fatalf("method = %s", got.method)
	}
	if got.params["validationId"] != float64(7) || got.params["token"] != "tok" {
		t.Fatalf("params = %v, want validationId 7 and the env token", got.params)
	}
	verdict, isObject := got.params["verdict"].(map[string]any)
	if !isObject {
		t.Fatalf("verdict on the wire is %T, want a JSON object", got.params["verdict"])
	}
	if verdict["commitSha"] != want.CommitSHA || verdict["contractId"] != float64(want.ContractID) {
		t.Fatalf("verdict = %v, want the commitSha and contractId that were sent", verdict)
	}
	items, _ := verdict["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("verdict.items = %v, want the one item that was sent", verdict["items"])
	}
	if item, _ := items[0].(map[string]any); item["id"] != want.Items[0].ID || item["kind"] != want.Items[0].Kind {
		t.Fatalf("verdict.items[0] = %v, want %s %s", items[0], want.Items[0].Kind, want.Items[0].ID)
	}

	// An unknown field inside the verdict is a schema error (the daemon
	// parses verdicts strictly), so nothing may reach the daemon.
	withExtra := make(map[string]any, len(asMap)+1)
	for k, v := range asMap {
		withExtra[k] = v
	}
	withExtra["token"] = "not-a-verdict-field"
	if msg, rejected := callVerdictSubmit(t, cs, map[string]any{"verdict": withExtra}); !rejected {
		t.Fatalf("an unknown field inside the verdict was accepted: %s", msg)
	}
	select {
	case c := <-calls:
		t.Fatalf("the rejected call reached the daemon: %s %v", c.method, c.params)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestToolSchemas_NoByteArrayInputs guards the whole catalogue against the
// original bug's signature: any property whose schema is an integer array
// (what the SDK infers for json.RawMessage) makes a JSON object
// unsendable, so neither the Lead's nor the validator's tools may have one.
func TestToolSchemas_NoByteArrayInputs(t *testing.T) {
	lead := newMCPSession(t)
	validator := validatorSession(t, validatorEnv{ValidationID: 7, ContractID: 3, Token: "tok"})
	for _, catalogue := range []struct {
		name string
		cs   *mcp.ClientSession
	}{{"lead", lead.cs}, {"validator", validator}} {
		res, err := catalogue.cs.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range res.Tools {
			raw, err := json.Marshal(tool.InputSchema)
			if err != nil {
				t.Fatal(err)
			}
			var schema map[string]any
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("%s %s: decode schema: %v", catalogue.name, tool.Name, err)
			}
			props, _ := schema["properties"].(map[string]any)
			for name, prop := range props {
				if intentionalIntegerArrays[tool.Name+"."+name] {
					continue
				}
				walkSchemaForIntegerArrays(t, catalogue.name+" "+tool.Name, prop, ".properties."+name)
			}
		}
	}
}

// intentionalIntegerArrays are genuine []int64 inputs, not json.RawMessage.
var intentionalIntegerArrays = map[string]bool{"account_usage.accountIds": true}

// walkSchemaForIntegerArrays recurses through a decoded JSON schema and
// fails on any integer-array node, naming the tool and the path to it.
func walkSchemaForIntegerArrays(t *testing.T, tool string, node any, path string) {
	t.Helper()
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	// The SDK emits ["null","array"] for a nil-able slice, "array" otherwise.
	isArray := obj["type"] == "array"
	if types, ok := obj["type"].([]any); ok {
		for _, ty := range types {
			isArray = isArray || ty == "array"
		}
	}
	if isArray {
		if items, ok := obj["items"].(map[string]any); ok && items["type"] == "integer" {
			t.Errorf("%s %s: integer-array property (the json.RawMessage inference); a JSON object cannot be sent", tool, strings.TrimPrefix(path, "."))
		}
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		walkSchemaForIntegerArrays(t, tool, obj[k], path+"."+k)
	}
}

// TestValidatorVerdictSchema_Golden pins validation_verdict_submit's input
// schema: the verdict must be an object. Regenerate with
// UPDATE_GOLDEN=1 go test ./cmd/smind -run TestValidatorVerdictSchema_Golden
func TestValidatorVerdictSchema_Golden(t *testing.T) {
	cs := validatorSession(t, validatorEnv{ValidationID: 7, ContractID: 3, Token: "tok"})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	for _, tool := range res.Tools {
		if tool.Name != "validation_verdict_submit" {
			continue
		}
		schema = tool.InputSchema
	}
	if schema == nil {
		t.Fatal("validation_verdict_submit is not in the validator catalogue")
	}
	got, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "validation_verdict_submit.schema.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (regenerate with UPDATE_GOLDEN=1)", err)
	}
	if string(got) != string(want) {
		t.Fatalf("validation_verdict_submit schema drifted from %s:\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}
